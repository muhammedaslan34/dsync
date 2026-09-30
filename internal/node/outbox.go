package node

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The outbox holds data that has no file of its own, like a pasted image or
// a very long message, so it can be sent (and retried) like any file.
const outboxMaxAge = 14 * 24 * time.Hour

func (n *Node) outboxDir() string { return filepath.Join(n.cfg.Dir(), "outbox") }

// SendData saves data under name in the outbox and sends it as a file.
func (n *Node) SendData(ctx context.Context, peerID, name string, data []byte) error {
	if len(data) == 0 {
		return errors.New("nothing to send")
	}
	dir := n.outboxDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := uniquePath(dir, safeFileName(name))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return n.SendFiles(ctx, peerID, []string{path})
}

// cleanOutbox deletes old outbox files, which are only needed for retries.
func (n *Node) cleanOutbox() {
	dir := n.outboxDir()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > outboxMaxAge {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// imageExts are the file types the app shows as pictures.
var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true}

// ImagePath returns the local path of message id's file if it is an image
// that exists on disk, for showing it in the conversation. Only files that
// are part of the history can be looked up this way.
func (n *Node) ImagePath(id int64) (string, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, m := range n.history {
		if m.ID != id || m.File == nil || m.File.Path == "" {
			continue
		}
		if m.Incoming && m.File.Status != StatusDone {
			return "", false
		}
		if !imageExts[strings.ToLower(filepath.Ext(m.File.Path))] {
			return "", false
		}
		if st, err := os.Stat(m.File.Path); err != nil || !st.Mode().IsRegular() {
			return "", false
		}
		return m.File.Path, true
	}
	return "", false
}
