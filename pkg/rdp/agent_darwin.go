package rdp

import (
	"fmt"
	"os/exec"
)

func launch(target, username, password string) error {
	url := fmt.Sprintf("ms-rdp:full%%20address=s:%s", target)
	if err := exec.Command("open", url).Start(); err == nil {
		return nil
	}
	return exec.Command("open", fmt.Sprintf("rdp://%s@%s", username, target)).Start()
}
