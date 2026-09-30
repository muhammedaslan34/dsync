package node

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"dsync/internal/clip"
	"dsync/internal/config"
	"dsync/internal/proto"
)

// Clipboard sync: when it is on, whatever is copied here goes to every
// online paired device that also has it on, and what they copy lands here.

// EventClipboard reports clipboard content received from another device.
const EventClipboard = "clipboard" // data: ClipboardEvent

const (
	maxClipText  = proto.MaxTextBytes
	maxClipImage = 25 << 20
	// echoWindow is how long after writing a received image to our
	// clipboard we ignore image changes: some systems re-encode images, so
	// the change we see isn't byte-identical to what we wrote. Text comes
	// back unchanged, so the hash check is enough for it.
	echoWindow = 2 * time.Second
)

// ClipboardEvent tells the UI that clipboard content arrived.
type ClipboardEvent struct {
	PeerName string `json:"peerName"`
	Kind     string `json:"kind"`
}

// ClipboardStatus is shown in the settings.
type ClipboardStatus struct {
	Enabled   bool   `json:"enabled"`
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"` // why it isn't available
}

type clipState struct {
	mu          sync.Mutex
	board       clip.Board
	boardErr    error
	lastApplied [32]byte // content we got from another device
	appliedKind string
	appliedAt   time.Time
	lastSent    [32]byte
}

// SetClipboard gives the node the clipboard to sync, or the reason there
// is none. Call it before Run.
func (n *Node) SetClipboard(b clip.Board, err error) {
	n.clip.mu.Lock()
	defer n.clip.mu.Unlock()
	n.clip.board, n.clip.boardErr = b, err
}

func (n *Node) ClipboardStatus() ClipboardStatus {
	n.mu.Lock()
	enabled := n.cfg.ClipboardSync
	n.mu.Unlock()
	n.clip.mu.Lock()
	defer n.clip.mu.Unlock()
	st := ClipboardStatus{Enabled: enabled, Available: n.clip.board != nil}
	if n.clip.boardErr != nil {
		st.Error = n.clip.boardErr.Error()
	} else if n.clip.board == nil {
		st.Error = "no clipboard on this computer"
	}
	return st
}

func (n *Node) SetClipboardSync(on bool) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.cfg.ClipboardSync = on
	return n.cfg.Save()
}

func (n *Node) clipboardOn() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.cfg.ClipboardSync
}

// clipLoop sends local clipboard changes to paired devices.
func (n *Node) clipLoop(ctx context.Context) {
	n.clip.mu.Lock()
	board := n.clip.board
	n.clip.mu.Unlock()
	if board == nil {
		return
	}
	for c := range board.Watch(ctx) {
		if !n.clipboardOn() || !n.shouldSend(c) {
			continue
		}
		n.sendClipboard(ctx, c)
	}
}

// shouldSend filters out content we must not or need not send.
func (n *Node) shouldSend(c clip.Content) bool {
	switch {
	case c.Sensitive, len(c.Data) == 0:
		return false // passwords from password managers stay here
	case c.Kind == clip.Text && len(c.Data) > maxClipText,
		c.Kind == clip.Image && len(c.Data) > maxClipImage:
		return false
	}
	h := c.Hash()
	n.clip.mu.Lock()
	defer n.clip.mu.Unlock()
	if h == n.clip.lastApplied || h == n.clip.lastSent {
		return false // what another device just sent us, or already sent
	}
	if c.Kind == clip.Image && n.clip.appliedKind == clip.Image && time.Since(n.clip.appliedAt) < echoWindow {
		return false // our own write of a received image, re-encoded
	}
	n.clip.lastSent = h
	return true
}

func (n *Node) sendClipboard(ctx context.Context, c clip.Content) {
	msg := proto.ClipboardData{Kind: c.Kind, FromName: n.Self().Name}
	if c.Kind == clip.Text {
		msg.Text = string(c.Data)
	} else {
		msg.Image = c.Data
	}

	type target struct{ addr, fp string }
	var targets []target
	n.mu.Lock()
	for _, p := range n.peers {
		if t, ok := n.cfg.TrustedByID(p.ID); ok && p.Online {
			targets = append(targets, target{p.Addr, t.Fingerprint})
		}
	}
	n.mu.Unlock()

	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			// Devices with sync off refuse; that's fine.
			n.cl.SendClipboard(sctx, t.addr, t.fp, msg)
		}()
	}
	wg.Wait()
}

var errClipboardOff = errors.New("clipboard sync is off on this device")

func (n *Node) handleClipboard(w http.ResponseWriter, r *http.Request, from config.TrustedPeer) {
	if !n.clipboardOn() {
		http.Error(w, errClipboardOff.Error(), http.StatusForbidden)
		return
	}
	n.clip.mu.Lock()
	board := n.clip.board
	n.clip.mu.Unlock()
	if board == nil {
		http.Error(w, "no clipboard on this device", http.StatusServiceUnavailable)
		return
	}

	// Base64 in JSON makes an image about a third bigger.
	var msg proto.ClipboardData
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxClipImage*4/3+64<<10)).Decode(&msg); err != nil {
		http.Error(w, "bad clipboard data", http.StatusBadRequest)
		return
	}
	c := clip.Content{Kind: msg.Kind}
	switch msg.Kind {
	case clip.Text:
		c.Data = []byte(msg.Text)
	case clip.Image:
		c.Data = msg.Image
	default:
		http.Error(w, "unknown clipboard kind", http.StatusBadRequest)
		return
	}
	if len(c.Data) == 0 || (c.Kind == clip.Text && len(c.Data) > maxClipText) {
		http.Error(w, "bad clipboard data", http.StatusBadRequest)
		return
	}

	// Remember it before writing, so the watcher doesn't send it back.
	n.clip.mu.Lock()
	n.clip.lastApplied, n.clip.appliedKind, n.clip.appliedAt = c.Hash(), c.Kind, time.Now()
	n.clip.mu.Unlock()
	if err := board.Write(r.Context(), c); err != nil {
		http.Error(w, "could not set the clipboard: "+err.Error(), http.StatusInternalServerError)
		return
	}
	name := n.learnPeer(from, msg.FromName, 0, r.RemoteAddr)
	n.emit(EventClipboard, ClipboardEvent{PeerName: name, Kind: c.Kind})
	w.WriteHeader(http.StatusNoContent)
}
