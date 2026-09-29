//go:build !windows

package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
)

func openPath(path string) error {
	return start(opener(), path)
}

func revealPath(path string) error {
	if runtime.GOOS == "darwin" {
		return start("open", "-R", path)
	}
	return start(opener(), filepath.Dir(path))
}

func opener() string {
	if runtime.GOOS == "darwin" {
		return "open"
	}
	return "xdg-open"
}

func start(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
