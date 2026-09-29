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

func Load() (*Config, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "config.json")

	var c Config
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

func (c *Config) Save() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	out, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(filepath.Join(dir, "config.json"), out, 0o600)
}
