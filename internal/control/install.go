package control

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Installing Moonlight and Sunshine for the user: winget on Windows,
// pacman (through pkexec's password window) on Arch, or a per-user Flatpak.

type pkg struct {
	brew    string // Homebrew cask, macOS
	winget  string // winget package id
	pacman  string // Arch package name
	flatpak string // Flathub app id
}

var packages = map[string]pkg{
	"moonlight": {"moonlight", "MoonlightGameStreamingProject.Moonlight", "moonlight-qt", "com.moonlight_stream.Moonlight"},
	"sunshine":  {"", "LizardByte.Sunshine", "sunshine", "dev.lizardbyte.app.Sunshine"},
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
	case runtimeOS == "darwin" && p.brew != "" && has("brew"):
		return []string{"brew", "install", "--cask", p.brew}, nil
	case runtimeOS == "darwin":
		return nil, errors.New(InstallHint(program))
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
	if program == "sunshine" && runtimeOS == "linux" {
		if unit := SunshineUnit(); unit != "" {
			// Start it now and at every login, like on Windows where it's a
			// service.
			exec.Command("systemctl", "--user", "enable", "--now", unit).Run()
		}
		// Windows' Sunshine installer opens the firewall; on Linux we do.
		if SunshineFirewallBlocked() {
			if err := AllowSunshineFirewall(ctx); err != nil {
				return fmt.Errorf("Sunshine is installed, but the firewall still blocks it: %w", err)
			}
		}
	}
	return nil
}

// Sunshine's streaming ports. Its settings page (47990) is left closed so
// it stays reachable only from this computer.
const (
	sunshineTCP = "47984,47989,48010"
	sunshineUDP = "47998:48000,48002,48010"
)

// ufwRules is where ufw keeps the user's rules; a variable for tests.
var ufwRules = "/etc/ufw/user.rules"

// SunshineFirewallBlocked reports whether ufw is on and doesn't allow
// Sunshine's ports, so other computers can't control this one.
func SunshineFirewallBlocked() bool {
	if runtimeOS != "linux" {
		return false
	}
	if _, err := lookPath("ufw"); err != nil {
		return false
	}
	if exec.Command("systemctl", "is-active", "-q", "ufw").Run() != nil {
		return false
	}
	rules, err := os.ReadFile(ufwRules)
	if err != nil {
		return false // can't tell; don't nag
	}
	return !ufwAllowsSunshine(string(rules))
}

// ufwAllowsSunshine reports whether user.rules allows Sunshine's main port.
func ufwAllowsSunshine(rules string) bool {
	for _, line := range strings.Split(rules, "\n") {
		if !strings.HasPrefix(line, "### tuple ### allow") {
			continue
		}
		f := strings.Fields(line)
		// ### tuple ### allow <proto> <ports> ...
		if len(f) > 5 && (f[4] == "tcp" || f[4] == "any") {
			for _, p := range strings.Split(f[5], ",") {
				if p == "47989" || p == "any" {
					return true
				}
				if lo, hi, ok := strings.Cut(p, ":"); ok && lo <= "47989" && "47989" <= hi && len(lo) == 5 && len(hi) == 5 {
					return true
				}
			}
		}
	}
	return false
}

// AllowSunshineFirewall opens Sunshine's ports in ufw (asks for the
// password in a window).
func AllowSunshineFirewall(ctx context.Context) error {
	if _, err := lookPath("pkexec"); err != nil {
		return errors.New("run: sudo ufw allow proto tcp to any port " + sunshineTCP + " && sudo ufw allow proto udp to any port " + sunshineUDP)
	}
	script := "ufw allow proto tcp to any port " + sunshineTCP + " comment 'Sunshine (dsync remote control)' && " +
		"ufw allow proto udp to any port " + sunshineUDP + " comment 'Sunshine (dsync remote control)'"
	out, err := exec.CommandContext(ctx, "pkexec", "sh", "-c", script).CombinedOutput()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && (ee.ExitCode() == 126 || ee.ExitCode() == 127) {
			return errors.New("the password prompt was canceled")
		}
		return errors.New(strings.TrimSpace(string(out)))
	}
	return nil
}
