package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// downloadsDir is the user's Downloads folder as set in user-dirs.dirs,
// which is where desktops (and translated systems) put it.
func downloadsDir() string {
	if d := xdgDownloadDir(); d != "" {
		return d
	}
	return defaultDownloads()
}

func xdgDownloadDir() string {
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		cfg = filepath.Join(home, ".config")
	}
	f, err := os.Open(filepath.Join(cfg, "user-dirs.dirs"))
	if err != nil {
		return ""
	}
	defer f.Close()
	return parseUserDirs(bufio.NewScanner(f), os.Getenv("HOME"))
}

// parseUserDirs reads XDG_DOWNLOAD_DIR="$HOME/Downloads" style lines.
func parseUserDirs(sc *bufio.Scanner, home string) string {
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		v, ok := strings.CutPrefix(line, "XDG_DOWNLOAD_DIR=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"`)
		v = filepath.Clean(strings.Replace(v, "$HOME", home, 1))
		// Pointing at the home folder itself means "not set".
		if filepath.IsAbs(v) && v != filepath.Clean(home) {
			return v
		}
	}
	return ""
}
