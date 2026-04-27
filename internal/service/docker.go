package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"deploy-manager/internal/model"
)

type DockerService struct{}

var DockerSvc = &DockerService{}

func (s *DockerService) Init() error {
	return nil
}

type ContainerConfig struct {
	Name    string            `json:"name"`
	Image   string            `json:"image"`
	Cmd     []string          `json:"cmd,omitempty"`
	Env     []string          `json:"env,omitempty"`
	Ports   map[string]string `json:"ports,omitempty"`
	Volumes map[string]string `json:"volumes,omitempty"`
	Restart string            `json:"restart,omitempty"`
	Network string            `json:"network,omitempty"`
	Memory  int64             `json:"memory,omitempty"`
}

type Container struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	Status  string `json:"status"`
	State   string `json:"state"`
	Ports   string `json:"ports"`
	Created string `json:"created"`
}

type ContainerStats struct {
	ID         string  `json:"id"`
	CPUPercent float64 `json:"cpu_percent"`
	MemoryMB   float64 `json:"memory_mb"`
	NetIO      string  `json:"net_io"`
	BlockIO    string  `json:"block_io"`
}

func (s *DockerService) ListContainers(server *model.Server, all bool) ([]Container, error) {
	allFlag := ""
	if all {
		allFlag = "-a"
	}
	cmd := fmt.Sprintf("docker ps %s --format '{{.ID}}|{{.Names}}|{{.Image}}|{{.Status}}|{{.State}}|{{.Ports}}|{{.CreatedAt}}'", allFlag)
	output, err := s.executeCommand(server, cmd, 10*time.Second)
	if err != nil {
		return nil, err
	}

	var containers []Container
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) >= 6 {
			containers = append(containers, Container{
				ID:      parts[0],
				Name:    parts[1],
				Image:   parts[2],
				Status:  parts[3],
				State:   parts[4],
				Ports:   parts[5],
				Created: parts[6],
			})
		}
	}
	return containers, nil
}

func (s *DockerService) StartContainer(server *model.Server, containerID string) error {
	cmd := "docker start " + containerID
	_, err := s.executeCommand(server, cmd, 30*time.Second)
	return err
}

func (s *DockerService) StopContainer(server *model.Server, containerID string) error {
	cmd := "docker stop " + containerID
	_, err := s.executeCommand(server, cmd, 30*time.Second)
	return err
}

func (s *DockerService) RestartContainer(server *model.Server, containerID string) error {
	cmd := "docker restart " + containerID
	_, err := s.executeCommand(server, cmd, 60*time.Second)
	return err
}

func (s *DockerService) RemoveContainer(ctx context.Context, server interface{}, name string, force bool) error {
	srv := server.(*model.Server)
	forceFlag := ""
	if force {
		forceFlag = "-f"
	}
	cmd := fmt.Sprintf("docker rm %s %s", forceFlag, name)
	_, err := s.executeCommand(srv, cmd, 30*time.Second)
	return err
}

func (s *DockerService) GetContainerLogs(ctx context.Context, server interface{}, containerID string, tail string, offset string) (string, error) {
	srv := server.(*model.Server)
	tailFlag := ""
	if tail != "" {
		tailFlag = "--tail " + tail
	}
	offsetFlag := ""
	if offset != "" && offset != "0" {
		offsetFlag = "--offset " + offset
	}
	cmd := fmt.Sprintf("docker logs %s %s %s 2>&1", tailFlag, offsetFlag, containerID)
	output, err := s.executeCommand(srv, cmd, 30*time.Second)
	return output, err
}

func (s *DockerService) GetContainerStats(server *model.Server, containerID string) ([]ContainerStats, error) {
	cmd := fmt.Sprintf("docker stats %s --no-stream --format '{{.ID}}|{{.CPUPerc}}|{{.MemUsage}}|{{.NetIO}}|{{.BlockIO}}'", containerID)
	output, err := s.executeCommand(server, cmd, 10*time.Second)
	if err != nil {
		return nil, err
	}

	var stats []ContainerStats
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) >= 5 {
			stat := ContainerStats{
				ID:      parts[0],
				NetIO:   parts[3],
				BlockIO: parts[4],
			}
			stat.CPUPercent = parsePercent(parts[1])
			stat.MemoryMB = parseMemUsage(parts[2])
			stats = append(stats, stat)
		}
	}
	return stats, nil
}

func (s *DockerService) ExecContainer(server *model.Server, containerID string, cmdStr string) (string, error) {
	fullCmd := fmt.Sprintf("docker exec %s %s", containerID, cmdStr)
	output, err := s.executeCommand(server, fullCmd, 30*time.Second)
	return output, err
}

func (s *DockerService) GetContainerInfo(server *model.Server, containerID string) (map[string]interface{}, error) {
	cmd := "docker inspect " + containerID
	output, err := s.executeCommand(server, cmd, 10*time.Second)
	if err != nil {
		return nil, err
	}

	var result []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return nil, err
	}

	if len(result) > 0 {
		return result[0], nil
	}
	return nil, nil
}

func (s *DockerService) ListImages(server *model.Server) ([]string, error) {
	cmd := "docker images --format '{{.Repository}}:{{.Tag}}'"
	output, err := s.executeCommand(server, cmd, 10*time.Second)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(output, "\n")
	var images []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			images = append(images, line)
		}
	}
	return images, nil
}

func (s *DockerService) CheckDockerInstalled(server *model.Server) bool {
	output, err := s.executeCommand(server, "docker --version", 5*time.Second)
	return err == nil && len(output) > 0
}

func (s *DockerService) RunContainer(ctx context.Context, server interface{}, cfg *ContainerConfig) error {
	srv := server.(*model.Server)
	cmd := fmt.Sprintf("docker run -d --name %s", cfg.Name)

	if cfg.Restart == "always" || cfg.Restart == "unless-stopped" || cfg.Restart == "on-failure" {
		cmd += fmt.Sprintf(" --restart %s", cfg.Restart)
	}

	for hostPort, containerPort := range cfg.Ports {
		cmd += fmt.Sprintf(" -p %s:%s", hostPort, containerPort)
	}

	for hostPath, containerPath := range cfg.Volumes {
		cmd += fmt.Sprintf(" -v %s:%s", hostPath, containerPath)
	}

	for _, env := range cfg.Env {
		cmd += fmt.Sprintf(" -e '%s'", env)
	}

	cmd += fmt.Sprintf(" %s", cfg.Image)

	if len(cfg.Cmd) > 0 {
		for _, c := range cfg.Cmd {
			cmd += fmt.Sprintf(" %s", c)
		}
	}

	_, err := s.executeCommand(srv, cmd, 60*time.Second)
	return err
}

func EncodeAuthToBase64(authConfig interface{}) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (s *DockerService) executeCommand(server *model.Server, cmd string, timeout time.Duration) (string, error) {
	return SSHSvc.ExecuteCommand(server, cmd, timeout)
}

func parsePercent(s string) float64 {
	s = strings.ReplaceAll(s, "%", "")
	var result float64
	fmt.Sscanf(s, "%f", &result)
	return result
}

func parseMemUsage(s string) float64 {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, "/")
	if len(parts) >= 2 {
		numStr := strings.TrimSpace(parts[0])
		numStr = strings.ReplaceAll(numStr, "MiB", "")
		numStr = strings.ReplaceAll(numStr, "GiB", "")
		numStr = strings.ReplaceAll(numStr, "MB", "")
		numStr = strings.ReplaceAll(numStr, "GB", "")
		var num float64
		fmt.Sscanf(numStr, "%f", &num)
		if strings.Contains(parts[0], "GiB") || strings.Contains(parts[0], "GB") {
			num = num * 1024
		}
		return num
	}
	return 0
}
