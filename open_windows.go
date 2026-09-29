package main

import (
	"fmt"
	"os/exec"
	"syscall"
)

func openPath(path string) error {
	return start(exec.Command("rundll32", "url.dll,FileProtocolHandler", path))
}

func revealPath(path string) error {
	// explorer needs the quotes after "/select," exactly like this, which
	// Go's normal argument escaping doesn't produce.
	cmd := exec.Command("explorer")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: fmt.Sprintf(`explorer /select,"%s"`, path)}
	return start(cmd)
}

func start(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
