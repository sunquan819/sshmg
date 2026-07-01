package rdp

import (
	"fmt"
	"os/exec"
	"runtime"
)

type ConnectRequest struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	JumpEnabled  bool   `json:"jump_enabled"`
	JumpIP       string `json:"jump_ip"`
	JumpPort     int    `json:"jump_port"`
	JumpUser     string `json:"jump_user"`
	JumpPassword string `json:"jump_password"`
	JumpKey      string `json:"jump_key"`
	ProxyEnabled bool   `json:"proxy_enabled"`
	ProxyType    string `json:"proxy_type"`
	ProxyHost    string `json:"proxy_host"`
	ProxyPort    int    `json:"proxy_port"`
}

func Launch(req ConnectRequest) error {
	if req.Port == 0 {
		req.Port = 3389
	}
	target := fmt.Sprintf("%s:%d", req.Host, req.Port)
	return launch(target, req.Username, req.Password)
}

func storeCredentials(host, username, password string) {
	if runtime.GOOS != "windows" {
		return
	}
	exec.Command("cmdkey", "/generic:TERMSRV/"+host, "/user:"+username, "/pass:"+password).Run()
}
