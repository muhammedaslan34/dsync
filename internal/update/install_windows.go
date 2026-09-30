package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// Kind reports how the running dsync was installed: by the setup (which
// leaves uninstall.exe next to it) or otherwise.
func Kind() string {
	exe, err := os.Executable()
	if err != nil {
		return KindManual
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(exe), "uninstall.exe")); err == nil {
		return KindInstaller
	}
	return KindManual
}

// Install starts the downloaded setup. Windows asks for permission, and the
// setup closes the running dsync, updates it and can start it again.
func Install(ctx context.Context, kind, file string) error {
	if kind != KindInstaller {
		return errors.New("this copy of dsync can't update itself; download the new version from the release page")
	}
	verb, _ := windows.UTF16PtrFromString("open")
	path, _ := windows.UTF16PtrFromString(file)
	if err := windows.ShellExecute(0, verb, path, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		return errors.New("could not start the setup: " + err.Error())
	}
	return nil
}

// Restart isn't needed on Windows: the setup starts dsync again.
func Restart() error { return nil }
