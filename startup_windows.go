package main

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// Starting at login uses the per-user Run key in the registry.

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func autostartSupported() bool { return true }
func appMenuSupported() bool   { return false }
func appMenuEnabled() bool     { return false }
func setAppMenu(bool) error    { return errors.ErrUnsupported }

func autostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue("dsync")
	return err == nil
}

func setAutostart(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue("dsync"); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	return k.SetStringValue("dsync", `"`+exe+`" --hidden`)
}
