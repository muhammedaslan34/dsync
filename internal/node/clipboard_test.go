package node

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"dsync/internal/clip"
)

// fakeBoard is a clipboard for tests. Copy simulates the user copying;
// writes from sync are recorded and, like a real clipboard, reported to
// the watcher as a change (optionally re-encoded, as Windows does with
// images).
type fakeBoard struct {
	mu       sync.Mutex
	changes  chan clip.Content
	written  []clip.Content
	reencode bool
}

func newFakeBoard() *fakeBoard { return &fakeBoard{changes: make(chan clip.Content, 16)} }

func (f *fakeBoard) Watch(ctx context.Context) <-chan clip.Content { return f.changes }

func (f *fakeBoard) Write(ctx context.Context, c clip.Content) error {
	f.mu.Lock()
	f.written = append(f.written, c)
	f.mu.Unlock()
	if f.reencode && c.Kind == clip.Image {
		c.Data = append([]byte("re-encoded:"), c.Data...)
	}
	f.changes <- c
	return nil
}

func (f *fakeBoard) Copy(c clip.Content) { f.changes <- c }

func (f *fakeBoard) Written() []clip.Content {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]clip.Content(nil), f.written...)
}

// clipPair sets up two paired nodes with fake clipboards and sync as given.
func clipPair(t *testing.T, aOn, bOn bool) (testNode, testNode, *fakeBoard, *fakeBoard) {
	t.Helper()
	a, b := newTestNode(t, "A"), newTestNode(t, "B")
	pair(a, b, b.addr)
	pair(b, a, a.addr)
	ab, bb := newFakeBoard(), newFakeBoard()
	a.SetClipboard(ab, nil)
	b.SetClipboard(bb, nil)
	a.cfg.ClipboardSync, b.cfg.ClipboardSync = aOn, bOn
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go a.clipLoop(ctx)
	go b.clipLoop(ctx)
	return a, b, ab, bb
}

func waitWritten(t *testing.T, f *fakeBoard, n int) []clip.Content {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if w := f.Written(); len(w) >= n {
			return w
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("clipboard got %d writes, want %d", len(f.Written()), n)
	return nil
}

func TestClipboardTextAndImageSync(t *testing.T) {
	_, _, ab, bb := clipPair(t, true, true)

	ab.Copy(clip.Content{Kind: clip.Text, Data: []byte("sudo pacman -Syu")})
	w := waitWritten(t, bb, 1)
	if w[0].Kind != clip.Text || string(w[0].Data) != "sudo pacman -Syu" {
		t.Fatalf("B got %+v", w[0])
	}

	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{9}, 100_000)...)
	bb.Copy(clip.Content{Kind: clip.Image, Data: png})
	w = waitWritten(t, ab, 1)
	if w[0].Kind != clip.Image || !bytes.Equal(w[0].Data, png) {
		t.Fatal("A did not get the image")
	}
}

func TestClipboardDoesNotEcho(t *testing.T) {
	for _, reencode := range []bool{false, true} {
		_, _, ab, bb := clipPair(t, true, true)
		bb.reencode = reencode // Windows re-encodes images it is given

		ab.Copy(clip.Content{Kind: clip.Image, Data: []byte("\x89PNG-original")})
		waitWritten(t, bb, 1)
		ab.Copy(clip.Content{Kind: clip.Text, Data: []byte("hello")})
		waitWritten(t, bb, 2)

		time.Sleep(300 * time.Millisecond)
		if w := ab.Written(); len(w) != 0 {
			t.Errorf("reencode=%v: A's own copies came back to it: %d writes", reencode, len(w))
		}
		if w := bb.Written(); len(w) != 2 {
			t.Errorf("reencode=%v: B got %d writes, want exactly 2 (a loop?)", reencode, len(w))
		}
	}
}

func TestClipboardNewCopyRightAfterReceiving(t *testing.T) {
	_, _, ab, bb := clipPair(t, true, true)
	ab.Copy(clip.Content{Kind: clip.Text, Data: []byte("from A")})
	waitWritten(t, bb, 1)
	// The user on B copies something else straight away.
	bb.Copy(clip.Content{Kind: clip.Text, Data: []byte("from B")})
	w := waitWritten(t, ab, 1)
	if string(w[0].Data) != "from B" {
		t.Fatalf("A got %q", w[0].Data)
	}
}

func TestClipboardSkipsPasswordsAndRespectsOff(t *testing.T) {
	// Password manager content is never sent.
	_, _, ab, bb := clipPair(t, true, true)
	ab.Copy(clip.Content{Kind: clip.Text, Data: []byte("hunter2"), Sensitive: true})
	// Receiver with sync off keeps its clipboard.
	_, _, cb, db := clipPair(t, true, false)
	cb.Copy(clip.Content{Kind: clip.Text, Data: []byte("not for D")})
	// Sender with sync off sends nothing.
	_, _, eb, fb := clipPair(t, false, true)
	eb.Copy(clip.Content{Kind: clip.Text, Data: []byte("not from E")})

	time.Sleep(400 * time.Millisecond)
	for name, f := range map[string]*fakeBoard{"password": bb, "receiver off": db, "sender off": fb} {
		if w := f.Written(); len(w) != 0 {
			t.Errorf("%s: clipboard was written: %q", name, w[0].Data)
		}
	}
}
