package rdp

import (
	"fmt"
	"os"
	"os/exec"
)

func launch(target, username, password string) error {
	storeCredentials("127.0.0.1", username, password)
	rdpFile := os.Getenv("TEMP") + "\\rdp_connect.rdp"
	content := fmt.Sprintf(
		"full address:s:%s\r\nusername:s:%s\r\nprompt for credentials:i:0\r\nauthentication level:i:2\r\n",
		target, username)
	if err := os.WriteFile(rdpFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("create .rdp file failed: %w", err)
	}
	return exec.Command("mstsc", rdpFile).Start()
}
