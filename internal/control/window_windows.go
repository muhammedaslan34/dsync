package control

import (
	"os/exec"
	"syscall"
)

func detach(*exec.Cmd) {}

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
