package node

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dsync/internal/client"
	"dsync/internal/config"
	"dsync/internal/proto"
)

// testNode is a node serving on a random local port.
type testNode struct {
	*Node
	addr string
	srv  *http.Server
}

type testCredentialStore struct {
	mu      sync.Mutex
	secrets map[string]string
}

func (s *testCredentialStore) key(service, account string) string { return service + "\x00" + account }
func (s *testCredentialStore) Set(service, account, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets[s.key(service, account)] = secret
	return nil
}
func (s *testCredentialStore) Get(service, account string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	secret, ok := s.secrets[s.key(service, account)]
	if !ok {
		return "", config.ErrCredentialNotFound
	}
	return secret, nil
}
func (s *testCredentialStore) Delete(service, account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.key(service, account)
	if _, ok := s.secrets[key]; !ok {
		return config.ErrCredentialNotFound
	}
	delete(s.secrets, key)
	return nil
}

func newTestNode(t *testing.T, name string) testNode {
	t.Helper()
	return newTestNodeWith(t, name, nil)
}

// newTestNodeWith is newTestNode with an event callback.
func newTestNodeWith(t *testing.T, name string, emit func(string, any)) testNode {
	t.Helper()
	dir := t.TempDir()
	cfg, err := config.LoadFromWithCredentialStore(dir, &testCredentialStore{secrets: map[string]string{}})
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

func directFileRequest(size int64, tid string, body io.Reader) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/file", body)
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set(proto.HeaderFromName, "sender")
	r.Header.Set(proto.HeaderFromPort, "1234")
	r.Header.Set(proto.HeaderFileName, "data.bin")
	r.Header.Set(proto.HeaderFileSize, strconv.FormatInt(size, 10))
	r.Header.Set(proto.HeaderTransferID, tid)
	r.Header.Set(proto.HeaderOffset, "0")
	return r
}

func TestWriteIncomingRejectsOversizeBeforeWritingExtraByte(t *testing.T) {
	part := filepath.Join(t.TempDir(), "part")
	f, err := os.OpenFile(part, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("abcde"))
	_, err = writeIncoming(context.Background(), r, http.NewResponseController(httptest.NewRecorder()), f, 0, 4,
		func() (string, error) { t.Fatal("oversized file reached destination"); return "", nil }, func(int64) {})
	if !errors.Is(err, errByteCount) {
		t.Fatalf("want byte-count error, got %v", err)
	}
	if st, statErr := os.Stat(part); statErr != nil || st.Size() != 4 {
		t.Fatalf("part size = %v (err %v), want hard cap of 4", st, statErr)
	}
}

func TestReceiveFileDeletesByteCountFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int64
		body string
	}{
		{name: "short", size: 5, body: "abcd"},
		{name: "oversize", size: 3, body: "abcd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := newTestNode(t, "receiver")
			tid := strings.Repeat("ab", 16)
			w := httptest.NewRecorder()
			n.receiveFile(w, directFileRequest(tc.size, tid, strings.NewReader(tc.body)), config.TrustedPeer{ID: "peer", Name: "sender"})
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400; body %q", w.Code, w.Body.String())
			}
			if _, err := os.Stat(n.peerPartPath("peer", tid)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unusable part remains: %v", err)
			}
		})
	}
}

func TestReceiveFileChecksDiskSpaceWithoutOffsetRequest(t *testing.T) {
	n := newTestNode(t, "receiver")
	n.freeSpace = func(string) (int64, error) { return diskReserve + 3, nil }
	tid := strings.Repeat("cd", 16)
	w := httptest.NewRecorder()
	n.receiveFile(w, directFileRequest(4, tid, strings.NewReader("data")), config.TrustedPeer{ID: "peer", Name: "sender"})
	if w.Code != http.StatusInsufficientStorage {
		t.Fatalf("status %d, want 507; body %q", w.Code, w.Body.String())
	}
	if _, err := os.Stat(n.peerPartPath("peer", tid)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disk-space refusal created a part: %v", err)
	}
}

func TestReceiveFileCapsAccumulatedPeerParts(t *testing.T) {
	n := newTestNode(t, "receiver")
	if err := os.MkdirAll(n.ReceiveDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxPeerPartialFiles; i++ {
		tid := fmt.Sprintf("%032x", i+1)
		if err := os.WriteFile(n.peerPartPath("peer", tid), []byte{0}, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tid := strings.Repeat("ef", 16)
	w := httptest.NewRecorder()
	n.receiveFile(w, directFileRequest(1, tid, strings.NewReader("x")), config.TrustedPeer{ID: "peer", Name: "sender"})
	if w.Code != http.StatusInsufficientStorage {
		t.Fatalf("status %d, want 507; body %q", w.Code, w.Body.String())
	}
	if _, err := os.Stat(n.peerPartPath("peer", tid)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("peer cap created another part: %v", err)
	}
}

func TestReceiveFileRejectsTransferLargerThanPeerQuota(t *testing.T) {
	n := newTestNode(t, "receiver")
	n.freeSpace = func(string) (int64, error) { return maxPeerPartialBytes + diskReserve + 1, nil }
	tid := strings.Repeat("fa", 16)
	w := httptest.NewRecorder()
	n.receiveFile(w, directFileRequest(maxPeerPartialBytes+1, tid, strings.NewReader("x")), config.TrustedPeer{ID: "peer", Name: "sender"})
	if w.Code != http.StatusInsufficientStorage {
		t.Fatalf("status %d, want 507; body %q", w.Code, w.Body.String())
	}
	if _, err := os.Stat(n.peerPartPath("peer", tid)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("oversized transfer created a part: %v", err)
	}
}

func TestPeerPartDeclaredSizeCannotGrow(t *testing.T) {
	n := newTestNode(t, "receiver")
	if err := os.MkdirAll(n.ReceiveDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	tid := strings.Repeat("bc", 16)
	part := n.peerPartPath("peer", tid)
	if err := os.WriteFile(part, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writePartMeta(part, 4); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	n.receiveFile(w, directFileRequest(5, tid, strings.NewReader("12345")), config.TrustedPeer{ID: "peer", Name: "sender"})
	if w.Code != http.StatusConflict {
		t.Fatalf("grown declaration status %d, want 409: %s", w.Code, w.Body.String())
	}
	if st, err := os.Stat(part); err != nil || st.Size() != 1 {
		t.Fatalf("bound part changed: %v, err %v", st, err)
	}

	// The binding is persisted, so a new Node instance enforces it too.
	restarted, err := New(n.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if release, err := restarted.reservePeerPart("peer", part, 5, 1); !errors.Is(err, errPartConflict) {
		if release != nil {
			release()
		}
		t.Fatalf("restart accepted larger declaration: %v", err)
	}
}

func TestPeerPartQuotaReservationsAreAtomic(t *testing.T) {
	n := newTestNode(t, "receiver")
	n.freeSpace = func(string) (int64, error) { return 8 << 30, nil }
	if err := os.MkdirAll(n.ReceiveDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	const attempts = 4
	start := make(chan struct{})
	releases := make(chan func(), attempts)
	results := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		i := i
		go func() {
			<-start
			part := n.peerPartPath("peer", fmt.Sprintf("%032x", i+100))
			release, err := n.reservePeerPart("peer", part, maxPeerPartialBytes/2, 0)
			if release != nil {
				releases <- release
			}
			results <- err
		}()
	}
	close(start)
	accepted := 0
	for i := 0; i < attempts; i++ {
		if err := <-results; err == nil {
			accepted++
		} else if !errors.Is(err, errPartQuota) {
			t.Fatalf("unexpected reservation error: %v", err)
		}
	}
	close(releases)
	for release := range releases {
		release()
	}
	if accepted != 2 {
		t.Fatalf("accepted %d half-quota transfers, want exactly 2", accepted)
	}
}

func TestDiskSpaceReservationsAreAtomicAcrossPeers(t *testing.T) {
	n := newTestNode(t, "receiver")
	n.freeSpace = func(string) (int64, error) { return diskReserve + 100, nil }
	if err := os.MkdirAll(n.ReceiveDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	release, err := n.reservePeerPart("peer-a", n.peerPartPath("peer-a", strings.Repeat("01", 16)), 80, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	release2, err := n.reservePeerPart("peer-b", n.peerPartPath("peer-b", strings.Repeat("02", 16)), 80, 0)
	if release2 != nil {
		release2()
	}
	if !errors.Is(err, errPartQuota) {
		t.Fatalf("second disk reservation error = %v, want quota refusal", err)
	}
}

func TestCleanPartsLoopRemovesPartsThatBecomeStale(t *testing.T) {
	n := newTestNode(t, "receiver")
	if err := os.MkdirAll(n.ReceiveDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	part := filepath.Join(n.ReceiveDir(), partPrefix+"stale"+partSuffix)
	if err := os.WriteFile(part, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go n.cleanPartsLoop(ctx, 10*time.Millisecond)
	// It is initially fresh, so the loop's startup pass leaves it alone.
	time.Sleep(20 * time.Millisecond)
	old := time.Now().Add(-partMaxAge - time.Hour)
	if err := os.Chtimes(part, old, old); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(part); errors.Is(err, os.ErrNotExist) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("periodic cleanup did not remove stale part")
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
		if st, err := os.Stat(b.peerPartPath(a.cfg.ID, h.TransferID)); err == nil && st.Size() >= 6<<20 {
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
