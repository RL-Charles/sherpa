//go:build !windows

package review

import (
	"os/exec"
	"syscall"
)

func shell(command string) *exec.Cmd { return exec.Command("sh", "-c", command) }

// detach puts the reviewer in its own session so it outlives the caller and
// a Ctrl-C in the author's terminal.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

func alive(pid int) bool { return pid > 0 && syscall.Kill(pid, 0) == nil }
