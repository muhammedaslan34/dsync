package node

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"dsync/internal/client"
	"dsync/internal/config"
	"dsync/internal/proto"
)

// A folder is sent as its files, one after another, each with the usual
// resume and checksum handling. Sender and receiver each show the whole
// folder as a single message.

// FolderEntry is a file to send as part of a folder.
type FolderEntry struct {
	Path string // on this computer
	Rel  string // "/" separated, starting with the folder's own name
	Size int64
}

// ListFolder returns the regular files under root in a stable order.
// Symlinks and special files are skipped, as are empty folders.
func ListFolder(root string) ([]FolderEntry, int64, error) {
	parent := filepath.Dir(root)
	var entries []FolderEntry
	var total int64
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(parent, p)
		if err != nil {
			return err
		}
		entries = append(entries, FolderEntry{Path: p, Rel: filepath.ToSlash(rel), Size: info.Size()})
		total += info.Size()
		return nil
	})
	return entries, total, err
}

// NewRunID makes the id of one attempt at sending a folder.
func NewRunID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (n *Node) sendFolder(ctx context.Context, peer Peer, fp string, id int64, root string) {
	ctx, cancel := n.trackTransfer(ctx, id)
	defer n.untrackTransfer(id, cancel)
	if ctx.Err() != nil { // canceled while queued
		n.finishTransfer(ctx, id, "", ctx.Err())
		return
	}
	entries, total, err := ListFolder(root)
	if err == nil && len(entries) == 0 {
		err = errors.New("the folder has no files")
	}
	if err != nil {
		n.finishTransfer(ctx, id, "", err)
		return
	}

	self := n.Self()
	fid := client.FolderID(self.ID, root)
	run := NewRunID()
	n.updateFile(id, func(f *FileInfo) {
		f.Size, f.Files, f.FolderID, f.DoneFiles, f.DoneBytes = total, len(entries), fid, 0, 0
	})

	// One reporter for the whole folder, so the speed is averaged over all
	// files instead of jumping around on small ones.
	report := n.progressReporter(id, total)
	var doneBytes int64
	for i, e := range entries {
		h := client.FileHeader{FolderID: fid, FolderRun: run, FolderSize: total, FolderFiles: len(entries), RelPath: e.Rel}
		err = n.transmit(ctx, peer, fp, id, e.Path, h, func(int64) func(int64) {
			return func(d int64) { report(doneBytes + d) }
		})
		if errors.Is(err, client.ErrAlreadyReceived) {
			err = nil // sent in an earlier attempt
		}
		if err != nil {
			err = fmt.Errorf("%s: %w", e.Rel, err)
			break
		}
		doneBytes += e.Size
		n.updateFile(id, func(f *FileInfo) { f.DoneFiles, f.DoneBytes = i+1, doneBytes })
		report(doneBytes)
	}

	// Tell the receiver how it ended, so its copy doesn't look stuck.
	end := proto.FolderEnd{FolderID: fid, Status: StatusDone}
	switch {
	case errors.Is(context.Cause(ctx), errCanceledByUser):
		end.Status = StatusCanceled
	case err != nil:
		end.Status, end.Error = StatusFailed, err.Error()
	}
	go n.cl.FolderEnd(context.WithoutCancel(ctx), n.peerAddr(peer), fp, end)
	n.finishTransfer(ctx, id, "", err)
}

// peerAddr is the current address of a peer; it can change while a long
// transfer runs.
func (n *Node) peerAddr(peer Peer) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if p := n.peers[peer.ID]; p != nil {
		return p.Addr
	}
	return peer.Addr
}

// errFolderCanceled is also used for single files canceled by the receiver.
var errFolderCanceled = errors.New("canceled on the receiving device")

// incomingFolder finds or creates the message for an incoming folder and
// returns it with its destination folder. A folder the user canceled here
// refuses the rest of that sending run, but a later retry reopens it.
func (n *Node) incomingFolder(from config.TrustedPeer, fromName, fid, run, rootName string, size int64, files int) (int64, string, error) {
	if m, ok := n.findFolder(from.ID, fid); ok {
		f := m.File
		if f.Status == StatusCanceled && f.Run == run {
			return 0, "", errFolderCanceled
		}
		if f.Status != StatusActive || f.Run != run {
			n.updateFile(m.ID, func(f *FileInfo) { f.Status, f.Error, f.Run = StatusActive, "", run })
		}
		return m.ID, f.Path, nil
	}

	dir := n.ReceiveDir()
	root := uniquePath(dir, safeFileName(rootName))
	if err := os.MkdirAll(root, 0o755); err != nil {
		return 0, "", err
	}
	m := n.addMessage(Message{
		PeerID: from.ID, PeerName: fromName, Incoming: true,
		File: &FileInfo{
			Name: filepath.Base(root), Size: size, Path: root, Status: StatusActive,
			Folder: true, Files: files, FolderID: fid, Run: run,
		},
	})
	return m.ID, root, nil
}

// findFolder returns the latest unfinished incoming folder message for a
// sender's folder id.
func (n *Node) findFolder(peerID, fid string) (Message, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for i := len(n.history) - 1; i >= 0; i-- {
		m := n.history[i]
		if m.Incoming && m.PeerID == peerID && m.File != nil && m.File.Folder && m.File.FolderID == fid {
			if m.File.Status == StatusDone {
				return Message{}, false // sending it again makes a new copy
			}
			return m, true
		}
	}
	return Message{}, false
}

// folderDest turns a relative path from the network into a path inside
// root. Every segment is made safe, and the result can't leave root.
func folderDest(root, rel string) (string, error) {
	parts := strings.Split(rel, "/")
	var segs []string
	for _, p := range parts[1:] { // parts[0] is the folder's own name
		if p == "" || p == "." {
			continue
		}
		segs = append(segs, safeFileName(p))
	}
	if len(segs) == 0 {
		return "", errors.New("bad file path in folder")
	}
	dest := filepath.Join(append([]string{root}, segs...)...)
	if !strings.HasPrefix(dest, filepath.Clean(root)+string(filepath.Separator)) {
		return "", errors.New("bad file path in folder")
	}
	return dest, nil
}

// folderFileDone counts a received file towards its folder.
func (n *Node) folderFileDone(id int64, size int64) {
	n.updateFile(id, func(f *FileInfo) {
		f.DoneFiles++
		f.DoneBytes += size
		if f.DoneFiles >= f.Files {
			f.Status, f.Error = StatusDone, ""
			f.DoneFiles, f.DoneBytes = f.Files, f.Size
		}
	})
	if n.fileInfo(id).Status == StatusDone {
		n.dropFolderReporter(id)
	}
}

func (n *Node) dropFolderReporter(id int64) {
	n.mu.Lock()
	delete(n.folderReps, id)
	n.mu.Unlock()
}

// handleFolderEnd records how the sender's folder transfer ended.
func (n *Node) handleFolderEnd(w http.ResponseWriter, r *http.Request, from config.TrustedPeer) {
	var e proto.FolderEnd
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&e); err != nil || e.FolderID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if m, ok := n.findFolder(from.ID, e.FolderID); ok {
		n.updateFile(m.ID, func(f *FileInfo) {
			switch {
			case f.Status == StatusCanceled || f.Status == StatusDone:
				// Our own ending wins.
			case e.Status == StatusDone:
				f.Status, f.Error, f.DoneFiles, f.DoneBytes = StatusDone, "", f.Files, f.Size
			case e.Status == StatusCanceled:
				f.Status, f.Error = StatusCanceled, "canceled by the sender"
			default:
				f.Status, f.Error = StatusFailed, e.Error
			}
		})
		n.dropFolderReporter(m.ID)
	}
	n.completeIncomingApproval(from.ID, incomingMeta{folderID: e.FolderID})
	w.WriteHeader(http.StatusNoContent)
}
