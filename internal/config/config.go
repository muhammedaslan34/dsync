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
	// Language is the language of the window, tray menu and notifications
	// ("en", "ar", "tr", "fr"); empty follows the system.
	Language string `json:"language,omitempty"`
	// TrayHintShown records that the "still running in the tray" notice was
	// shown, so it appears only once.
	TrayHintShown bool `json:"tray_hint_shown,omitempty"`
	// ClipboardSync shares the clipboard with paired devices.
	ClipboardSync bool `json:"clipboard_sync,omitempty"`
	// AskBeforeReceiving requires local approval before a paired computer can
	// start an incoming file or folder transfer.
	AskBeforeReceiving bool `json:"ask_before_receiving,omitempty"`
	// Sunshine web UI login, if saved, lets dsync finish remote-control
	// setup after local approval. The password is held only in the OS
	// credential store; SunshineWeb overrides the API address.
	SunshineUser     string `json:"sunshine_user,omitempty"`
	SunshinePassword string `json:"-"`
	SunshineWeb      string `json:"sunshine_web,omitempty"`
	// PointerRestore is the pointer speed to put back after remote
	// control, kept here in case dsync quits while controlling.
	PointerRestore string `json:"pointer_restore,omitempty"`
	// Phones are phones paired through a QR code (see docs/phone-protocol.md).
	Phones []Phone `json:"phones,omitempty"`
	// Trusted are the paired devices. Only they may send us data.
	Trusted []TrustedPeer `json:"trusted,omitempty"`

	dir                   string          // where this config lives
	credentials           CredentialStore // OS keychain, injectable in tests
	sunshineCredentialErr error           // non-fatal read failure at startup
}

// TrustedPeer is a paired device, identified by its key fingerprint.
type TrustedPeer struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
	// CanControl is a separate, explicitly granted capability. Pairing by
	// itself only permits sharing text and files.
	CanControl bool `json:"can_control,omitempty"`
}

// Phone is a paired phone. Key is the base64 secretbox key from the QR code.
type Phone struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"` // ios or android
	Key      string `json:"key"`
}

// PhoneByID finds a paired phone.
func (c *Config) PhoneByID(id string) (Phone, bool) {
	for _, p := range c.Phones {
		if p.ID == id {
			return p, true
		}
	}
	return Phone{}, false
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

// Trust adds or updates a paired device. An established id/key binding cannot
// be replaced: the user must explicitly unpair it first.
func (c *Config) Trust(t TrustedPeer) error {
	for i, x := range c.Trusted {
		if x.ID == t.ID && x.Fingerprint != t.Fingerprint {
			return fmt.Errorf("device id %s is already paired with a different key; unpair it first", t.ID)
		}
		if x.Fingerprint == t.Fingerprint && x.ID != t.ID {
			return fmt.Errorf("device key is already paired as %s; unpair it first", x.ID)
		}
		if x.ID == t.ID {
			c.Trusted[i] = t
			return nil
		}
	}
	c.Trusted = append(c.Trusted, t)
	return nil
}

// Untrust removes the paired device with this id.
func (c *Config) Untrust(id string) {
	c.Trusted = slices.DeleteFunc(c.Trusted, func(x TrustedPeer) bool { return x.ID == id })
}

// ReceiveDir returns the folder for received files: the one chosen in the
// settings, or a dsync folder in the user's Downloads folder.
func (c *Config) ReceiveDir() string {
	if c.DownloadDir != "" {
		return c.DownloadDir
	}
	return filepath.Join(downloadsDir(), "dsync")
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
	return LoadFromWithCredentialStore(dir, systemCredentialStore{})
}

// LoadFromWithCredentialStore is LoadFrom with an explicit credential store.
// It is useful for tests and headless environments with their own keychain
// adapter. A nil store uses the operating system credential store.
func LoadFromWithCredentialStore(dir string, credentials CredentialStore) (*Config, error) {
	if credentials == nil {
		credentials = systemCredentialStore{}
	}
	path := filepath.Join(dir, "config.json")

	c := Config{dir: dir, credentials: credentials}
	var legacy struct {
		SunshinePassword string `json:"sunshine_password"`
	}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if err := json.Unmarshal(data, &legacy); err != nil {
			return nil, fmt.Errorf("parse legacy credentials in %s: %w", path, err)
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
	migrated, err := c.loadSunshineCredential(legacy.SunshinePassword)
	if err != nil {
		return nil, err
	}
	changed = changed || migrated
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
	out, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			tmp.Close()
			os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(out); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	path := filepath.Join(c.dir, "config.json")
	if err := replaceFile(tmpPath, path); err != nil {
		return err
	}
	keep = true
	return syncDir(c.dir)
}
