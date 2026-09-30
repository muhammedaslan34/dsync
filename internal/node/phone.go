package node

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"dsync/internal/config"
	"dsync/internal/discovery"
	"dsync/internal/phone"
	"dsync/internal/proto"
)

// Phones pair by scanning a QR code and then use the protocol in
// docs/phone-protocol.md on their own port. They show in the device list
// like computers; what the computer sends them waits in the history until
// the phone's next sync picks it up.

const (
	pairingTTL      = 10 * time.Minute
	phoneOnlineFor  = 15 * time.Second
	phoneChunkSize  = 512 << 10
	phoneMaxRead    = 1 << 20
	phoneSyncLimit  = 200
	phoneUploadIdle = 10 * time.Minute
)

// EventPhonePaired reports a newly paired phone (data: Peer).
const EventPhonePaired = "phone:paired"

type pendingPhone struct {
	key     *[phone.KeySize]byte
	expires time.Time
}

type phoneUpload struct {
	phoneID  string
	name     string
	size     int64
	received int64
	file     *os.File
	msgID    int64
	touched  time.Time
	// resumable uploads (the phone sent a transferId) keep their .part file
	// when interrupted, so the phone can continue where it stopped.
	resumable bool
}

type phoneState struct {
	mu       sync.Mutex
	pairings map[string]*pendingPhone
	uploads  map[string]*phoneUpload
	nonces   map[string]time.Time // seen nonces (hex), for replay protection
}

// PhonePairing is the QR code content for connecting a phone.
type PhonePairing struct {
	URL     string `json:"url"`
	Expires int64  `json:"expires"` // unix ms
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// StartPhonePairing makes a one-time pairing code for the QR code.
func (n *Node) StartPhonePairing() PhonePairing {
	pid := randomHex(12)
	key := phone.NewKey()
	exp := time.Now().Add(pairingTTL)
	n.phones.mu.Lock()
	for id, p := range n.phones.pairings {
		if time.Now().After(p.expires) {
			delete(n.phones.pairings, id)
		}
	}
	n.phones.pairings[pid] = &pendingPhone{key: key, expires: exp}
	n.phones.mu.Unlock()

	self := n.Self()
	var addrs []string
	for _, a := range discovery.LocalAddrs() {
		addrs = append(addrs, a.IP)
	}
	q := url.Values{}
	q.Set("v", "1")
	q.Set("pid", pid)
	q.Set("key", base64.RawURLEncoding.EncodeToString(key[:]))
	q.Set("name", self.Name)
	q.Set("cid", self.ID)
	q.Set("os", runtime.GOOS)
	q.Set("port", strconv.Itoa(proto.PhonePort))
	q.Set("addr", strings.Join(addrs, ","))
	return PhonePairing{URL: "dsync://pair?" + q.Encode(), Expires: exp.UnixMilli()}
}

// loadPhones puts the paired phones in the device list.
func (n *Node) loadPhones() {
	for _, p := range n.cfg.Phones {
		n.peers[p.ID] = &Peer{ID: p.ID, Name: p.Name, OS: p.Platform, Phone: true}
	}
}

func (n *Node) phoneHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /phone/v1/pair", n.handlePhonePair)
	for path, h := range map[string]phoneHandlerFunc{
		"sync":          n.phoneSync,
		"text":          n.phoneText,
		"upload/start":  n.phoneUploadStart,
		"upload/chunk":  n.phoneUploadChunk,
		"upload/finish": n.phoneUploadFinish,
		"download":      n.phoneDownload,
		"unpair":        n.phoneUnpair,
	} {
		mux.HandleFunc("POST /phone/v1/"+path, n.phoneAuth(h))
	}
	return mux
}

// phoneReply is what a handler returns: a body to seal, or an error with
// its HTTP status.
type phoneReply struct {
	body   any
	status int
	err    string
}

func ok(v any) phoneReply                    { return phoneReply{body: v, status: http.StatusOK} }
func fail(status int, msg string) phoneReply { return phoneReply{status: status, err: msg} }

type phoneHandlerFunc func(ph config.Phone, raw json.RawMessage) phoneReply

// readSealed reads a request body and opens it with key, rejecting replays.
func (n *Node) readSealed(r *http.Request, key *[phone.KeySize]byte, v any) error {
	b, err := io.ReadAll(io.LimitReader(r.Body, 2*phoneMaxRead))
	if err != nil {
		return err
	}
	nonce, err := phone.OpenJSON(key, strings.TrimSpace(string(b)), v)
	if err != nil {
		return err
	}
	id := hex.EncodeToString(nonce[:])
	n.phones.mu.Lock()
	defer n.phones.mu.Unlock()
	now := time.Now()
	for k, t := range n.phones.nonces {
		if now.Sub(t) > 3*time.Minute {
			delete(n.phones.nonces, k)
		}
	}
	if _, seen := n.phones.nonces[id]; seen {
		return errors.New("replayed message")
	}
	n.phones.nonces[id] = now
	return nil
}

func writeSealed(w http.ResponseWriter, key *[phone.KeySize]byte, v any) {
	sealed, err := phone.SealJSON(key, v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	io.WriteString(w, sealed)
}

func phoneKey(p config.Phone) (*[phone.KeySize]byte, bool) {
	b, err := base64.StdEncoding.DecodeString(p.Key)
	if err != nil || len(b) != phone.KeySize {
		return nil, false
	}
	var k [phone.KeySize]byte
	copy(k[:], b)
	return &k, true
}

func (n *Node) handlePhonePair(w http.ResponseWriter, r *http.Request) {
	pid := r.Header.Get("X-Dsync-Pair")
	n.phones.mu.Lock()
	pending := n.phones.pairings[pid]
	n.phones.mu.Unlock()
	if pending == nil || time.Now().After(pending.expires) {
		http.Error(w, "pairing code expired or already used; show a new one on the computer", http.StatusGone)
		return
	}
	var req struct {
		Name     string `json:"name"`
		Platform string `json:"platform"`
	}
	if err := n.readSealed(r, pending.key, &req); err != nil {
		http.Error(w, "could not open the request", http.StatusUnauthorized)
		return
	}
	n.phones.mu.Lock()
	if n.phones.pairings[pid] != pending { // used meanwhile
		n.phones.mu.Unlock()
		http.Error(w, "pairing code already used", http.StatusGone)
		return
	}
	delete(n.phones.pairings, pid)
	n.phones.mu.Unlock()

	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 64 {
		name = "Phone"
	}
	platform := req.Platform
	if platform != "ios" && platform != "android" {
		platform = "phone"
	}
	ph := config.Phone{ID: "phone-" + randomHex(8), Name: name, Platform: platform, Key: base64.StdEncoding.EncodeToString(pending.key[:])}
	peer := Peer{ID: ph.ID, Name: ph.Name, OS: ph.Platform, Phone: true, Online: true, LastSeen: time.Now().UnixMilli()}
	n.mu.Lock()
	n.cfg.Phones = append(n.cfg.Phones, ph)
	err := n.cfg.Save()
	n.peers[ph.ID] = &peer
	n.mu.Unlock()
	if err != nil {
		http.Error(w, "could not save the pairing", http.StatusInternalServerError)
		return
	}
	n.emit(EventPeers, n.Peers())
	n.emit(EventPhonePaired, peer)
	self := n.Self()
	writeSealed(w, pending.key, map[string]string{"phoneId": ph.ID, "computerId": self.ID, "computerName": self.Name, "os": self.OS})
}

// phoneAuth checks the phone and its key, opens the body, runs h and seals
// its reply.
func (n *Node) phoneAuth(h phoneHandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Dsync-Phone")
		n.mu.Lock()
		ph, known := n.cfg.PhoneByID(id)
		n.mu.Unlock()
		key, okKey := phoneKey(ph)
		if !known || !okKey {
			http.Error(w, "unknown phone; pair again", http.StatusUnauthorized)
			return
		}
		var raw json.RawMessage
		if err := n.readSealed(r, key, &raw); err != nil {
			http.Error(w, "could not open the request", http.StatusUnauthorized)
			return
		}
		n.phoneSeen(id)
		rep := h(ph, raw)
		if rep.err != "" {
			http.Error(w, rep.err, rep.status)
			return
		}
		writeSealed(w, key, rep.body)
	}
}

// phoneSeen marks a phone online (its requests are how we know).
func (n *Node) phoneSeen(id string) {
	now := time.Now().UnixMilli()
	n.mu.Lock()
	p := n.peers[id]
	wasOnline := p != nil && p.Online
	if p != nil {
		p.LastSeen, p.LastHeard, p.Online = now, now, true
	}
	n.mu.Unlock()
	if p != nil && !wasOnline {
		n.emit(EventPeers, n.Peers())
	}
}

// PhoneMessage is a message as the phone sees it.
type PhoneMessage struct {
	ID        int64      `json:"id"`
	Time      int64      `json:"time"`
	FromPhone bool       `json:"fromPhone"`
	Text      string     `json:"text,omitempty"`
	File      *phoneFile `json:"file,omitempty"`
}

type phoneFile struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func toPhoneMessage(m Message) PhoneMessage {
	pm := PhoneMessage{ID: m.ID, Time: m.Time, FromPhone: m.Incoming, Text: m.Text}
	if m.File != nil {
		pm.File = &phoneFile{Name: m.File.Name, Size: m.File.Size, Status: m.File.Status, Error: m.File.Error}
	}
	return pm
}

func (n *Node) phoneSync(ph config.Phone, raw json.RawMessage) phoneReply {
	var req struct {
		Since int64 `json:"since"`
	}
	if json.Unmarshal(raw, &req) != nil {
		return fail(http.StatusBadRequest, "bad request")
	}
	var msgs []PhoneMessage
	for _, m := range n.History() {
		if m.PeerID == ph.ID && m.ID > req.Since {
			msgs = append(msgs, toPhoneMessage(m))
			if len(msgs) == phoneSyncLimit {
				break
			}
		}
	}
	if msgs == nil {
		msgs = []PhoneMessage{}
	}
	self := n.Self()
	return ok(map[string]any{
		"computer": map[string]string{"id": self.ID, "name": self.Name, "os": self.OS},
		"messages": msgs,
	})
}

func (n *Node) phoneText(ph config.Phone, raw json.RawMessage) phoneReply {
	var req struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &req) != nil || strings.TrimSpace(req.Text) == "" {
		return fail(http.StatusBadRequest, "empty message")
	}
	if len(req.Text) > proto.MaxTextBytes {
		return fail(http.StatusRequestEntityTooLarge, "message too long; send it as a file")
	}
	m := n.addMessage(Message{PeerID: ph.ID, PeerName: ph.Name, Incoming: true, Text: req.Text})
	return ok(map[string]any{"message": toPhoneMessage(m)})
}

func (n *Node) phoneUploadStart(ph config.Phone, raw json.RawMessage) phoneReply {
	var req struct {
		Name       string `json:"name"`
		Size       int64  `json:"size"`
		TransferID string `json:"transferId"` // optional: makes the upload resumable
	}
	if json.Unmarshal(raw, &req) != nil || req.Size < 0 || (req.TransferID != "" && !validTransferID(req.TransferID)) {
		return fail(http.StatusBadRequest, "bad request")
	}
	n.dropIdleUploads()
	dir := n.ReceiveDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fail(http.StatusInternalServerError, "cannot create the download folder")
	}
	name := safeFileName(req.Name)
	if req.TransferID != "" {
		return n.phoneUploadResume(ph, name, req.Size, req.TransferID)
	}
	if free, err := freeSpace(dir); err == nil && free < req.Size+diskReserve {
		return fail(http.StatusInsufficientStorage, fmt.Sprintf("not enough disk space on %s", n.Self().Name))
	}
	f, err := os.CreateTemp(dir, partPrefix+"phone-*"+partSuffix)
	if err != nil {
		return fail(http.StatusInternalServerError, "cannot create the file")
	}
	m := n.addMessage(Message{PeerID: ph.ID, PeerName: ph.Name, Incoming: true,
		File: &FileInfo{Name: name, Size: req.Size, Status: StatusActive}})
	id := randomHex(12)
	n.phones.mu.Lock()
	n.phones.uploads[id] = &phoneUpload{phoneID: ph.ID, name: name, size: req.Size, file: f, msgID: m.ID, touched: time.Now()}
	n.phones.mu.Unlock()
	return ok(map[string]any{"uploadId": id, "chunkSize": phoneChunkSize, "offset": 0})
}

// resumableUploadID is the upload id of a phone's resumable upload: the same
// for the same phone and transfer, so a restarted upload finds its data.
func resumableUploadID(phoneID, tid string) string {
	sum := sha256.Sum256([]byte(phoneID + "/phone/" + tid))
	return "r" + hex.EncodeToString(sum[:15])
}

// phoneUploadResume starts or continues an upload the phone can resume. The
// data goes to a .part file named after the phone and transfer id; if one is
// there (the phone was closed, or the computer restarted), the reply's
// offset says how much of the file the computer already has.
func (n *Node) phoneUploadResume(ph config.Phone, name string, size int64, tid string) phoneReply {
	id := resumableUploadID(ph.ID, tid)
	n.phones.mu.Lock()
	if u := n.phones.uploads[id]; u != nil && u.phoneID == ph.ID {
		if u.size == size {
			u.touched = time.Now()
			got := u.received
			n.phones.mu.Unlock()
			n.updateFile(u.msgID, func(f *FileInfo) { f.Status, f.Error = StatusActive, "" })
			return ok(map[string]any{"uploadId": id, "chunkSize": phoneChunkSize, "offset": got})
		}
		// Same id, different file: start over.
		delete(n.phones.uploads, id)
		u.file.Close()
	}
	n.phones.mu.Unlock()

	part := n.partPath(ph.ID, "phone/"+tid)
	f, err := os.OpenFile(part, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fail(http.StatusInternalServerError, "cannot create the file")
	}
	have := int64(0)
	if st, err := f.Stat(); err == nil {
		have = st.Size()
	}
	if have > size {
		have = 0
	}
	if err := f.Truncate(have); err != nil {
		f.Close()
		return fail(http.StatusInternalServerError, err.Error())
	}
	if _, err := f.Seek(have, io.SeekStart); err != nil {
		f.Close()
		return fail(http.StatusInternalServerError, err.Error())
	}
	if free, err := freeSpace(n.ReceiveDir()); err == nil && free < size-have+diskReserve {
		f.Close()
		return fail(http.StatusInsufficientStorage, fmt.Sprintf("not enough disk space on %s", n.Self().Name))
	}

	// Keep showing the same message in the conversation.
	var msgID int64
	for _, m := range n.History() {
		if m.PeerID == ph.ID && m.Incoming && m.File != nil && m.File.TransferID == tid {
			msgID = m.ID
		}
	}
	if msgID != 0 {
		n.updateFile(msgID, func(fi *FileInfo) { fi.Status, fi.Error, fi.Name, fi.Size = StatusActive, "", name, size })
	} else {
		m := n.addMessage(Message{PeerID: ph.ID, PeerName: ph.Name, Incoming: true,
			File: &FileInfo{Name: name, Size: size, Status: StatusActive, TransferID: tid}})
		msgID = m.ID
	}
	u := &phoneUpload{phoneID: ph.ID, name: name, size: size, received: have, file: f, msgID: msgID, touched: time.Now(), resumable: true}
	n.phones.mu.Lock()
	n.phones.uploads[id] = u
	n.phones.mu.Unlock()
	if have > 0 {
		n.emit(EventProgress, Progress{ID: msgID, Done: have})
	}
	return ok(map[string]any{"uploadId": id, "chunkSize": phoneChunkSize, "offset": have})
}

func (n *Node) upload(ph config.Phone, id string) *phoneUpload {
	n.phones.mu.Lock()
	defer n.phones.mu.Unlock()
	u := n.phones.uploads[id]
	if u == nil || u.phoneID != ph.ID {
		return nil
	}
	u.touched = time.Now()
	return u
}

func (n *Node) phoneUploadChunk(ph config.Phone, raw json.RawMessage) phoneReply {
	var req struct {
		UploadID string `json:"uploadId"`
		Offset   int64  `json:"offset"`
		Data     string `json:"data"`
	}
	if json.Unmarshal(raw, &req) != nil {
		return fail(http.StatusBadRequest, "bad request")
	}
	u := n.upload(ph, req.UploadID)
	if u == nil {
		return fail(http.StatusNotFound, "no such upload")
	}
	data, err := base64.StdEncoding.DecodeString(req.Data)
	switch {
	case err != nil:
		return fail(http.StatusBadRequest, "bad data")
	case len(data) > phoneChunkSize:
		return fail(http.StatusRequestEntityTooLarge, "chunk too big")
	case req.Offset != u.received:
		return fail(http.StatusBadRequest, fmt.Sprintf("expected offset %d", u.received))
	case u.received+int64(len(data)) > u.size:
		return fail(http.StatusBadRequest, "more data than the file's size")
	}
	if _, err := u.file.Write(data); err != nil {
		return fail(http.StatusInternalServerError, err.Error())
	}
	u.received += int64(len(data))
	n.emit(EventProgress, Progress{ID: u.msgID, Done: u.received})
	return ok(map[string]int64{"received": u.received})
}

func (n *Node) phoneUploadFinish(ph config.Phone, raw json.RawMessage) phoneReply {
	var req struct {
		UploadID string `json:"uploadId"`
	}
	if json.Unmarshal(raw, &req) != nil {
		return fail(http.StatusBadRequest, "bad request")
	}
	u := n.upload(ph, req.UploadID)
	if u == nil {
		return fail(http.StatusNotFound, "no such upload")
	}
	if u.received != u.size {
		return fail(http.StatusBadRequest, fmt.Sprintf("only %d of %d bytes arrived", u.received, u.size))
	}
	n.phones.mu.Lock()
	delete(n.phones.uploads, req.UploadID)
	n.phones.mu.Unlock()
	u.file.Close()
	os.Chmod(u.file.Name(), 0o644)
	final := uniquePath(n.ReceiveDir(), u.name)
	if err := os.Rename(u.file.Name(), final); err != nil {
		os.Remove(u.file.Name())
		n.updateFile(u.msgID, func(f *FileInfo) { f.Status, f.Error = StatusFailed, err.Error() })
		return fail(http.StatusInternalServerError, err.Error())
	}
	n.updateFile(u.msgID, func(f *FileInfo) { f.Status, f.Path, f.Error = StatusDone, final, "" })
	for _, m := range n.History() {
		if m.ID == u.msgID {
			return ok(map[string]any{"message": toPhoneMessage(m)})
		}
	}
	return ok(map[string]any{})
}

// dropIdleUploads gives up on uploads the phone stopped sending.
func (n *Node) dropIdleUploads() {
	n.phones.mu.Lock()
	var stale []*phoneUpload
	for id, u := range n.phones.uploads {
		if time.Since(u.touched) > phoneUploadIdle {
			stale = append(stale, u)
			delete(n.phones.uploads, id)
		}
	}
	n.phones.mu.Unlock()
	for _, u := range stale {
		u.file.Close()
		if u.resumable {
			// Keep the .part file: the phone continues from it when it's back.
			n.updateFile(u.msgID, func(f *FileInfo) { f.Status, f.Error = StatusFailed, errInterrupted.Error() })
			continue
		}
		os.Remove(u.file.Name())
		n.updateFile(u.msgID, func(f *FileInfo) { f.Status, f.Error = StatusFailed, "the phone stopped sending" })
	}
}

func (n *Node) phoneDownload(ph config.Phone, raw json.RawMessage) phoneReply {
	var req struct {
		MessageID int64 `json:"messageId"`
		Offset    int64 `json:"offset"`
		Length    int64 `json:"length"`
	}
	if json.Unmarshal(raw, &req) != nil || req.Offset < 0 || req.Length <= 0 {
		return fail(http.StatusBadRequest, "bad request")
	}
	var path string
	for _, m := range n.History() {
		if m.ID == req.MessageID && m.PeerID == ph.ID && !m.Incoming && m.File != nil && !m.File.Folder {
			path = m.File.Path
		}
	}
	if path == "" {
		return fail(http.StatusNotFound, "no such file")
	}
	f, err := os.Open(path)
	if err != nil {
		return fail(http.StatusNotFound, "the file is no longer there")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return fail(http.StatusInternalServerError, err.Error())
	}
	buf := make([]byte, min(req.Length, phoneMaxRead))
	nr, err := f.ReadAt(buf, req.Offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return fail(http.StatusInternalServerError, err.Error())
	}
	return ok(map[string]any{
		"data": base64.StdEncoding.EncodeToString(buf[:nr]),
		"size": st.Size(),
		"eof":  req.Offset+int64(nr) >= st.Size(),
	})
}

func (n *Node) phoneUnpair(ph config.Phone, raw json.RawMessage) phoneReply {
	n.forgetPhone(ph.ID)
	return ok(map[string]any{})
}

func (n *Node) forgetPhone(id string) {
	n.mu.Lock()
	var kept []config.Phone
	for _, p := range n.cfg.Phones {
		if p.ID != id {
			kept = append(kept, p)
		}
	}
	n.cfg.Phones = kept
	n.cfg.Save()
	delete(n.peers, id)
	n.mu.Unlock()
	n.emit(EventPeers, n.Peers())
}

// sendToPhone stores text or files for a phone to pick up on its next sync.
func (n *Node) sendToPhone(peer Peer, text string, paths []string) error {
	if text != "" {
		n.addMessage(Message{PeerID: peer.ID, PeerName: peer.Name, Text: text})
		return nil
	}
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			return err
		}
		if st.IsDir() {
			return errors.New("phones can't receive folders yet")
		}
	}
	for _, p := range paths {
		st, _ := os.Stat(p)
		n.addMessage(Message{PeerID: peer.ID, PeerName: peer.Name,
			File: &FileInfo{Name: filepath.Base(p), Size: st.Size(), Path: p, Status: StatusDone}})
	}
	return nil
}

// servePhones runs the phone server until ctx ends.
func (n *Node) servePhones(ctx context.Context) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", proto.PhonePort))
	if err != nil {
		return fmt.Errorf("listen tcp :%d (phones): %w", proto.PhonePort, err)
	}
	srv := &http.Server{Handler: n.phoneHandler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
