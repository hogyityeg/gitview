//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// detach runs cmd in its own session, without a controlling terminal, so
// ssh can't prompt on gitview's terminal and fails instead.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
