package node

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dsync/internal/phone"
)

// fakePhone speaks docs/phone-protocol.md like the phone app does.
type fakePhone struct {
	t       *testing.T
	base    string
	key     *[phone.KeySize]byte
	phoneID string
}

func (p *fakePhone) call(path string, headerName, headerVal string, body, out any) (int, string) {
	p.t.Helper()
	sealed, err := phone.SealJSON(p.key, body)
	if err != nil {
		p.t.Fatal(err)
	}
	return p.raw(path, headerName, headerVal, sealed, out)
}

func (p *fakePhone) raw(path, headerName, headerVal, sealed string, out any) (int, string) {
	p.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, p.base+path, strings.NewReader(sealed))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set(headerName, headerVal)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		p.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, string(b)
	}
	if out != nil {
		if _, err := phone.OpenJSON(p.key, string(b), out); err != nil {
			p.t.Fatalf("%s: could not open the reply: %v", path, err)
		}
	}
	return 200, ""
}

func (p *fakePhone) do(path string, body, out any) (int, string) {
	return p.call(path, "X-Dsync-Phone", p.phoneID, body, out)
}

// pairFakePhone scans the computer's QR code and pairs.
func pairFakePhone(t *testing.T, n testNode) (*fakePhone, string) {
	t.Helper()
	srv := httptest.NewServer(n.phoneHandler())
	t.Cleanup(srv.Close)
	pr := n.StartPhonePairing()
	u, err := url.Parse(pr.URL)
	if err != nil || u.Scheme != "dsync" || u.Host != "pair" {
		t.Fatalf("pairing URL %q", pr.URL)
	}
	q := u.Query()
	kb, err := base64.RawURLEncoding.DecodeString(q.Get("key"))
	if err != nil || len(kb) != phone.KeySize || q.Get("port") != "47102" || q.Get("name") != n.Self().Name {
		t.Fatalf("QR fields: %v", q)
	}
	var key [phone.KeySize]byte
	copy(key[:], kb)
	p := &fakePhone{t: t, base: srv.URL, key: &key}
	var resp struct {
		PhoneID, ComputerID, ComputerName, OS string
	}
	if code, msg := p.call("/phone/v1/pair", "X-Dsync-Pair", q.Get("pid"), map[string]string{"name": "Muhammed's iPhone", "platform": "ios"}, &resp); code != 200 {
		t.Fatalf("pair: %d %s", code, msg)
	}
	if resp.ComputerID != n.cfg.ID || resp.PhoneID == "" {
		t.Fatalf("pair reply %+v", resp)
	}
	p.phoneID = resp.PhoneID
	return p, q.Get("pid")
}

type syncReply struct {
	Computer struct{ ID, Name string }
	Messages []PhoneMessage
}

func TestPhoneEndToEnd(t *testing.T) {
	n := newTestNode(t, "Laptop")
	p, pid := pairFakePhone(t, n)

	// The phone shows up as a paired, online device.
	var ph Peer
	for _, x := range n.Peers() {
		if x.ID == p.phoneID {
			ph = x
		}
	}
	if !ph.Phone || !ph.Paired || !ph.Online || ph.Name != "Muhammed's iPhone" || ph.OS != "ios" {
		t.Fatalf("phone in device list: %+v", ph)
	}
	// A pairing code works once.
	if code, _ := p.call("/phone/v1/pair", "X-Dsync-Pair", pid, map[string]string{"name": "x"}, nil); code != http.StatusGone {
		t.Errorf("reusing the code: %d, want 410", code)
	}

	// Phone -> computer text.
	if code, msg := p.do("/phone/v1/text", map[string]string{"text": "hello from the phone"}, nil); code != 200 {
		t.Fatalf("text: %d %s", code, msg)
	}

	// Phone -> computer file, in chunks.
	photo := bytes.Repeat([]byte("jpegdata"), 150_000) // 1.2 MB
	var start struct {
		UploadID  string
		ChunkSize int
	}
	if code, msg := p.do("/phone/v1/upload/start", map[string]any{"name": "IMG_1.jpg", "size": len(photo)}, &start); code != 200 {
		t.Fatalf("upload start: %d %s", code, msg)
	}
	if code, _ := p.do("/phone/v1/upload/chunk", map[string]any{"uploadId": start.UploadID, "offset": 5, "data": "AAAA"}, nil); code != http.StatusBadRequest {
		t.Errorf("wrong offset: %d, want 400", code)
	}
	for off := 0; off < len(photo); off += start.ChunkSize {
		end := min(off+start.ChunkSize, len(photo))
		chunk := base64.StdEncoding.EncodeToString(photo[off:end])
		if code, msg := p.do("/phone/v1/upload/chunk", map[string]any{"uploadId": start.UploadID, "offset": off, "data": chunk}, nil); code != 200 {
			t.Fatalf("chunk at %d: %d %s", off, code, msg)
		}
	}
	var fin struct{ Message PhoneMessage }
	if code, msg := p.do("/phone/v1/upload/finish", map[string]any{"uploadId": start.UploadID}, &fin); code != 200 {
		t.Fatalf("finish: %d %s", code, msg)
	}
	if fin.Message.File == nil || fin.Message.File.Status != StatusDone || !fin.Message.FromPhone {
		t.Fatalf("finished upload: %+v", fin.Message)
	}
	if got, _ := os.ReadFile(filepath.Join(n.ReceiveDir(), "IMG_1.jpg")); !bytes.Equal(got, photo) {
		t.Fatal("the photo on the computer differs")
	}

	// Computer -> phone: text and a file, picked up by sync.
	doc := filepath.Join(t.TempDir(), "notes.pdf")
	os.WriteFile(doc, []byte("%PDF the notes"), 0o644)
	if err := n.SendText(context.Background(), p.phoneID, "hi phone"); err != nil {
		t.Fatal(err)
	}
	if err := n.SendFiles(context.Background(), p.phoneID, []string{doc}); err != nil {
		t.Fatal(err)
	}
	var s syncReply
	p.do("/phone/v1/sync", map[string]int64{"since": 0}, &s)
	if s.Computer.ID != n.cfg.ID || len(s.Messages) != 4 {
		t.Fatalf("sync: computer %+v, %d messages", s.Computer, len(s.Messages))
	}
	last := s.Messages[3]
	if last.FromPhone || last.File == nil || last.File.Name != "notes.pdf" || last.File.Status != StatusDone {
		t.Fatalf("file offered to the phone: %+v", last)
	}
	var s2 syncReply
	p.do("/phone/v1/sync", map[string]int64{"since": last.ID}, &s2)
	if len(s2.Messages) != 0 {
		t.Errorf("since the last id should be empty, got %d", len(s2.Messages))
	}

	// Download it in two pieces.
	var got []byte
	for off := 0; ; {
		var d struct {
			Data string
			Size int64
			EOF  bool
		}
		if code, msg := p.do("/phone/v1/download", map[string]any{"messageId": last.ID, "offset": off, "length": 8}, &d); code != 200 {
			t.Fatalf("download: %d %s", code, msg)
		}
		b, _ := base64.StdEncoding.DecodeString(d.Data)
		got = append(got, b...)
		off += len(b)
		if d.EOF {
			break
		}
	}
	if string(got) != "%PDF the notes" {
		t.Fatalf("downloaded %q", got)
	}
	// The phone can't download what it sent itself or someone else's files.
	if code, _ := p.do("/phone/v1/download", map[string]any{"messageId": fin.Message.ID, "offset": 0, "length": 10}, nil); code != http.StatusNotFound {
		t.Errorf("downloading its own upload: %d, want 404", code)
	}

	// Unpair.
	if code, _ := p.do("/phone/v1/unpair", map[string]any{}, nil); code != 200 {
		t.Fatal("unpair failed")
	}
	if code, _ := p.do("/phone/v1/sync", map[string]int64{"since": 0}, nil); code != http.StatusUnauthorized {
		t.Errorf("after unpairing: %d, want 401", code)
	}
}

func TestPhoneSecurity(t *testing.T) {
	n := newTestNode(t, "Laptop")
	p, _ := pairFakePhone(t, n)

	// A replayed request is refused.
	sealed, _ := phone.SealJSON(p.key, map[string]string{"text": "once"})
	if code, _ := p.raw("/phone/v1/text", "X-Dsync-Phone", p.phoneID, sealed, nil); code != 200 {
		t.Fatalf("first send: %d", code)
	}
	if code, _ := p.raw("/phone/v1/text", "X-Dsync-Phone", p.phoneID, sealed, nil); code != http.StatusUnauthorized {
		t.Errorf("replay: %d, want 401", code)
	}

	// A wrong key is refused.
	wrong := &fakePhone{t: t, base: p.base, key: phone.NewKey(), phoneID: p.phoneID}
	if code, _ := wrong.do("/phone/v1/sync", map[string]int64{"since": 0}, nil); code != http.StatusUnauthorized {
		t.Errorf("wrong key: %d, want 401", code)
	}
	// An unknown phone id is refused.
	stranger := &fakePhone{t: t, base: p.base, key: p.key, phoneID: "phone-nope"}
	if code, _ := stranger.do("/phone/v1/sync", map[string]int64{"since": 0}, nil); code != http.StatusUnauthorized {
		t.Errorf("unknown phone: %d, want 401", code)
	}
	// An unknown pairing code is refused.
	if code, _ := p.call("/phone/v1/pair", "X-Dsync-Pair", "made-up", map[string]string{"name": "x"}, nil); code != http.StatusGone {
		t.Errorf("made-up pairing code: %d, want 410", code)
	}
	// Uploading more than announced is refused.
	var start struct{ UploadID string }
	p.do("/phone/v1/upload/start", map[string]any{"name": "a.txt", "size": 3}, &start)
	if code, _ := p.do("/phone/v1/upload/chunk", map[string]any{"uploadId": start.UploadID, "offset": 0, "data": base64.StdEncoding.EncodeToString([]byte("toolong"))}, nil); code != http.StatusBadRequest {
		t.Errorf("oversized chunk: %d, want 400", code)
	}
	// Folders can't be sent to phones (yet).
	if err := n.SendFiles(context.Background(), p.phoneID, []string{t.TempDir()}); err == nil {
		t.Error("sending a folder to a phone should fail clearly")
	}
}

func TestPhoneUploadResume(t *testing.T) {
	n := newTestNode(t, "Laptop")
	p, _ := pairFakePhone(t, n)
	video := bytes.Repeat([]byte("0123456789abcdef"), 90_000) // 1.44 MB: three chunks
	tid := "00112233445566778899aabbccddeeff"
	type startReply struct {
		UploadID  string
		ChunkSize int
		Offset    int64
	}
	start := func(size int) startReply {
		t.Helper()
		var r startReply
		if code, msg := p.do("/phone/v1/upload/start", map[string]any{"name": "clip.mp4", "size": size, "transferId": tid}, &r); code != 200 {
			t.Fatalf("upload start: %d %s", code, msg)
		}
		return r
	}
	chunk := func(id string, off int, end int) {
		t.Helper()
		data := base64.StdEncoding.EncodeToString(video[off:end])
		if code, msg := p.do("/phone/v1/upload/chunk", map[string]any{"uploadId": id, "offset": off, "data": data}, nil); code != 200 {
			t.Fatalf("chunk at %d: %d %s", off, code, msg)
		}
	}
	fileMsgs := func() (out []Message) {
		for _, m := range n.History() {
			if m.PeerID == p.phoneID && m.File != nil {
				out = append(out, m)
			}
		}
		return out
	}

	s := start(len(video))
	if s.Offset != 0 || s.UploadID == "" {
		t.Fatalf("first start: %+v", s)
	}
	chunk(s.UploadID, 0, s.ChunkSize)

	// The phone asks again (it was closed and reopened): same upload, continue after the first chunk.
	s2 := start(len(video))
	if s2.UploadID != s.UploadID || s2.Offset != int64(s.ChunkSize) {
		t.Fatalf("restart while the upload is still open: %+v, want offset %d", s2, s.ChunkSize)
	}
	chunk(s2.UploadID, int(s2.Offset), 2*s.ChunkSize)

	// The computer forgets the open upload (idle, or it restarted): the .part file stays.
	n.phones.mu.Lock()
	for _, u := range n.phones.uploads {
		u.touched = u.touched.Add(-2 * phoneUploadIdle)
	}
	n.phones.mu.Unlock()
	n.dropIdleUploads()
	if ms := fileMsgs(); len(ms) != 1 || ms[0].File.Status != StatusFailed {
		t.Fatalf("after going idle: %+v", ms)
	}
	parts, _ := filepath.Glob(filepath.Join(n.ReceiveDir(), partPrefix+"*"+partSuffix))
	if len(parts) != 1 {
		t.Fatalf("want the .part file kept, got %v", parts)
	}

	s3 := start(len(video))
	if s3.Offset != int64(2*s.ChunkSize) {
		t.Fatalf("resume from disk: offset %d, want %d", s3.Offset, 2*s.ChunkSize)
	}
	if ms := fileMsgs(); len(ms) != 1 || ms[0].File.Status != StatusActive {
		t.Fatalf("resuming reuses the message: %+v", ms)
	}
	chunk(s3.UploadID, int(s3.Offset), len(video))
	var fin struct{ Message PhoneMessage }
	if code, msg := p.do("/phone/v1/upload/finish", map[string]any{"uploadId": s3.UploadID}, &fin); code != 200 {
		t.Fatalf("finish: %d %s", code, msg)
	}
	if fin.Message.File == nil || fin.Message.File.Status != StatusDone {
		t.Fatalf("finished: %+v", fin.Message)
	}
	if got, _ := os.ReadFile(filepath.Join(n.ReceiveDir(), "clip.mp4")); !bytes.Equal(got, video) {
		t.Fatal("the resumed file differs")
	}
	if parts, _ := filepath.Glob(filepath.Join(n.ReceiveDir(), partPrefix+"*"+partSuffix)); len(parts) != 0 {
		t.Errorf("leftover .part files: %v", parts)
	}

	// A transfer id must be hex.
	if code, _ := p.do("/phone/v1/upload/start", map[string]any{"name": "x", "size": 1, "transferId": "nothex"}, nil); code != http.StatusBadRequest {
		t.Errorf("bad transfer id: %d, want 400", code)
	}
	// Uploads without a transfer id (phone app 1.0.11) still work and start at 0.
	var old startReply
	if code, _ := p.do("/phone/v1/upload/start", map[string]any{"name": "old.txt", "size": 2}, &old); code != 200 || old.Offset != 0 {
		t.Errorf("old-style start: %d %+v", code, old)
	}
}
