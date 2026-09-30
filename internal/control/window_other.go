//go:build !windows

package control

import "os/exec"

func hideWindow(*exec.Cmd) {}
