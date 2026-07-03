//go:build !windows

package handler

import "os/exec"

func hideLocalAgentCommandWindow(cmd *exec.Cmd) {}
