package update

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
)

// Kind reports whether dsync runs from an app bundle (dsync.app).
func Kind() string {
	exe, err := os.Executable()
	if err != nil || !strings.Contains(exe, ".app/Contents/MacOS/") {
		return KindManual
	}
	return KindMacApp
}

// Install opens the downloaded disk image in Finder, where the new dsync
// is dragged into Applications to replace this one (after dsync quits).
func Install(ctx context.Context, kind, file string) error {
	if kind != KindMacApp {
		return errors.New("download the new version from the release page")
	}
	return exec.CommandContext(ctx, "open", file).Run()
}

// Restart isn't possible before the new app is copied in; the user opens
// it from Applications.
func Restart() error { return nil }
