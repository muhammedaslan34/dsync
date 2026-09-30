package control

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSunshine is a Sunshine web API. The pairing request from Moonlight
// shows up after a delay, like the real thing.
type fakeSunshine struct {
	legacy   bool
	appear   time.Time
	mu       sync.Mutex
	gotPIN   map[string]string
	gotToken string
}

func (f *fakeSunshine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if u, p, ok := r.BasicAuth(); !ok || u != "admin" || p != "secret" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch {
	case r.URL.Path == "/api/config":
		w.Write([]byte(`{}`))
	case r.URL.Path == "/api/csrf-token" && !f.legacy:
		json.NewEncoder(w).Encode(map[string]string{"csrf_token": "tok123"})
	case r.URL.Path == "/api/pin" && r.Method == http.MethodGet && !f.legacy:
		var ps []map[string]string
		if time.Now().After(f.appear) {
			ps = []map[string]string{
				{"id": "aaaa", "name": "other", "address": "192.168.1.50"},
				{"id": "bbbb", "name": "", "address": "::ffff:192.168.1.11"},
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"pairings": ps})
	case r.URL.Path == "/api/pin" && r.Method == http.MethodPost:
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.gotPIN, f.gotToken = body, r.Header.Get("X-CSRF-Token")
		f.mu.Unlock()
		if f.legacy {
			w.Write([]byte(`{"status":"true"}`))
		} else {
			json.NewEncoder(w).Encode(map[string]bool{"status": body["pairing_id"] == "bbbb" && f.gotToken == "tok123"})
		}
	default:
		http.NotFound(w, r)
	}
}

func TestSubmitPINNewSunshine(t *testing.T) {
	f := &fakeSunshine{appear: time.Now().Add(700 * time.Millisecond)}
	srv := httptest.NewTLSServer(f)
	defer srv.Close()
	api := SunshineAPI{User: "admin", Password: "secret", Base: srv.URL}

	if err := api.CheckLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := api.SubmitPIN(context.Background(), "4821", "dsync: Mylaptop", "192.168.1.11", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if f.gotPIN["pairing_id"] != "bbbb" || f.gotPIN["pin"] != "4821" || f.gotToken != "tok123" {
		t.Errorf("sent %v with token %q", f.gotPIN, f.gotToken)
	}
}

func TestSubmitPINLegacySunshine(t *testing.T) {
	f := &fakeSunshine{legacy: true}
	srv := httptest.NewTLSServer(f)
	defer srv.Close()
	api := SunshineAPI{User: "admin", Password: "secret", Base: srv.URL}
	if err := api.SubmitPIN(context.Background(), "1234", "dsync", "10.0.0.2", time.Second); err != nil {
		t.Fatal(err)
	}
	if f.gotPIN["pin"] != "1234" || f.gotPIN["pairing_id"] != "" {
		t.Errorf("legacy request: %v", f.gotPIN)
	}
}

func TestSunshineBadLoginAndNoRequest(t *testing.T) {
	f := &fakeSunshine{appear: time.Now().Add(time.Hour)}
	srv := httptest.NewTLSServer(f)
	defer srv.Close()

	bad := SunshineAPI{User: "admin", Password: "wrong", Base: srv.URL}
	if err := bad.CheckLogin(context.Background()); !errors.Is(err, ErrBadLogin) {
		t.Errorf("wrong password: %v", err)
	}
	good := SunshineAPI{User: "admin", Password: "secret", Base: srv.URL}
	if err := good.SubmitPIN(context.Background(), "1234", "x", "192.168.1.11", 600*time.Millisecond); err == nil || !strings.Contains(err.Error(), "no pairing request") {
		t.Errorf("no waiting client: %v", err)
	}
}

// fakeMoonlight puts a "moonlight" script on the PATH that records its
// arguments and answers `list` like Moonlight does, depending on state.
func fakeMoonlight(t *testing.T, state string) (Moonlight, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := `#!/bin/sh
echo "$@" >> "` + log + `"
if [ "$1" = list ]; then
  case "` + state + `" in
    paired) echo Desktop; exit 0 ;;
    unpaired) echo "Computer $2 has not been paired. Please open Moonlight to pair before retrieving games list." >&2; exit 255 ;;
    down) echo "Failed to connect to $2" >&2; exit 255 ;;
  esac
fi
exit 0
`
	os.WriteFile(filepath.Join(dir, "moonlight"), []byte(script), 0o755)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	m, ok := FindMoonlight()
	if !ok {
		t.Fatal("fake moonlight not found")
	}
	return m, log
}

func TestMoonlightPairedStates(t *testing.T) {
	for state, want := range map[string]string{"paired": "true", "unpaired": "false", "down": "error"} {
		m, _ := fakeMoonlight(t, state)
		ok, err := m.Paired(context.Background(), "192.168.1.20")
		got := map[bool]string{true: "true", false: "false"}[ok]
		if err != nil {
			got = "error"
		}
		if got != want {
			t.Errorf("%s: got %s (%v)", state, got, err)
		}
	}
}

func TestMoonlightCommands(t *testing.T) {
	m, log := fakeMoonlight(t, "paired")
	cmd, err := m.StartPair("192.168.1.20", "4821")
	if err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	if err := m.Stream("192.168.1.20"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	calls, _ := os.ReadFile(log)
	want := "pair 192.168.1.20 --pin 4821\nstream 192.168.1.20 Desktop\n"
	if string(calls) != want {
		t.Errorf("calls:\n%s\nwant:\n%s", calls, want)
	}
}
