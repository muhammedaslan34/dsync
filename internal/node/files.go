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
	"unicode"

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

	// Folders: Path is the folder, Size the total of its files.
	Folder    bool   `json:"folder,omitempty"`
	Files     int    `json:"files,omitempty"`
	DoneFiles int    `json:"doneFiles,omitempty"`
	DoneBytes int64  `json:"doneBytes,omitempty"`
	FolderID  string `json:"folderId,omitempty"`
	Run       string `json:"run,omitempty"` // the sender's current attempt (receiver side)
}

var (
	errCanceledByUser = errors.New("canceled")
	errChecksum       = errors.New("checksum mismatch, file is corrupted")
	errInterrupted    = errors.New("interrupted; it resumes when sent again")
	errByteCount      = errors.New("received byte count does not match declared size")
	errSuperseded     = errors.New("replaced by a newer attempt")
)

const (
	// Partial downloads older than this are deleted while the node runs.
	partMaxAge = 7 * 24 * time.Hour
	// readIdleTimeout ends an incoming transfer when no data arrives for
	// this long, e.g. after the sender's Wi-Fi drops without closing.
	readIdleTimeout = 60 * time.Second
	// diskReserve is left free when checking space for an incoming file.
	diskReserve = 64 << 20
	// Do not let one peer accumulate an unbounded number of abandoned
	// resumable transfers. Existing transfers can always be resumed, even
	// after reaching this limit.
	maxPeerPartialFiles = 64
	maxPeerPartialBytes = 1 << 30
	partCleanupInterval = time.Hour
	partPrefix          = ".dsync-"
	partSuffix          = ".part"
	partMetaSuffix      = ".meta"
)

var (
	errPartQuota    = errors.New("partial transfer quota exceeded")
	errPartConflict = errors.New("partial transfer metadata changed")
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

// SendFiles sends files and folders to a peer one after another in the
// background. Progress and results are reported through events.
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
	if peer.Phone {
		return n.sendToPhone(peer, "", paths)
	}
	t, paired := n.trusted(peerID)
	if !paired {
		return fmt.Errorf("pair with %s first", peer.Name)
	}
	infos := make([]os.FileInfo, len(paths))
	for i, path := range paths {
		st, err := os.Stat(path)
		if err != nil {
			return err
		}
		infos[i] = st
	}

	// Create all the messages up front so the queue shows at once. A
	// folder's size and file count are filled in when it starts.
	ids := make([]int64, len(paths))
	for i, path := range paths {
		f := &FileInfo{Name: filepath.Base(path), Size: infos[i].Size(), Path: path, Status: StatusActive}
		if infos[i].IsDir() {
			f.Folder, f.Size = true, 0
		}
		ids[i] = n.addMessage(Message{PeerID: peer.ID, PeerName: peer.Name, File: f}).ID
	}
	go func() {
		for i, path := range paths {
			if infos[i].IsDir() {
				n.sendFolder(ctx, peer, t.Fingerprint, ids[i], path)
			} else {
				n.sendFile(ctx, peer, t.Fingerprint, ids[i], path)
			}
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
	n.mu.Lock()
	if c, ok := n.transfers[id]; ok && c == nil {
		delete(n.transfers, id) // a cancel marker left from when it was queued
	}
	n.mu.Unlock()
	n.updateFile(id, func(f *FileInfo) { f.Status, f.Error = StatusActive, "" })
	if msg.File.Folder {
		go n.sendFolder(ctx, peer, t.Fingerprint, id, msg.File.Path)
	} else {
		go n.sendFile(ctx, peer, t.Fingerprint, id, msg.File.Path)
	}
	return nil
}

func (n *Node) sendFile(ctx context.Context, peer Peer, fp string, id int64, path string) {
	ctx, cancel := n.trackTransfer(ctx, id)
	defer n.untrackTransfer(id, cancel)
	if ctx.Err() != nil { // canceled while queued
		n.finishTransfer(ctx, id, "", ctx.Err())
		return
	}
	err := n.transmit(ctx, peer, fp, id, path, client.FileHeader{}, func(size int64) func(int64) {
		return n.progressReporter(id, size)
	})
	n.finishTransfer(ctx, id, "", err)
}

// transmit sends one file, retrying for a while if the connection drops and
// showing retry notices on message id. h may carry folder fields; the rest
// is filled in here. newProgress is called for each attempt with the file's
// size and returns that attempt's progress callback.
func (n *Node) transmit(ctx context.Context, peer Peer, fp string, id int64, path string, h client.FileHeader, newProgress func(size int64) func(int64)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}

	self := n.Self()
	h.FromID, h.FromName, h.FromPort = self.ID, self.Name, self.Port
	h.Name, h.Size = filepath.Base(path), st.Size()
	h.TransferID = client.TransferID(self.ID, path, st.Size(), st.ModTime())
	for attempt := 0; ; attempt++ {
		// Look the address up each time; it may change while we wait.
		err = n.cl.SendFile(ctx, n.peerAddr(peer), fp, h, f, newProgress(st.Size()))
		if attempt > 0 {
			n.updateFile(id, func(f *FileInfo) { f.Error = "" })
		}
		if err == nil || ctx.Err() != nil || !client.Retryable(err) || attempt >= len(retryDelays) {
			return err
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
}

// handleFileOffset tells a sender how much of a transfer we already have.
func (n *Node) handleFileOffset(w http.ResponseWriter, r *http.Request, from config.TrustedPeer) {
	var req proto.OffsetRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || req.Size < 0 || !validTransferID(req.TransferID) {
		http.Error(w, "bad offset request", http.StatusBadRequest)
		return
	}
	meta := incomingMeta{transferID: req.TransferID, name: req.Name, size: req.Size, sizeKnown: true, memberSize: req.Size, probe: true}
	var existingFolder Message
	var haveExistingFolder bool
	if req.FolderID != "" {
		meta.folderID, meta.name, meta.relPath = req.FolderID, folderRootName(req.RelPath), req.RelPath
		meta.size, meta.files = req.FolderSize, req.FolderFiles
		meta.sizeKnown, meta.filesKnown = req.FolderFiles > 0, req.FolderFiles > 0
		existingFolder, haveExistingFolder = n.findFolder(from.ID, req.FolderID)
		if haveExistingFolder {
			meta.baseFiles, meta.baseBytes = existingFolder.File.DoneFiles, existingFolder.File.DoneBytes
			if dest, err := folderDest(existingFolder.File.Path, req.RelPath); err == nil {
				if st, err := os.Stat(dest); err == nil && st.Mode().IsRegular() && st.Size() == req.Size {
					meta.memberDone = true
				}
			}
		}
	}
	if !n.authorizeIncoming(w, from, meta) {
		return
	}
	clearApproval := true
	defer func() {
		if clearApproval {
			n.completeIncomingApproval(from.ID, meta)
		}
	}()
	if req.FolderID != "" {
		if m, ok := existingFolder, haveExistingFolder; ok {
			if m.File.Status == StatusCanceled && m.File.Run == req.FolderRun {
				n.completeIncomingApproval(from.ID, meta)
				http.Error(w, errFolderCanceled.Error(), http.StatusForbidden)
				return
			}
			// Already have this file from an earlier attempt?
			if dest, err := folderDest(m.File.Path, req.RelPath); err == nil {
				if st, err := os.Stat(dest); err == nil && st.Mode().IsRegular() && st.Size() == req.Size {
					w.Header().Set("Content-Type", "application/json")
					clearApproval = false
					json.NewEncoder(w).Encode(proto.OffsetResponse{Offset: req.Size, Complete: true})
					return
				}
			}
		}
	}

	var off int64
	part := n.preparePeerPartPath(from.ID, req.TransferID)
	if bound, ok := readPartMeta(part); ok && bound.Size != req.Size {
		http.Error(w, errPartConflict.Error(), http.StatusConflict)
		return
	}
	if st, err := os.Stat(part); err == nil {
		if st.Size() <= req.Size {
			off = st.Size()
		} else {
			os.Remove(part) // not the same file after all
			os.Remove(partMetaPath(part))
		}
	}
	// Refuse up front rather than failing when the disk fills up.
	dir := n.ReceiveDir()
	os.MkdirAll(dir, 0o755)
	if free, ok := n.hasReceiveSpace(dir, req.Size-off); !ok {
		http.Error(w, fmt.Sprintf("not enough disk space on %s (needs %s, %s free)",
			n.Self().Name, humanSize(req.Size-off), humanSize(free)), http.StatusInsufficientStorage)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	clearApproval = false
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
	meta := incomingMeta{transferID: tid, name: rawName, size: size, sizeKnown: true, memberSize: size}
	if fid := r.Header.Get(proto.HeaderFolderID); fid != "" {
		rel, _ := url.QueryUnescape(r.Header.Get(proto.HeaderRelPath))
		folderSizeText, folderFilesText := r.Header.Get(proto.HeaderFolderSize), r.Header.Get(proto.HeaderFolderFiles)
		meta.folderID, meta.name, meta.relPath = fid, folderRootName(rel), rel
		meta.size, err = strconv.ParseInt(folderSizeText, 10, 64)
		meta.files, oerr = strconv.Atoi(folderFilesText)
		if err != nil || oerr != nil || meta.size < 0 || meta.files <= 0 {
			http.Error(w, "missing or bad folder headers", http.StatusBadRequest)
			return
		}
		meta.sizeKnown, meta.filesKnown = true, true
		if existing, ok := n.findFolder(from.ID, fid); ok {
			meta.baseFiles, meta.baseBytes = existing.File.DoneFiles, existing.File.DoneBytes
		}
	}
	if !n.authorizeIncoming(w, from, meta) {
		return
	}
	clearApproval := true
	defer func() {
		if clearApproval {
			n.completeIncomingApproval(from.ID, meta)
		}
	}()
	// The id comes from the pinned key, never from the headers.
	fromName = n.learnPeer(from, fromName, fromPort, r.RemoteAddr)

	dir := n.ReceiveDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		http.Error(w, "cannot create download folder", http.StatusInternalServerError)
		return
	}
	part := n.preparePeerPartPath(from.ID, tid)
	pctx, release := n.claimPart(r.Context(), from.ID, part, http.NewResponseController(w))
	defer release()
	releaseReservation, err := n.reservePeerPart(from.ID, part, size, offset)
	if err != nil {
		code := http.StatusInsufficientStorage
		if errors.Is(err, errPartConflict) {
			code = http.StatusConflict
		}
		http.Error(w, err.Error(), code)
		return
	}
	defer releaseReservation()
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

	// Where the file goes, and which message shows it.
	var (
		id       int64
		dest     func() (string, error)
		progress func(int64)
		inFolder = r.Header.Get(proto.HeaderFolderID) != ""
	)
	if inFolder {
		fsize, _ := strconv.ParseInt(r.Header.Get(proto.HeaderFolderSize), 10, 64)
		ffiles, _ := strconv.Atoi(r.Header.Get(proto.HeaderFolderFiles))
		rel, _ := url.QueryUnescape(r.Header.Get(proto.HeaderRelPath))
		rootName, _, _ := strings.Cut(rel, "/")
		var root string
		id, root, err = n.incomingFolder(from, fromName, r.Header.Get(proto.HeaderFolderID), r.Header.Get(proto.HeaderFolderRun), rootName, fsize, ffiles)
		if err == nil {
			_, err = folderDest(root, rel)
		}
		if err != nil {
			f.Close()
			code := http.StatusBadRequest
			if errors.Is(err, errFolderCanceled) {
				code = http.StatusForbidden
			}
			http.Error(w, err.Error(), code)
			return
		}
		dest = func() (string, error) {
			p, err := folderDest(root, rel)
			if err != nil {
				return "", err
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return "", err
			}
			return uniquePath(filepath.Dir(p), filepath.Base(p)), nil
		}
		report := n.folderReporter(id, fsize)
		base := n.fileInfo(id).DoneBytes
		progress = func(d int64) { report(base + d) }
	} else {
		id = n.incomingFileMessage(from.ID, fromName, tid, name, size)
		dest = func() (string, error) { return uniquePath(dir, name), nil }
		progress = n.progressReporter(id, size)
	}
	ctx, cancel := n.trackTransfer(pctx, id)
	defer n.untrackTransfer(id, cancel)

	rc := http.NewResponseController(w)
	final, err := writeIncoming(ctx, r, rc, f, offset, size, dest, progress)
	if err != nil {
		// Keep what arrived so the sender can resume, unless the data is
		// bad or the user stopped it here.
		userCanceled := errors.Is(context.Cause(ctx), errCanceledByUser)
		if errors.Is(err, errChecksum) || errors.Is(err, errByteCount) || userCanceled {
			os.Remove(part)
			os.Remove(partMetaPath(part))
		}
		n.finishTransfer(ctx, id, "", err)
		if errors.Is(err, errInterrupted) && !userCanceled {
			clearApproval = false
		}
		if errors.Is(context.Cause(ctx), errSuperseded) {
			clearApproval = false
		}
		if userCanceled {
			http.Error(w, errFolderCanceled.Error(), http.StatusForbidden)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}
	if inFolder {
		n.folderFileDone(id, size)
		clearApproval = n.fileInfo(id).Status == StatusDone
	} else {
		n.finishTransfer(ctx, id, final, nil)
	}
	os.Remove(partMetaPath(part))
	w.WriteHeader(http.StatusNoContent)
}

// fileInfo returns a copy of message id's file info.
func (n *Node) fileInfo(id int64) FileInfo {
	n.mu.Lock()
	defer n.mu.Unlock()
	for i := len(n.history) - 1; i >= 0; i-- {
		if n.history[i].ID == id && n.history[i].File != nil {
			return *n.history[i].File
		}
	}
	return FileInfo{}
}

// folderReporter returns the progress callback shared by all files of an
// incoming folder, so its speed is averaged over the whole folder.
func (n *Node) folderReporter(id, size int64) func(int64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if r, ok := n.folderReps[id]; ok {
		return r
	}
	r := n.progressReporter(id, size)
	n.folderReps[id] = r
	return r
}

type partClaim struct {
	peerID string
	stop   func()
	done   chan struct{}
}

// claimPart gives one request at a time the right to write a partial file.
// A sender only sends a transfer once at a time, so a new request for the
// same part means the old connection is dead even if we haven't noticed:
// the old request is stopped (its blocked read is woken up) and we wait for
// it to finish before going on. release must be called when done.
func (n *Node) claimPart(ctx context.Context, peerID, part string, rc *http.ResponseController) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(ctx)
	c := &partClaim{
		peerID: peerID,
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
func writeIncoming(ctx context.Context, r *http.Request, rc *http.ResponseController, f *os.File, offset, size int64, dest func() (string, error), progress func(int64)) (string, error) {
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
	remaining := size - offset
	// Copy at most the declared remainder. Then consume one probe byte so an
	// oversized chunked body is rejected without ever writing past size.
	_, err := io.CopyBuffer(cw, io.LimitReader(rd, remaining), make([]byte, 1<<20))
	if err != nil && err == rd.err {
		err = errInterrupted // the sender went away; disk errors are reported as they are
	}
	if err == nil && cw.n == size {
		if n, probeErr := io.CopyN(io.Discard, rd, 1); n != 0 {
			err = fmt.Errorf("expected %d bytes, got more: %w", size, errByteCount)
		} else if probeErr != nil && !errors.Is(probeErr, io.EOF) {
			if probeErr == rd.err {
				err = errInterrupted
			} else {
				err = probeErr
			}
		}
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	if cw.n != size {
		return "", fmt.Errorf("expected %d bytes, got %d: %w", size, cw.n, errByteCount)
	}
	if want := r.Trailer.Get(proto.TrailerSHA256); want == "" || want != hex.EncodeToString(h.Sum(nil)) {
		return "", errChecksum
	}
	final, err := dest()
	if err != nil {
		return "", err
	}
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

// peerPartPath includes a hash of the peer id separately, which lets quotas
// account for every computer-to-computer partial (including folder members)
// without revealing the id in the filename. partPath is the pre-quota format
// and remains in use by phone uploads.
func (n *Node) peerPartPath(peerID, tid string) string {
	peer := sha256.Sum256([]byte(peerID))
	transfer := sha256.Sum256([]byte(peerID + "/" + tid))
	name := partPrefix + hex.EncodeToString(peer[:8]) + "-" + hex.EncodeToString(transfer[:12]) + partSuffix
	return filepath.Join(n.ReceiveDir(), name)
}

// preparePeerPartPath transparently adopts a partial made by an older dsync
// version, preserving resumability across the filename format change.
func (n *Node) preparePeerPartPath(peerID, tid string) string {
	part := n.peerPartPath(peerID, tid)
	if _, err := os.Stat(part); err == nil {
		return part
	}
	legacy := n.partPath(peerID, tid)
	if err := os.Rename(legacy, part); err == nil {
		return part
	}
	return part
}

type partMeta struct {
	Size int64 `json:"size"`
}

type partReservation struct {
	peerID    string
	remaining int64
}

func partMetaPath(part string) string { return part + partMetaSuffix }

func readPartMeta(part string) (partMeta, bool) {
	b, err := os.ReadFile(partMetaPath(part))
	if err != nil {
		return partMeta{}, false
	}
	var m partMeta
	if json.Unmarshal(b, &m) != nil || m.Size < 0 {
		return partMeta{}, false
	}
	return m, true
}

func writePartMeta(part string, size int64) error {
	path := partMetaPath(part)
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".dsync-meta-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0o600); err == nil {
		err = json.NewEncoder(f).Encode(partMeta{Size: size})
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, path)
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

// hasReceiveSpace checks without integer overflow whether needed bytes can be
// written while preserving diskReserve. Failure to query the filesystem is
// left to the subsequent write, as before.
func (n *Node) hasReceiveSpace(dir string, needed int64) (free int64, ok bool) {
	free, err := n.freeSpace(dir)
	if err != nil {
		return 0, true
	}
	return free, free >= diskReserve && needed <= free-diskReserve
}

// reservePeerPart binds a part to its original declared size and atomically
// reserves both the peer quota and currently available disk space. The small
// sidecar makes the binding survive a restart. Legacy parts are bound on their
// first resumed request, preserving compatibility without allowing later size
// inflation.
func (n *Node) reservePeerPart(peerID, part string, declaredSize, offset int64) (func(), error) {
	if declaredSize < 0 || offset < 0 || offset > declaredSize {
		return nil, errPartConflict
	}
	dir := filepath.Dir(part)
	free, freeErr := n.freeSpace(dir)

	n.mu.Lock()
	defer n.mu.Unlock()
	if bound, ok := readPartMeta(part); ok && bound.Size != declaredSize {
		return nil, errPartConflict
	}

	peer := sha256.Sum256([]byte(peerID))
	prefix := partPrefix + hex.EncodeToString(peer[:8]) + "-"
	entries, _ := os.ReadDir(dir)
	accounted := make(map[string]bool)
	var files int64
	var bytes int64
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), partSuffix) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		accounted[path] = true
		files++
		amount := int64(0)
		if path == part {
			amount = declaredSize
		} else if bound, ok := readPartMeta(path); ok {
			amount = bound.Size
		} else if st, err := entry.Info(); err == nil {
			amount = st.Size()
		}
		if amount > maxPeerPartialBytes-bytes {
			return nil, errPartQuota
		}
		bytes += amount
	}
	// A reservation may precede creation of its part, so include it exactly
	// once in the same locked admission decision.
	for path, reservation := range n.partReservations {
		if reservation.peerID != peerID || accounted[path] {
			continue
		}
		files++
		bound, ok := readPartMeta(path)
		if !ok || bound.Size > maxPeerPartialBytes-bytes {
			return nil, errPartQuota
		}
		bytes += bound.Size
	}
	if !accounted[part] {
		files++
		if declaredSize > maxPeerPartialBytes-bytes {
			return nil, errPartQuota
		}
		bytes += declaredSize
	}
	if files > maxPeerPartialFiles || bytes > maxPeerPartialBytes {
		return nil, errPartQuota
	}

	remaining := declaredSize - offset
	if freeErr == nil {
		if free < diskReserve {
			return nil, fmt.Errorf("%w: not enough disk space", errPartQuota)
		}
		reserved := int64(0)
		for path, reservation := range n.partReservations {
			if path == part {
				continue
			}
			if reservation.remaining > free-diskReserve-reserved {
				return nil, fmt.Errorf("%w: not enough disk space", errPartQuota)
			}
			reserved += reservation.remaining
		}
		if remaining > free-diskReserve-reserved {
			return nil, fmt.Errorf("%w: not enough disk space", errPartQuota)
		}
	}
	if _, ok := readPartMeta(part); !ok {
		if err := writePartMeta(part, declaredSize); err != nil {
			return nil, err
		}
	}
	n.partReservations[part] = partReservation{peerID: peerID, remaining: remaining}
	return func() {
		n.mu.Lock()
		delete(n.partReservations, part)
		n.mu.Unlock()
	}, nil
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
			path := filepath.Join(dir, name)
			n.mu.Lock()
			if n.parts[path] == nil {
				os.Remove(path)
				os.Remove(partMetaPath(path))
			}
			n.mu.Unlock()
		}
	}
}

func (n *Node) cleanPartsLoop(ctx context.Context, interval time.Duration) {
	n.cleanParts()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n.cleanParts()
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
	var approvalPeer string
	var approvalMeta incomingMeta
	for i := len(n.history) - 1; i >= 0; i-- {
		m := n.history[i]
		if m.ID == id && m.Incoming && m.File != nil {
			approvalPeer = m.PeerID
			approvalMeta = incomingMeta{transferID: m.File.TransferID, folderID: m.File.FolderID}
			break
		}
	}
	if !known && n.isOutgoingLocked(id) {
		// Still queued to send; trackTransfer will see this when it starts.
		// (An incoming folder between files needs no marker: its status
		// refuses the rest of the sender's run.)
		n.transfers[id] = nil
	}
	n.mu.Unlock()
	if approvalPeer != "" {
		n.completeIncomingApproval(approvalPeer, approvalMeta)
	}
	if cancel != nil {
		cancel(errCanceledByUser)
	}
	// Mark it now rather than waiting for the transfer to notice: a file
	// that was just finishing still completes, but a folder stays canceled
	// and refuses its next file.
	n.updateFile(id, func(f *FileInfo) {
		if f.Status == StatusActive {
			f.Status = StatusCanceled
		}
	})
}

func (n *Node) isOutgoingLocked(id int64) bool {
	for i := len(n.history) - 1; i >= 0; i-- {
		if n.history[i].ID == id {
			return !n.history[i].Incoming
		}
	}
	return false
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
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
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
