package control

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Installing Moonlight and Sunshine for the user: winget on Windows,
// pacman (through pkexec's password window) on Arch, or a per-user Flatpak.

type pkg struct {
	winget  string // winget package id
	pacman  string // Arch package name
	flatpak string // Flathub app id
}

var packages = map[string]pkg{
	"moonlight": {"MoonlightGameStreamingProject.Moonlight", "moonlight-qt", "com.moonlight_stream.Moonlight"},
	"sunshine":  {"LizardByte.Sunshine", "sunshine", "dev.lizardbyte.app.Sunshine"},
}

// installCommand picks how to install program on this computer.
func installCommand(program string) ([]string, error) {
	p, ok := packages[program]
	if !ok {
		return nil, fmt.Errorf("unknown program %q", program)
	}
	has := func(tool string) bool { _, err := lookPath(tool); return err == nil }
	switch {
	case runtimeOS == "windows" && has("winget"):
		return []string{"winget", "install", "--id", p.winget, "--exact", "--silent",
			"--accept-package-agreements", "--accept-source-agreements"}, nil
	case runtimeOS == "windows":
		return nil, errors.New("winget isn't available; " + InstallHint(program))
	case has("pacman") && has("pkexec"):
		return []string{"pkexec", "pacman", "-S", "--needed", "--noconfirm", p.pacman}, nil
	case has("flatpak"):
		return []string{"flatpak", "install", "--user", "--noninteractive", "flathub", p.flatpak}, nil
	}
	return nil, errors.New("no supported way to install it here; " + InstallHint(program))
}

// Install installs Moonlight or Sunshine. On Linux it may show a password
// window; on Windows the installer may ask for permission.
func Install(ctx context.Context, program string) error {
	args, err := installCommand(program)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && args[0] == "pkexec" && (ee.ExitCode() == 126 || ee.ExitCode() == 127) {
			return errors.New("the password prompt was canceled")
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		return fmt.Errorf("installing %s failed: %s", program, strings.TrimSpace(lines[len(lines)-1]))
	}
	if program == "sunshine" && runtimeOS == "linux" && unitExists("sunshine.service") {
		// Start it at login from now on, like on Windows where it's a service.
		exec.Command("systemctl", "--user", "enable", "--now", "sunshine").Run()
	}
	return nil
}
