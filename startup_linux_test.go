package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopFilesLinux(t *testing.T) {
	cfgHome, dataHome := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_DATA_HOME", dataHome)

	if autostartEnabled() || appMenuEnabled() {
		t.Fatal("enabled before turning on")
	}
	if err := setAutostart(true); err != nil {
		t.Fatal(err)
	}
	if err := setAppMenu(true); err != nil {
		t.Fatal(err)
	}
	if !autostartEnabled() || !appMenuEnabled() {
		t.Fatal("not enabled after turning on")
	}

	auto, _ := os.ReadFile(filepath.Join(cfgHome, "autostart", "dsync.desktop"))
	menu, _ := os.ReadFile(filepath.Join(dataHome, "applications", "dsync.desktop"))
	if !strings.Contains(string(auto), "--hidden") || strings.Contains(string(menu), "--hidden") {
		t.Error("only the login entry should start hidden")
	}
	if _, err := os.Stat(filepath.Join(dataHome, "icons", "hicolor", "256x256", "apps", "dsync.png")); err != nil {
		t.Error("icon not installed")
	}
	if v, err := exec.LookPath("desktop-file-validate"); err == nil {
		for _, f := range []string{filepath.Join(cfgHome, "autostart", "dsync.desktop"), filepath.Join(dataHome, "applications", "dsync.desktop")} {
			if out, err := exec.Command(v, f).CombinedOutput(); err != nil {
				t.Errorf("%s: %s", f, out)
			}
		}
	}

	setAutostart(false)
	setAppMenu(false)
	if autostartEnabled() || appMenuEnabled() {
		t.Fatal("still enabled after turning off")
	}
}

func TestQuoteExec(t *testing.T) {
	for in, want := range map[string]string{
		"/home/me/sync program/dsync-gui": `"/home/me/sync program/dsync-gui"`,
		`/opt/a"b$c` + "`d" + `\e`:        `"/opt/a\"b\$c` + "\\`d" + `\\\\e"`,
	} {
		if got := quoteExec(in); got != want {
			t.Errorf("quoteExec(%q) = %s, want %s", in, got, want)
		}
	}
}
