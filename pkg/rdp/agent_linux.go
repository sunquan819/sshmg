package rdp

import "os/exec"

func launch(target, username, password string) error {
	if cmd, err := exec.LookPath("xfreerdp"); err == nil {
		return exec.Command(cmd, "/v:"+target, "/u:"+username, "/dynamic-resolution").Start()
	}
	if cmd, err := exec.LookPath("rdesktop"); err == nil {
		return exec.Command(cmd, target, "-u", username).Start()
	}
	return exec.LookPath("xfreerdp")
}
