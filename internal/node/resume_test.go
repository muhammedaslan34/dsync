package node

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dsync/internal/client"
	"dsync/internal/config"
)

// testNode is a node serving on a random local port.
type testNode struct {
	*Node
	addr string
	srv  *http.Server
}

func newTestNode(t *testing.T, name string) testNode {
	t.Helper()
	return newTestNodeWith(t, name, nil)
}

// newTestNodeWith is newTestNode with an event callback.
func newTestNodeWith(t *testing.T, name string, emit func(string, any)) testNode {
	t.Helper()
	dir := t.TempDir()
	cfg, err := config.LoadFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Name = name
	cfg.DownloadDir = filepath.Join(dir, "recv")
	n, err := New(cfg, emit)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: n.handler()}
	go srv.Serve(tls.NewListener(ln, n.tlsConfig()))
	t.Cleanup(func() { srv.Close() })
	return testNode{n, ln.Addr().String(), srv}
}

// pair makes a and b trust each other, with a reaching b at addr.
func pair(a, b testNode, addr string) {
	a.cfg.Trust(config.TrustedPeer{ID: b.cfg.ID, Name: b.cfg.Name, Fingerprint: b.id.Fingerprint})
	b.cfg.Trust(config.TrustedPeer{ID: a.cfg.ID, Name: a.cfg.Name, Fingerprint: a.id.Fingerprint})
	a.peers[b.cfg.ID] = &Peer{ID: b.cfg.ID, Name: b.cfg.Name, Addr: addr, Online: true}
}

// cuttingProxy forwards TCP to target, but closes the first connection
// once more than cutAfter bytes have gone from client to server.
type cuttingProxy struct {
	addr      string
	forwarded atomic.Int64 // client->server bytes, all connections
}

func newCuttingProxy(t *testing.T, target string, cutAfter int64) *cuttingProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	p := &cuttingProxy{addr: ln.Addr().String()}
	var first atomic.Bool
	first.Store(true)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s, err := net.Dial("tcp", target)
			if err != nil {
				c.Close()
				continue
			}
			limit := int64(-1)
			if first.Swap(false) {
				limit = cutAfter
			}
			go func() {
				defer c.Close()
				defer s.Close()
				io.Copy(c, s)
			}()
			go func() {
				defer c.Close()
				defer s.Close()
				buf := make([]byte, 32<<10)
				var sent int64
				for {
					n, err := c.Read(buf)
					if n > 0 {
						if _, werr := s.Write(buf[:n]); werr != nil {
							return
						}
						sent += int64(n)
						p.forwarded.Add(int64(n))
						if limit >= 0 && sent > limit {
							return // cut the connection mid-transfer
						}
					}
					if err != nil {
						return
					}
				}
			}()
		}
	}()
	return p
}

func writeRandomFile(t *testing.T, size int) (string, []byte) {
	t.Helper()
	data := make([]byte, size)
	rand.Read(data)
	path := filepath.Join(t.TempDir(), "data.bin")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path, data
}

// waitFile waits until message id's transfer is no longer active.
func waitFile(t *testing.T, n *Node, id int64) FileInfo {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range n.History() {
			if m.ID == id && m.File != nil && m.File.Status != StatusActive {
				return *m.File
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("transfer %d did not finish", id)
	return FileInfo{}
}

func lastID(n *Node) int64 {
	h := n.History()
	return h[len(h)-1].ID
}

func fastRetries(t *testing.T) {
	old := retryDelays
	retryDelays = []time.Duration{50 * time.Millisecond, 50 * time.Millisecond, 50 * time.Millisecond}
	t.Cleanup(func() { retryDelays = old })
}

func incomingFiles(n *Node) []Message {
	var out []Message
	for _, m := range n.History() {
		if m.Incoming && m.File != nil {
			out = append(out, m)
		}
	}
	return out
}

func partFiles(t *testing.T, n *Node) []string {
	t.Helper()
	entries, _ := os.ReadDir(n.ReceiveDir())
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), partSuffix) {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestSendResumesAfterConnectionDrop(t *testing.T) {
	fastRetries(t)
	a, b := newTestNode(t, "A"), newTestNode(t, "B")
	const size = 24 << 20
	proxy := newCuttingProxy(t, b.addr, 5<<20)
	pair(a, b, proxy.addr)
	path, data := writeRandomFile(t, size)

	if err := a.SendFiles(context.Background(), b.cfg.ID, []string{path}); err != nil {
		t.Fatal(err)
	}
	sent := waitFile(t, a.Node, lastID(a.Node))
	if sent.Status != StatusDone || sent.Error != "" {
		t.Fatalf("sender: status %q error %q", sent.Status, sent.Error)
	}

	got, err := os.ReadFile(filepath.Join(b.ReceiveDir(), "data.bin"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("received file differs (err %v)", err)
	}
	// The first connection was cut after 5 MiB. Restarting would send about
	// size+5 MiB in total; resuming sends only a little over size (what
	// was in flight when the connection dropped, plus TLS overhead).
	f := proxy.forwarded.Load()
	if f < 5<<20 {
		t.Fatalf("proxy forwarded only %d bytes; the connection was never cut", f)
	}
	if f > size+(2<<20) {
		t.Errorf("forwarded %d bytes for a %d byte file; transfer restarted instead of resuming", f, size)
	}
	t.Logf("forwarded %.1f MiB for a %.1f MiB file", float64(f)/(1<<20), float64(size)/(1<<20))
	if msgs := incomingFiles(b.Node); len(msgs) != 1 || msgs[0].File.Status != StatusDone {
		t.Errorf("receiver should show one finished file, got %+v", msgs)
	}
	if parts := partFiles(t, b.Node); len(parts) != 0 {
		t.Errorf("partial files left behind: %v", parts)
	}
}

func TestCorruptPartialIsDiscardedAndRetryWorks(t *testing.T) {
	a, b := newTestNode(t, "A"), newTestNode(t, "B")
	pair(a, b, b.addr)
	path, data := writeRandomFile(t, 3<<20)

	// Plant a partial download with the wrong bytes for this exact transfer.
	st, _ := os.Stat(path)
	tid := client.TransferID(a.cfg.ID, path, st.Size(), st.ModTime())
	os.MkdirAll(b.ReceiveDir(), 0o755)
	os.WriteFile(b.partPath(a.cfg.ID, tid), bytes.Repeat([]byte{0xAA}, 1<<20), 0o644)

	a.SendFiles(context.Background(), b.cfg.ID, []string{path})
	id := lastID(a.Node)
	if f := waitFile(t, a.Node, id); f.Status != StatusFailed || !strings.Contains(f.Error, "checksum") {
		t.Fatalf("want checksum failure, got %q %q", f.Status, f.Error)
	}
	if parts := partFiles(t, b.Node); len(parts) != 0 {
		t.Fatalf("corrupt partial not removed: %v", parts)
	}

	if err := a.RetryTransfer(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if f := waitFile(t, a.Node, id); f.Status != StatusDone {
		t.Fatalf("retry: status %q error %q", f.Status, f.Error)
	}
	got, _ := os.ReadFile(filepath.Join(b.ReceiveDir(), "data.bin"))
	if !bytes.Equal(got, data) {
		t.Fatal("received file differs after retry")
	}
}

func TestReceiverCancelDeletesPartial(t *testing.T) {
	a, b := newTestNode(t, "A"), newTestNode(t, "B")
	pair(a, b, b.addr)
	path, _ := writeRandomFile(t, 64<<20)

	// Cancel on the receiving side as soon as the transfer shows up there.
	var once sync.Once
	go func() {
		for {
			for _, m := range incomingFiles(b.Node) {
				if m.File.Status == StatusActive {
					once.Do(func() { b.CancelTransfer(m.ID) })
					return
				}
			}
			time.Sleep(time.Millisecond)
		}
	}()
	a.SendFiles(context.Background(), b.cfg.ID, []string{path})
	waitFile(t, a.Node, lastID(a.Node))

	msgs := incomingFiles(b.Node)
	if len(msgs) != 1 {
		t.Fatalf("want 1 incoming message, got %d", len(msgs))
	}
	if f := waitFile(t, b.Node, msgs[0].ID); f.Status != StatusCanceled {
		t.Fatalf("receiver status %q, want canceled", f.Status)
	}
	if parts := partFiles(t, b.Node); len(parts) != 0 {
		t.Errorf("partial kept after receiver canceled: %v", parts)
	}
}

// stallingReader serves data until stallAt, then blocks until release is
// closed, like a sender whose process or network died with the connection
// still open.
type stallingReader struct {
	*bytes.Reader
	stallAt int64
	release chan struct{}
}

func (s *stallingReader) Read(p []byte) (int, error) {
	pos, _ := s.Seek(0, io.SeekCurrent)
	if pos >= s.stallAt {
		<-s.release
		return 0, io.ErrUnexpectedEOF
	}
	if rem := s.stallAt - pos; int64(len(p)) > rem {
		p = p[:rem]
	}
	return s.Reader.Read(p)
}

func TestNewAttemptReplacesStalledOne(t *testing.T) {
	a, b := newTestNode(t, "A"), newTestNode(t, "B")
	pair(a, b, b.addr)
	path, data := writeRandomFile(t, 16<<20)
	st, _ := os.Stat(path)
	h := client.FileHeader{
		FromID: a.cfg.ID, FromName: "A", Name: "data.bin", Size: st.Size(),
		TransferID: client.TransferID(a.cfg.ID, path, st.Size(), st.ModTime()),
	}

	// First attempt: sends 6 MiB, then hangs with the connection open.
	release := make(chan struct{})
	defer close(release)
	stalled := &stallingReader{Reader: bytes.NewReader(data), stallAt: 6 << 20, release: release}
	go a.cl.SendFile(context.Background(), b.addr, b.id.Fingerprint, h, stalled, nil)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if st, err := os.Stat(b.partPath(a.cfg.ID, h.TransferID)); err == nil && st.Size() >= 6<<20 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first attempt never wrote its data")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Second attempt with the real file must take over and finish.
	f, _ := os.Open(path)
	defer f.Close()
	var firstDone int64 = -1
	start := time.Now()
	err := a.cl.SendFile(context.Background(), b.addr, b.id.Fingerprint, h, f, func(d int64) {
		if firstDone < 0 {
			firstDone = d
		}
	})
	if err != nil {
		t.Fatalf("second attempt: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("second attempt took %v; it waited on the stalled one", time.Since(start))
	}
	if firstDone < 6<<20 {
		t.Errorf("second attempt started at byte %d; want it to resume after the first 6 MiB", firstDone)
	}
	got, _ := os.ReadFile(filepath.Join(b.ReceiveDir(), "data.bin"))
	if !bytes.Equal(got, data) {
		t.Fatal("received file differs")
	}
	if msgs := incomingFiles(b.Node); len(msgs) != 1 || msgs[0].File.Status != StatusDone {
		t.Errorf("receiver should show one finished file, got %d messages", len(msgs))
	}
}
