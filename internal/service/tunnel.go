package service

import (
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"deploy-manager/internal/database"
	"deploy-manager/internal/model"
	"deploy-manager/pkg/ssh"
)

type Tunnel struct {
	ServerID   uint             `json:"server_id"`
	ServerName string           `json:"server_name"`
	ServerIP   string           `json:"server_ip"`
	LocalPort  int              `json:"local_port"`
	Status     string           `json:"status"`
	SSHClient  *ssh.Client     `json:"-"`
	ConnCount  int              `json:"conn_count"`
}

var (
	tunnels     = make(map[uint]*Tunnel)
	tunnelsMu   sync.RWMutex
	nextPort    = 10800
)

func findAvailablePort(start int) int {
	for port := start; port < start+100; port++ {
		conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err != nil {
			return port
		}
		conn.Close()
	}
	return start
}

func StartDynamicTunnel(server *model.Server, localPort int, bindAddress string) (int, error) {
	tunnelsMu.Lock()
	defer tunnelsMu.Unlock()

	if localPort == 0 {
		localPort = findAvailablePort(nextPort)
		nextPort = localPort + 1
	}

	if bindAddress == "" {
		bindAddress = "127.0.0.1"
	}

	existing, ok := tunnels[server.ID]
	if ok && existing.Status == "running" {
		return existing.LocalPort, fmt.Errorf("tunnel already running on port %d", existing.LocalPort)
	}

	// 构建 SSH 客户端
	sshClient := ssh.NewClient(server.IP, server.Port, server.Username, server.Password, server.SSHKey)
	sshClient.JumpEnabled = server.JumpEnabled
	sshClient.JumpHost = server.JumpIP
	sshClient.JumpPort = server.JumpPort
	sshClient.JumpUser = server.JumpUser
	sshClient.JumpPassword = server.JumpPassword
	sshClient.JumpKey = server.JumpKey

	// 处理跳板机
	if server.JumpServerID > 0 {
		chain, err := buildJumpChain(server)
		if err != nil {
			return 0, fmt.Errorf("failed to build jump chain: %w", err)
		}
		sshClient.JumpChain = chain
		sshClient.JumpEnabled = true
	}

	if err := sshClient.Connect(); err != nil {
		log.Printf("SSH connect error: %v", err)
		return 0, fmt.Errorf("failed to connect: %w", err)
	}

	log.Printf("SSH connected successfully, starting SOCKS5 listener on %s:%d", bindAddress, localPort)

	// 监听本地端口
	listener, err := net.Listen("tcp", bindAddress+":"+strconv.Itoa(localPort))
	if err != nil {
		sshClient.Close()
		return 0, fmt.Errorf("failed to listen on port %d: %w", localPort, err)
	}

	// 启动 SOCKS5 代理处理
	serverID := server.ID
	SafeGo("tunnel.SOCKS5", func() {
		defer listener.Close()
		for {
			conn, err := listener.Accept()
			if err != nil {
				break
			}
			go handleSocks5Connection(conn, serverID)
		}
	})

	tunnel := &Tunnel{
		ServerID:   server.ID,
		ServerName: server.Name,
		ServerIP:   server.IP,
		LocalPort:  localPort,
		Status:     "running",
		SSHClient:  sshClient,
	}
	tunnels[server.ID] = tunnel

	log.Printf("Tunnel started: server=%s, port=%d", server.Name, localPort)
	return localPort, nil
}

func handleSocks5Connection(localConn net.Conn, serverID uint) {
	defer localConn.Close()

	// 从 tunnel 获取 SSH 客户端
	tunnelsMu.RLock()
	tunnel, ok := tunnels[serverID]
	tunnelsMu.RUnlock()

	if !ok || tunnel.SSHClient == nil {
		log.Printf("SOCKS5: tunnel not found or SSH client nil")
		return
	}

	sshClient := tunnel.SSHClient
	tunnel.ConnCount++

	log.Printf("SOCKS5: new connection from %s", localConn.RemoteAddr())

	// SOCKS5 握手 - 客户端发送: VER + NMETHODS + METHODS
	buf := make([]byte, 2)
	if _, err := io.ReadFull(localConn, buf); err != nil {
		log.Printf("SOCKS5: handshake1 read error: %v", err)
		return
	}
	if buf[0] != 5 {
		log.Printf("SOCKS5: unsupported version %d", buf[0])
		return
	}

	// 读取 METHODS
	nMethods := int(buf[1])
	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(localConn, methods); err != nil {
		log.Printf("SOCKS5: handshake2 read error: %v", err)
		return
	}

	// 响应: VER + METHOD (0 = 无认证)
	localConn.Write([]byte{5, 0})
	log.Printf("SOCKS5: handshake complete, nMethods=%d", nMethods)

	// 读取请求头 (4 bytes)
	header := make([]byte, 4)
	if _, err := io.ReadFull(localConn, header); err != nil {
		log.Printf("SOCKS5: request header read error: %v", err)
		return
	}

	log.Printf("SOCKS5: header bytes = %v", header)

	cmd := header[1]
	addrType := header[3]

	log.Printf("SOCKS5: cmd=%d, addrType=%d", cmd, addrType)

	var targetAddr string
	var extraBytes int

	if addrType == 1 { // IPv4: 4 bytes IP + 2 bytes port
		extraBytes = 6
		extra := make([]byte, extraBytes)
		if _, err := io.ReadFull(localConn, extra); err != nil {
			log.Printf("SOCKS5: IPv4 read error: %v", err)
			return
		}
		ip := net.IP(extra[0:4])
		port := int(extra[4])<<8 | int(extra[5])
		targetAddr = fmt.Sprintf("%s:%d", ip, port)
	} else if addrType == 3 { // 域名: 1 byte length + domain + 2 bytes port
		domainLenBuf := make([]byte, 1)
		if _, err := io.ReadFull(localConn, domainLenBuf); err != nil {
			log.Printf("SOCKS5: domain len read error: %v", err)
			return
		}
		domainLen := int(domainLenBuf[0])
		extraBytes = domainLen + 2
		extra := make([]byte, extraBytes)
		if _, err := io.ReadFull(localConn, extra); err != nil {
			log.Printf("SOCKS5: domain read error: %v", err)
			return
		}
		domain := string(extra[0:domainLen])
		port := int(extra[domainLen])<<8 | int(extra[domainLen+1])
		targetAddr = fmt.Sprintf("%s:%d", domain, port)
	} else if addrType == 4 { // IPv6: 16 bytes IP + 2 bytes port
		extraBytes = 18
		extra := make([]byte, extraBytes)
		if _, err := io.ReadFull(localConn, extra); err != nil {
			log.Printf("SOCKS5: IPv6 read error: %v", err)
			return
		}
		ip := net.IP(extra[0:16])
		port := int(extra[16])<<8 | int(extra[17])
		targetAddr = fmt.Sprintf("[%s]:%d", ip.String(), port)
	} else {
		log.Printf("SOCKS5: unsupported address type %d", addrType)
		return
	}

	if cmd != 1 { // 只支持 connect
		log.Printf("SOCKS5: unsupported command %d", cmd)
		return
	}

	log.Printf("SOCKS5: connecting to %s", targetAddr)

	// 通过 SSH 连接目标
	nativeClient, err := sshClient.GetNativeClient()
	if err != nil {
		log.Printf("SOCKS5: get native client error: %v", err)
		localConn.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}

	// Dial 加超时，防止 SSH 连接已断开时永久阻塞
	type dialResult struct {
		conn net.Conn
		err  error
	}
	dialCh := make(chan dialResult, 1)
	go func() {
		remoteConn, err := nativeClient.Dial("tcp", targetAddr)
		dialCh <- dialResult{conn: remoteConn, err: err}
	}()

	var remoteConn net.Conn
	select {
	case res := <-dialCh:
		if res.err != nil {
			log.Printf("SOCKS5: dial error: %v", res.err)
			localConn.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
			return
		}
		remoteConn = res.conn
		log.Printf("SOCKS5: connected to %s, forwarding", targetAddr)
	case <-time.After(15 * time.Second):
		log.Printf("SOCKS5: dial timeout to %s (SSH connection may be dead)", targetAddr)
		localConn.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	// 响应成功
	localConn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})

	// 同步转发
	go func() {
		defer remoteConn.Close()
		defer localConn.Close()
		io.Copy(remoteConn, localConn)
		log.Printf("SOCKS5: forward remote->local done %s", targetAddr)
	}()

	io.Copy(localConn, remoteConn)
	log.Printf("SOCKS5: forward local->remote done %s", targetAddr)
}

func buildJumpChain(server *model.Server) ([]ssh.JumpServer, error) {
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

	// 反转顺序
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

func StopTunnel(serverID uint) error {
	tunnelsMu.Lock()
	defer tunnelsMu.Unlock()

	tunnel, ok := tunnels[serverID]
	if !ok {
		return fmt.Errorf("tunnel not found")
	}

	if tunnel.SSHClient != nil {
		tunnel.SSHClient.Close()
	}

	delete(tunnels, serverID)
	log.Printf("Tunnel stopped: server_id=%d", serverID)
	return nil
}

func ListTunnels() []Tunnel {
	tunnelsMu.RLock()
	defer tunnelsMu.RUnlock()

	result := make([]Tunnel, 0, len(tunnels))
	for _, t := range tunnels {
		result = append(result, *t)
	}
	return result
}

func GetTunnel(serverID uint) *Tunnel {
	tunnelsMu.RLock()
	defer tunnelsMu.RUnlock()
	return tunnels[serverID]
}

func init() {
	nextPort = findAvailablePort(10800)
}
