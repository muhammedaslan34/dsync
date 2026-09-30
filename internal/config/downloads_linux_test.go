package config

import (
	"bufio"
	"strings"
	"testing"
)

func TestParseUserDirs(t *testing.T) {
	for in, want := range map[string]string{
		`XDG_DOWNLOAD_DIR="$HOME/Downloads"`: "/home/me/Downloads",
		"# comment\nXDG_DESKTOP_DIR=\"$HOME/Masaüstü\"\nXDG_DOWNLOAD_DIR=\"$HOME/İndirilenler\"": "/home/me/İndirilenler",
		`XDG_DOWNLOAD_DIR="/data/downloads"`: "/data/downloads",
		`XDG_DOWNLOAD_DIR="$HOME/"`:          "", // disabled: points at home itself
		`XDG_MUSIC_DIR="$HOME/Music"`:        "",
	} {
		if got := parseUserDirs(bufio.NewScanner(strings.NewReader(in)), "/home/me"); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
