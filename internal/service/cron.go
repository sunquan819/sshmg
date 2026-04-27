package service

import (
	"fmt"
	"strings"
	"time"

	"deploy-manager/internal/model"
)

type CronService struct{}

var CronSvc = &CronService{}

func (s *CronService) AddCronJob(server *model.Server, name, schedule, command string) error {
	cronLine := fmt.Sprintf("%s %s", schedule, command)

	getCmd := "crontab -l 2>/dev/null || echo ''"
	existing, err := SSHSvc.ExecuteCommand(server, getCmd, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to get existing crontab: %w", err)
	}

	existing = strings.TrimSpace(existing)
	var newCrontab string
	if existing == "" {
		newCrontab = cronLine
	} else {
		newCrontab = existing + "\n" + cronLine
	}

	setCmd := fmt.Sprintf(`echo '%s' | crontab -`, newCrontab)
	_, err = SSHSvc.ExecuteCommand(server, setCmd, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to set crontab: %w", err)
	}

	return nil
}

func (s *CronService) RemoveCronJob(server *model.Server, schedule, command string) error {
	getCmd := "crontab -l 2>/dev/null || echo ''"
	existing, err := SSHSvc.ExecuteCommand(server, getCmd, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to get existing crontab: %w", err)
	}

	lines := strings.Split(existing, "\n")
	var newLines []string
	targetLine := fmt.Sprintf("%s %s", schedule, command)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && line != targetLine && !strings.Contains(line, command) {
			newLines = append(newLines, line)
		}
	}

	if len(newLines) == 0 {
		removeCmd := "crontab -r"
		_, err = SSHSvc.ExecuteCommand(server, removeCmd, 5*time.Second)
	} else {
		newCrontab := strings.Join(newLines, "\n")
		setCmd := fmt.Sprintf(`echo '%s' | crontab -`, newCrontab)
		_, err = SSHSvc.ExecuteCommand(server, setCmd, 5*time.Second)
	}

	if err != nil && !strings.Contains(err.Error(), "no crontab") {
		return fmt.Errorf("failed to remove cron job: %w", err)
	}

	return nil
}

func (s *CronService) ListCronJobs(server *model.Server) ([]string, error) {
	cmd := "crontab -l 2>/dev/null || echo ''"
	output, err := SSHSvc.ExecuteCommand(server, cmd, 5*time.Second)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	var jobs []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			jobs = append(jobs, line)
		}
	}

	return jobs, nil
}

func (s *CronService) ExecuteCommand(server *model.Server, command string, timeout time.Duration) (string, error) {
	return SSHSvc.ExecuteCommand(server, command, timeout)
}
