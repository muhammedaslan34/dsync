package update

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// Kind reports how the running dsync was installed.
func Kind() string {
	exe, err := os.Executable()
	if err != nil {
		return KindManual
	}
	exe, _ = filepath.EvalSymlinks(exe)
	if strings.HasPrefix(exe, "/usr/") {
		if out, err := exec.Command("pacman", "-Qqo", exe).Output(); err == nil && strings.TrimSpace(string(out)) == "dsync" {
			return KindPacman
		}
	}
	if home, err := os.UserHomeDir(); err == nil && filepath.Dir(exe) == filepath.Join(home, ".local", "lib", "dsync") {
		return KindLocal
	}
	return KindManual
}

// Install installs a downloaded update. For the Arch package, pkexec asks
// for the password in a window.
func Install(ctx context.Context, kind, file string) error {
	switch kind {
	case KindPacman:
		if _, err := exec.LookPath("pkexec"); err != nil {
			return errors.New("pkexec is missing; install the update with: sudo pacman -U " + file)
		}
		out, err := exec.CommandContext(ctx, "pkexec", "pacman", "-U", "--noconfirm", file).CombinedOutput()
		if err != nil {
			if ee := (*exec.ExitError)(nil); errors.As(err, &ee) && (ee.ExitCode() == 126 || ee.ExitCode() == 127) {
				return errors.New("the password prompt was canceled")
			}
			return errors.New("pacman failed: " + lastLine(string(out)))
		}
		return nil
	case KindLocal:
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		exe, _ = filepath.EvalSymlinks(exe)
		return installLocal(file, filepath.Dir(exe))
	}
	return errors.New("this copy of dsync can't update itself; download the new version from the release page")
}

// Restart starts the updated dsync a moment after this one quits (only one
// can run at a time). The caller should quit right after.
func Restart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe) // the updated file at the same path
	cmd := exec.Command("sh", "-c", `sleep 1.5; exec "$0"`, exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
