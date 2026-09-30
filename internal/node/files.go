package node

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"dsync/internal/client"
	"dsync/internal/config"
	"dsync/internal/proto"
)

// File transfer states.
const (
	StatusActive   = "active"
	StatusDone     = "done"
	StatusFailed   = "failed"
	StatusCanceled = "canceled"
)

// FileInfo is attached to messages that carry a file.
type FileInfo struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Path   string `json:"path"` // source file when sending, saved file when received
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// Progress reports how far a running transfer has got.
type Progress struct {
	ID   int64 `json:"id"` // message id
	Done int64 `json:"done"`
	Rate int64 `json:"rate"` // bytes per second
}

func (n *Node) ReceiveDir() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.cfg.ReceiveDir()
}

func (n *Node) SetReceiveDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return errors.New("folder must be an absolute path")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.cfg.DownloadDir = dir
	return n.cfg.Save()
}

// SendFiles sends files to a peer one after another in the background.
// Progress and results are reported through events.
func (n *Node) SendFiles(ctx context.Context, peerID string, paths []string) error {
	n.mu.Lock()
	p, ok := n.peers[peerID]
	var peer Peer
	if ok {
		peer = *p
	}
	n.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown device %s", peerID)
	}
	t, paired := n.trusted(peerID)
	if !paired {
		return fmt.Errorf("pair with %s first", peer.Name)
	}
	for _, path := range paths {
		st, err := os.Stat(path)
		if err != nil {
			return err
		}
		if st.IsDir() {
			return fmt.Errorf("%s is a folder; sending folders isn't supported yet", filepath.Base(path))
		}
	}

	// Create all the messages up front so the queue shows at once.
	ids := make([]int64, len(paths))
	for i, path := range paths {
		st, _ := os.Stat(path)
		m := n.addMessage(Message{
			PeerID: peer.ID, PeerName: peer.Name,
			File: &FileInfo{Name: filepath.Base(path), Size: st.Size(), Path: path, Status: StatusActive},
		})
		ids[i] = m.ID
	}
	go func() {
		for i, path := range paths {
			n.sendFile(ctx, peer, t.Fingerprint, ids[i], path)
		}
	}()
	return nil
}

func (n *Node) sendFile(ctx context.Context, peer Peer, fp string, id int64, path string) {
	ctx, cancel := n.trackTransfer(ctx, id)
	defer n.untrackTransfer(id, cancel)
	if ctx.Err() != nil { // canceled while queued
		n.finishTransfer(ctx, id, "", ctx.Err())
		return
	}

	f, err := os.Open(path)
	if err != nil {
		n.finishTransfer(ctx, id, "", err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		n.finishTransfer(ctx, id, "", err)
		return
	}

	self := n.Self()
	h := client.FileHeader{FromID: self.ID, FromName: self.Name, FromPort: self.Port, Name: filepath.Base(path), Size: st.Size()}
	err = n.cl.SendFile(ctx, peer.Addr, fp, h, f, n.progressReporter(id, st.Size()))
	n.finishTransfer(ctx, id, "", err)
}

func (n *Node) receiveFile(w http.ResponseWriter, r *http.Request, from config.TrustedPeer) {
	fromName, _ := url.QueryUnescape(r.Header.Get(proto.HeaderFromName))
	fromPort, _ := strconv.Atoi(r.Header.Get(proto.HeaderFromPort))
	rawName, _ := url.QueryUnescape(r.Header.Get(proto.HeaderFileName))
	name := safeFileName(rawName)
	size, err := strconv.ParseInt(r.Header.Get(proto.HeaderFileSize), 10, 64)
	if err != nil || size < 0 {
		http.Error(w, "missing or bad file headers", http.StatusBadRequest)
		return
	}
	// The id comes from the pinned key, never from the headers.
	fromName = n.learnPeer(from, fromName, fromPort, r.RemoteAddr)

	dir := n.ReceiveDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		http.Error(w, "cannot create download folder", http.StatusInternalServerError)
		return
	}
	tmp, err := os.CreateTemp(dir, ".dsync-*.part")
	if err != nil {
		http.Error(w, "cannot create file", http.StatusInternalServerError)
		return
	}

	m := n.addMessage(Message{
		PeerID: from.ID, PeerName: fromName, Incoming: true,
		File: &FileInfo{Name: name, Size: size, Status: StatusActive},
	})
	ctx, cancel := n.trackTransfer(r.Context(), m.ID)
	defer n.untrackTransfer(m.ID, cancel)

	final, err := n.writeIncoming(ctx, r, tmp, dir, name, size, n.progressReporter(m.ID, size))
	if err != nil {
		os.Remove(tmp.Name())
		n.finishTransfer(ctx, m.ID, "", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	n.finishTransfer(ctx, m.ID, final, nil)
	w.WriteHeader(http.StatusNoContent)
}

// writeIncoming copies the request body into tmp, verifies it, and moves it
// to its final name, which it returns.
func (n *Node) writeIncoming(ctx context.Context, r *http.Request, tmp *os.File, dir, name string, size int64, progress func(int64)) (string, error) {
	h := sha256.New()
	cw := &countingWriter{w: io.MultiWriter(tmp, h), progress: progress}
	_, err := io.CopyBuffer(cw, ctxReader{ctx, r.Body}, make([]byte, 1<<20))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	if cw.n != size {
		return "", fmt.Errorf("expected %d bytes, got %d", size, cw.n)
	}
	if want := r.Trailer.Get(proto.TrailerSHA256); want == "" || want != hex.EncodeToString(h.Sum(nil)) {
		return "", errors.New("checksum mismatch, file is corrupted")
	}
	os.Chmod(tmp.Name(), 0o644) // CreateTemp makes it private to the user
	final := uniquePath(dir, name)
	if err := os.Rename(tmp.Name(), final); err != nil {
		return "", err
	}
	return final, nil
}

func (n *Node) finishTransfer(ctx context.Context, id int64, savedPath string, err error) {
	n.updateFile(id, func(f *FileInfo) {
		switch {
		case err == nil:
			f.Status = StatusDone
			if savedPath != "" {
				f.Path = savedPath
			}
		case ctx.Err() != nil:
			f.Status = StatusCanceled
		default:
			f.Status, f.Error = StatusFailed, err.Error()
		}
	})
}

// CancelTransfer stops a running or queued transfer.
func (n *Node) CancelTransfer(id int64) {
	n.mu.Lock()
	cancel, known := n.transfers[id]
	if !known {
		n.transfers[id] = nil // still queued; trackTransfer will see this
	}
	n.mu.Unlock()
	if cancel != nil {
		cancel()
		return
	}
	n.updateFile(id, func(f *FileInfo) {
		if f.Status == StatusActive {
			f.Status = StatusCanceled
		}
	})
}

func (n *Node) trackTransfer(ctx context.Context, id int64) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	n.mu.Lock()
	if _, canceledEarly := n.transfers[id]; canceledEarly {
		cancel()
	}
	n.transfers[id] = cancel
	n.mu.Unlock()
	return ctx, cancel
}

func (n *Node) untrackTransfer(id int64, cancel context.CancelFunc) {
	cancel()
	n.mu.Lock()
	delete(n.transfers, id)
	n.mu.Unlock()
}

// progressReporter returns a callback that emits at most ~6 progress events
// per second for a transfer.
func (n *Node) progressReporter(id, size int64) func(done int64) {
	start := time.Now()
	var last time.Time
	return func(done int64) {
		now := time.Now()
		if now.Sub(last) < 150*time.Millisecond && done != size {
			return
		}
		last = now
		var rate int64
		if el := now.Sub(start).Seconds(); el > 0 {
			rate = int64(float64(done) / el)
		}
		n.emit(EventProgress, Progress{ID: id, Done: done, Rate: rate})
	}
}

type countingWriter struct {
	w        io.Writer
	n        int64
	progress func(int64)
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	c.progress(c.n)
	return n, err
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

var reservedWindowsName = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])(\..*)?$`)

// safeFileName turns a name from the network into something safe to create
// in the download folder on both Windows and Linux.
func safeFileName(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimRight(name, ". ")
	if name == "" || name == "." || name == ".." {
		name = "file"
	}
	if reservedWindowsName.MatchString(name) {
		name = "_" + name
	}
	return name
}

// uniquePath returns dir/name, or "name (2).ext" etc. if that exists.
func uniquePath(dir, name string) string {
	p := filepath.Join(dir, name)
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) {
			return p
		}
		p = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
	}
}
