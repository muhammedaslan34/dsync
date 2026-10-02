package node

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"dsync/internal/config"
	"dsync/internal/identity"
)

const (
	EventIncomingRequest = "incoming:request"
	EventIncomingClosed  = "incoming:closed"

	incomingRequestTTL  = 2 * time.Minute
	incomingDecisionTTL = 10 * time.Minute
	maxPendingIncoming  = 3
)

// ReceiveSettings controls whether paired computers need approval for files.
type ReceiveSettings struct {
	AskBeforeAccepting bool `json:"askBeforeAccepting"`
}

// IncomingRequest is shown locally before an incoming file or folder starts.
type IncomingRequest struct {
	ID          string `json:"id"`
	PeerID      string `json:"peerId"`
	PeerName    string `json:"peerName"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	Folder      bool   `json:"folder,omitempty"`
	Files       int    `json:"files,omitempty"`
}

type pendingIncoming struct {
	req      IncomingRequest
	key      string
	approval incomingApproval
	timer    *time.Timer
}

type incomingDecision struct {
	allow    bool
	approval incomingApproval
	expires  time.Time
	timer    *time.Timer
	members  map[string]incomingMember
	files    int
	bytes    int64
}

type incomingApproval struct {
	name       string
	nameKnown  bool
	size       int64
	sizeKnown  bool
	folder     bool
	files      int
	filesKnown bool
	baseFiles  int
	baseBytes  int64
}

// matches is deliberately asymmetric: a complete approved decision may
// authorize an older sender's sparse offset probe, but a sparse approval can
// never authorize a later request that reveals or changes metadata.
func (a incomingApproval) matches(candidate incomingApproval) bool {
	if a.folder != candidate.folder {
		return false
	}
	if candidate.nameKnown && (!a.nameKnown || a.name != candidate.name) {
		return false
	}
	if candidate.sizeKnown && (!a.sizeKnown || a.size != candidate.size) {
		return false
	}
	if candidate.filesKnown && (!a.filesKnown || a.files != candidate.files) {
		return false
	}
	return true
}

func (a incomingApproval) complete() bool {
	return a.nameKnown && a.sizeKnown && (!a.folder || a.filesKnown)
}

type incomingMember struct {
	path string
	size int64
}

type incomingMeta struct {
	transferID string
	name       string
	size       int64
	sizeKnown  bool
	folderID   string
	files      int
	filesKnown bool
	relPath    string
	memberSize int64
	baseFiles  int
	baseBytes  int64
	probe      bool
	memberDone bool
}

func (n *Node) ReceiveSettings() ReceiveSettings {
	n.mu.Lock()
	defer n.mu.Unlock()
	return ReceiveSettings{AskBeforeAccepting: n.cfg.AskBeforeReceiving}
}

func (n *Node) SetAskBeforeReceiving(on bool) error {
	n.mu.Lock()
	old := n.cfg.AskBeforeReceiving
	n.cfg.AskBeforeReceiving = on
	err := n.cfg.Save()
	var closed []string
	if err != nil {
		n.cfg.AskBeforeReceiving = old
	}
	if err == nil && !on {
		for id, p := range n.incoming {
			p.timer.Stop()
			closed = append(closed, id)
		}
		n.incoming = map[string]*pendingIncoming{}
		for _, decision := range n.incomingDecisions {
			decision.timer.Stop()
		}
		n.incomingDecisions = map[string]*incomingDecision{}
	}
	n.mu.Unlock()
	for _, id := range closed {
		n.emit(EventIncomingClosed, id)
	}
	return err
}

func (n *Node) PendingIncoming() []IncomingRequest {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]IncomingRequest, 0, len(n.incoming))
	for _, p := range n.incoming {
		out = append(out, p.req)
	}
	return out
}

func (n *Node) AnswerIncoming(requestID string, accept bool) {
	n.mu.Lock()
	p := n.incoming[requestID]
	if p != nil {
		delete(n.incoming, requestID)
		p.timer.Stop()
		decision := &incomingDecision{
			allow: accept, approval: p.approval, expires: time.Now().Add(incomingDecisionTTL),
			members: map[string]incomingMember{}, files: p.approval.baseFiles, bytes: p.approval.baseBytes,
		}
		decision.timer = time.AfterFunc(incomingDecisionTTL, func() {
			n.mu.Lock()
			if n.incomingDecisions[p.key] == decision {
				delete(n.incomingDecisions, p.key)
			}
			n.mu.Unlock()
		})
		n.incomingDecisions[p.key] = decision
	}
	n.mu.Unlock()
	if p != nil {
		n.emit(EventIncomingClosed, requestID)
	}
}

func incomingKey(peerID string, meta incomingMeta) string {
	if meta.folderID != "" {
		return peerID + "\x00folder\x00" + meta.folderID
	}
	return peerID + "\x00file\x00" + meta.transferID
}

// authorizeIncoming returns true when the transfer may proceed. A first
// attempt creates a prompt and gets 409, which existing senders retry. This
// keeps HTTP handlers non-blocking while the person decides.
func (n *Node) authorizeIncoming(w http.ResponseWriter, from config.TrustedPeer, meta incomingMeta) bool {
	approval := incomingApproval{
		name: safeFileName(meta.name), nameKnown: meta.name != "",
		size: meta.size, sizeKnown: meta.sizeKnown, folder: meta.folderID != "",
		files: meta.files, filesKnown: meta.filesKnown,
		baseFiles: meta.baseFiles, baseBytes: meta.baseBytes,
	}
	if approval.name == "file" && approval.folder {
		approval.name = "folder"
	}
	member := incomingMember{size: meta.memberSize}
	if approval.folder {
		var err error
		member.path, err = canonicalFolderMember(meta.relPath)
		if err != nil || meta.memberSize < 0 || !validTransferID(meta.transferID) {
			http.Error(w, "bad folder member metadata", http.StatusBadRequest)
			return false
		}
	}
	n.mu.Lock()
	if !n.cfg.AskBeforeReceiving {
		n.mu.Unlock()
		return true
	}
	now := time.Now()
	for key, decision := range n.incomingDecisions {
		if now.After(decision.expires) {
			decision.timer.Stop()
			delete(n.incomingDecisions, key)
		}
	}
	key := incomingKey(from.ID, meta)
	if decision, ok := n.incomingDecisions[key]; ok {
		if decision.approval.matches(approval) {
			if decision.allow && approval.folder {
				if old, exists := decision.members[meta.transferID]; exists {
					if old != member {
						decision.timer.Stop()
						delete(n.incomingDecisions, key)
						n.mu.Unlock()
						http.Error(w, "approved folder member metadata changed", http.StatusConflict)
						return false
					}
				} else {
					if !decision.approval.complete() && meta.probe {
						// A sparse legacy offset may proceed only far enough for
						// the sender to reveal complete metadata on its POST.
					} else if !decision.approval.complete() || (!meta.memberDone && (decision.files >= decision.approval.files ||
						member.size > decision.approval.size-decision.bytes)) {
						decision.timer.Stop()
						delete(n.incomingDecisions, key)
						n.mu.Unlock()
						http.Error(w, "folder exceeds approved file count or size", http.StatusForbidden)
						return false
					}
					if decision.approval.complete() {
						decision.members[meta.transferID] = member
						if !meta.memberDone {
							decision.files++
							decision.bytes += member.size
						}
					}
				}
			}
			allowed := decision.allow
			n.mu.Unlock()
			if allowed {
				return true
			}
			http.Error(w, "incoming transfer declined", http.StatusForbidden)
			return false
		}
		// A legacy sparse approval cannot authorize a later complete upload.
		// Retire it and ask for exact metadata. Any change to an already exact
		// approval is rejected without offering a replacement prompt.
		if !decision.approval.complete() && approval.complete() {
			decision.timer.Stop()
			delete(n.incomingDecisions, key)
		} else {
			decision.timer.Stop()
			delete(n.incomingDecisions, key)
			n.mu.Unlock()
			http.Error(w, "incoming transfer metadata changed after approval", http.StatusConflict)
			return false
		}
	}
	for _, p := range n.incoming {
		if p.key == key {
			if !p.approval.matches(approval) {
				n.mu.Unlock()
				http.Error(w, "incoming transfer metadata changed while awaiting approval", http.StatusConflict)
				return false
			}
			n.mu.Unlock()
			http.Error(w, "waiting for approval on the receiving device", http.StatusConflict)
			return false
		}
	}
	if len(n.incoming) >= maxPendingIncoming {
		n.mu.Unlock()
		http.Error(w, "too many incoming transfers waiting for approval", http.StatusTooManyRequests)
		return false
	}
	idb := make([]byte, 8)
	if _, err := rand.Read(idb); err != nil {
		n.mu.Unlock()
		http.Error(w, "could not create approval request", http.StatusInternalServerError)
		return false
	}
	id := hex.EncodeToString(idb)
	req := IncomingRequest{
		ID: id, PeerID: from.ID, PeerName: from.Name, Fingerprint: identity.Short(from.Fingerprint),
		Name: approval.name, Size: approval.size, Folder: approval.folder, Files: approval.files,
	}
	p := &pendingIncoming{req: req, key: key, approval: approval}
	p.timer = time.AfterFunc(incomingRequestTTL, func() {
		n.mu.Lock()
		if n.incoming[id] == p {
			delete(n.incoming, id)
			n.mu.Unlock()
			n.emit(EventIncomingClosed, id)
			return
		}
		n.mu.Unlock()
	})
	n.incoming[id] = p
	n.mu.Unlock()
	n.emit(EventIncomingRequest, req)
	http.Error(w, "waiting for approval on the receiving device", http.StatusConflict)
	return false
}

func (n *Node) completeIncomingApproval(peerID string, meta incomingMeta) {
	key := incomingKey(peerID, meta)
	n.mu.Lock()
	if decision := n.incomingDecisions[key]; decision != nil {
		decision.timer.Stop()
		delete(n.incomingDecisions, key)
	}
	n.mu.Unlock()
}

func canonicalFolderMember(rel string) (string, error) {
	parts := strings.Split(rel, "/")
	if len(parts) < 2 {
		return "", errors.New("bad folder member path")
	}
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		clean = append(clean, safeFileName(part))
	}
	if len(clean) < 2 {
		return "", errors.New("bad folder member path")
	}
	return strings.Join(clean, "/"), nil
}

func folderRootName(rel string) string {
	root, _, _ := strings.Cut(rel, "/")
	return root
}
