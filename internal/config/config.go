// Package config loads the per-device settings, creating them on first run.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"dsync/internal/proto"
)

type Config struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Port int    `json:"port"`
	// ManualPeers are HOST:PORT addresses added by hand, for devices that
	// broadcast discovery can't reach (e.g. over Tailscale).
	ManualPeers []string `json:"manual_peers,omitempty"`
	// DownloadDir is where received files go; empty means ~/Downloads/dsync.
	DownloadDir string `json:"download_dir,omitempty"`
	// QuitOnClose quits the app when its window is closed, instead of
	// keeping it running in the tray.
	QuitOnClose bool `json:"quit_on_close,omitempty"`
	// TrayHintShown records that the "still running in the tray" notice was
	// shown, so it appears only once.
	TrayHintShown bool `json:"tray_hint_shown,omitempty"`
	// ClipboardSync shares the clipboard with paired devices.
	ClipboardSync bool `json:"clipboard_sync,omitempty"`
	// Trusted are the paired devices. Only they may send us data.
	Trusted []TrustedPeer `json:"trusted,omitempty"`

	dir string // where this config lives
}

// TrustedPeer is a paired device, identified by its key fingerprint.
type TrustedPeer struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
}

// TrustedByFingerprint finds the paired device with this key.
func (c *Config) TrustedByFingerprint(fp string) (TrustedPeer, bool) {
	for _, t := range c.Trusted {
		if t.Fingerprint == fp {
			return t, true
		}
	}
	return TrustedPeer{}, false
}

// TrustedByID finds the paired device with this device id.
func (c *Config) TrustedByID(id string) (TrustedPeer, bool) {
	for _, t := range c.Trusted {
		if t.ID == id {
			return t, true
		}
	}
	return TrustedPeer{}, false
}

// Trust adds or updates a paired device. A device id or key can only belong
// to one entry, so old entries for either are replaced.
func (c *Config) Trust(t TrustedPeer) {
	c.Untrust(t.ID)
	c.Trusted = slices.DeleteFunc(c.Trusted, func(x TrustedPeer) bool { return x.Fingerprint == t.Fingerprint })
	c.Trusted = append(c.Trusted, t)
}

// Untrust removes the paired device with this id.
func (c *Config) Untrust(id string) {
	c.Trusted = slices.DeleteFunc(c.Trusted, func(x TrustedPeer) bool { return x.ID == id })
}

// ReceiveDir returns the folder for received files.
func (c *Config) ReceiveDir() string {
	if c.DownloadDir != "" {
		return c.DownloadDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, "Downloads", "dsync")
}

// Dir returns the config directory. DSYNC_CONFIG_DIR overrides it, which is
// handy for running two instances on one machine.
func Dir() (string, error) {
	if d := os.Getenv("DSYNC_CONFIG_DIR"); d != "" {
		return d, nil
	}
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "dsync"), nil
}

// Load reads the config from Dir, creating it on first run.
func Load() (*Config, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	return LoadFrom(dir)
}

// LoadFrom reads the config from dir, creating it on first run.
func LoadFrom(dir string) (*Config, error) {
	path := filepath.Join(dir, "config.json")

	c := Config{dir: dir}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}

	changed := false
	if c.ID == "" {
		b := make([]byte, 8)
		rand.Read(b)
		c.ID = hex.EncodeToString(b)
		changed = true
	}
	if c.Name == "" {
		c.Name, _ = os.Hostname()
		if c.Name == "" {
			c.Name = "unknown"
		}
		changed = true
	}
	if c.Port == 0 {
		c.Port = proto.DefaultHTTPPort
		changed = true
	}
	if changed {
		if err := c.Save(); err != nil {
			return nil, err
		}
	}
	return &c, nil
}

// Dir is the folder this config was loaded from; the identity and history
// live there too.
func (c *Config) Dir() string { return c.dir }

func (c *Config) Save() error {
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return err
	}
	out, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(filepath.Join(c.dir, "config.json"), out, 0o600)
}
