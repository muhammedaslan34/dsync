package node

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Path       string `json:"path"` // source file when sending, saved file when received
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"` // why it failed, or a retry notice while active
	TransferID string `json:"transferId,omitempty"`
}

var (
	errCanceledByUser = errors.New("canceled")
	errChecksum       = errors.New("checksum mismatch, file is corrupted")
	errInterrupted    = errors.New("interrupted; it resumes when sent again")
	errSuperseded     = errors.New("replaced by a newer attempt")
)

const (
	// Partial downloads older than this are deleted at startup.
	partMaxAge = 7 * 24 * time.Hour
	// readIdleTimeout ends an incoming transfer when no data arrives for
	// this long, e.g. after the sender's Wi-Fi drops without closing.
	readIdleTimeout = 60 * time.Second
	// diskReserve is left free when checking space for an incoming file.
	diskReserve = 64 << 20
	partPrefix  = ".dsync-"
	partSuffix  = ".part"
)

// retryDelays are the waits between attempts when a connection drops while
// sending, about four minutes in total.
var retryDelays = []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second, 30 * time.Second, 30 * time.Second, 30 * time.Second, 30 * time.Second, 30 * time.Second, 30 * time.Second}

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

// RetryTransfer sends a failed or canceled outgoing file again. It resumes
// where the other device left off if the file hasn't changed.
func (n *Node) RetryTransfer(ctx context.Context, id int64) error {
	n.mu.Lock()
	var m *Message
	for i := range n.history {
		if n.history[i].ID == id {
			m = &n.history[i]
		}
	}
	var msg Message
	if m != nil {
		msg = *m
	}
	p := n.peers[msg.PeerID]
	var peer Peer
	if p != nil {
		peer = *p
	}
	n.mu.Unlock()

	switch {
	case m == nil || msg.File == nil || msg.Incoming:
		return errors.New("only files you sent can be retried")
	case msg.File.Status == StatusActive || msg.File.Status == StatusDone:
		return nil
	case p == nil:
		return fmt.Errorf("%s isn't available", msg.PeerName)
	}
	t, paired := n.trusted(peer.ID)
	if !paired {
		return fmt.Errorf("pair with %s first", peer.Name)
	}
	n.updateFile(id, func(f *FileInfo) { f.Status, f.Error = StatusActive, "" })
	go n.sendFile(ctx, peer, t.Fingerprint, id, msg.File.Path)
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
	h := client.FileHeader{
		FromID: self.ID, FromName: self.Name, FromPort: self.Port,
		Name: filepath.Base(path), Size: st.Size(),
		TransferID: client.TransferID(self.ID, path, st.Size(), st.ModTime()),
	}
	n.updateFile(id, func(f *FileInfo) { f.TransferID = h.TransferID })
	for attempt := 0; ; attempt++ {
		// Look the address up each time; it may change while we wait.
		addr := peer.Addr
		n.mu.Lock()
		if p := n.peers[peer.ID]; p != nil {
			addr = p.Addr
		}
		n.mu.Unlock()

		err = n.cl.SendFile(ctx, addr, fp, h, f, n.progressReporter(id, st.Size()))
		if err == nil || ctx.Err() != nil || !client.Retryable(err) || attempt >= len(retryDelays) {
			break
		}
		wait := retryDelays[attempt]
		n.updateFile(id, func(f *FileInfo) {
			f.Error = fmt.Sprintf("Connection lost. Retrying in %ds…", int(wait.Seconds()))
		})
		select {
		case <-time.After(wait):
		case <-ctx.Done():
		}
		n.updateFile(id, func(f *FileInfo) { f.Error = "Reconnecting…" })
	}
	n.finishTransfer(ctx, id, "", err)
}

// handleFileOffset tells a sender how much of a transfer we already have.
func (n *Node) handleFileOffset(w http.ResponseWriter, r *http.Request, from config.TrustedPeer) {
	var req proto.OffsetRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || !validTransferID(req.TransferID) {
		http.Error(w, "bad offset request", http.StatusBadRequest)
		return
	}
	var off int64
	part := n.partPath(from.ID, req.TransferID)
	if st, err := os.Stat(part); err == nil {
		if st.Size() <= req.Size {
			off = st.Size()
		} else {
			os.Remove(part) // not the same file after all
		}
	}
	// Refuse up front rather than failing when the disk fills up.
	dir := n.ReceiveDir()
	os.MkdirAll(dir, 0o755)
	if free, err := freeSpace(dir); err == nil && free < req.Size-off+diskReserve {
		http.Error(w, fmt.Sprintf("not enough disk space on %s (needs %s, %s free)",
			n.Self().Name, humanSize(req.Size-off), humanSize(free)), http.StatusInsufficientStorage)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(proto.OffsetResponse{Offset: off})
}

func (n *Node) receiveFile(w http.ResponseWriter, r *http.Request, from config.TrustedPeer) {
	fromName, _ := url.QueryUnescape(r.Header.Get(proto.HeaderFromName))
	fromPort, _ := strconv.Atoi(r.Header.Get(proto.HeaderFromPort))
	rawName, _ := url.QueryUnescape(r.Header.Get(proto.HeaderFileName))
	name := safeFileName(rawName)
	tid := r.Header.Get(proto.HeaderTransferID)
	size, err := strconv.ParseInt(r.Header.Get(proto.HeaderFileSize), 10, 64)
	offset, oerr := strconv.ParseInt(r.Header.Get(proto.HeaderOffset), 10, 64)
	if err != nil || oerr != nil || size < 0 || offset < 0 || offset > size || !validTransferID(tid) {
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
	part := n.partPath(from.ID, tid)
	pctx, release := n.claimPart(r.Context(), part, http.NewResponseController(w))
	defer release()
	f, err := os.OpenFile(part, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		http.Error(w, "cannot create file", http.StatusInternalServerError)
		return
	}
	if st, err := f.Stat(); err != nil || st.Size() < offset {
		f.Close()
		http.Error(w, "resume position is past what was received; send again", http.StatusConflict)
		return
	}

	id := n.incomingFileMessage(from.ID, fromName, tid, name, size)
	ctx, cancel := n.trackTransfer(pctx, id)
	defer n.untrackTransfer(id, cancel)

	rc := http.NewResponseController(w)
	final, err := writeIncoming(ctx, r, rc, f, offset, dir, name, size, n.progressReporter(id, size))
	if err != nil {
		// Keep what arrived so the sender can resume, unless the data is
		// bad or the user stopped it here.
		if errors.Is(err, errChecksum) || errors.Is(context.Cause(ctx), errCanceledByUser) {
			os.Remove(part)
		}
		n.finishTransfer(ctx, id, "", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	n.finishTransfer(ctx, id, final, nil)
	w.WriteHeader(http.StatusNoContent)
}

type partClaim struct {
	stop func()
	done chan struct{}
}

// claimPart gives one request at a time the right to write a partial file.
// A sender only sends a transfer once at a time, so a new request for the
// same part means the old connection is dead even if we haven't noticed:
// the old request is stopped (its blocked read is woken up) and we wait for
// it to finish before going on. release must be called when done.
func (n *Node) claimPart(ctx context.Context, part string, rc *http.ResponseController) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(ctx)
	c := &partClaim{
		stop: func() {
			cancel(errSuperseded)
			rc.SetReadDeadline(time.Now())
		},
		done: make(chan struct{}),
	}
	for {
		n.mu.Lock()
		old := n.parts[part]
		if old == nil {
			n.parts[part] = c
			n.mu.Unlock()
			break
		}
		n.mu.Unlock()
		old.stop()
		select {
		case <-old.done:
		case <-time.After(10 * time.Second):
			n.mu.Lock()
			if n.parts[part] == old {
				delete(n.parts, part) // give up on it
			}
			n.mu.Unlock()
		}
	}
	return ctx, func() {
		cancel(nil)
		n.mu.Lock()
		if n.parts[part] == c {
			delete(n.parts, part)
		}
		n.mu.Unlock()
		close(c.done)
	}
}

// incomingFileMessage returns the message for an incoming transfer, reusing
// the one from an earlier interrupted attempt so a resumed file shows once.
func (n *Node) incomingFileMessage(peerID, peerName, tid, name string, size int64) int64 {
	n.mu.Lock()
	var id int64
	for i := len(n.history) - 1; i >= 0; i-- {
		m := n.history[i]
		if m.Incoming && m.PeerID == peerID && m.File != nil && m.File.TransferID == tid && m.File.Status != StatusDone {
			id = m.ID
			break
		}
	}
	n.mu.Unlock()
	if id != 0 {
		n.updateFile(id, func(f *FileInfo) { f.Status, f.Error = StatusActive, "" })
		return id
	}
	return n.addMessage(Message{
		PeerID: peerID, PeerName: peerName, Incoming: true,
		File: &FileInfo{Name: name, Size: size, Status: StatusActive, TransferID: tid},
	}).ID
}

// writeIncoming appends the request body to the partial file f from offset,
// verifies the whole file, and moves it to its final name, which it returns.
func writeIncoming(ctx context.Context, r *http.Request, rc *http.ResponseController, f *os.File, offset int64, dir, name string, size int64, progress func(int64)) (string, error) {
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.NewSectionReader(f, 0, offset)); err != nil {
		return "", err
	}
	if err := f.Truncate(offset); err != nil {
		return "", err
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return "", err
	}
	cw := &countingWriter{w: io.MultiWriter(f, h), n: offset, progress: progress}
	rd := &ctxReader{ctx: ctx, r: r.Body, rc: rc}
	_, err := io.CopyBuffer(cw, rd, make([]byte, 1<<20))
	if err != nil && err == rd.err {
		err = errInterrupted // the sender went away; disk errors are reported as they are
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	if cw.n != size {
		return "", fmt.Errorf("expected %d bytes, got %d", size, cw.n)
	}
	if want := r.Trailer.Get(proto.TrailerSHA256); want == "" || want != hex.EncodeToString(h.Sum(nil)) {
		return "", errChecksum
	}
	final := uniquePath(dir, name)
	if err := os.Rename(f.Name(), final); err != nil {
		return "", err
	}
	return final, nil
}

// partPath is where a partial download is kept. It depends on the sender
// and transfer id, so only the same sender can resume it.
func (n *Node) partPath(peerID, tid string) string {
	sum := sha256.Sum256([]byte(peerID + "/" + tid))
	return filepath.Join(n.ReceiveDir(), partPrefix+hex.EncodeToString(sum[:12])+partSuffix)
}

func validTransferID(tid string) bool {
	if len(tid) < 16 || len(tid) > 64 {
		return false
	}
	_, err := hex.DecodeString(tid)
	return err == nil
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// cleanParts deletes partial downloads nobody resumed.
func (n *Node) cleanParts() {
	dir := n.ReceiveDir()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, partPrefix) || !strings.HasSuffix(name, partSuffix) {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > partMaxAge {
			os.Remove(filepath.Join(dir, name))
		}
	}
}

func (n *Node) finishTransfer(ctx context.Context, id int64, savedPath string, err error) {
	n.updateFile(id, func(f *FileInfo) {
		f.Error = ""
		switch {
		case err == nil:
			f.Status = StatusDone
			if savedPath != "" {
				f.Path = savedPath
			}
		case errors.Is(context.Cause(ctx), errCanceledByUser):
			f.Status = StatusCanceled
		case ctx.Err() != nil:
			f.Status, f.Error = StatusFailed, "interrupted"
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
		cancel(errCanceledByUser)
		return
	}
	n.updateFile(id, func(f *FileInfo) {
		if f.Status == StatusActive {
			f.Status = StatusCanceled
		}
	})
}

func (n *Node) trackTransfer(ctx context.Context, id int64) (context.Context, context.CancelCauseFunc) {
	ctx, cancel := context.WithCancelCause(ctx)
	n.mu.Lock()
	if _, canceledEarly := n.transfers[id]; canceledEarly {
		cancel(errCanceledByUser)
	}
	n.transfers[id] = cancel
	n.mu.Unlock()
	return ctx, cancel
}

func (n *Node) untrackTransfer(id int64, cancel context.CancelCauseFunc) {
	cancel(nil)
	n.mu.Lock()
	delete(n.transfers, id)
	n.mu.Unlock()
}

// progressReporter returns a callback that emits at most ~6 progress events
// per second for one attempt at a transfer. The rate counts only bytes moved
// in this attempt, so a resumed transfer doesn't look faster than it is.
func (n *Node) progressReporter(id, size int64) func(done int64) {
	var start, last time.Time
	var base int64 = -1
	return func(done int64) {
		now := time.Now()
		if base < 0 {
			base, start = done, now
		}
		if now.Sub(last) < 150*time.Millisecond && done != size {
			return
		}
		last = now
		var rate int64
		if el := now.Sub(start).Seconds(); el > 0.2 {
			rate = int64(float64(done-base) / el)
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

// ctxReader stops reading when ctx is done or no data arrives for
// readIdleTimeout, and remembers its last error.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
	rc  *http.ResponseController
	err error
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if c.err = c.ctx.Err(); c.err != nil {
		return 0, c.err
	}
	if c.rc != nil {
		c.rc.SetReadDeadline(time.Now().Add(readIdleTimeout))
	}
	var n int
	n, c.err = c.r.Read(p)
	return n, c.err
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
