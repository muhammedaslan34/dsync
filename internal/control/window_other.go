//go:build !windows

package control

import (
	"os/exec"
	"syscall"
)

func hideWindow(*exec.Cmd) {}

// detach puts the process in its own session, so it isn't stopped along
// with dsync.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
