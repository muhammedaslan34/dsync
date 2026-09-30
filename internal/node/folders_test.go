package node

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// makeTree creates files (relative path -> size) under a new folder named
// name and returns the folder's path.
func makeTree(t *testing.T, name string, files map[string]int) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	for rel, size := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		data := make([]byte, size)
		rand.Read(data)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// treeFiles lists the files under root as relative path -> contents.
func treeFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			rel, _ := filepath.Rel(root, p)
			out[filepath.ToSlash(rel)], _ = os.ReadFile(p)
		}
		return nil
	})
	return out
}

func sameTree(t *testing.T, want, got string) {
	t.Helper()
	w, g := treeFiles(t, want), treeFiles(t, got)
	var wk, gk []string
	for k := range w {
		wk = append(wk, k)
	}
	for k := range g {
		gk = append(gk, k)
	}
	sort.Strings(wk)
	sort.Strings(gk)
	if strings.Join(wk, "\n") != strings.Join(gk, "\n") {
		t.Fatalf("file lists differ\nwant: %v\ngot:  %v", wk, gk)
	}
	for k := range w {
		if !bytes.Equal(w[k], g[k]) {
			t.Errorf("%s differs", k)
		}
	}
}

func incomingFolders(n *Node) []Message {
	var out []Message
	for _, m := range incomingFiles(n) {
		if m.File.Folder {
			out = append(out, m)
		}
	}
	return out
}

// waitFolderSettled waits for the receiver's copy of a folder to stop being
// active (the sender's end notice arrives just after it finishes).
func waitFolderSettled(t *testing.T, n *Node) Message {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if fs := incomingFolders(n); len(fs) > 0 && fs[len(fs)-1].File.Status != StatusActive {
			return fs[len(fs)-1]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("receiver's folder never settled")
	return Message{}
}

func TestSendFolderRecreatesTree(t *testing.T) {
	a, b := newTestNode(t, "A"), newTestNode(t, "B")
	pair(a, b, b.addr)
	root := makeTree(t, "Photos", map[string]int{
		"a.jpg":                 100_000,
		"2024/summer/b.jpg":     250_000,
		"2024/summer/c.txt":     0,
		"2024/winter/d.png":     3 << 20,
		"docs/notes (draft).md": 1234,
		"docs/résumé.pdf":       5000,
	})
	os.MkdirAll(filepath.Join(root, "empty"), 0o755)
	if runtime.GOOS != "windows" {
		os.Symlink("/etc/passwd", filepath.Join(root, "link"))
	}

	if err := a.SendFiles(context.Background(), b.cfg.ID, []string{root}); err != nil {
		t.Fatal(err)
	}
	sent := waitFile(t, a.Node, lastID(a.Node))
	if sent.Status != StatusDone || !sent.Folder || sent.Files != 6 || sent.DoneFiles != 6 {
		t.Fatalf("sender: %+v", sent)
	}
	got := waitFolderSettled(t, b.Node)
	if got.File.Status != StatusDone || got.File.Files != 6 || got.File.DoneBytes != got.File.Size {
		t.Fatalf("receiver: %+v", *got.File)
	}
	if got.File.Path != filepath.Join(b.ReceiveDir(), "Photos") {
		t.Errorf("received into %s", got.File.Path)
	}
	sameTree(t, root, got.File.Path) // the symlink is not followed or copied
	if len(incomingFiles(b.Node)) != 1 {
		t.Errorf("receiver shows %d messages, want 1 for the folder", len(incomingFiles(b.Node)))
	}
	if parts := partFiles(t, b.Node); len(parts) != 0 {
		t.Errorf("partial files left: %v", parts)
	}
}

func TestFolderRetrySkipsReceivedFiles(t *testing.T) {
	old := retryDelays
	retryDelays = nil // fail on the first cut, so we can retry by hand
	t.Cleanup(func() { retryDelays = old })

	a, b := newTestNode(t, "A"), newTestNode(t, "B")
	files := map[string]int{}
	for _, n := range []string{"1", "2", "3", "4", "5"} {
		files["f"+n+".bin"] = 2 << 20
	}
	root := makeTree(t, "Data", files)
	const total = 10 << 20
	proxy := newCuttingProxy(t, b.addr, 5<<20) // dies inside the third file
	pair(a, b, proxy.addr)

	a.SendFiles(context.Background(), b.cfg.ID, []string{root})
	id := lastID(a.Node)
	if f := waitFile(t, a.Node, id); f.Status != StatusFailed || f.DoneFiles < 1 || f.DoneFiles >= 5 {
		t.Fatalf("first attempt: want a partial failure, got %+v", f)
	}
	if err := a.RetryTransfer(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if f := waitFile(t, a.Node, id); f.Status != StatusDone || f.DoneFiles != 5 {
		t.Fatalf("retry: %+v", f)
	}

	got := waitFolderSettled(t, b.Node)
	sameTree(t, root, got.File.Path) // also proves no "f1 (2).bin" duplicates
	if len(incomingFolders(b.Node)) != 1 {
		t.Errorf("retry created a second folder on the receiver")
	}
	if fwd := proxy.forwarded.Load(); fwd > total+(2<<20) {
		t.Errorf("forwarded %d bytes for %d; received files were sent again", fwd, total)
	}
}

func TestSendingFolderAgainMakesSecondCopy(t *testing.T) {
	a, b := newTestNode(t, "A"), newTestNode(t, "B")
	pair(a, b, b.addr)
	root := makeTree(t, "F", map[string]int{"x.txt": 10, "sub/y.txt": 20})

	for range 2 {
		a.SendFiles(context.Background(), b.cfg.ID, []string{root})
		if f := waitFile(t, a.Node, lastID(a.Node)); f.Status != StatusDone {
			t.Fatalf("send: %+v", f)
		}
		waitFolderSettled(t, b.Node)
	}
	sameTree(t, root, filepath.Join(b.ReceiveDir(), "F"))
	sameTree(t, root, filepath.Join(b.ReceiveDir(), "F (2)"))
}

func TestReceiverCancelStopsFolderButRetryWorks(t *testing.T) {
	a, b := newTestNode(t, "A"), newTestNode(t, "B")
	pair(a, b, b.addr)
	files := map[string]int{}
	for i := range 20 {
		files[string(rune('a'+i))+".bin"] = 4 << 20
	}
	root := makeTree(t, "Big", files)

	// Cancel on the receiver once a couple of files have arrived.
	go func() {
		for {
			for _, m := range incomingFolders(b.Node) {
				if m.File.Status == StatusActive && m.File.DoneFiles >= 2 {
					b.CancelTransfer(m.ID)
					return
				}
			}
			time.Sleep(time.Millisecond)
		}
	}()
	a.SendFiles(context.Background(), b.cfg.ID, []string{root})
	id := lastID(a.Node)
	f := waitFile(t, a.Node, id)
	if f.Status != StatusFailed || !strings.Contains(f.Error, "canceled on the receiving device") {
		t.Fatalf("sender after receiver cancel: %q %q", f.Status, f.Error)
	}
	if f.DoneFiles >= 20 {
		t.Fatal("sender kept going after the receiver canceled")
	}
	if got := waitFolderSettled(t, b.Node); got.File.Status != StatusCanceled {
		t.Fatalf("receiver status %q, want canceled", got.File.Status)
	}

	// The sender can still decide to send it after all.
	if err := a.RetryTransfer(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if f := waitFile(t, a.Node, id); f.Status != StatusDone {
		t.Fatalf("retry after cancel: %+v", f)
	}
	got := waitFolderSettled(t, b.Node)
	sameTree(t, root, got.File.Path)
}

func TestFolderDestStaysInsideRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "F")
	for _, rel := range []string{
		"F/../../etc/passwd",
		"F/a/../../../x",
		`F/..\..\x`,
		"F//etc/passwd",
		"F/./../x",
		"../x",
	} {
		p, err := folderDest(root, rel)
		if err != nil {
			continue // refusing is fine too
		}
		if !strings.HasPrefix(p, root+string(filepath.Separator)) {
			t.Errorf("%q escaped to %q", rel, p)
		}
	}
	for _, rel := range []string{"F", "F/", "F/."} {
		if _, err := folderDest(root, rel); err == nil {
			t.Errorf("%q should be refused (no file name)", rel)
		}
	}
}

func TestEmptyFolderFails(t *testing.T) {
	a, b := newTestNode(t, "A"), newTestNode(t, "B")
	pair(a, b, b.addr)
	root := filepath.Join(t.TempDir(), "Nothing")
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	a.SendFiles(context.Background(), b.cfg.ID, []string{root})
	if f := waitFile(t, a.Node, lastID(a.Node)); f.Status != StatusFailed || !strings.Contains(f.Error, "no files") {
		t.Fatalf("want a clear failure, got %q %q", f.Status, f.Error)
	}
}

// Canceling an incoming folder between two files (no file in flight) must
// not block a later retry from the sender.
func TestReceiverCancelBetweenFilesThenRetry(t *testing.T) {
	a, b := newTestNode(t, "A"), newTestNode(t, "B")
	pair(a, b, b.addr)
	root := makeTree(t, "Two", map[string]int{"1.bin": 1000, "2.bin": 1000})

	a.SendFiles(context.Background(), b.cfg.ID, []string{root})
	id := lastID(a.Node)
	waitFile(t, a.Node, id)
	got := waitFolderSettled(t, b.Node)

	// Pretend the folder was still in progress and the user canceled it
	// while no file was being received, then the sender retries.
	b.updateFile(got.ID, func(f *FileInfo) { f.Status, f.DoneFiles, f.DoneBytes = StatusActive, 1, 1000 })
	os.Remove(filepath.Join(got.File.Path, "2.bin"))
	b.CancelTransfer(got.ID)
	a.updateFile(id, func(f *FileInfo) { f.Status = StatusFailed })

	if err := a.RetryTransfer(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if f := waitFile(t, a.Node, id); f.Status != StatusDone {
		t.Fatalf("retry after a between-files cancel: %q %q", f.Status, f.Error)
	}
	sameTree(t, root, got.File.Path)
}
