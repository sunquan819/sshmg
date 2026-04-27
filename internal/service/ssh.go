package service

import (
	"fmt"
	"net"
	"sync"
	"time"

	"deploy-manager/internal/database"
	"deploy-manager/internal/model"
	"deploy-manager/pkg/ssh"
)

type SSHService struct {
	clients map[uint]*ssh.Client
	mu      sync.RWMutex
}

var SSHSvc = &SSHService{
	clients: make(map[uint]*ssh.Client),
}

func (s *SSHService) BuildJumpChain(server *model.Server) ([]ssh.JumpServer, error) {
	var chain []ssh.JumpServer

	currentServer := server
	visited := make(map[uint]bool)

	for currentServer.JumpEnabled && currentServer.JumpServerID > 0 {
		if visited[currentServer.JumpServerID] {
			return nil, fmt.Errorf("detect jump chain loop at server: %s", currentServer.Name)
		}
		visited[currentServer.JumpServerID] = true

		var jumpServer model.Server
		if err := database.DB.First(&jumpServer, currentServer.JumpServerID).Error; err != nil {
			return nil, fmt.Errorf("failed to find jump server: %w", err)
		}

		chain = append(chain, ssh.JumpServer{
			Host:     jumpServer.IP,
			Port:     jumpServer.Port,
			User:     jumpServer.Username,
			Password: jumpServer.Password,
			Key:      jumpServer.SSHKey,
		})

		currentServer = &jumpServer
	}

	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

func (s *SSHService) GetClient(server *model.Server) (*ssh.Client, error) {
	s.mu.RLock()
	client, exists := s.clients[server.ID]
	s.mu.RUnlock()

	if exists && client.IsConnected() {
		return client, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	client = ssh.NewClient(server.IP, server.Port, server.Username, server.Password, server.SSHKey)
	client.JumpEnabled = server.JumpEnabled
	client.JumpHost = server.JumpIP
	client.JumpPort = server.JumpPort
	client.JumpUser = server.JumpUser
	client.JumpPassword = server.JumpPassword
	client.JumpKey = server.JumpKey

	if server.JumpServerID > 0 {
		chain, err := s.BuildJumpChain(server)
		if err != nil {
			return nil, fmt.Errorf("failed to build jump chain: %w", err)
		}
		client.JumpChain = chain
		client.JumpEnabled = true
	}

	if err := client.Connect(); err != nil {
		return nil, err
	}

	s.clients[server.ID] = client
	return client, nil
}

func (s *SSHService) RemoveClient(serverID uint) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if client, exists := s.clients[serverID]; exists {
		client.Close()
		delete(s.clients, serverID)
	}
}

func (s *SSHService) ExecuteCommand(server *model.Server, cmd string, timeout time.Duration) (string, error) {
	client, err := s.GetClient(server)
	if err != nil {
		return "", err
	}

	result, err := client.Execute(cmd, timeout)
	if err != nil {
		s.RemoveClient(server.ID)
		if result.Error != "" {
			return result.Output, fmt.Errorf("%s", result.Error)
		}
		return "", err
	}

	return result.Output, nil
}

func (s *SSHService) TestConnection(server *model.Server) error {
	if server.Username == "" && server.ServerType == "proxy" {
		return nil
	}
	if server.Username == "" {
		return fmt.Errorf("未配置用户名")
	}
	if server.OsType == "windows" {
		return testWindowsConnection(server)
	}

	client := ssh.NewClient(server.IP, server.Port, server.Username, server.Password, server.SSHKey)
	client.JumpEnabled = server.JumpEnabled
	client.JumpHost = server.JumpIP
	client.JumpPort = server.JumpPort
	client.JumpUser = server.JumpUser
	client.JumpPassword = server.JumpPassword
	client.JumpKey = server.JumpKey
	client.ProxyEnabled = server.ProxyEnabled
	client.ProxyType = server.ProxyType
	client.ProxyHost = server.ProxyHost
	client.ProxyPort = server.ProxyPort

	if server.JumpServerID > 0 {
		chain, err := s.BuildJumpChain(server)
		if err != nil {
			return fmt.Errorf("failed to build jump chain: %w", err)
		}
		client.JumpChain = chain
		client.JumpEnabled = true
	}

	if err := client.Connect(); err != nil {
		return err
	}
	defer client.Close()

	return client.TestConnection()
}

func testWindowsConnection(server *model.Server) error {
	port := 3389
	if server.Port > 0 && server.Port != 22 {
		port = server.Port
	}

	address := fmt.Sprintf("%s:%d", server.IP, port)
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return fmt.Errorf("无法连接 %s: %v", address, err)
	}
	conn.Close()
	return nil
}

func (s *SSHService) CheckDockerInstalled(server *model.Server) (bool, error) {
	output, err := s.ExecuteCommand(server, "docker --version", 5*time.Second)
	if err != nil {
		return false, err
	}
	return len(output) > 0, nil
}

func (s *SSHService) CheckSystemdInstalled(server *model.Server) (bool, error) {
	output, err := s.ExecuteCommand(server, "systemctl --version", 5*time.Second)
	if err != nil {
		return false, nil
	}
	return len(output) > 0, nil
}

func (s *SSHService) InstallDocker(server *model.Server) error {
	commands := []string{
		"curl -fsSL https://get.docker.com -o get-docker.sh",
		"sh get-docker.sh",
		"rm get-docker.sh",
		"systemctl enable docker",
		"systemctl start docker",
	}

	for _, cmd := range commands {
		if _, err := s.ExecuteCommand(server, cmd, 300*time.Second); err != nil {
			return fmt.Errorf("failed to execute: %s, error: %w", cmd, err)
		}
	}
	return nil
}

func (s *SSHService) UploadFile(server *model.Server, localPath, remotePath string) error {
	client, err := s.GetClient(server)
	if err != nil {
		return err
	}
	return client.UploadFile(localPath, remotePath)
}

func (s *SSHService) UploadDir(server *model.Server, localDir, remoteDir string) error {
	client, err := s.GetClient(server)
	if err != nil {
		return err
	}
	return client.UploadDir(localDir, remoteDir)
}

func (s *SSHService) RemoteMkdir(server *model.Server, remotePath string) error {
	client, err := s.GetClient(server)
	if err != nil {
		return err
	}
	return client.RemoteMkdir(remotePath)
}
