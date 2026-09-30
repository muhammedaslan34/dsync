package control

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// SunshineAPI talks to the Sunshine web API on this computer, which needs
// the username and password set in Sunshine's web UI. dsync uses it to
// enter a Moonlight pairing PIN for a paired device, so remote control can
// be set up without being at this computer.
type SunshineAPI struct {
	User, Password string
	// Base is the web UI address; empty means https://localhost:47990.
	Base string
}

// ErrBadLogin means Sunshine refused the username or password.
var ErrBadLogin = errors.New("sunshine refused the username or password")

// sunshineClient accepts Sunshine's self-signed certificate; it only ever
// talks to this computer.
var sunshineClient = &http.Client{
	Timeout:   10 * time.Second,
	Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
}

func (s SunshineAPI) base() string {
	if s.Base != "" {
		return s.Base
	}
	return fmt.Sprintf("https://localhost:%d", SunshineWebPort)
}

func (s SunshineAPI) do(ctx context.Context, method, path, csrf string, in, out any) (int, error) {
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.base()+path, body)
	if err != nil {
		return 0, err
	}
	req.SetBasicAuth(s.User, s.Password)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	resp, err := sunshineClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return resp.StatusCode, ErrBadLogin
	}
	if out != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

// CheckLogin reports whether the username and password work.
func (s SunshineAPI) CheckLogin(ctx context.Context) error {
	code, err := s.do(ctx, http.MethodGet, "/api/config", "", nil, nil)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("sunshine answered %d", code)
	}
	return nil
}

type pendingPairing struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
}

// SubmitPIN enters pin for the Moonlight client at fromIP that is waiting
// to pair. Newer Sunshine lists waiting clients and needs a CSRF token;
// older versions take the PIN alone. It waits up to wait for the client's
// request to show up, since Moonlight may still be connecting.
func (s SunshineAPI) SubmitPIN(ctx context.Context, pin, name, fromIP string, wait time.Duration) error {
	var tok struct {
		Token string `json:"csrf_token"`
	}
	code, err := s.do(ctx, http.MethodGet, "/api/csrf-token", "", nil, &tok)
	if err != nil {
		return err
	}
	if code == http.StatusNotFound {
		return s.submitLegacy(ctx, pin, name)
	}
	if code != http.StatusOK || tok.Token == "" {
		return fmt.Errorf("sunshine csrf-token answered %d", code)
	}

	// Find the waiting pairing request from that computer.
	deadline := time.Now().Add(wait)
	var id string
	for id == "" {
		var list struct {
			Pairings []pendingPairing `json:"pairings"`
		}
		if _, err := s.do(ctx, http.MethodGet, "/api/pin", "", nil, &list); err != nil {
			return err
		}
		id = pickPairing(list.Pairings, fromIP)
		if id != "" {
			break
		}
		if time.Now().After(deadline) {
			return errors.New("no pairing request from moonlight arrived at sunshine")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	var res struct {
		Status bool `json:"status"`
	}
	body := map[string]string{"pairing_id": id, "pin": pin, "name": name}
	if code, err = s.do(ctx, http.MethodPost, "/api/pin", tok.Token, body, &res); err != nil {
		return err
	}
	if code != http.StatusOK || !res.Status {
		return errors.New("sunshine did not accept the pin")
	}
	return nil
}

func (s SunshineAPI) submitLegacy(ctx context.Context, pin, name string) error {
	var res struct {
		Status any `json:"status"`
	}
	code, err := s.do(ctx, http.MethodPost, "/api/pin", "", map[string]string{"pin": pin, "name": name}, &res)
	if err != nil {
		return err
	}
	// Older versions answer {"status":"true"} or {"status":true}.
	if code != http.StatusOK || fmt.Sprint(res.Status) != "true" {
		return errors.New("sunshine did not accept the pin")
	}
	return nil
}

// pickPairing chooses the waiting request from fromIP, or the only one.
func pickPairing(ps []pendingPairing, fromIP string) string {
	for _, p := range ps {
		host := p.Address
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.TrimPrefix(host, "::ffff:")
		if host == fromIP {
			return p.ID
		}
	}
	if len(ps) == 1 {
		return ps[0].ID
	}
	return ""
}

// nonConfigKeys come back from GET /api/config but aren't settings; saving
// them would write them into Sunshine's settings file.
var nonConfigKeys = []string{"status", "platform", "version"}

// Config returns Sunshine's settings.
func (s SunshineAPI) Config(ctx context.Context) (map[string]any, error) {
	cfg := map[string]any{}
	code, err := s.do(ctx, http.MethodGet, "/api/config", "", nil, &cfg)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("sunshine answered %d", code)
	}
	for _, k := range nonConfigKeys {
		delete(cfg, k)
	}
	return cfg, nil
}

// csrf gets a CSRF token, or "" from older Sunshine that doesn't use them.
func (s SunshineAPI) csrf(ctx context.Context) (string, error) {
	var tok struct {
		Token string `json:"csrf_token"`
	}
	code, err := s.do(ctx, http.MethodGet, "/api/csrf-token", "", nil, &tok)
	if err != nil {
		return "", err
	}
	if code == http.StatusNotFound {
		return "", nil
	}
	return tok.Token, nil
}

// SaveConfig replaces Sunshine's settings with cfg, which should be what
// Config returned with changes: Sunshine rewrites its whole settings file
// from it. Empty values remove a setting.
func (s SunshineAPI) SaveConfig(ctx context.Context, cfg map[string]any) error {
	tok, err := s.csrf(ctx)
	if err != nil {
		return err
	}
	var res struct {
		Status bool `json:"status"`
	}
	code, err := s.do(ctx, http.MethodPost, "/api/config", tok, cfg, &res)
	if err != nil {
		return err
	}
	if code != http.StatusOK || !res.Status {
		return fmt.Errorf("sunshine didn't save its settings (%d)", code)
	}
	return nil
}

// Restart restarts Sunshine so new settings take effect. Sunshine may drop
// the connection while restarting, which isn't an error.
func (s SunshineAPI) Restart(ctx context.Context) error {
	tok, err := s.csrf(ctx)
	if err != nil {
		return err
	}
	code, err := s.do(ctx, http.MethodPost, "/api/restart", tok, struct{}{}, nil)
	if err != nil {
		return nil
	}
	if code == http.StatusUnauthorized || code == http.StatusForbidden {
		return fmt.Errorf("sunshine refused to restart (%d)", code)
	}
	return nil
}
