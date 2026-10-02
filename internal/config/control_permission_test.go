package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyTrustedPeerDefaultsToNoControl(t *testing.T) {
	dir := t.TempDir()
	legacy := `{
  "id": "self",
  "name": "This PC",
  "port": 47101,
  "trusted": [{"id":"old-peer","name":"Old laptop","fingerprint":"abc123"}]
}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	peer, ok := cfg.TrustedByID("old-peer")
	if !ok {
		t.Fatal("legacy trusted peer was not loaded")
	}
	if peer.CanControl {
		t.Fatal("legacy peer unexpectedly gained remote-control permission")
	}
}
