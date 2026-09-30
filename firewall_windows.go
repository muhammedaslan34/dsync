package main

import (
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const createNoWindow = 0x08000000

// hidden runs a console program without flashing a window.
func hidden(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd
}

func firewallStatus() FirewallStatus {
	st := FirewallStatus{Supported: true}
	// Reading the rules needs no admin rights.
	st.RuleOK = hidden("netsh", "advfirewall", "firewall", "show", "rule", "name=dsync").Run() == nil
	out, err := hidden("powershell", "-NoProfile", "-NonInteractive", "-Command", netProfilesCommand).Output()
	if err != nil {
		st.Error = "could not read the network type: " + err.Error()
		return st
	}
	if st.PublicNetworks, err = parseNetProfiles(out); err != nil {
		st.Error = "could not read the network type: " + err.Error()
	}
	return st
}

// dsyncPrograms are the installed dsync programs to allow.
func dsyncPrograms() ([]string, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	progs := []string{exe}
	if cli := filepath.Join(filepath.Dir(exe), "dsync.exe"); cli != exe {
		if _, err := os.Stat(cli); err == nil {
			progs = append(progs, cli)
		}
	}
	return progs, nil
}

func fixFirewall() error {
	progs, err := dsyncPrograms()
	if err != nil {
		return err
	}
	return runElevated(firewallScript(progs...))
}

func makeNetworkPrivate() error { return runElevated(privateNetworkScript) }

// runElevated runs a PowerShell script as administrator. Windows shows its
// usual "Do you want to allow this app to make changes" prompt.
func runElevated(script string) error {
	enc := base64.StdEncoding.EncodeToString(utf16le(script))
	// The outer PowerShell (not elevated) starts the inner one elevated and
	// waits for it; declining the prompt makes Start-Process fail.
	outer := "$p = Start-Process powershell -Verb RunAs -Wait -PassThru -WindowStyle Hidden " +
		"-ArgumentList '-NoProfile','-NonInteractive','-EncodedCommand','" + enc + "'; exit $p.ExitCode"
	err := hidden("powershell", "-NoProfile", "-NonInteractive", "-Command", outer).Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return errors.New("it didn't work, or the admin prompt was declined")
	}
	return err
}
