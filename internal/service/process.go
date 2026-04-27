package service

import (
	"fmt"
	"strings"
	"time"

	"deploy-manager/internal/model"
)

type ProcessService struct{}

var ProcessSvc = &ProcessService{}

type ProcessConfig struct {
	Name        string   `json:"name"`
	Command     string   `json:"command"`
	WorkDir     string   `json:"work_dir"`
	User        string   `json:"user"`
	Env         []string `json:"env"`
	Restart     string   `json:"restart"`
	StdoutLog   string   `json:"stdout_log"`
	StderrLog   string   `json:"stderr_log"`
	NumProcs    int      `json:"num_procs"`
	StopWaitSec int      `json:"stop_wait_sec"`
}

func (s *ProcessService) CreateSystemdService(server *model.Server, cfg *ProcessConfig) error {
	unitContent := fmt.Sprintf(`[Unit]
Description=%s
After=network.target

[Service]
Type=simple
User=%s
WorkingDirectory=%s
ExecStart=%s
Restart=%s
RestartSec=5
StandardOutput=append:%s
StandardError=append:%s
`, cfg.Name, cfg.User, cfg.WorkDir, cfg.Command, cfg.Restart, cfg.StdoutLog, cfg.StderrLog)

	if len(cfg.Env) > 0 {
		for _, env := range cfg.Env {
			unitContent += fmt.Sprintf("Environment=\"%s\"\n", env)
		}
	}

	unitContent += `[Install]
WantedBy=multi-user.target
`

	servicePath := fmt.Sprintf("/etc/systemd/system/%s.service", cfg.Name)

	cmd := fmt.Sprintf(`cat > %s << 'EOF'
%s
EOF`, servicePath, unitContent)

	if _, err := SSHSvc.ExecuteCommand(server, cmd, 10*time.Second); err != nil {
		return fmt.Errorf("failed to create service file: %w", err)
	}

	if _, err := SSHSvc.ExecuteCommand(server, "systemctl daemon-reload", 5*time.Second); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}

	return nil
}

func (s *ProcessService) StartService(server *model.Server, name string) error {
	cmd := fmt.Sprintf("systemctl start %s", name)
	_, err := SSHSvc.ExecuteCommand(server, cmd, 10*time.Second)
	return err
}

func (s *ProcessService) StopService(server *model.Server, name string) error {
	cmd := fmt.Sprintf("systemctl stop %s", name)
	_, err := SSHSvc.ExecuteCommand(server, cmd, 10*time.Second)
	return err
}

func (s *ProcessService) RestartService(server *model.Server, name string) error {
	cmd := fmt.Sprintf("systemctl restart %s", name)
	_, err := SSHSvc.ExecuteCommand(server, cmd, 10*time.Second)
	return err
}

func (s *ProcessService) EnableService(server *model.Server, name string) error {
	cmd := fmt.Sprintf("systemctl enable %s", name)
	_, err := SSHSvc.ExecuteCommand(server, cmd, 5*time.Second)
	return err
}

func (s *ProcessService) DisableService(server *model.Server, name string) error {
	cmd := fmt.Sprintf("systemctl disable %s", name)
	_, err := SSHSvc.ExecuteCommand(server, cmd, 5*time.Second)
	return err
}

func (s *ProcessService) GetServiceStatus(server *model.Server, name string) (string, error) {
	cmd := fmt.Sprintf("systemctl is-active %s", name)
	output, err := SSHSvc.ExecuteCommand(server, cmd, 5*time.Second)
	if err != nil {
		return "unknown", err
	}
	return strings.TrimSpace(output), nil
}

func (s *ProcessService) GetServiceLogs(server *model.Server, name string, lines int) (string, error) {
	cmd := fmt.Sprintf("journalctl -u %s -n %d --no-pager", name, lines)
	return SSHSvc.ExecuteCommand(server, cmd, 10*time.Second)
}

func (s *ProcessService) RemoveService(server *model.Server, name string) error {
	servicePath := fmt.Sprintf("/etc/systemd/system/%s.service", name)

	cmds := []string{
		fmt.Sprintf("systemctl stop %s", name),
		fmt.Sprintf("systemctl disable %s", name),
		fmt.Sprintf("rm -f %s", servicePath),
		"systemctl daemon-reload",
	}

	for _, cmd := range cmds {
		_, err := SSHSvc.ExecuteCommand(server, cmd, 5*time.Second)
		if err != nil && !strings.Contains(err.Error(), "failed") {
			return fmt.Errorf("failed to execute: %s, error: %w", cmd, err)
		}
	}

	return nil
}

func (s *ProcessService) CreateSupervisorConfig(server *model.Server, cfg *ProcessConfig) error {
	configContent := fmt.Sprintf(`[program:%s]
command=%s
directory=%s
user=%s
autostart=true
autorestart=%s
stdout_logfile=%s
stderr_logfile=%s
`, cfg.Name, cfg.Command, cfg.WorkDir, cfg.User,
		map[bool]string{true: "true", false: "false"}[cfg.Restart != "no"],
		cfg.StdoutLog, cfg.StderrLog)

	if len(cfg.Env) > 0 {
		configContent += "environment="
		for i, env := range cfg.Env {
			if i > 0 {
				configContent += ","
			}
			configContent += fmt.Sprintf("\"%s\"", env)
		}
		configContent += "\n"
	}

	configPath := fmt.Sprintf("/etc/supervisor/conf.d/%s.conf", cfg.Name)

	cmd := fmt.Sprintf(`cat > %s << 'EOF'
%s
EOF`, configPath, configContent)

	if _, err := SSHSvc.ExecuteCommand(server, cmd, 10*time.Second); err != nil {
		return fmt.Errorf("failed to create supervisor config: %w", err)
	}

	if _, err := SSHSvc.ExecuteCommand(server, "supervisorctl reread", 5*time.Second); err != nil {
		return fmt.Errorf("failed to reread supervisor config: %w", err)
	}

	if _, err := SSHSvc.ExecuteCommand(server, "supervisorctl update", 5*time.Second); err != nil {
		return fmt.Errorf("failed to update supervisor: %w", err)
	}

	return nil
}

func (s *ProcessService) SupervisorControl(server *model.Server, action, name string) error {
	cmd := fmt.Sprintf("supervisorctl %s %s", action, name)
	_, err := SSHSvc.ExecuteCommand(server, cmd, 10*time.Second)
	return err
}
