package node

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strings"
	"time"

	"dsync/internal/config"
	"dsync/internal/control"
	"dsync/internal/proto"
)

// Remote control: Moonlight on this computer shows and controls another
// computer's desktop, served by Sunshine there. This file runs the steps
// (checks, starting Sunshine, the one-time pairing, launching the stream)
// and answers the other side's questions.

const (
	// EventControl reports progress of StartControl.
	EventControl = "control" // data: ControlState
	// EventControlPIN asks the user here to type a PIN into Sunshine,
	// because dsync has no Sunshine login to do it itself.
	EventControlPIN = "control:pin" // data: ControlPIN

	pairWait = 2 * time.Minute
)

// ControlState is one step of setting up remote control.
type ControlState struct {
	PeerID  string `json:"peerId"`
	Step    string `json:"step"` // checking, starting, pairing, streaming, done, error, canceled
	Message string `json:"message,omitempty"`
	PIN     string `json:"pin,omitempty"`
	Hint    string `json:"hint,omitempty"` // how to fix an error, e.g. an install command
}

// ControlPIN is shown on the controlled computer when the PIN has to be
// entered by hand.
type ControlPIN struct {
	PeerName string `json:"peerName"`
	PIN      string `json:"pin"`
	URL      string `json:"url"`
}

// HostInfo describes this computer's remote control setup, for settings.
type HostInfo struct {
	Moonlight         bool   `json:"moonlight"`
	MoonlightHint     string `json:"moonlightHint,omitempty"`
	SunshineInstalled bool   `json:"sunshineInstalled"`
	SunshineRunning   bool   `json:"sunshineRunning"`
	SunshineHint      string `json:"sunshineHint,omitempty"`
	SunshineLogin     bool   `json:"sunshineLogin"` // a login is saved
	// SunshineBlocked means this computer's firewall (ufw) keeps other
	// computers from reaching Sunshine.
	SunshineBlocked bool   `json:"sunshineBlocked"`
	SunshineURL     string `json:"sunshineUrl"`
}

func (n *Node) sunshineAPI() (control.SunshineAPI, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	api := control.SunshineAPI{User: n.cfg.SunshineUser, Password: n.cfg.SunshinePassword, Base: n.cfg.SunshineWeb}
	return api, api.User != ""
}

// HostInfo reports what this computer has for remote control.
func (n *Node) HostInfo() HostInfo {
	_, ml := control.FindMoonlight()
	_, sun := control.FindSunshine()
	_, login := n.sunshineAPI()
	h := HostInfo{
		Moonlight:         ml,
		SunshineInstalled: sun,
		SunshineRunning:   control.SunshineRunning(),
		SunshineLogin:     login,
		SunshineBlocked:   sun && control.SunshineFirewallBlocked(),
		SunshineURL:       fmt.Sprintf("https://localhost:%d", control.SunshineWebPort),
	}
	if !ml {
		h.MoonlightHint = control.InstallHint("moonlight")
	}
	if !sun {
		h.SunshineHint = control.InstallHint("sunshine")
	}
	return h
}

// StartLocalSunshine starts Sunshine on this computer if it isn't running,
// e.g. before opening its setup page.
func (n *Node) StartLocalSunshine() error {
	if control.SunshineRunning() {
		return nil
	}
	s, ok := control.FindSunshine()
	if !ok {
		return errors.New("sunshine is not installed")
	}
	return s.Start()
}

// SetSunshineLogin saves (after checking it works) the Sunshine web UI login
// that lets paired devices finish remote control setup on their own. An
// empty user removes it.
func (n *Node) SetSunshineLogin(ctx context.Context, user, password string) error {
	if user != "" {
		if base := n.cfg.SunshineWeb; base == "" {
			if err := n.StartLocalSunshine(); err != nil {
				return fmt.Errorf("could not start Sunshine: %w", err)
			}
		}
		n.mu.Lock()
		base := n.cfg.SunshineWeb
		n.mu.Unlock()
		api := control.SunshineAPI{User: user, Password: password, Base: base}
		if err := api.CheckLogin(ctx); err != nil {
			if errors.Is(err, control.ErrBadLogin) {
				return err
			}
			return fmt.Errorf("could not reach Sunshine: %w", err)
		}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.cfg.SunshineUser, n.cfg.SunshinePassword = user, password
	return n.cfg.Save()
}

// --- the controlled computer ---

func (n *Node) hostStatus(ctx context.Context) proto.ControlStatus {
	_, installed := control.FindSunshine()
	api, login := n.sunshineAPI()
	running := control.SunshineRunning()
	st := proto.ControlStatus{SunshineInstalled: installed || running, SunshineRunning: running, AutoPIN: login}
	if !login || !running {
		return st
	}
	// With the login, report the screens and current settings, so the
	// other computer can offer a choice.
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cfg, err := api.Config(cctx)
	if err != nil {
		return st
	}
	st.Configurable = true
	st.Screen, _ = cfg["output_name"].(string)
	st.MatchResolution = cfg["dd_resolution_option"] == "auto"
	if ds, err := control.Displays(cfg); err == nil {
		for _, d := range ds {
			st.Displays = append(st.Displays, proto.Display(d))
		}
	}
	return st
}

func (n *Node) handleControlStatus(w http.ResponseWriter, r *http.Request, from config.TrustedPeer) {
	writeJSON(w, n.hostStatus(r.Context()))
}

// handleControlConfigure changes this computer's Sunshine settings for a
// paired device about to control it: which screen it shows, and whether
// this computer switches to the stream's resolution (which makes things
// look bigger there). Sunshine restarts only if something changed.
func (n *Node) handleControlConfigure(w http.ResponseWriter, r *http.Request, from config.TrustedPeer) {
	var req proto.ControlConfigure
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	api, login := n.sunshineAPI()
	if !login {
		http.Error(w, "save the Sunshine login in dsync on "+n.Self().Name+" first", http.StatusConflict)
		return
	}
	cfg, err := api.Config(r.Context())
	if err != nil {
		http.Error(w, "could not read Sunshine's settings: "+err.Error(), http.StatusBadGateway)
		return
	}
	changed := false
	if cur, _ := cfg["output_name"].(string); cur != req.Screen {
		cfg["output_name"] = req.Screen // "" removes it: the main screen
		changed = true
	}
	want := "disabled"
	if req.MatchResolution {
		want = "auto"
	}
	if cur, _ := cfg["dd_resolution_option"].(string); cur != want && !(cur == "" && want == "disabled") {
		cfg["dd_resolution_option"] = want
		changed = true
	}
	if changed {
		if err := api.SaveConfig(r.Context(), cfg); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if err := api.Restart(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		// Wait for it to go away and come back.
		time.Sleep(1500 * time.Millisecond)
		for range 60 {
			if control.SunshineRunning() {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
	writeJSON(w, n.hostStatus(r.Context()))
}

func (n *Node) handleStartSunshine(w http.ResponseWriter, r *http.Request, from config.TrustedPeer) {
	s, ok := control.FindSunshine()
	if !ok && !control.SunshineRunning() {
		http.Error(w, "sunshine is not installed", http.StatusNotFound)
		return
	}
	if err := s.Start(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, n.hostStatus(r.Context()))
}

// handleControlPIN takes the PIN a paired device's Moonlight is pairing
// with, and enters it into Sunshine here (or asks the user to).
func (n *Node) handleControlPIN(w http.ResponseWriter, r *http.Request, from config.TrustedPeer) {
	var req proto.ControlPINRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || len(req.PIN) != 4 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := n.learnPeer(from, req.FromName, 0, r.RemoteAddr)
	manual := func() {
		n.emit(EventControlPIN, ControlPIN{PeerName: name, PIN: req.PIN, URL: fmt.Sprintf("https://localhost:%d/pin", control.SunshineWebPort)})
	}

	api, login := n.sunshineAPI()
	if !login {
		manual()
		writeJSON(w, proto.ControlPINResult{})
		return
	}
	fromIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	err := api.SubmitPIN(r.Context(), req.PIN, "dsync: "+name, fromIP, 40*time.Second)
	if err != nil {
		manual() // let someone here finish it
		writeJSON(w, proto.ControlPINResult{Error: err.Error()})
		return
	}
	writeJSON(w, proto.ControlPINResult{Auto: true})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// --- the controlling computer ---

// ControlOptions are chosen before controlling a device.
type ControlOptions struct {
	Screen string `json:"screen"` // the device's Sunshine output_name; "" = keep its current one
	// Resolution is the stream's size, e.g. "1280x720"; "" = Moonlight's
	// setting. A smaller size makes things look bigger: on Windows the
	// controlled PC switches to it while controlled.
	Resolution  string `json:"resolution"`
	FPS         int    `json:"fps"`
	DisplayMode string `json:"displayMode"`
}

// ControlInfo tells the options dialog what the device offers.
type ControlInfo struct {
	OS           string          `json:"os"`
	Configurable bool            `json:"configurable"`
	Displays     []proto.Display `json:"displays"`
	Screen       string          `json:"screen"`
	Error        string          `json:"error,omitempty"`
}

// ControlInfo asks a device which screens it has, for the options dialog.
func (n *Node) ControlInfo(ctx context.Context, peerID string) ControlInfo {
	n.mu.Lock()
	p := n.peers[peerID]
	var peer Peer
	if p != nil {
		peer = *p
	}
	n.mu.Unlock()
	t, paired := n.trusted(peerID)
	if p == nil || !paired {
		return ControlInfo{Error: "not paired"}
	}
	info := ControlInfo{OS: peer.OS}
	st, err := n.cl.ControlStatus(ctx, n.peerAddr(peer), t.Fingerprint)
	if err != nil {
		info.Error = err.Error()
		return info
	}
	info.Configurable, info.Displays, info.Screen = st.Configurable, st.Displays, st.Screen
	return info
}

// StartControl opens a Moonlight window controlling peerID's desktop,
// setting things up first if needed. Progress arrives as EventControl.
func (n *Node) StartControl(ctx context.Context, peerID string, opts ControlOptions) error {
	n.mu.Lock()
	p, ok := n.peers[peerID]
	var peer Peer
	if ok {
		peer = *p
	}
	n.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown device %s", peerID)
	}
	t, paired := n.trusted(peerID)
	if !paired {
		return fmt.Errorf("pair with %s first", peer.Name)
	}

	ctx, cancel := context.WithCancel(ctx)
	n.mu.Lock()
	if old := n.controls[peerID]; old != nil {
		old()
	}
	n.controls[peerID] = cancel
	n.mu.Unlock()

	go func() {
		defer func() {
			cancel()
			n.mu.Lock()
			delete(n.controls, peerID)
			n.mu.Unlock()
		}()
		state := func(step, msg string) { n.emit(EventControl, ControlState{PeerID: peerID, Step: step, Message: msg}) }
		err := n.runControl(ctx, peer, t.Fingerprint, opts, state)
		switch {
		case ctx.Err() != nil:
			state("canceled", "")
		case err != nil:
			var ce *controlError
			st := ControlState{PeerID: peerID, Step: "error", Message: err.Error()}
			if errors.As(err, &ce) {
				st.Hint = ce.hint
			}
			n.emit(EventControl, st)
		default:
			state("done", fmt.Sprintf("Moonlight is showing %s's screen.", peer.Name))
		}
	}()
	return nil
}

// CancelControl stops setting up remote control of peerID.
func (n *Node) CancelControl(peerID string) {
	n.mu.Lock()
	cancel := n.controls[peerID]
	n.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

type controlError struct {
	msg, hint string
}

func (e *controlError) Error() string { return e.msg }

// Hint says how to fix the problem, e.g. an install command.
func (e *controlError) Hint() string { return e.hint }

func (n *Node) runControl(ctx context.Context, peer Peer, fp string, opts ControlOptions, state func(step, msg string)) error {
	state("checking", "Checking Moonlight here and Sunshine on "+peer.Name+"…")
	ml, ok := control.FindMoonlight()
	if !ok {
		return &controlError{"Moonlight isn't installed on this computer.", control.InstallHint("moonlight")}
	}
	addr := n.peerAddr(peer)
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}

	st, err := n.cl.ControlStatus(ctx, addr, fp)
	if err != nil {
		return fmt.Errorf("could not ask %s: %w", peer.Name, err)
	}
	if !st.SunshineInstalled {
		return &controlError{"Sunshine isn't installed on " + peer.Name + ".", sunshineHintFor(peer.OS)}
	}
	if !st.SunshineRunning {
		state("starting", "Starting Sunshine on "+peer.Name+"…")
		if st, err = n.cl.StartSunshine(ctx, addr, fp); err != nil {
			return &controlError{"Could not start Sunshine on " + peer.Name + ": " + err.Error(), "Open Sunshine on " + peer.Name + " once by hand."}
		}
	}

	if !control.SunshineReachable(host) {
		return &controlError{"Sunshine is running on " + peer.Name + ", but this computer can't reach it: its firewall blocks Sunshine.", control.SunshineFirewallHint(peer.OS)}
	}

	// A smaller stream only makes things bigger if the host switches to
	// it, which Sunshine does on Windows.
	stream := control.StreamOptions{Resolution: opts.Resolution, FPS: opts.FPS, DisplayMode: opts.DisplayMode}
	match := opts.Resolution != "" && strings.EqualFold(peer.OS, "windows")
	screen := opts.Screen
	if screen == "" {
		screen = st.Screen // keep the current choice unless asked
	}
	if st.Configurable && (screen != st.Screen || match != st.MatchResolution) {
		state("configuring", "Setting up the screen on "+peer.Name+"…")
		if _, err := n.cl.ControlConfigure(ctx, addr, fp, proto.ControlConfigure{Screen: screen, MatchResolution: match}); err != nil {
			return fmt.Errorf("could not change Sunshine's settings on %s: %w", peer.Name, err)
		}
		for i := 0; i < 40 && !control.SunshineReachable(host); i++ {
			time.Sleep(500 * time.Millisecond)
		}
		st.MatchResolution = match
	}
	stream.MatchHost = st.Configurable && st.MatchResolution

	pairedML, err := ml.Paired(ctx, host)
	if err != nil {
		return err
	}
	if !pairedML {
		if err := n.pairMoonlight(ctx, ml, peer, host, addr, fp, st.AutoPIN); err != nil {
			return err
		}
	}

	state("streaming", "Opening Moonlight…")
	return ml.Stream(host, stream)
}

// pairMoonlight pairs Moonlight with Sunshine on the peer once, sending the
// PIN over dsync so nobody has to type it on the other computer (if it has
// a Sunshine login saved), or showing it there otherwise.
func (n *Node) pairMoonlight(ctx context.Context, ml control.Moonlight, peer Peer, host, addr, fp string, auto bool) error {
	pin := randomPIN()
	msg := "Setting up remote control with " + peer.Name + " (only needed once)…"
	if !auto {
		msg = "On " + peer.Name + ", enter this PIN in Sunshine. dsync shows it there too."
	}
	n.emit(EventControl, ControlState{PeerID: peer.ID, Step: "pairing", Message: msg, PIN: pin})

	cmd, err := ml.StartPair(host, pin)
	if err != nil {
		return err
	}
	exited := make(chan struct{})
	go func() {
		cmd.Wait()
		close(exited)
	}()
	defer func() {
		select {
		case <-exited:
		default:
			cmd.Process.Kill() // close Moonlight's pairing window
		}
	}()

	go func() {
		res, err := n.cl.ControlPIN(ctx, addr, fp, proto.ControlPINRequest{PIN: pin, FromName: n.Self().Name})
		if err == nil && !res.Auto && auto {
			n.emit(EventControl, ControlState{PeerID: peer.ID, Step: "pairing", PIN: pin,
				Message: "Sunshine on " + peer.Name + " didn't take the PIN automatically (" + res.Error + "). Enter it there by hand."})
		}
	}()

	deadline := time.Now().Add(pairWait)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-exited:
		case <-time.After(2 * time.Second):
		}
		if ok, _ := ml.Paired(ctx, host); ok {
			return nil
		}
		select {
		case <-exited:
			// Moonlight gave up (e.g. wrong PIN); one last check above failed.
			return errors.New("pairing didn't finish; try again")
		default:
		}
	}
	return errors.New("pairing timed out; try again")
}

func randomPIN() string {
	v, _ := rand.Int(rand.Reader, big.NewInt(10000))
	return fmt.Sprintf("%04d", v.Int64())
}

func sunshineHintFor(os string) string {
	switch strings.ToLower(os) {
	case "windows":
		return "On that PC: winget install LizardByte.Sunshine, then open https://localhost:47990 once to set a username and password."
	case "linux":
		return "On that computer: sudo pacman -S sunshine (Arch) or flatpak install flathub dev.lizardbyte.app.Sunshine, then open it once to set a username and password."
	}
	return "Install Sunshine from app.lizardbyte.dev on that computer."
}

// RunControl is StartControl for a device that may not be in the device
// list (the command-line tool), and waits until Moonlight opens.
func (n *Node) RunControl(ctx context.Context, peer Peer, fp string) error {
	return n.runControl(ctx, peer, fp, ControlOptions{}, func(step, msg string) {
		n.emit(EventControl, ControlState{PeerID: peer.ID, Step: step, Message: msg})
	})
}
