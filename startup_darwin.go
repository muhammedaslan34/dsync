package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// On macOS, starting at login is a LaunchAgent in the user's Library.

const launchAgentLabel = "io.github.muhammedaslan34.dsync"

func autostartSupported() bool { return true }
func appMenuSupported() bool   { return false } // /Applications is the app menu
func appMenuEnabled() bool     { return false }
func setAppMenu(bool) error    { return errors.ErrUnsupported }

func launchAgentFile() (string, error) {
	home, err := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist"), err
}

func autostartEnabled() bool {
	p, err := launchAgentFile()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

func setAutostart(on bool) error {
	p, err := launchAgentFile()
	if err != nil {
		return err
	}
	if !on {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(launchAgentPlist(exe)), 0o644)
}

func launchAgentPlist(exe string) string {
	esc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`, launchAgentLabel, esc(exe))
}
