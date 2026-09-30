package node

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"dsync/internal/client"
	"dsync/internal/config"
	"dsync/internal/identity"
	"dsync/internal/proto"
)

// Pairing events.
const (
	// EventPairRequest asks the user to accept or decline another device.
	EventPairRequest = "pair:request" // data: PairRequest
	// EventPairClosed means a request is no longer waiting (answered, timed
	// out or withdrawn), so its dialog should close.
	EventPairClosed = "pair:closed" // data: request id
	// EventPairResult reports how a pairing we started ended.
	EventPairResult = "pair:result" // data: PairResult
)

const (
	pairTimeout = 60 * time.Second
	// maxPendingPairs stops a device on the network from flooding the user
	// with pairing dialogs.
	maxPendingPairs = 3
)

// PairRequest is an incoming request waiting for the user.
type PairRequest struct {
	ID     string `json:"id"`
	PeerID string `json:"peerId"`
	Name   string `json:"name"`
	OS     string `json:"os"`
	Code   string `json:"code"`
}

// PairResult is the outcome of a pairing this device started.
type PairResult struct {
	PeerID string `json:"peerId"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
}

type pendingPair struct {
	req    PairRequest
	answer chan bool
}

// Fingerprint returns this device's key fingerprint in short form.
func (n *Node) Fingerprint() string { return identity.Short(n.id.Fingerprint) }

// StartPair asks a device to pair and returns the code both screens show.
// The answer arrives later as EventPairResult.
func (n *Node) StartPair(ctx context.Context, peerID string) (string, error) {
	n.mu.Lock()
	p, ok := n.peers[peerID]
	var addr string
	if ok {
		addr = p.Addr
	}
	n.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("unknown device %s", peerID)
	}

	// Learn the key the device presents now, and pin it for the request so
	// the device can't be swapped between showing the code and accepting.
	d, fp, err := n.cl.Info(ctx, addr)
	if err != nil {
		return "", fmt.Errorf("could not reach device: %w", err)
	}
	if d.ID != peerID {
		return "", errors.New("a different device answered at that address; scan again")
	}
	code := identity.PairCode(n.id.Fingerprint, fp)

	pctx, cancel := context.WithTimeout(ctx, pairTimeout+10*time.Second)
	n.mu.Lock()
	if old := n.pairing[peerID]; old != nil {
		old()
	}
	n.pairing[peerID] = cancel
	n.mu.Unlock()

	go func() {
		defer cancel()
		self := n.Self()
		resp, err := n.cl.Pair(pctx, addr, fp, proto.PairRequest{FromID: self.ID, FromName: self.Name, FromPort: self.Port, OS: runtime.GOOS})
		n.mu.Lock()
		delete(n.pairing, peerID)
		if err == nil {
			n.cfg.Trust(config.TrustedPeer{ID: peerID, Name: resp.Name, Fingerprint: fp})
			err = n.cfg.Save()
		}
		n.mu.Unlock()
		res := PairResult{PeerID: peerID, OK: err == nil}
		if err != nil {
			res.Error = pairErrorText(err, pctx)
		}
		n.emit(EventPairResult, res)
		n.emit(EventPeers, n.Peers())
	}()
	return code, nil
}

func pairErrorText(err error, ctx context.Context) string {
	var se *client.StatusError
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return "canceled"
	case errors.As(err, &se) && se.Code == http.StatusForbidden:
		return "the other device declined"
	case errors.As(err, &se) && se.Code == http.StatusRequestTimeout:
		return "no answer from the other device"
	}
	return err.Error()
}

// CancelPair withdraws a pairing request this device started.
func (n *Node) CancelPair(peerID string) {
	n.mu.Lock()
	cancel := n.pairing[peerID]
	n.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// AnswerPair accepts or declines an incoming pairing request.
func (n *Node) AnswerPair(requestID string, accept bool) {
	n.mu.Lock()
	p := n.pending[requestID]
	n.mu.Unlock()
	if p != nil {
		select {
		case p.answer <- accept:
		default:
		}
	}
}

// PendingPairs lists incoming requests still waiting for an answer, for a
// UI that starts while one is open.
func (n *Node) PendingPairs() []PairRequest {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]PairRequest, 0, len(n.pending))
	for _, p := range n.pending {
		out = append(out, p.req)
	}
	return out
}

func (n *Node) handlePair(w http.ResponseWriter, r *http.Request) {
	fp := callerFingerprint(r)
	var req proto.PairRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || req.FromID == "" || fp == "" {
		http.Error(w, "bad pair request", http.StatusBadRequest)
		return
	}
	self := n.Self()
	if req.FromID == self.ID || fp == n.id.Fingerprint {
		http.Error(w, "cannot pair with itself", http.StatusBadRequest)
		return
	}

	n.mu.Lock()
	if len(n.pending) >= maxPendingPairs {
		n.mu.Unlock()
		http.Error(w, "too many pairing requests waiting", http.StatusTooManyRequests)
		return
	}
	for _, p := range n.pending {
		if p.req.PeerID == req.FromID {
			n.mu.Unlock()
			http.Error(w, "a pairing request from this device is already waiting", http.StatusConflict)
			return
		}
	}
	idb := make([]byte, 8)
	rand.Read(idb)
	pr := PairRequest{
		ID: hex.EncodeToString(idb), PeerID: req.FromID, Name: req.FromName, OS: req.OS,
		Code: identity.PairCode(n.id.Fingerprint, fp),
	}
	p := &pendingPair{req: pr, answer: make(chan bool, 1)}
	n.pending[pr.ID] = p
	n.mu.Unlock()

	defer func() {
		n.mu.Lock()
		delete(n.pending, pr.ID)
		n.mu.Unlock()
		n.emit(EventPairClosed, pr.ID)
	}()
	n.emit(EventPairRequest, pr)

	timer := time.NewTimer(pairTimeout)
	defer timer.Stop()
	select {
	case ok := <-p.answer:
		if !ok {
			http.Error(w, "declined", http.StatusForbidden)
			return
		}
	case <-timer.C:
		http.Error(w, "no answer", http.StatusRequestTimeout)
		return
	case <-r.Context().Done():
		return
	}

	t := config.TrustedPeer{ID: req.FromID, Name: req.FromName, Fingerprint: fp}
	n.mu.Lock()
	n.cfg.Trust(t)
	err := n.cfg.Save()
	n.mu.Unlock()
	if err != nil {
		http.Error(w, "could not save pairing", http.StatusInternalServerError)
		return
	}
	n.learnPeer(t, req.FromName, req.FromPort, r.RemoteAddr)
	n.emit(EventPeers, n.Peers())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(proto.PairResponse{ID: self.ID, Name: self.Name})
}

// Unpair forgets a device's key on both sides (the other side is told on a
// best-effort basis).
func (n *Node) Unpair(ctx context.Context, peerID string) error {
	n.mu.Lock()
	_, isPhone := n.cfg.PhoneByID(peerID)
	n.mu.Unlock()
	if isPhone {
		n.forgetPhone(peerID)
		return nil
	}
	n.mu.Lock()
	t, ok := n.cfg.TrustedByID(peerID)
	var addr string
	if p := n.peers[peerID]; p != nil {
		addr = p.Addr
	}
	n.cfg.Untrust(peerID)
	err := n.cfg.Save()
	n.mu.Unlock()
	n.emit(EventPeers, n.Peers())
	if ok && addr != "" {
		go n.cl.Unpair(context.WithoutCancel(ctx), addr, t.Fingerprint)
	}
	return err
}

func (n *Node) handleUnpair(w http.ResponseWriter, r *http.Request, from config.TrustedPeer) {
	n.mu.Lock()
	n.cfg.Untrust(from.ID)
	err := n.cfg.Save()
	n.mu.Unlock()
	n.emit(EventPeers, n.Peers())
	if err != nil {
		http.Error(w, "could not save", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
