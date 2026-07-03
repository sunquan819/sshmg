package handler

import (
	"os/exec"
	"syscall"
)

func hideLocalAgentCommandWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
