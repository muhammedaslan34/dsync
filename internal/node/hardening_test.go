package node

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dsync/internal/config"
	"dsync/internal/identity"
	"dsync/internal/proto"
)

func TestSafeFileNameStripsUnicodeFormatControls(t *testing.T) {
	for input, want := range map[string]string{
		"invoice\u202efdp.exe":  "invoicefdp.exe",
		"safe\u200bname.pdf":    "safename.pdf",
		"\u2066photo.jpg\u2069": "photo.jpg",
		"bad\u0001:name.txt":    "bad__name.txt",
	} {
		if got := safeFileName(input); got != want {
			t.Errorf("safeFileName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLearnPeerKeepsTrustedNameAndShowsFingerprint(t *testing.T) {
	n := newTestNode(t, "receiver")
	trusted := config.TrustedPeer{ID: "peer", Name: "My Laptop", Fingerprint: strings.Repeat("ab", 32)}
	if err := n.cfg.Trust(trusted); err != nil {
		t.Fatal(err)
	}
	if got := n.learnPeer(trusted, "Finance Server", 47101, "127.0.0.1:1234"); got != trusted.Name {
		t.Fatalf("learnPeer returned self-asserted name %q", got)
	}
	stored, _ := n.cfg.TrustedByID(trusted.ID)
	if stored.Name != trusted.Name {
		t.Fatalf("trusted name changed to %q", stored.Name)
	}
	n.addMessage(Message{PeerID: trusted.ID, PeerName: "Finance Server", Incoming: true, Text: "hello"})
	m := n.History()[0]
	if m.PeerName != trusted.Name || m.PeerFingerprint != identity.Short(trusted.Fingerprint) {
		t.Fatalf("history identity = %q %q", m.PeerName, m.PeerFingerprint)
	}
	peers := n.Peers()
	if len(peers) != 1 || peers[0].Name != trusted.Name || peers[0].Fingerprint != identity.Short(trusted.Fingerprint) {
		t.Fatalf("peer identity = %+v", peers)
	}
}

func requestWithCertificate(t *testing.T, sender testNode, body any) *http.Request {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/pair", bytes.NewReader(b))
	cert, err := x509.ParseCertificate(sender.id.Cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	r.RemoteAddr = "127.0.0.1:1234"
	return r
}

func TestPairRequestShowsFingerprintAndAlreadyPairedWarning(t *testing.T) {
	events := make(chan PairRequest, 1)
	receiver := newTestNodeWith(t, "receiver", func(event string, data any) {
		if event == EventPairRequest {
			events <- data.(PairRequest)
		}
	})
	sender := newTestNode(t, "sender")
	trusted := config.TrustedPeer{ID: "stable-id", Name: "Trusted name", Fingerprint: sender.id.Fingerprint}
	if err := receiver.cfg.Trust(trusted); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	httpReq := requestWithCertificate(t, sender, proto.PairRequest{FromID: trusted.ID, FromName: "Imposter", OS: "linux"})
	done := make(chan struct{})
	go func() {
		receiver.handlePair(w, httpReq)
		close(done)
	}()
	var req PairRequest
	select {
	case req = <-events:
	case <-time.After(time.Second):
		t.Fatal("pair request was not emitted")
	}
	if !req.AlreadyPaired || req.Name != trusted.Name || req.Fingerprint != identity.Short(sender.id.Fingerprint) || req.ExistingFingerprint != req.Fingerprint {
		t.Fatalf("pair request identity = %+v", req)
	}
	receiver.AnswerPair(req.ID, false)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("pair handler did not finish")
	}
}

func TestPairRejectsDifferentKeyForTrustedDeviceID(t *testing.T) {
	events := make(chan PairRequest, 1)
	receiver := newTestNodeWith(t, "receiver", func(event string, data any) {
		if event == EventPairRequest {
			events <- data.(PairRequest)
		}
	})
	oldKey, newKey := newTestNode(t, "old"), newTestNode(t, "new")
	if err := receiver.cfg.Trust(config.TrustedPeer{ID: "stable-id", Name: "Old", Fingerprint: oldKey.id.Fingerprint}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	httpReq := requestWithCertificate(t, newKey, proto.PairRequest{FromID: "stable-id", FromName: "New"})
	done := make(chan struct{})
	go func() {
		receiver.handlePair(w, httpReq)
		close(done)
	}()
	var req PairRequest
	select {
	case req = <-events:
	case <-time.After(time.Second):
		t.Fatal("key-change warning was not emitted")
	}
	if !req.KeyChanged || req.Name != "Old" || req.Fingerprint != identity.Short(newKey.id.Fingerprint) || req.ExistingFingerprint != identity.Short(oldKey.id.Fingerprint) {
		t.Fatalf("key-change request = %+v", req)
	}
	receiver.AnswerPair(req.ID, true)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("pair handler did not finish")
	}
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "unpair") {
		t.Fatalf("status %d body %q", w.Code, w.Body.String())
	}
	got, _ := receiver.cfg.TrustedByID("stable-id")
	if got.Fingerprint != oldKey.id.Fingerprint {
		t.Fatal("trusted key was replaced")
	}
}

func offsetRequest(t *testing.T, req proto.OffsetRequest) *http.Request {
	t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewRequest(http.MethodPost, "/api/v1/file/offset", bytes.NewReader(b))
}

func TestIncomingFileApprovalFlow(t *testing.T) {
	events := make(chan IncomingRequest, 2)
	n := newTestNodeWith(t, "receiver", func(event string, data any) {
		if event == EventIncomingRequest {
			events <- data.(IncomingRequest)
		}
	})
	if err := n.SetAskBeforeReceiving(true); err != nil {
		t.Fatal(err)
	}
	from := config.TrustedPeer{ID: "peer", Name: "Trusted laptop", Fingerprint: strings.Repeat("cd", 32)}
	req := proto.OffsetRequest{TransferID: strings.Repeat("ab", 16), Size: 4, Name: "invoice\u202efdp.exe"}
	w := httptest.NewRecorder()
	n.handleFileOffset(w, offsetRequest(t, req), from)
	if w.Code != http.StatusConflict {
		t.Fatalf("first status %d, want 409", w.Code)
	}
	prompt := <-events
	if prompt.PeerName != from.Name || prompt.Fingerprint != identity.Short(from.Fingerprint) || prompt.Name != "invoicefdp.exe" {
		t.Fatalf("approval prompt = %+v", prompt)
	}
	n.AnswerIncoming(prompt.ID, true)
	w = httptest.NewRecorder()
	n.handleFileOffset(w, offsetRequest(t, req), from)
	if w.Code != http.StatusOK {
		t.Fatalf("accepted status %d, want 200: %s", w.Code, w.Body.String())
	}
	changed := req
	changed.Name, changed.Size = "different.exe", 5
	w = httptest.NewRecorder()
	n.handleFileOffset(w, offsetRequest(t, changed), from)
	if w.Code != http.StatusConflict {
		t.Fatalf("changed metadata status %d, want rejection", w.Code)
	}
	select {
	case next := <-events:
		t.Fatalf("changed metadata opened a replacement prompt: %+v", next)
	default:
	}
}

func TestIncomingFileApprovalDeclineAndDirectUpload(t *testing.T) {
	events := make(chan IncomingRequest, 2)
	n := newTestNodeWith(t, "receiver", func(event string, data any) {
		if event == EventIncomingRequest {
			events <- data.(IncomingRequest)
		}
	})
	if err := n.SetAskBeforeReceiving(true); err != nil {
		t.Fatal(err)
	}
	from := config.TrustedPeer{ID: "peer", Name: "Trusted laptop", Fingerprint: strings.Repeat("ef", 32)}
	tid := strings.Repeat("ab", 16)
	w := httptest.NewRecorder()
	n.receiveFile(w, directFileRequest(4, tid, strings.NewReader("data")), from)
	if w.Code != http.StatusConflict {
		t.Fatalf("direct upload status %d, want 409", w.Code)
	}
	if _, err := os.Stat(n.peerPartPath(from.ID, tid)); !os.IsNotExist(err) {
		t.Fatalf("unapproved upload created a part: %v", err)
	}
	prompt := <-events
	n.AnswerIncoming(prompt.ID, false)
	w = httptest.NewRecorder()
	n.receiveFile(w, directFileRequest(4, tid, strings.NewReader("data")), from)
	if w.Code != http.StatusForbidden {
		t.Fatalf("declined status %d, want 403", w.Code)
	}
}

func TestIncomingApprovalClearsAfterReceiveError(t *testing.T) {
	n := newTestNode(t, "receiver")
	if err := n.SetAskBeforeReceiving(true); err != nil {
		t.Fatal(err)
	}
	from := config.TrustedPeer{ID: "peer", Name: "Laptop"}
	tid := strings.Repeat("ab", 16)
	w := httptest.NewRecorder()
	n.receiveFile(w, directFileRequest(2, tid, strings.NewReader("x")), from)
	p := n.PendingIncoming()
	n.AnswerIncoming(p[0].ID, true)
	w = httptest.NewRecorder()
	n.receiveFile(w, directFileRequest(2, tid, strings.NewReader("x")), from)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("short upload status = %d", w.Code)
	}
	if len(n.incomingDecisions) != 0 {
		t.Fatal("receive error left approval cached")
	}
	if _, err := os.Stat(partMetaPath(n.peerPartPath(from.ID, tid))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("receive error left part binding: %v", err)
	}
}

func TestIncomingApprovalDefaultsOffAndCoversWholeFolder(t *testing.T) {
	n := newTestNode(t, "receiver")
	from := config.TrustedPeer{ID: "peer", Name: "Trusted laptop", Fingerprint: strings.Repeat("aa", 32)}
	first := proto.OffsetRequest{
		TransferID: strings.Repeat("01", 16), Size: 1, Name: "a.txt",
		FolderID: "folder-id", RelPath: "Docs/a.txt", FolderSize: 2, FolderFiles: 2,
	}
	w := httptest.NewRecorder()
	n.handleFileOffset(w, offsetRequest(t, first), from)
	if w.Code != http.StatusOK {
		t.Fatalf("default status %d, want 200", w.Code)
	}
	if err := n.SetAskBeforeReceiving(true); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	n.handleFileOffset(w, offsetRequest(t, first), from)
	if w.Code != http.StatusConflict {
		t.Fatalf("folder prompt status %d, want 409", w.Code)
	}
	pending := n.PendingIncoming()
	if len(pending) != 1 || !pending[0].Folder || pending[0].Name != "Docs" || pending[0].Files != 2 || pending[0].Size != 2 {
		t.Fatalf("folder prompt = %+v", pending)
	}
	n.AnswerIncoming(pending[0].ID, true)
	second := first
	second.TransferID = strings.Repeat("02", 16)
	second.RelPath = "Docs/b.txt"
	w = httptest.NewRecorder()
	n.handleFileOffset(w, offsetRequest(t, second), from)
	if w.Code != http.StatusOK || len(n.PendingIncoming()) != 0 {
		t.Fatalf("second folder file status %d pending %+v", w.Code, n.PendingIncoming())
	}
}

func TestIncomingApprovalRemainsCompatibleWithOlderOffsetMetadata(t *testing.T) {
	n := newTestNode(t, "receiver")
	if err := n.SetAskBeforeReceiving(true); err != nil {
		t.Fatal(err)
	}
	from := config.TrustedPeer{ID: "peer", Name: "Laptop", Fingerprint: strings.Repeat("aa", 32)}
	legacy := incomingMeta{transferID: strings.Repeat("ab", 16), size: 4}
	w := httptest.NewRecorder()
	if n.authorizeIncoming(w, from, legacy) || w.Code != http.StatusConflict {
		t.Fatalf("legacy offset was not held for approval: %d", w.Code)
	}
	pending := n.PendingIncoming()
	n.AnswerIncoming(pending[0].ID, true)
	w = httptest.NewRecorder()
	actual := incomingMeta{transferID: legacy.transferID, name: "actual-name.txt", size: 4, sizeKnown: true}
	if n.authorizeIncoming(w, from, actual) || w.Code != http.StatusConflict {
		t.Fatalf("complete metadata silently used sparse approval: %d %s", w.Code, w.Body.String())
	}
	pending = n.PendingIncoming()
	if len(pending) != 1 || pending[0].Name != "actual-name.txt" {
		t.Fatalf("exact follow-up prompt = %+v", pending)
	}
	n.AnswerIncoming(pending[0].ID, true)
	w = httptest.NewRecorder()
	if !n.authorizeIncoming(w, from, legacy) {
		t.Fatalf("complete approval did not accept legacy offset: %d %s", w.Code, w.Body.String())
	}
}

func TestIncomingFolderApprovalBindsMembersAndTotals(t *testing.T) {
	n := newTestNode(t, "receiver")
	if err := n.SetAskBeforeReceiving(true); err != nil {
		t.Fatal(err)
	}
	from := config.TrustedPeer{ID: "peer", Name: "Laptop"}
	first := incomingMeta{
		transferID: strings.Repeat("01", 16), folderID: "folder", name: "Docs",
		size: 2, sizeKnown: true, files: 2, filesKnown: true,
		relPath: "Docs/a.txt", memberSize: 1,
	}
	w := httptest.NewRecorder()
	if n.authorizeIncoming(w, from, first) {
		t.Fatal("folder started without approval")
	}
	p := n.PendingIncoming()
	n.AnswerIncoming(p[0].ID, true)
	w = httptest.NewRecorder()
	if !n.authorizeIncoming(w, from, first) {
		t.Fatalf("first member rejected: %d %s", w.Code, w.Body.String())
	}

	second := first
	second.transferID, second.relPath = strings.Repeat("02", 16), "Docs/b.txt"
	w = httptest.NewRecorder()
	if !n.authorizeIncoming(w, from, second) {
		t.Fatalf("second member rejected: %d %s", w.Code, w.Body.String())
	}
	extra := first
	extra.transferID, extra.relPath = strings.Repeat("03", 16), "Docs/extra.txt"
	w = httptest.NewRecorder()
	if n.authorizeIncoming(w, from, extra) || w.Code != http.StatusForbidden {
		t.Fatalf("extra member status = %d, want 403", w.Code)
	}
}

func TestIncomingFolderApprovalRejectsChangedMemberMetadata(t *testing.T) {
	n := newTestNode(t, "receiver")
	if err := n.SetAskBeforeReceiving(true); err != nil {
		t.Fatal(err)
	}
	from := config.TrustedPeer{ID: "peer", Name: "Laptop"}
	meta := incomingMeta{
		transferID: strings.Repeat("01", 16), folderID: "folder", name: "Docs",
		size: 2, sizeKnown: true, files: 2, filesKnown: true,
		relPath: "Docs/a.txt", memberSize: 1,
	}
	w := httptest.NewRecorder()
	n.authorizeIncoming(w, from, meta)
	p := n.PendingIncoming()
	n.AnswerIncoming(p[0].ID, true)
	w = httptest.NewRecorder()
	if !n.authorizeIncoming(w, from, meta) {
		t.Fatal("approved member was rejected")
	}
	meta.relPath = "Docs/renamed.txt"
	w = httptest.NewRecorder()
	if n.authorizeIncoming(w, from, meta) || w.Code != http.StatusConflict {
		t.Fatalf("changed member metadata status = %d", w.Code)
	}
	if len(n.incomingDecisions) != 0 {
		t.Fatal("failed member left approval cached")
	}
}

func TestIncomingFolderSparseApprovalCannotAuthorizeFullMetadata(t *testing.T) {
	n := newTestNode(t, "receiver")
	if err := n.SetAskBeforeReceiving(true); err != nil {
		t.Fatal(err)
	}
	from := config.TrustedPeer{ID: "peer", Name: "Laptop"}
	sparse := incomingMeta{
		transferID: strings.Repeat("01", 16), folderID: "folder", name: "Docs",
		relPath: "Docs/a.txt", memberSize: 1, probe: true,
	}
	w := httptest.NewRecorder()
	n.authorizeIncoming(w, from, sparse)
	p := n.PendingIncoming()
	n.AnswerIncoming(p[0].ID, true)
	w = httptest.NewRecorder()
	if !n.authorizeIncoming(w, from, sparse) {
		t.Fatalf("legacy probe rejected after approval: %d", w.Code)
	}
	full := sparse
	full.probe, full.size, full.sizeKnown, full.files, full.filesKnown = false, 1, true, 1, true
	w = httptest.NewRecorder()
	if n.authorizeIncoming(w, from, full) || w.Code != http.StatusConflict {
		t.Fatalf("full metadata silently rode sparse approval: %d", w.Code)
	}
	if p := n.PendingIncoming(); len(p) != 1 || p[0].Size != 1 || p[0].Files != 1 {
		t.Fatalf("exact follow-up prompt = %+v", p)
	}
}

func TestIncomingFolderResumeDoesNotDoubleCountCompletedMember(t *testing.T) {
	n := newTestNode(t, "receiver")
	if err := n.SetAskBeforeReceiving(true); err != nil {
		t.Fatal(err)
	}
	from := config.TrustedPeer{ID: "peer", Name: "Laptop"}
	root := filepath.Join(n.ReceiveDir(), "Docs")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	n.addMessage(Message{PeerID: from.ID, Incoming: true, File: &FileInfo{
		Name: "Docs", Path: root, Status: StatusActive, Folder: true, FolderID: "folder",
		Size: 2, Files: 2, DoneBytes: 1, DoneFiles: 1,
	}})
	first := proto.OffsetRequest{
		TransferID: strings.Repeat("01", 16), Size: 1, Name: "a.txt",
		FolderID: "folder", RelPath: "Docs/a.txt", FolderSize: 2, FolderFiles: 2,
	}
	w := httptest.NewRecorder()
	n.handleFileOffset(w, offsetRequest(t, first), from)
	p := n.PendingIncoming()
	n.AnswerIncoming(p[0].ID, true)
	w = httptest.NewRecorder()
	n.handleFileOffset(w, offsetRequest(t, first), from)
	if w.Code != http.StatusOK {
		t.Fatalf("completed member probe = %d: %s", w.Code, w.Body.String())
	}
	second := first
	second.TransferID, second.Name, second.RelPath = strings.Repeat("02", 16), "b.txt", "Docs/b.txt"
	w = httptest.NewRecorder()
	n.handleFileOffset(w, offsetRequest(t, second), from)
	if w.Code != http.StatusOK {
		t.Fatalf("remaining member was double-counted: %d %s", w.Code, w.Body.String())
	}
}

func TestSendFileWaitsForIncomingApprovalThenCompletes(t *testing.T) {
	fastRetries(t)
	events := make(chan IncomingRequest, 1)
	a := newTestNode(t, "sender")
	b := newTestNodeWith(t, "receiver", func(event string, data any) {
		if event == EventIncomingRequest {
			events <- data.(IncomingRequest)
		}
	})
	pair(a, b, b.addr)
	if err := b.SetAskBeforeReceiving(true); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "approved.txt")
	if err := os.WriteFile(path, []byte("approved"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.SendFiles(context.Background(), b.cfg.ID, []string{path}); err != nil {
		t.Fatal(err)
	}
	var prompt IncomingRequest
	select {
	case prompt = <-events:
	case <-time.After(time.Second):
		t.Fatal("receiver did not ask for approval")
	}
	if prompt.Name != "approved.txt" || prompt.PeerName != a.cfg.Name {
		t.Fatalf("prompt = %+v", prompt)
	}
	b.AnswerIncoming(prompt.ID, true)
	if f := waitFile(t, a.Node, lastID(a.Node)); f.Status != StatusDone {
		t.Fatalf("sender status %q: %s", f.Status, f.Error)
	}
	if got, err := os.ReadFile(filepath.Join(b.ReceiveDir(), "approved.txt")); err != nil || string(got) != "approved" {
		t.Fatalf("received %q, err %v", got, err)
	}
	if len(b.incomingDecisions) != 0 {
		t.Fatal("completed transfer left approval cached")
	}
}
