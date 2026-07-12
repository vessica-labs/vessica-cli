//go:build windows

package cli

import (
	"os/exec"
	"syscall"
)

func detachDashboardProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008}
}
