package node

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dsync/internal/client"
)

func TestLongTextIsSentAsFile(t *testing.T) {
	a, b := newTestNode(t, "A"), newTestNode(t, "B")
	pair(a, b, b.addr)
	text := strings.Repeat("log line 0123456789\n", 200_000) // ~4 MB, over the message limit

	if err := a.SendText(context.Background(), b.cfg.ID, text); err != nil {
		t.Fatal(err)
	}
	f := waitFile(t, a.Node, lastID(a.Node))
	if f.Status != StatusDone || !strings.HasSuffix(f.Name, ".txt") {
		t.Fatalf("want a finished .txt transfer, got %q %q", f.Name, f.Status)
	}
	got, err := os.ReadFile(filepath.Join(b.ReceiveDir(), f.Name))
	if err != nil || string(got) != text {
		t.Fatalf("received text differs (err %v)", err)
	}
}

func TestPastedImageIsSentAndPreviewable(t *testing.T) {
	a, b := newTestNode(t, "A"), newTestNode(t, "B")
	pair(a, b, b.addr)
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 50_000)...)

	if err := a.SendData(context.Background(), b.cfg.ID, "Pasted image.png", png); err != nil {
		t.Fatal(err)
	}
	sentID := lastID(a.Node)
	if f := waitFile(t, a.Node, sentID); f.Status != StatusDone {
		t.Fatalf("send: %q %q", f.Status, f.Error)
	}
	recv := incomingFiles(b.Node)
	if len(recv) != 1 {
		t.Fatalf("want 1 received file, got %d", len(recv))
	}
	got, _ := os.ReadFile(recv[0].File.Path)
	if !bytes.Equal(got, png) {
		t.Fatal("received image differs")
	}

	// Both sides can show it.
	if p, ok := a.ImagePath(sentID); !ok || !strings.HasPrefix(p, a.outboxDir()) {
		t.Errorf("sender preview: %q %v", p, ok)
	}
	if _, ok := b.ImagePath(recv[0].ID); !ok {
		t.Error("receiver preview not available")
	}
}

func TestImagePathOnlyServesFinishedImagesFromHistory(t *testing.T) {
	a, b := newTestNode(t, "A"), newTestNode(t, "B")
	pair(a, b, b.addr)
	dir := t.TempDir()
	txt := filepath.Join(dir, "notes.txt")
	os.WriteFile(txt, []byte("hi"), 0o644)

	a.SendFiles(context.Background(), b.cfg.ID, []string{txt})
	id := lastID(a.Node)
	waitFile(t, a.Node, id)
	if _, ok := a.ImagePath(id); ok {
		t.Error("served a non-image file")
	}
	if _, ok := a.ImagePath(99999); ok {
		t.Error("served an unknown message id")
	}

	// An incoming image that hasn't finished must not be served.
	m := b.addMessage(Message{PeerID: a.cfg.ID, Incoming: true, File: &FileInfo{Name: "x.png", Path: txt, Status: StatusActive}})
	if _, ok := b.ImagePath(m.ID); ok {
		t.Error("served an unfinished download")
	}
}

func TestNotEnoughDiskSpaceIsRefusedUpFront(t *testing.T) {
	a, b := newTestNode(t, "A"), newTestNode(t, "B")
	pair(a, b, b.addr)
	if free, err := freeSpace(t.TempDir()); err != nil || free <= 0 {
		t.Fatalf("freeSpace: %d %v", free, err)
	}

	// Claim an exabyte-sized file; the receiver must refuse before any data.
	h := client.FileHeader{FromID: a.cfg.ID, Name: "huge.bin", Size: 1 << 60, TransferID: strings.Repeat("ab", 16)}
	err := a.cl.SendFile(context.Background(), b.addr, b.id.Fingerprint, h, bytes.NewReader(nil), nil)
	if err == nil || !strings.Contains(err.Error(), "not enough disk space") {
		t.Fatalf("want a disk space error, got %v", err)
	}
	if client.Retryable(err) {
		t.Error("a full disk should not be retried")
	}
	if msgs := incomingFiles(b.Node); len(msgs) != 0 {
		t.Errorf("receiver recorded %d transfers for a refused file", len(msgs))
	}
}

func TestClearHistory(t *testing.T) {
	a, b := newTestNode(t, "A"), newTestNode(t, "B")
	c := newTestNode(t, "C")
	pair(a, b, b.addr)
	pair(a, c, c.addr)
	a.SendText(context.Background(), b.cfg.ID, "to B")
	a.SendText(context.Background(), c.cfg.ID, "to C")
	a.SendData(context.Background(), b.cfg.ID, "pasted.png", []byte("\x89PNG pasted"))
	waitFile(t, a.Node, lastID(a.Node))
	var pasted string
	for _, m := range a.History() {
		if m.File != nil {
			pasted = m.File.Path
		}
	}
	// A transfer still running must survive.
	running := a.addMessage(Message{PeerID: b.cfg.ID, File: &FileInfo{Name: "big.iso", Status: StatusActive}})

	if err := a.ClearHistory(b.cfg.ID); err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, m := range a.History() {
		if m.File != nil {
			left = append(left, m.File.Name)
		} else {
			left = append(left, m.Text)
		}
	}
	if len(left) != 2 || left[0] != "to C" || left[1] != "big.iso" {
		t.Fatalf("after clearing B: %v", left)
	}
	if _, err := os.Stat(pasted); err == nil {
		t.Error("pasted data kept only for resending should be deleted")
	}

	// It stays cleared after a restart.
	again, err := New(a.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h := again.History(); len(h) != 2 {
		t.Fatalf("reloaded history has %d messages", len(h))
	}

	// Clearing everything keeps only the running transfer.
	a.ClearHistory("")
	if h := a.History(); len(h) != 1 || h[0].ID != running.ID {
		t.Fatalf("after clearing all: %+v", h)
	}
}
