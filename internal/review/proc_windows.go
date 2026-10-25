//go:build windows

package review

import (
	"os"
	"os/exec"
	"syscall"
)

func shell(command string) *exec.Cmd { return exec.Command("cmd", "/C", command) }

const createNewProcessGroup = 0x00000200

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

func alive(pid int) bool {
	p, err := os.FindProcess(pid) // fails on Windows when the process is gone
	if err != nil {
		return false
	}
	_ = p.Release()
	return true
}
