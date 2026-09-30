// Package client talks to other devices over TLS. Every connection presents
// this device's certificate, and requests to a paired device check that it
// presents the key pinned at pairing time.
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"dsync/internal/identity"
	"dsync/internal/proto"
)

// ErrIdentityChanged means a device presented a different key than the one
// it was paired with.
var ErrIdentityChanged = errors.New("device identity changed; unpair and pair again if you trust it")

type Client struct {
	cert tls.Certificate

	mu      sync.Mutex
	clients map[string]*http.Client // by expected fingerprint; "" = any key
}

func New(id *identity.Identity) *Client {
	return &Client{cert: id.Cert, clients: map[string]*http.Client{}}
}

// forKey returns an HTTP client whose connections only succeed if the server
// presents the key with fingerprint fp. An empty fp accepts any key.
func (c *Client) forKey(fp string) *http.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if hc, ok := c.clients[fp]; ok {
		return hc
	}
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{c.cert},
		MinVersion:   tls.VersionTLS13,
		// Certificates are self-signed; trust comes from the pinned
		// fingerprint checked below, not from a CA.
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("no certificate from device")
			}
			if fp == "" {
				return nil
			}
			got, err := identity.Fingerprint(cs.PeerCertificates[0].Raw)
			if err != nil {
				return err
			}
			if got != fp {
				return ErrIdentityChanged
			}
			return nil
		},
	}
	hc := &http.Client{Transport: &http.Transport{
		TLSClientConfig:     tlsCfg,
		TLSHandshakeTimeout: 5 * time.Second,
		IdleConnTimeout:     60 * time.Second,
	}}
	c.clients[fp] = hc
	return hc
}

func endpoint(addr, path string) string { return "https://" + addr + path }

// Info asks a device to describe itself, and returns the fingerprint of the
// key it presented.
func (c *Client) Info(ctx context.Context, addr string) (proto.Device, string, error) {
	var d proto.Device
	ctx, cancel := withDefaultTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(addr, "/api/v1/info"), nil)
	if err != nil {
		return d, "", err
	}
	resp, err := c.forKey("").Do(req)
	if err != nil {
		return d, "", err
	}
	defer resp.Body.Close()
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return d, "", err
	}
	if resp.TLS == nil || len(resp.TLS.PeerCertificates) == 0 {
		return d, "", errors.New("no certificate from device")
	}
	fp, err := identity.Fingerprint(resp.TLS.PeerCertificates[0].Raw)
	if err != nil {
		return d, "", err
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&d)
	return d, fp, err
}

// SendText sends a message to the paired device with key fp.
func (c *Client) SendText(ctx context.Context, addr, fp string, m proto.TextMessage) error {
	ctx, cancel := withDefaultTimeout(ctx, 15*time.Second)
	defer cancel()
	return c.postJSON(ctx, addr, fp, "/api/v1/text", m, nil, http.StatusNoContent)
}

// Pair asks the device with key fp to trust us. It blocks until the other
// side accepts, declines or times out, so ctx should allow about a minute.
func (c *Client) Pair(ctx context.Context, addr, fp string, r proto.PairRequest) (proto.PairResponse, error) {
	var out proto.PairResponse
	err := c.postJSON(ctx, addr, fp, "/api/v1/pair", r, &out, http.StatusOK)
	return out, err
}

// Unpair tells a paired device we no longer trust each other.
func (c *Client) Unpair(ctx context.Context, addr, fp string) error {
	ctx, cancel := withDefaultTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.postJSON(ctx, addr, fp, "/api/v1/unpair", struct{}{}, nil, http.StatusNoContent)
}

func (c *Client) postJSON(ctx context.Context, addr, fp, path string, in, out any, want int) error {
	if fp == "" {
		return errors.New("device is not paired")
	}
	body, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(addr, path), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.forKey(fp).Do(req)
	if err != nil {
		return unwrap(err)
	}
	defer resp.Body.Close()
	if err := checkStatus(resp, want); err != nil {
		return err
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(out)
	}
	return nil
}

// StatusError is a non-success reply from a device.
type StatusError struct {
	Code int
	Msg  string
}

func (e *StatusError) Error() string {
	if e.Msg == "" {
		return http.StatusText(e.Code)
	}
	return e.Msg
}

func checkStatus(resp *http.Response, want int) error {
	if resp.StatusCode == want {
		return nil
	}
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return &StatusError{Code: resp.StatusCode, Msg: strings.TrimSpace(string(msg))}
}

// unwrap turns a pinned-key failure buried in a url.Error into
// ErrIdentityChanged so callers can show a clear message.
func unwrap(err error) error {
	if errors.Is(err, ErrIdentityChanged) {
		return ErrIdentityChanged
	}
	return err
}

func withDefaultTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d)
}
