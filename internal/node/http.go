package node

import (
	"crypto/tls"
	"encoding/json"
	"net/http"

	"dsync/internal/config"
	"dsync/internal/discovery"
	"dsync/internal/identity"
	"dsync/internal/proto"
)

// tlsConfig is the server side of every connection: TLS 1.3 with our
// certificate, and a certificate required from the client so we know which
// key is calling. Whether that key is trusted is decided per request.
func (n *Node) tlsConfig() *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{n.id.Cert},
		MinVersion:   tls.VersionTLS13,
		ClientAuth:   tls.RequireAnyClientCert,
		NextProtos:   []string{"http/1.1"},
	}
}

func (n *Node) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/info", n.handleInfo)
	mux.HandleFunc("POST /api/v1/pair", n.handlePair)
	mux.HandleFunc("POST /api/v1/unpair", n.paired(n.handleUnpair))
	mux.HandleFunc("POST /api/v1/text", n.paired(n.handleText))
	mux.HandleFunc("POST /api/v1/file", n.paired(n.receiveFile))
	mux.HandleFunc("POST /api/v1/file/offset", n.paired(n.handleFileOffset))
	mux.HandleFunc("POST /api/v1/folder/end", n.paired(n.handleFolderEnd))
	mux.HandleFunc("POST /api/v1/clipboard", n.paired(n.handleClipboard))
	mux.HandleFunc("POST /api/v1/control/status", n.controlAllowed(n.handleControlStatus))
	mux.HandleFunc("POST /api/v1/control/start-sunshine", n.controlAllowed(n.handleStartSunshine))
	mux.HandleFunc("POST /api/v1/control/pin", n.controlAllowed(n.handleControlPIN))
	mux.HandleFunc("POST /api/v1/control/configure", n.controlAllowed(n.handleControlConfigure))
	return mux
}

// controlAllowed requires both pairing and the separate remote-control
// permission granted locally for that peer.
func (n *Node) controlAllowed(h pairedHandler) http.HandlerFunc {
	return n.paired(func(w http.ResponseWriter, r *http.Request, from config.TrustedPeer) {
		if !from.CanControl {
			http.Error(w, "remote control is not allowed for this device", http.StatusForbidden)
			return
		}
		h(w, r, from)
	})
}

// callerFingerprint returns the key fingerprint of the connecting device.
func callerFingerprint(r *http.Request) string {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return ""
	}
	fp, _ := identity.Fingerprint(r.TLS.PeerCertificates[0].Raw)
	return fp
}

type pairedHandler func(w http.ResponseWriter, r *http.Request, from config.TrustedPeer)

// paired only lets devices we have paired with through.
func (n *Node) paired(h pairedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fp := callerFingerprint(r)
		n.mu.Lock()
		t, ok := n.cfg.TrustedByFingerprint(fp)
		n.mu.Unlock()
		if fp == "" || !ok {
			http.Error(w, "not paired with this device", http.StatusForbidden)
			return
		}
		h(w, r, t)
	}
}

func (n *Node) handleInfo(w http.ResponseWriter, r *http.Request) {
	device := n.Self()
	fp := callerFingerprint(r)
	n.mu.Lock()
	_, trusted := n.cfg.TrustedByFingerprint(fp)
	n.mu.Unlock()
	if fp == "" || !trusted {
		device = discovery.PublicDevice(device)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(device)
}

func (n *Node) handleText(w http.ResponseWriter, r *http.Request, from config.TrustedPeer) {
	r.Body = http.MaxBytesReader(w, r.Body, proto.MaxTextBytes+4096)
	var m proto.TextMessage
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if m.Text == "" {
		http.Error(w, "empty text", http.StatusBadRequest)
		return
	}
	// The id comes from the pinned key, never from the message body.
	name := n.learnPeer(from, m.FromName, m.FromPort, r.RemoteAddr)
	n.addMessage(Message{PeerID: from.ID, PeerName: name, Incoming: true, Text: m.Text})
	w.WriteHeader(http.StatusNoContent)
}
