package config

import (
	"os"
	"path/filepath"
)

// defaultDownloads is ~/Downloads, used when the system doesn't say where
// the Downloads folder is.
func defaultDownloads() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, "Downloads")
}
