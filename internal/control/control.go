// Package control sets up remote control of another computer with the
// Moonlight client and the Sunshine host, which do the actual screen
// streaming and input. dsync finds them, starts Sunshine if needed, pairs
// them once, and launches the stream.
package control

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// SunshinePort is Sunshine's GameStream HTTP port; the web UI is one above.
const (
	SunshinePort    = 47989
	SunshineWebPort = SunshinePort + 1
	// DesktopApp is the Sunshine app that streams the whole desktop.
	DesktopApp = "Desktop"
)

// ErrNotPaired means Moonlight isn't paired with that host yet.
var ErrNotPaired = errors.New("moonlight is not paired with this computer")

// Moonlight is a way to run the Moonlight client.
type Moonlight struct {
	cmd []string // program and leading arguments
}

// lookPath and runtimeOS are variables so tests can fake them.
var (
	lookPath  = exec.LookPath
	runtimeOS = runtime.GOOS
	statFile  = os.Stat
)

// FindMoonlight looks for Moonlight: on the PATH, as a Flatpak, or in the
// usual Windows install folder.
func FindMoonlight() (Moonlight, bool) {
	for _, name := range []string{"moonlight", "moonlight-qt"} {
		if p, err := lookPath(name); err == nil {
			return Moonlight{cmd: []string{p}}, true
		}
	}
	if runtimeOS == "linux" && flatpakInstalled("com.moonlight_stream.Moonlight") {
		return Moonlight{cmd: []string{"flatpak", "run", "com.moonlight_stream.Moonlight"}}, true
	}
	if runtimeOS == "windows" {
		for _, dir := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			p := filepath.Join(dir, "Moonlight Game Streaming", "Moonlight.exe")
			if dir != "" && fileExists(p) {
				return Moonlight{cmd: []string{p}}, true
			}
		}
	}
	return Moonlight{}, false
}

func (m Moonlight) command(ctx context.Context, args ...string) *exec.Cmd {
	all := append(append([]string{}, m.cmd[1:]...), args...)
	if ctx == nil {
		return exec.Command(m.cmd[0], all...)
	}
	return exec.CommandContext(ctx, m.cmd[0], all...)
}

// Paired reports whether Moonlight is paired with host, by listing its
// apps, which only works once paired. Call it after checking that host's
// Sunshine is reachable (SunshineReachable): Moonlight on Windows exits
// without saying why, so any other failure is taken as "not paired yet".
func (m Moonlight) Paired(ctx context.Context, host string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := m.command(ctx, "list", host).CombinedOutput()
	if err == nil {
		return true, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if strings.Contains(string(out), "Failed to connect") {
		return false, fmt.Errorf("moonlight could not reach %s; is Sunshine running there?", host)
	}
	return false, nil
}

// SunshineReachable reports whether host's Sunshine accepts connections
// from here, i.e. it runs and its firewall lets Moonlight in. It is a
// variable so tests can fake it.
var SunshineReachable = func(host string) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, fmt.Sprint(SunshinePort)), 3*time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// SunshineFirewallHint says how to let Moonlight reach Sunshine on a
// computer running os.
func SunshineFirewallHint(os string) string {
	if strings.EqualFold(os, "windows") {
		return "On that PC, allow Sunshine in Windows Firewall (its installer normally does), and make sure the network is set to Private: dsync → Settings → Windows Firewall."
	}
	return "On that computer: dsync → Settings → Remote control → Allow through firewall, or run: sudo ufw allow proto tcp to any port 47984,47989,48010 && sudo ufw allow proto udp to any port 47998:48000,48002,48010"
}

// StartPair starts pairing with host using pin, which must then be entered
// in (or sent to) Sunshine on that computer. Moonlight shows a small window
// while it waits. The caller should Wait on or Kill the returned command.
func (m Moonlight) StartPair(host, pin string) (*exec.Cmd, error) {
	cmd := m.command(nil, "pair", host, "--pin", pin)
	return cmd, cmd.Start()
}

// Stream opens a Moonlight window controlling host's desktop. It returns
// once Moonlight has started; the stream runs on its own.
func (m Moonlight) Stream(host string) error {
	cmd := m.command(nil, "stream", host, DesktopApp)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// Sunshine is the Sunshine host on this computer.
type Sunshine struct {
	path    string   // executable, if found
	service []string // command that starts it as a service, if any
}

// FindSunshine looks for Sunshine on this computer.
func FindSunshine() (Sunshine, bool) {
	var s Sunshine
	if p, err := lookPath("sunshine"); err == nil {
		s.path = p
	}
	switch runtimeOS {
	case "linux":
		if unit := SunshineUnit(); unit != "" {
			// Start it through its service, never as a second copy.
			s.service = []string{"systemctl", "--user", "start", unit}
		} else if s.path == "" && flatpakInstalled("dev.lizardbyte.app.Sunshine") {
			s.path = "flatpak"
			s.service = []string{"flatpak", "run", "dev.lizardbyte.app.Sunshine"}
		}
	case "windows":
		p := filepath.Join(os.Getenv("ProgramFiles"), "Sunshine", "sunshine.exe")
		if s.path == "" && fileExists(p) {
			s.path = p
		}
	}
	return s, s.path != "" || s.service != nil
}

// SunshineCheckAddr is where SunshineRunning looks for Sunshine; tests
// point it elsewhere so a real Sunshine on the machine doesn't interfere.
var SunshineCheckAddr = fmt.Sprintf("127.0.0.1:%d", SunshinePort)

// SunshineRunning reports whether a Sunshine host answers on this computer.
func SunshineRunning() bool {
	c, err := net.DialTimeout("tcp", SunshineCheckAddr, time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// Start starts Sunshine and waits up to 15 seconds for it to answer.
func (s Sunshine) Start() error {
	if SunshineRunning() {
		return nil
	}
	var cmd *exec.Cmd
	switch {
	case s.service != nil:
		cmd = exec.Command(s.service[0], s.service[1:]...)
	case s.path != "":
		cmd = exec.Command(s.path)
		detach(cmd) // keep running if dsync quits
	default:
		return errors.New("sunshine is not installed")
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	for range 30 {
		if SunshineRunning() {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("sunshine did not start; open it once by hand to finish its setup")
}

func fileExists(p string) bool {
	st, err := statFile(p)
	return err == nil && !st.IsDir()
}

func flatpakInstalled(id string) bool {
	if _, err := lookPath("flatpak"); err != nil {
		return false
	}
	return exec.Command("flatpak", "info", id).Run() == nil
}

// sunshineUnits are the names Sunshine's user service has had: current
// packages use the app id, older ones "sunshine".
var sunshineUnits = []string{"app-dev.lizardbyte.app.Sunshine.service", "sunshine.service"}

// SunshineUnit is the name of Sunshine's systemd user service, or "".
func SunshineUnit() string {
	for _, u := range sunshineUnits {
		if unitExists(u) {
			return u
		}
	}
	return ""
}

func unitExists(unit string) bool {
	if _, err := lookPath("systemctl"); err != nil {
		return false
	}
	out, _ := exec.Command("systemctl", "--user", "list-unit-files", unit).Output()
	return strings.Contains(string(out), unit)
}

// InstallHint says how to install the missing program on this OS.
func InstallHint(program string) string {
	switch program + "/" + runtimeOS {
	case "moonlight/linux":
		return "Install Moonlight: sudo pacman -S moonlight-qt (Arch), or flatpak install flathub com.moonlight_stream.Moonlight"
	case "moonlight/windows":
		return "Install Moonlight: winget install MoonlightGameStreamingProject.Moonlight, or download it from moonlight-stream.org"
	case "sunshine/linux":
		return "Install Sunshine: sudo pacman -S sunshine (Arch), or flatpak install flathub dev.lizardbyte.app.Sunshine, then open it once to set a username and password"
	case "sunshine/windows":
		return "Install Sunshine: winget install LizardByte.Sunshine, or download it from app.lizardbyte.dev, then open https://localhost:47990 once to set a username and password"
	}
	return "Install " + program
}
