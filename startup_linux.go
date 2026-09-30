package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Starting at login and the app menu entry use freedesktop .desktop files
// in the user's own folders.

func autostartSupported() bool { return true }
func appMenuSupported() bool   { return true }

func autostartFile() (string, error) {
	dir, err := os.UserConfigDir()
	return filepath.Join(dir, "autostart", "dsync.desktop"), err
}

func dataHome() (string, error) {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	return filepath.Join(home, ".local", "share"), err
}

func appMenuFile() (string, error) {
	d, err := dataHome()
	return filepath.Join(d, "applications", "dsync.desktop"), err
}

func iconFile() (string, error) {
	d, err := dataHome()
	return filepath.Join(d, "icons", "hicolor", "256x256", "apps", "dsync.png"), err
}

func exists(path string, err error) bool {
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func autostartEnabled() bool { return exists(autostartFile()) }
func appMenuEnabled() bool   { return exists(appMenuFile()) }

func setAutostart(on bool) error { return setDesktopFile(autostartFile, on, true) }
func setAppMenu(on bool) error   { return setDesktopFile(appMenuFile, on, false) }

func setDesktopFile(where func() (string, error), on, hidden bool) error {
	path, err := where()
	if err != nil {
		return err
	}
	if !on {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	entry, err := desktopEntry(hidden)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(entry), 0o644)
}

func desktopEntry(hidden bool) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	icon, err := iconFile()
	if err != nil {
		return "", err
	}
	os.MkdirAll(filepath.Dir(icon), 0o755)
	if err := os.WriteFile(icon, appIcon, 0o644); err != nil {
		return "", err
	}
	cmd := quoteExec(exe)
	if hidden {
		cmd += " --hidden"
	}
	return fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=dsync
Comment=Send text, files and the clipboard between your computers
Exec=%s
Icon=dsync
Terminal=false
Categories=Network;FileTransfer;
StartupWMClass=dsync-gui
X-GNOME-Autostart-enabled=true
`, cmd), nil
}

// quoteExec quotes a path for the Exec key, which splits on spaces and
// gives \ " ` $ special meaning inside quotes.
func quoteExec(path string) string {
	r := strings.NewReplacer(`\`, `\\\\`, `"`, `\"`, "`", "\\`", `$`, `\$`)
	return `"` + r.Replace(path) + `"`
}
