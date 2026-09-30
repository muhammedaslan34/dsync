// Package node is the long-running core of dsync: it receives messages,
// answers discovery, keeps the list of nearby devices and the message
// history, and reports changes through an event callback. The GUI and
// `dsync serve` are thin wrappers around it.
package node

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"dsync/internal/client"
	"dsync/internal/config"
	"dsync/internal/discovery"
	"dsync/internal/identity"
	"dsync/internal/proto"
)

// Events passed to the emit callback.
const (
	EventPeers   = "peers"   // data: []Peer
	EventMessage = "message" // data: Message
	// EventMessageUpdate reports a changed message, e.g. a finished transfer.
	EventMessageUpdate = "message:update" // data: Message
	EventProgress      = "progress"       // data: Progress
)

const (
	scanInterval = 5 * time.Second
	discoverWait = 1500 * time.Millisecond
	offlineAfter = 3*scanInterval + time.Second
	maxHistory   = 1000
)

// Peer is another device, as shown in the device list.
type Peer struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	OS       string `json:"os"`
	Addr     string `json:"addr"`
	Manual   bool   `json:"manual"`
	Online   bool   `json:"online"` // we can reach it
	Paired   bool   `json:"paired"`
	LastSeen int64  `json:"lastSeen"` // unix ms, when we last reached it
	// LastHeard is when it last contacted us (unix ms).
	LastHeard int64 `json:"lastHeard"`
	// OneWay means it reached us recently but we can't reach it: usually
	// its firewall blocks incoming connections.
	OneWay bool `json:"oneWay"`
}

// oneWayWindow is how recently a device must have contacted us for "we
// can't reach it" to be reported as one-way rather than just offline.
const oneWayWindow = 3 * time.Minute

// Message is a text sent to or received from a peer.
type Message struct {
	ID       int64     `json:"id"`
	Time     int64     `json:"time"` // unix ms
	PeerID   string    `json:"peerId"`
	PeerName string    `json:"peerName"`
	Incoming bool      `json:"incoming"`
	Text     string    `json:"text,omitempty"`
	File     *FileInfo `json:"file,omitempty"`
}

type Node struct {
	cfg  *config.Config
	id   *identity.Identity
	cl   *client.Client
	emit func(event string, data any)

	mu        sync.Mutex
	peers     map[string]*Peer
	history   []Message
	transfers map[int64]context.CancelCauseFunc
	pending   map[string]*pendingPair       // incoming pair requests by request id
	pairing   map[string]context.CancelFunc // outgoing pair requests by peer id
	parts     map[string]*partClaim         // partial files being written, by path
	// folderReps are progress callbacks of incoming folders, by message id.
	folderReps map[int64]func(int64)

	clip clipState
	// controls are remote control setups in progress, by peer id.
	controls map[string]context.CancelFunc
}

// New loads (or creates) this device's identity and history.
func New(cfg *config.Config, emit func(event string, data any)) (*Node, error) {
	if emit == nil {
		emit = func(string, any) {}
	}
	id, err := identity.Load(cfg.Dir())
	if err != nil {
		return nil, err
	}
	n := &Node{
		cfg: cfg, id: id, cl: client.New(id), emit: emit,
		peers:      map[string]*Peer{},
		transfers:  map[int64]context.CancelCauseFunc{},
		pending:    map[string]*pendingPair{},
		pairing:    map[string]context.CancelFunc{},
		parts:      map[string]*partClaim{},
		folderReps: map[int64]func(int64){},
		controls:   map[string]context.CancelFunc{},
	}
	n.loadHistory()
	return n, nil
}

func (n *Node) Self() proto.Device {
	n.mu.Lock()
	defer n.mu.Unlock()
	return proto.Device{ID: n.cfg.ID, Name: n.cfg.Name, OS: runtime.GOOS, Port: n.cfg.Port}
}

func (n *Node) SetName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("name cannot be empty")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.cfg.Name = name
	return n.cfg.Save()
}

// Run serves until ctx is cancelled or a listener fails.
func (n *Node) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", n.cfg.Port))
	if err != nil {
		return fmt.Errorf("listen tcp :%d: %w (is dsync already running?)", n.cfg.Port, err)
	}
	ln = tls.NewListener(ln, n.tlsConfig())
	httpSrv := &http.Server{Handler: n.handler(), ReadHeaderTimeout: 10 * time.Second}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errc := make(chan error, 2)
	go func() { errc <- discovery.Respond(ctx, n.Self) }()
	go func() {
		if err := httpSrv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	go n.scanLoop(ctx)
	go n.cleanParts()
	go n.cleanOutbox()
	go n.clipLoop(ctx)

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-errc:
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelShutdown()
	httpSrv.Shutdown(shutdownCtx)
	return runErr
}

func (n *Node) scanLoop(ctx context.Context) {
	t := time.NewTicker(scanInterval)
	defer t.Stop()
	for {
		n.Scan(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Scan looks for devices on the network and checks manually added ones.
func (n *Node) Scan(ctx context.Context) {
	self := n.Self()
	found, _ := discovery.Discover(ctx, self, discoverWait)

	seen := map[string]Peer{}
	for _, p := range found {
		seen[p.ID] = Peer{ID: p.ID, Name: p.Name, OS: p.OS, Addr: p.Addr()}
	}
	n.mu.Lock()
	manual := slices.Clone(n.cfg.ManualPeers)
	n.mu.Unlock()
	for _, addr := range manual {
		infoCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		d, _, err := n.cl.Info(infoCtx, addr)
		cancel()
		if err == nil && d.ID != self.ID {
			seen[d.ID] = Peer{ID: d.ID, Name: d.Name, OS: d.OS, Addr: addr, Manual: true}
		}
	}
	for _, p := range n.probeMissing(ctx, seen) {
		seen[p.ID] = p
	}

	now := time.Now().UnixMilli()
	n.mu.Lock()
	for id, p := range seen {
		p.LastSeen = now
		n.peers[id] = &p
	}
	for _, p := range n.peers {
		p.Online = now-p.LastSeen < offlineAfter.Milliseconds()
		p.OneWay = !p.Online && now-p.LastHeard < oneWayWindow.Milliseconds()
	}
	n.mu.Unlock()
	n.emit(EventPeers, n.Peers())
}

// probeMissing tries paired devices that broadcast discovery didn't find
// at their last known address, since some networks drop broadcasts.
func (n *Node) probeMissing(ctx context.Context, seen map[string]Peer) []Peer {
	n.mu.Lock()
	var try []Peer
	for id, p := range n.peers {
		if _, ok := seen[id]; ok || p.Addr == "" {
			continue
		}
		if _, paired := n.cfg.TrustedByID(id); paired {
			try = append(try, *p)
		}
	}
	n.mu.Unlock()

	var (
		mu    sync.Mutex
		found []Peer
		wg    sync.WaitGroup
	)
	for _, p := range try {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			defer cancel()
			d, _, err := n.cl.Info(pctx, p.Addr)
			if err != nil || d.ID != p.ID {
				return
			}
			mu.Lock()
			found = append(found, Peer{ID: d.ID, Name: d.Name, OS: d.OS, Addr: p.Addr, Manual: p.Manual})
			mu.Unlock()
		}()
	}
	wg.Wait()
	return found
}

func (n *Node) Peers() []Peer {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]Peer, 0, len(n.peers))
	for _, p := range n.peers {
		c := *p
		_, c.Paired = n.cfg.TrustedByID(p.ID)
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// AddPeer remembers a device by address, for networks where discovery
// doesn't work.
func (n *Node) AddPeer(ctx context.Context, addr string) (Peer, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return Peer{}, errors.New("address is empty")
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, strconv.Itoa(proto.DefaultHTTPPort))
	}
	d, _, err := n.cl.Info(ctx, addr)
	if err != nil {
		return Peer{}, fmt.Errorf("could not reach %s: %w", addr, err)
	}
	if d.ID == n.Self().ID {
		return Peer{}, errors.New("that address is this device")
	}

	p := Peer{ID: d.ID, Name: d.Name, OS: d.OS, Addr: addr, Manual: true, Online: true, LastSeen: time.Now().UnixMilli()}
	n.mu.Lock()
	n.peers[p.ID] = &p
	var saveErr error
	if !slices.Contains(n.cfg.ManualPeers, addr) {
		n.cfg.ManualPeers = append(n.cfg.ManualPeers, addr)
		saveErr = n.cfg.Save()
	}
	n.mu.Unlock()
	n.emit(EventPeers, n.Peers())
	_, p.Paired = n.trusted(p.ID)
	return p, saveErr
}

// trusted returns the pinned key for a paired device.
func (n *Node) trusted(peerID string) (config.TrustedPeer, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.cfg.TrustedByID(peerID)
}

// ForgetPeer removes a device from the list. Devices found by discovery
// come back on the next scan while they are online.
func (n *Node) ForgetPeer(id string) error {
	n.mu.Lock()
	p, ok := n.peers[id]
	var err error
	if ok {
		delete(n.peers, id)
		if p.Manual {
			n.cfg.ManualPeers = slices.DeleteFunc(n.cfg.ManualPeers, func(a string) bool { return a == p.Addr })
			err = n.cfg.Save()
		}
	}
	n.mu.Unlock()
	n.emit(EventPeers, n.Peers())
	return err
}

func (n *Node) SendText(ctx context.Context, peerID, text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("message is empty")
	}
	if len(text) > proto.MaxTextBytes {
		// Too big for a message; send it as a text file instead.
		name := "Message " + time.Now().Format("2006-01-02 15-04-05") + ".txt"
		return n.SendData(ctx, peerID, name, []byte(text))
	}
	n.mu.Lock()
	p, ok := n.peers[peerID]
	var addr, name string
	if ok {
		addr, name = p.Addr, p.Name
	}
	n.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown device %s", peerID)
	}
	t, paired := n.trusted(peerID)
	if !paired {
		return fmt.Errorf("pair with %s first", name)
	}

	self := n.Self()
	msg := proto.TextMessage{FromID: self.ID, FromName: self.Name, FromPort: self.Port, Text: text}
	if err := n.cl.SendText(ctx, addr, t.Fingerprint, msg); err != nil {
		return fmt.Errorf("send to %s: %w", name, err)
	}
	n.addMessage(Message{PeerID: peerID, PeerName: name, Text: text})
	return nil
}

// learnPeer records a paired device that contacted us, even if discovery
// can't see it (e.g. over a VPN), and keeps its name current. It returns the
// name to show.
func (n *Node) learnPeer(from config.TrustedPeer, name string, port int, remoteAddr string) string {
	if name == "" {
		name = from.Name
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	n.mu.Lock()
	if name != from.Name {
		from.Name = name
		n.cfg.Trust(from)
		n.cfg.Save()
	}
	now := time.Now().UnixMilli()
	p, known := n.peers[from.ID]
	switch {
	case known:
		p.Name, p.LastHeard = name, now
		if err == nil && port > 0 && p.Addr == "" {
			p.Addr = net.JoinHostPort(host, strconv.Itoa(port))
		}
	case err == nil && port > 0:
		// Whether we can reach it back is found out by the next scan.
		n.peers[from.ID] = &Peer{
			ID: from.ID, Name: name, Addr: net.JoinHostPort(host, strconv.Itoa(port)),
			LastHeard: now,
		}
	}
	n.mu.Unlock()
	if !known {
		n.emit(EventPeers, n.Peers())
	}
	return name
}

func (n *Node) History() []Message {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.history)
}

func (n *Node) addMessage(m Message) Message {
	n.mu.Lock()
	m.Time = time.Now().UnixMilli()
	if len(n.history) > 0 {
		m.ID = n.history[len(n.history)-1].ID + 1
	} else {
		m.ID = 1
	}
	n.history = append(n.history, m)
	if len(n.history) > maxHistory {
		n.history = slices.Clone(n.history[len(n.history)-maxHistory:])
	}
	n.saveHistoryLocked()
	n.mu.Unlock()
	n.emit(EventMessage, m)
	return m
}

// updateFile changes the file info of a message and reports it.
func (n *Node) updateFile(id int64, change func(f *FileInfo)) {
	n.mu.Lock()
	var updated *Message
	for i := len(n.history) - 1; i >= 0; i-- {
		if n.history[i].ID == id && n.history[i].File != nil {
			f := *n.history[i].File
			change(&f)
			n.history[i].File = &f
			updated = &n.history[i]
			break
		}
	}
	var m Message
	if updated != nil {
		m = *updated
		n.saveHistoryLocked()
	}
	n.mu.Unlock()
	if updated != nil {
		n.emit(EventMessageUpdate, m)
	}
}

func (n *Node) historyPath() string {
	return filepath.Join(n.cfg.Dir(), "history.json")
}

func (n *Node) loadHistory() {
	data, err := os.ReadFile(n.historyPath())
	if err != nil {
		return
	}
	json.Unmarshal(data, &n.history)
	// Transfers that were running when the app closed didn't finish.
	for i, m := range n.history {
		if m.File != nil && m.File.Status == StatusActive {
			f := *m.File
			f.Status, f.Error = StatusFailed, "interrupted"
			n.history[i].File = &f
		}
	}
}

func (n *Node) saveHistoryLocked() {
	data, _ := json.Marshal(n.history)
	os.WriteFile(n.historyPath(), data, 0o600)
}

// LocalAddrs lists this device's addresses, to show the user what to type
// on the other machine.
func (n *Node) LocalAddrs() []discovery.LocalAddr {
	return discovery.LocalAddrs()
}

// FindOnNetwork checks every address on the local network for dsync. It
// only reports what it finds; the caller decides which ones to add.
func (n *Node) FindOnNetwork(ctx context.Context) []Peer {
	self := n.Self()
	info := func(ctx context.Context, addr string) (proto.Device, error) {
		d, _, err := n.cl.Info(ctx, addr)
		return d, err
	}
	found := discovery.Sweep(ctx, self.ID, self.Port, info)
	out := make([]Peer, len(found))
	for i, p := range found {
		out[i] = Peer{ID: p.ID, Name: p.Name, OS: p.OS, Addr: p.Addr(), Online: true}
	}
	return out
}

// QuitOnClose reports whether closing the window quits instead of keeping
// dsync running in the tray.
func (n *Node) QuitOnClose() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.cfg.QuitOnClose
}

func (n *Node) SetQuitOnClose(quit bool) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.cfg.QuitOnClose = quit
	return n.cfg.Save()
}

// FirstTrayHint reports whether the "still running in the tray" notice
// should be shown, and records that it has been.
func (n *Node) FirstTrayHint() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.cfg.TrayHintShown {
		return false
	}
	n.cfg.TrayHintShown = true
	n.cfg.Save()
	return true
}

// OnlinePaired counts paired devices that are online right now.
func (n *Node) OnlinePaired() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	count := 0
	for _, p := range n.peers {
		if _, ok := n.cfg.TrustedByID(p.ID); ok && p.Online {
			count++
		}
	}
	return count
}

// EventHistoryCleared reports cleared messages (data: peer id, "" = all).
const EventHistoryCleared = "history:cleared"

// ClearHistory removes the messages with peerID, or with everyone if
// peerID is "". Received files stay on disk; transfers still running are
// kept so they can finish or be canceled. Pasted data that was kept only
// for sending again is deleted.
func (n *Node) ClearHistory(peerID string) error {
	outbox := n.outboxDir() + string(filepath.Separator)
	n.mu.Lock()
	kept := n.history[:0:0]
	var orphans []string
	for _, m := range n.history {
		if peerID != "" && m.PeerID != peerID {
			kept = append(kept, m)
			continue
		}
		if m.File != nil && m.File.Status == StatusActive {
			kept = append(kept, m)
			continue
		}
		if m.File != nil && !m.Incoming && strings.HasPrefix(m.File.Path, outbox) {
			orphans = append(orphans, m.File.Path)
		}
	}
	n.history = kept
	n.saveHistoryLocked()
	n.mu.Unlock()
	for _, p := range orphans {
		os.Remove(p)
	}
	n.emit(EventHistoryCleared, peerID)
	return nil
}
