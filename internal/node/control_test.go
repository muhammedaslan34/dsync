package node

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
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
}

func newControlFakes(t *testing.T, withMoonlight, withSunshine bool) *controlFakes {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses shell scripts")
	}
	f := &controlFakes{dir: t.TempDir(), pinSeen: make(chan string, 4)}
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
	t.Setenv("PATH", f.dir+string(os.PathListSeparator)+"/usr/bin:/bin")

	// Sunshine's web API: accepts the PIN only if it is the one Moonlight
	// is pairing with, which marks the pair as done.
	f.web = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, _ := r.BasicAuth(); u != "admin" || p != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/api/config":
			w.Write([]byte("{}"))
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

// sunshineUp pretends Sunshine is running here by listening on its port.
func sunshineUp(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:47989")
	if err != nil {
		t.Skip("port 47989 is busy (a real Sunshine running?)")
	}
	t.Cleanup(func() { ln.Close() })
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

	if err := a.StartControl(t.Context(), b.cfg.ID); err != nil {
		t.Fatal(err)
	}
	if s := la.waitControl(t); s.Step != "done" {
		t.Fatalf("ended with %+v", s)
	}
	if !strings.Contains(f.calls(), "stream 127.0.0.1 Desktop") {
		t.Errorf("moonlight calls:\n%s", f.calls())
	}
	if len(lb.pins()) != 0 {
		t.Error("the controlled computer asked for the PIN by hand despite having a login")
	}

	// Next time it is already paired: straight to the stream.
	la.reset()
	a.StartControl(t.Context(), b.cfg.ID)
	la.waitControl(t)
	if strings.Count(f.calls(), "pair ") != 1 {
		t.Errorf("paired again:\n%s", f.calls())
	}
}

func TestControlManualPIN(t *testing.T) {
	f := newControlFakes(t, true, true)
	sunshineUp(t)
	a, b, la, lb := controlPair(t)

	a.StartControl(t.Context(), b.cfg.ID)
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
	if !strings.Contains(f.calls(), "stream 127.0.0.1 Desktop") {
		t.Errorf("moonlight calls:\n%s", f.calls())
	}
}

func TestControlMissingPrograms(t *testing.T) {
	t.Run("no moonlight here", func(t *testing.T) {
		newControlFakes(t, false, true)
		a, b, la, _ := controlPair(t)
		a.StartControl(t.Context(), b.cfg.ID)
		s := la.waitControl(t)
		if s.Step != "error" || !strings.Contains(s.Message, "Moonlight") || !strings.Contains(s.Hint, "moonlight") {
			t.Errorf("got %+v", s)
		}
	})
	t.Run("no sunshine there", func(t *testing.T) {
		if control.SunshineRunning() {
			t.Skip("a Sunshine is running on this computer")
		}
		newControlFakes(t, true, false)
		a, b, la, _ := controlPair(t)
		a.peers[b.cfg.ID].OS = "windows"
		a.StartControl(t.Context(), b.cfg.ID)
		s := la.waitControl(t)
		if s.Step != "error" || !strings.Contains(s.Message, "Sunshine isn't installed") || !strings.Contains(s.Hint, "winget install LizardByte.Sunshine") {
			t.Errorf("got %+v", s)
		}
	})
}
