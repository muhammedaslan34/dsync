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
    silent) echo "00:00:00 - Qt Warning: SetProcessDpiAwarenessContext() failed" >&2; exit 1 ;;
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
	// "silent" is Moonlight on Windows, which exits without saying why.
	for state, want := range map[string]string{"paired": "true", "unpaired": "false", "down": "error", "silent": "false"} {
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
	if err := m.Stream("192.168.1.20", StreamOptions{}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	calls, _ := os.ReadFile(log)
	want := "pair 192.168.1.20 --pin 4821\nstream 192.168.1.20 Desktop\n"
	if string(calls) != want {
		t.Errorf("calls:\n%s\nwant:\n%s", calls, want)
	}
}

func TestInstallCommand(t *testing.T) {
	defer func(os string, lp func(string) (string, error)) { runtimeOS, lookPath = os, lp }(runtimeOS, lookPath)
	tools := map[string]bool{}
	lookPath = func(name string) (string, error) {
		if tools[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
	for _, c := range []struct {
		os    string
		tools []string
		prog  string
		want  string // "" = error
	}{
		{"windows", []string{"winget"}, "sunshine", "winget install --id LizardByte.Sunshine --exact --silent --accept-package-agreements --accept-source-agreements"},
		{"windows", nil, "moonlight", ""},
		{"linux", []string{"pacman", "pkexec", "flatpak"}, "moonlight", "pkexec pacman -S --needed --noconfirm moonlight-qt"},
		{"linux", []string{"flatpak"}, "sunshine", "flatpak install --user --noninteractive flathub dev.lizardbyte.app.Sunshine"},
		{"linux", nil, "moonlight", ""},
		{"linux", []string{"pacman", "pkexec"}, "photoshop", ""},
		{"darwin", []string{"brew"}, "moonlight", "brew install --cask moonlight"},
		{"darwin", nil, "moonlight", ""},
		{"darwin", []string{"brew"}, "sunshine", ""},
	} {
		runtimeOS = c.os
		clear(tools)
		for _, tl := range c.tools {
			tools[tl] = true
		}
		args, err := installCommand(c.prog)
		got := strings.Join(args, " ")
		if err != nil {
			got = ""
		}
		if got != c.want {
			t.Errorf("%s %v %s: got %q (%v), want %q", c.os, c.tools, c.prog, got, err, c.want)
		}
	}
}

func TestUfwAllowsSunshine(t *testing.T) {
	for rules, want := range map[string]bool{
		"### tuple ### allow udp 47100 0.0.0.0/0 any 0.0.0.0/0 in\n### tuple ### allow tcp 47101 0.0.0.0/0 any 0.0.0.0/0 in": false,
		"### tuple ### allow tcp 47984,47989,48010 0.0.0.0/0 any 0.0.0.0/0 in comment=53":                                    true,
		"### tuple ### allow tcp 47000:48000 0.0.0.0/0 any 0.0.0.0/0 in":                                                     true,
		"### tuple ### deny tcp 47989 0.0.0.0/0 any 0.0.0.0/0 in":                                                            false,
		"### tuple ### allow udp 47989 0.0.0.0/0 any 0.0.0.0/0 in":                                                           false,
		"### tuple ### allow any any 0.0.0.0/0 any 192.168.1.0/24 in":                                                        true,
	} {
		if got := ufwAllowsSunshine(rules); got != want {
			t.Errorf("%q: got %v", rules, got)
		}
	}
}

func TestParseDisplayLog(t *testing.T) {
	log := `[2026-09-30 10:00:00.000]: Info: Sunshine version: 2026.928
[2026-09-30 10:00:00.100]: Info: Currently available display devices:
[{"device_id":"{old}","display_name":"\\\\.\\DISPLAY9","friendly_name":"Old","info":{"primary":true,"resolution":{"height":720,"width":1280}}}]
[2026-09-30 10:05:00.100]: Info: Currently available display devices:
[
  {
    "device_id": "{a1}",
    "display_name": "\\\\.\\DISPLAY1",
    "edid": {"manufacturer_id": "DEL", "product_code": "A0B1", "serial_number": 1},
    "friendly_name": "DELL U2720Q",
    "info": {"hdr_state": null, "origin_point": {"x": 0, "y": 0}, "primary": false,
             "refresh_rate": {"numerator": 60, "denominator": 1},
             "resolution": {"height": 2160, "width": 3840}, "resolution_scale": {"numerator": 150, "denominator": 100}},
    "is_internal": false
  },
  {"device_id": "{b2}", "display_name": "\\\\.\\DISPLAY2", "friendly_name": "", "info": {"primary": true, "resolution": {"height": 1080, "width": 1920}}},
  {"device_id": "{c3}", "display_name": "\\\\.\\DISPLAY3", "friendly_name": "TV (off)", "info": null}
]
[2026-09-30 10:05:00.200]: Info: next line of the log
`
	got := parseDisplayLog([]byte(log))
	want := []Display{
		{ID: "{b2}", Name: `\\.\DISPLAY2`, Primary: true, Width: 1920, Height: 1080},
		{ID: "{a1}", Name: "DELL U2720Q", Width: 3840, Height: 2160},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("display %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
	if parseDisplayLog([]byte("no list here")) != nil {
		t.Error("a log without a list should give none")
	}
}

func TestSunshineConfigRoundTrip(t *testing.T) {
	var mu sync.Mutex
	saved := map[string]any{}
	var tokens []string
	restarted := false
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/api/csrf-token":
			w.Write([]byte(`{"csrf_token":"tok"}`))
		case "/api/config":
			if r.Method == http.MethodGet {
				w.Write([]byte(`{"status":true,"platform":"windows","version":"2026.9","sunshine_name":"PC","output_name":"{a1}"}`))
				return
			}
			tokens = append(tokens, r.Header.Get("X-CSRF-Token"))
			json.NewDecoder(r.Body).Decode(&saved)
			w.Write([]byte(`{"status":true}`))
		case "/api/restart":
			tokens = append(tokens, r.Header.Get("X-CSRF-Token"))
			restarted = true
			// Sunshine dies mid-request when it restarts.
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			c.Close()
		}
	}))
	defer srv.Close()
	api := SunshineAPI{User: "u", Password: "p", Base: srv.URL}

	cfg, err := api.Config(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, bad := cfg["status"]; bad || cfg["sunshine_name"] != "PC" {
		t.Fatalf("config: %v", cfg)
	}
	cfg["output_name"] = "{b2}"
	if err := api.SaveConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if err := api.Restart(context.Background()); err != nil {
		t.Fatalf("a dropped connection during restart is fine: %v", err)
	}
	if saved["output_name"] != "{b2}" || saved["sunshine_name"] != "PC" || saved["platform"] != nil {
		t.Errorf("saved %v (other settings must be kept, status fields dropped)", saved)
	}
	if !restarted || len(tokens) != 2 || tokens[0] != "tok" || tokens[1] != "tok" {
		t.Errorf("restarted=%v tokens=%v", restarted, tokens)
	}
}

func TestStreamOptionArgs(t *testing.T) {
	for _, c := range []struct {
		o    StreamOptions
		want string
	}{
		{StreamOptions{}, ""},
		{StreamOptions{Resolution: "1280x720", FPS: 60, DisplayMode: "fullscreen", MatchHost: true}, "--resolution 1280x720 --fps 60 --display-mode fullscreen --game-optimization"},
		{StreamOptions{DisplayMode: "evil; rm -rf"}, ""},
		{StreamOptions{Resolution: "1920x1080", Bitrate: 40000, YUV444: true}, "--resolution 1920x1080 --bitrate 40000 --yuv444"},
	} {
		if got := strings.Join(c.o.args(), " "); got != c.want {
			t.Errorf("%+v: %q", c.o, got)
		}
	}
}

func TestBitrate(t *testing.T) {
	for _, c := range []struct {
		res     string
		fps     int
		quality string
		want    int
	}{
		{"1920x1080", 60, "standard", 20000},
		{"1920x1080", 60, "high", 40000},
		{"1920x1080", 60, "best", 60000},
		{"1280x720", 60, "high", 17777},
		{"3840x2160", 120, "best", 150000}, // capped
		{"", 0, "", 40000},                 // defaults: 1080p, 60 fps, high
		{"320x200", 30, "standard", 5000},  // floor
	} {
		if got := Bitrate(c.res, c.fps, c.quality); got != c.want {
			t.Errorf("Bitrate(%q, %d, %q) = %d, want %d", c.res, c.fps, c.quality, got, c.want)
		}
	}
}
