package service

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"deploy-manager/internal/database"
	"deploy-manager/internal/model"
	"deploy-manager/pkg/ssh"
)

type SSHService struct {
	clients   map[uint]*ssh.Client
	lastUsed  map[uint]time.Time // 每个 client 上次使用时间,用于 idle TTL 清理
	mu        sync.RWMutex
	sem       chan struct{}            // 全局并发限制,防止一键刷新/批量执行时把目标机器打爆
	perSrv    map[uint]*sync.Mutex     // 每 server 一个 mutex,同 server SSH 串行执行
	perSrvMu  sync.Mutex
	stopIdle  chan struct{} // 控制 idle cleanup goroutine
	idleTTL   time.Duration // SSH 空闲超时,默认 60 分钟
}

// 默认全局 SSH 并发上限(同时跑的 SSH 操作数),可通过 SetMaxConcurrency 调整
const defaultSSHMaxConcurrency = 20

var SSHSvc = &SSHService{
	clients:  make(map[uint]*ssh.Client),
	lastUsed: make(map[uint]time.Time),
	sem:      make(chan struct{}, defaultSSHMaxConcurrency),
	perSrv:   make(map[uint]*sync.Mutex),
	stopIdle: make(chan struct{}),
	idleTTL:  60 * time.Minute,
}

// 启动后台 goroutine 定期清理 idle SSH 连接,防止脏缓存
func init() {
	go SSHSvc.idleCleanupLoop()
}

// 清理 idle 连接配置(sshIdleTTL 默认 60 分钟,可通过 SetIdleTTL 覆盖)
const sshIdleInterval = 1 * time.Minute

// idleCleanupLoop 每分钟扫描一次,关闭 idle 超时的 SSH 连接
// 关闭前先尝试 per-server mutex(若锁成功说明没人用,可以安全关)
func (s *SSHService) idleCleanupLoop() {
	t := time.NewTicker(sshIdleInterval)
	defer t.Stop()
	for {
		select {
		case <-s.stopIdle:
			return
		case <-t.C:
			cutoff := time.Now().Add(-s.idleTTL)
			s.mu.Lock()
			for id, last := range s.lastUsed {
				if last.Before(cutoff) {
					if client, ok := s.clients[id]; ok {
						// 尝试 per-server mutex,锁成功 = 没人用,安全关
						if pm, ok := s.getPerServerMutexNoLock(id); ok && pm.TryLock() {
							client.Close()
							delete(s.clients, id)
							delete(s.lastUsed, id)
							pm.Unlock()
							log.Printf("[SSHSvc] idle cleanup: closed SSH client server_id=%d", id)
						}
					}
				}
			}
			s.mu.Unlock()
		}
	}
}

// getPerServerMutexNoLock 不加 s.perSrvMu 直接读 map(仅 idle 清理用)
func (s *SSHService) getPerServerMutexNoLock(serverID uint) (*sync.Mutex, bool) {
	pm, ok := s.perSrv[serverID]
	return pm, ok
}

// SetMaxConcurrency 调整全局 SSH 并发上限(从配置读取)
func (s *SSHService) SetMaxConcurrency(n int) {
	if n <= 0 {
		n = defaultSSHMaxConcurrency
	}
	s.sem = make(chan struct{}, n)
}

// SetIdleTTL 设置 SSH 空闲连接超时
func (s *SSHService) SetIdleTTL(d time.Duration) {
	if d <= 0 {
		d = 60 * time.Minute
	}
	s.idleTTL = d
}

// getPerServerMutex 取出(或创建)对应 server 的串行 mutex
func (s *SSHService) getPerServerMutex(serverID uint) *sync.Mutex {
	s.perSrvMu.Lock()
	defer s.perSrvMu.Unlock()
	if pm, ok := s.perSrv[serverID]; ok {
		return pm
	}
	pm := &sync.Mutex{}
	s.perSrv[serverID] = pm
	return pm
}

// probeSessionOK 探测 cached client 是否真活着:开短 session 立即关
// 用 goroutine + 5s timeout 防止 SSH 半死状态时 NewSession 永远阻塞
func probeSessionOK(c *ssh.Client) bool {
	type result struct{ ok bool }
	ch := make(chan result, 1)
	go func() {
		native, err := c.GetNativeClient()
		if err != nil {
			ch <- result{false}
			return
		}
		sess, err := native.NewSession()
		if err != nil {
			ch <- result{false}
			return
		}
		sess.Close()
		ch <- result{true}
	}()
	select {
	case r := <-ch:
		return r.ok
	case <-time.After(5 * time.Second):
		log.Printf("[SSHSvc] probeSessionOK timeout, cached client 实际已死")
		return false
	}
}

// cleanPerServerMutex 当 RemoveClient 时,清掉对应 server 的 mutex 释放 map
func (s *SSHService) cleanPerServerMutex(serverID uint) {
	s.perSrvMu.Lock()
	delete(s.perSrv, serverID)
	s.perSrvMu.Unlock()
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
		// 二次验证:开一个短 session 立即关掉,确保 cached client 真活着
		// 避免 SSH server 重启/断网后 IsConnected 还返 true 的情况
		// (IsConnected 只看 c.client != nil,不测实际连通性)
		if probeSessionOK(client) {
			s.mu.Lock()
			s.lastUsed[server.ID] = time.Now()
			s.mu.Unlock()
			return client, nil
		}
		// cached client 实际已死,清掉重连
		log.Printf("[SSHSvc] cached client server_id=%d 探活失败,清掉重建", server.ID)
		s.RemoveClient(server.ID)
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
	// Proxy 字段:WebSSH / 部署 / 批量执行 全都依赖 GetClient,必须在这里设
	// (之前各 handler 各自设,容易漏;现在统一在这里)
	client.ProxyEnabled = server.ProxyEnabled
	client.ProxyType = server.ProxyType
	client.ProxyHost = server.ProxyHost
	client.ProxyPort = server.ProxyPort

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

	// 注意:line 193 已经 s.mu.Lock() + defer s.mu.Unlock(),
	// 这里不能再 s.mu.Lock() — Go mutex 不可重入,会死锁
	s.clients[server.ID] = client
	s.lastUsed[server.ID] = time.Now()
	return client, nil
}

func (s *SSHService) RemoveClient(serverID uint) {
	s.mu.Lock()
	if client, exists := s.clients[serverID]; exists {
		client.Close()
		delete(s.clients, serverID)
	}
	delete(s.lastUsed, serverID)
	s.mu.Unlock()

	s.cleanPerServerMutex(serverID)
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

// RunCommand 在已缓存的 SSH 客户端上跑命令，复用连接。
// 返回完整 SessionResult（带 exit code），错误时清缓存让下次重建。
// 多组件/多动作共享同一台服务器时，建议用这个方法而不是自己 NewClient。
//
// 信号量 + per-server mutex 双层保护：
//   1. 全局 sem:限制同时跑的 SSH 操作数(默认 20),防止一键刷新触发几百并发打爆目标机器
//   2. per-server mutex:同 server 的 SSH 串行,避免"check-running + 部署 + 日志"并发写 channel 错乱
func (s *SSHService) RunCommand(server *model.Server, cmd string, timeout time.Duration) (*ssh.SessionResult, error) {
	s.sem <- struct{}{}
	pm := s.getPerServerMutex(server.ID)
	pm.Lock()
	defer pm.Unlock()
	defer func() { <-s.sem }()

	client, err := s.GetClient(server)
	if err != nil {
		return nil, err
	}
	// 刷新 lastUsed(防止 idle cleanup 误关刚用过的连接)
	s.mu.Lock()
	s.lastUsed[server.ID] = time.Now()
	s.mu.Unlock()

	result, err := client.Execute(cmd, timeout)
	if err != nil {
		s.RemoveClient(server.ID)
	}
	return result, err
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

// DownloadToWriter 用 SFTP 流式下载远程文件到 writer(避免 base64+stdout 爆内存)
// 大文件也 OK,走 io.Copy
func (s *SSHService) DownloadToWriter(server *model.Server, remotePath string, w io.Writer) error {
	client, err := s.GetClient(server)
	if err != nil {
		return err
	}
	return client.DownloadToWriter(remotePath, w)
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
