package node

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"dsync/internal/control"
)

// controlFakes puts fake moonlight and sunshine programs on the PATH and
// runs a fake Sunshine web API. Moonlight's `pair` waits until Sunshine
// accepted its PIN; `list` succeeds once paired; `stream` is recorded.
type controlFakes struct {
	dir     string
	web     *httptest.Server
	pinSeen chan string // PINs Sunshine received through its API

	mu       sync.Mutex
	config   map[string]any // Sunshine's settings file
	saves    int
	restarts int
}

// withStatus is what GET /api/config returns: the settings plus fields
// that aren't settings.
func (f *controlFakes) withStatus() map[string]any {
	out := map[string]any{"status": true, "platform": "windows", "version": "2026.9"}
	for k, v := range f.config {
		out[k] = v
	}
	return out
}

func newControlFakes(t *testing.T, withMoonlight, withSunshine bool) *controlFakes {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses shell scripts")
	}
	f := &controlFakes{dir: t.TempDir(), pinSeen: make(chan string, 4), config: map[string]any{"sunshine_name": "PC"}}
	paired := filepath.Join(f.dir, "paired")
	if withMoonlight {
		script := `#!/bin/sh
echo "$@" >> "` + f.dir + `/calls"
case "$1" in
  list) [ -f "` + paired + `" ] && { echo Desktop; exit 0; }
        echo "Computer $2 has not been paired. Please open Moonlight to pair before retrieving games list." >&2; exit 255 ;;
  pair) echo "$4" > "` + f.dir + `/pairpin"
        for i in $(seq 1 100); do [ -f "` + paired + `" ] && exit 0; sleep 0.1; done; exit 1 ;;
esac
exit 0
`
		os.WriteFile(filepath.Join(f.dir, "moonlight"), []byte(script), 0o755)
	}
	if withSunshine {
		os.WriteFile(filepath.Join(f.dir, "sunshine"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	}
	// Only the fakes and the few tools the scripts use: a real Moonlight or
	// Sunshine installed on this machine must not be found.
	tools := filepath.Join(f.dir, "tools")
	os.Mkdir(tools, 0o755)
	for _, tool := range []string{"sh", "seq", "sleep"} {
		if p, err := exec.LookPath(tool); err == nil {
			os.Symlink(p, filepath.Join(tools, tool))
		}
	}
	t.Setenv("PATH", f.dir+string(os.PathListSeparator)+tools)
	// Not running unless the test starts a fake one; reachable when it is.
	old, oldReach := control.SunshineCheckAddr, control.SunshineReachable
	control.SunshineCheckAddr = "127.0.0.1:1"
	control.SunshineReachable = func(string) bool { return control.SunshineRunning() }
	t.Cleanup(func() { control.SunshineCheckAddr, control.SunshineReachable = old, oldReach })

	// Sunshine's web API: accepts the PIN only if it is the one Moonlight
	// is pairing with, which marks the pair as done.
	f.web = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, _ := r.BasicAuth(); u != "admin" || p != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/api/config" && r.Method == http.MethodGet:
			f.mu.Lock()
			json.NewEncoder(w).Encode(f.withStatus())
			f.mu.Unlock()
		case r.URL.Path == "/api/config":
			f.mu.Lock()
			f.config = map[string]any{}
			json.NewDecoder(r.Body).Decode(&f.config)
			f.saves++
			f.mu.Unlock()
			w.Write([]byte(`{"status":true}`))
		case r.URL.Path == "/api/restart":
			f.mu.Lock()
			f.restarts++
			f.mu.Unlock()
		case r.URL.Path == "/api/csrf-token":
			w.Write([]byte(`{"csrf_token":"t"}`))
		case r.URL.Path == "/api/pin" && r.Method == http.MethodGet:
			w.Write([]byte(`{"pairings":[{"id":"p1","name":"","address":"127.0.0.1"}]}`))
		case r.URL.Path == "/api/pin":
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			f.pinSeen <- b["pin"]
			ok := f.enterPIN(b["pin"])
			json.NewEncoder(w).Encode(map[string]bool{"status": ok})
		}
	}))
	t.Cleanup(f.web.Close)
	return f
}

// enterPIN is what typing the PIN into Sunshine does: it completes the
// pairing if it matches Moonlight's.
func (f *controlFakes) enterPIN(pin string) bool {
	for range 50 {
		if want, err := os.ReadFile(filepath.Join(f.dir, "pairpin")); err == nil {
			if strings.TrimSpace(string(want)) != pin {
				return false
			}
			os.WriteFile(filepath.Join(f.dir, "paired"), nil, 0o644)
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func (f *controlFakes) calls() string {
	b, _ := os.ReadFile(filepath.Join(f.dir, "calls"))
	return string(b)
}

// waitCall waits for the fake Moonlight to have been run with want: dsync
// reports "done" once Moonlight has started, and the fake process writes
// its line a moment later (slower on some machines, like macOS runners).
func (f *controlFakes) waitCall(t *testing.T, want string) {
	t.Helper()
	for range 250 {
		if strings.Contains(f.calls(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("moonlight calls:\n%s\nwant %q", f.calls(), want)
}

// sunshineUp pretends Sunshine is running here by listening where
// SunshineRunning looks.
func sunshineUp(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	control.SunshineCheckAddr = ln.Addr().String()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
}

type eventLog struct {
	mu     sync.Mutex
	events []any
}

func (l *eventLog) emit(event string, data any) {
	if event == EventControl || event == EventControlPIN {
		l.mu.Lock()
		l.events = append(l.events, data)
		l.mu.Unlock()
	}
}

// waitControl waits for StartControl to finish and returns its last state.
func (l *eventLog) waitControl(t *testing.T) ControlState {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		l.mu.Lock()
		for _, e := range l.events {
			if s, ok := e.(ControlState); ok && (s.Step == "done" || s.Step == "error" || s.Step == "canceled") {
				l.mu.Unlock()
				return s
			}
		}
		l.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("remote control setup never finished")
	return ControlState{}
}

func (l *eventLog) reset() {
	l.mu.Lock()
	l.events = nil
	l.mu.Unlock()
}

func (l *eventLog) pins() []ControlPIN {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []ControlPIN
	for _, e := range l.events {
		if p, ok := e.(ControlPIN); ok {
			out = append(out, p)
		}
	}
	return out
}

func controlPair(t *testing.T) (testNode, testNode, *eventLog, *eventLog) {
	la, lb := &eventLog{}, &eventLog{}
	a, b := newTestNodeWith(t, "Laptop", la.emit), newTestNodeWith(t, "Gaming-PC", lb.emit)
	pair(a, b, b.addr)
	pair(b, a, a.addr)
	return a, b, la, lb
}

func TestControlPairsAutomaticallyAndStreams(t *testing.T) {
	f := newControlFakes(t, true, true)
	sunshineUp(t)
	a, b, la, lb := controlPair(t)
	b.cfg.SunshineWeb = f.web.URL
	if err := b.SetSunshineLogin(t.Context(), "admin", "pw"); err != nil {
		t.Fatal(err)
	}

	if err := a.StartControl(t.Context(), b.cfg.ID, ControlOptions{}); err != nil {
		t.Fatal(err)
	}
	if s := la.waitControl(t); s.Step != "done" {
		t.Fatalf("ended with %+v", s)
	}
	f.waitCall(t, "stream 127.0.0.1 Desktop")
	if len(lb.pins()) != 0 {
		t.Error("the controlled computer asked for the PIN by hand despite having a login")
	}

	// Next time it is already paired: straight to the stream.
	la.reset()
	a.StartControl(t.Context(), b.cfg.ID, ControlOptions{})
	la.waitControl(t)
	if strings.Count(f.calls(), "pair ") != 1 {
		t.Errorf("paired again:\n%s", f.calls())
	}
}

func TestControlManualPIN(t *testing.T) {
	f := newControlFakes(t, true, true)
	sunshineUp(t)
	a, b, la, lb := controlPair(t)

	a.StartControl(t.Context(), b.cfg.ID, ControlOptions{})
	// The user at the other computer reads the PIN dsync shows there and
	// types it into Sunshine.
	var shown []ControlPIN
	for range 200 {
		if shown = lb.pins(); len(shown) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(shown) == 0 {
		t.Fatal("the PIN was not shown on the controlled computer")
	}
	if shown[0].PeerName != "Laptop" || !strings.HasSuffix(shown[0].URL, ":47990/pin") {
		t.Errorf("shown: %+v", shown[0])
	}
	f.enterPIN(shown[0].PIN)

	if s := la.waitControl(t); s.Step != "done" {
		t.Fatalf("ended with %+v", s)
	}
	f.waitCall(t, "stream 127.0.0.1 Desktop")
}

func TestControlMissingPrograms(t *testing.T) {
	t.Run("no moonlight here", func(t *testing.T) {
		newControlFakes(t, false, true)
		a, b, la, _ := controlPair(t)
		a.StartControl(t.Context(), b.cfg.ID, ControlOptions{})
		s := la.waitControl(t)
		if s.Step != "error" || !strings.Contains(s.Message, "Moonlight") || !strings.Contains(s.Hint, "moonlight") {
			t.Errorf("got %+v", s)
		}
	})
	t.Run("no sunshine there", func(t *testing.T) {
		newControlFakes(t, true, false)
		a, b, la, _ := controlPair(t)
		a.peers[b.cfg.ID].OS = "windows"
		a.StartControl(t.Context(), b.cfg.ID, ControlOptions{})
		s := la.waitControl(t)
		if s.Step != "error" || !strings.Contains(s.Message, "Sunshine isn't installed") || !strings.Contains(s.Hint, "winget install LizardByte.Sunshine") {
			t.Errorf("got %+v", s)
		}
	})
}

func TestControlFirewallBlocksSunshine(t *testing.T) {
	newControlFakes(t, true, true)
	sunshineUp(t)
	control.SunshineReachable = func(string) bool { return false } // its firewall drops us
	a, b, la, _ := controlPair(t)
	a.peers[b.cfg.ID].OS = "linux"
	a.StartControl(t.Context(), b.cfg.ID, ControlOptions{})
	s := la.waitControl(t)
	if s.Step != "error" || !strings.Contains(s.Message, "firewall blocks Sunshine") || !strings.Contains(s.Hint, "ufw allow") {
		t.Errorf("got %+v", s)
	}
}

func TestControlChoosesScreenAndZoom(t *testing.T) {
	f := newControlFakes(t, true, true)
	sunshineUp(t)
	// Sunshine's log on the controlled PC lists two screens.
	logPath := filepath.Join(f.dir, "sunshine.log")
	os.WriteFile(logPath, []byte(`[x]: Info: Currently available display devices:
[{"device_id":"{a1}","display_name":"\\\\.\\DISPLAY1","friendly_name":"DELL U2720Q","info":{"primary":false,"resolution":{"width":3840,"height":2160}}},
 {"device_id":"{b2}","display_name":"\\\\.\\DISPLAY2","friendly_name":"Laptop screen","info":{"primary":true,"resolution":{"width":1920,"height":1080}}}]
`), 0o644)
	f.config["log_path"] = logPath

	a, b, la, _ := controlPair(t)
	a.peers[b.cfg.ID].OS = "windows"
	b.cfg.SunshineWeb = f.web.URL
	if err := b.SetSunshineLogin(t.Context(), "admin", "pw"); err != nil {
		t.Fatal(err)
	}

	info := a.ControlInfo(t.Context(), b.cfg.ID)
	if !info.Configurable || len(info.Displays) != 2 || info.Displays[0].Name != "Laptop screen" || !info.Displays[0].Primary {
		t.Fatalf("control info: %+v", info)
	}

	opts := ControlOptions{Screen: "{a1}", Resolution: "1280x720", FPS: 60, DisplayMode: "fullscreen", Zoom: true, Quality: "high"}
	a.StartControl(t.Context(), b.cfg.ID, opts)
	if s := la.waitControl(t); s.Step != "done" {
		t.Fatalf("ended with %+v", s)
	}
	f.mu.Lock()
	cfg, saves, restarts := f.config, f.saves, f.restarts
	f.mu.Unlock()
	if cfg["output_name"] != "{a1}" || cfg["dd_resolution_option"] != "auto" || cfg["sunshine_name"] != "PC" || cfg["status"] != nil {
		t.Errorf("Sunshine settings: %v", cfg)
	}
	if saves != 1 || restarts != 1 {
		t.Errorf("saves=%d restarts=%d, want 1 and 1", saves, restarts)
	}
	f.waitCall(t, "stream 127.0.0.1 Desktop --resolution 1280x720 --fps 60 --bitrate 17777 --display-mode fullscreen --absolute-mouse --game-optimization")

	// Same choices again: nothing to change, so no restart.
	la.reset()
	a.StartControl(t.Context(), b.cfg.ID, opts)
	la.waitControl(t)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saves != 1 || f.restarts != 1 {
		t.Errorf("restarted Sunshine again for the same settings: saves=%d restarts=%d", f.saves, f.restarts)
	}
}

func TestPointerSpeedRestoredWhenMoonlightQuits(t *testing.T) {
	var mu sync.Mutex
	speed := 0.0
	oldAdj, oldRes := adjustPointer, restorePointer
	adjustPointer = func(step int) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		prev := speed
		speed += float64(step) * 0.3
		return fmt.Sprint(prev), nil
	}
	restorePointer = func(s string) error {
		mu.Lock()
		defer mu.Unlock()
		fmt.Sscan(s, &speed)
		return nil
	}
	t.Cleanup(func() { adjustPointer, restorePointer = oldAdj, oldRes })
	get := func() float64 { mu.Lock(); defer mu.Unlock(); return speed }

	n := newTestNode(t, "Laptop")
	done := make(chan struct{})
	n.adjustPointerWhile(2, done)
	if get() != 0.6 || n.cfg.PointerRestore != "0" {
		t.Fatalf("while controlling: speed %v, saved %q", get(), n.cfg.PointerRestore)
	}
	close(done) // Moonlight quits
	for i := 0; i < 100 && get() != 0; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if get() != 0 {
		t.Fatalf("speed not restored: %v", get())
	}

	// dsync quit while controlling: the next start restores it.
	n.adjustPointerWhile(-1, make(chan struct{}))
	if get() == 0 {
		t.Fatal("not adjusted")
	}
	n.restoreSavedPointer() // what Run does at startup
	if get() != 0 || n.cfg.PointerRestore != "" {
		t.Fatalf("after restart: speed %v, saved %q", get(), n.cfg.PointerRestore)
	}
}
