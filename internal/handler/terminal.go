package handler

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"deploy-manager/internal/database"
	"deploy-manager/internal/model"
	"deploy-manager/internal/service"
	sshPkg "deploy-manager/pkg/ssh"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type TerminalSession struct {
	WS      *websocket.Conn
	Stdin   io.WriteCloser
	Stdout  io.Reader
	Stderr  io.Reader
	Server  *model.Server
	Session *ssh.Session
	mu      sync.Mutex
}

var (
	terminalSessions = make(map[string]*TerminalSession)
	sessionMu        sync.RWMutex
)

type TerminalHandler struct{}

func NewTerminalHandler() *TerminalHandler {
	return &TerminalHandler{}
}

func (h *TerminalHandler) Connect(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server id"})
		return
	}

	cols, _ := strconv.Atoi(c.DefaultQuery("cols", "80"))
	rows, _ := strconv.Atoi(c.DefaultQuery("rows", "40"))

	var server model.Server
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	// 创建 SSH 客户端
	sshClient := sshPkg.NewClient(server.IP, server.Port, server.Username, server.Password, server.SSHKey)
	sshClient.JumpEnabled = server.JumpEnabled
	sshClient.JumpHost = server.JumpIP
	sshClient.JumpPort = server.JumpPort
	sshClient.JumpUser = server.JumpUser
	sshClient.JumpPassword = server.JumpPassword
	sshClient.JumpKey = server.JumpKey
	sshClient.ProxyEnabled = server.ProxyEnabled
	sshClient.ProxyType = server.ProxyType
	sshClient.ProxyHost = server.ProxyHost
	sshClient.ProxyPort = server.ProxyPort

	if server.JumpServerID > 0 {
		chain, err := service.SSHSvc.BuildJumpChain(&server)
		if err != nil {
			conn.WriteMessage(websocket.TextMessage, []byte("SSH连接失败: 构建跳板机链失败: "+err.Error()+"\r\n"))
			conn.Close()
			return
		}
		sshClient.JumpChain = chain
		sshClient.JumpEnabled = true
		log.Printf("[Terminal] 使用多级跳板机, ChainLen=%d, 目标服务器: %s:%d user=%s hasPwd=%v", len(chain), server.IP, server.Port, server.Username, server.Password != "")
	}

	if err := sshClient.Connect(); err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("SSH连接失败: "+err.Error()+"\r\n"))
		conn.Close()
		return
	}

	// 获取原生 SSH 客户端
	nativeClient, err := sshClient.GetNativeClient()
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("获取SSH客户端失败: "+err.Error()+"\r\n"))
		conn.Close()
		return
	}

	// 创建 SSH 会话
	session, err := nativeClient.NewSession()
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("创建会话失败: "+err.Error()+"\r\n"))
		conn.Close()
		return
	}

	// 设置 PTY
	if err := session.RequestPty("xterm-256color", cols, rows, ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("请求PTY失败: "+err.Error()+"\r\n"))
		conn.Close()
		return
	}

	// 获取 stdin
	stdin, err := session.StdinPipe()
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Failed to get stdin: "+err.Error()))
		conn.Close()
		return
	}

	// 获取 stdout
	stdout, err := session.StdoutPipe()
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Failed to get stdout: "+err.Error()))
		conn.Close()
		return
	}

	// 获取 stderr
	stderr, err := session.StderrPipe()
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Failed to get stderr: "+err.Error()))
		conn.Close()
		return
	}

	// 启动 shell，优先使用 bash
	if err := session.Shell(); err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Failed to start shell: "+err.Error()))
		conn.Close()
		return
	}
	// 尝试切换到 bash
	session.Run("bash --login 2>/dev/null || bash 2>/dev/null || true")

	sessionID := strconv.FormatUint(id, 10)
	terminalSession := &TerminalSession{
		WS:      conn,
		Stdin:   stdin,
		Stdout:  stdout,
		Stderr:  stderr,
		Server:  &server,
		Session: session,
	}

	sessionMu.Lock()
	terminalSessions[sessionID] = terminalSession
	sessionMu.Unlock()

	// 启动读取 SSH 输出的 goroutine
	go h.handleSSHOutput(terminalSession, sessionID)
	// 启动读取 SSH stderr 的 goroutine
	go h.handleSSHErr(terminalSession, sessionID)
	// 启动读取 WebSocket 输入的 goroutine
	go h.handleWSRead(terminalSession, sessionID)

	c.JSON(http.StatusOK, gin.H{"session_id": sessionID})
}

func (h *TerminalHandler) handleSSHOutput(session *TerminalSession, sessionID string) {
	defer func() {
		session.WS.Close()
		sessionMu.Lock()
		delete(terminalSessions, sessionID)
		sessionMu.Unlock()
	}()

	buf := make([]byte, 1024)
	for {
		n, err := session.Stdout.Read(buf)
		if err != nil {
			log.Printf("SSH stdout error: %v", err)
			return
		}
		if n > 0 {
			log.Printf("SSH output: %s", string(buf[:n]))
			session.mu.Lock()
			session.WS.WriteMessage(websocket.BinaryMessage, buf[:n])
			session.mu.Unlock()
		}
	}
}

func (h *TerminalHandler) handleSSHErr(session *TerminalSession, sessionID string) {
	buf := make([]byte, 1024)
	for {
		n, err := session.Stderr.Read(buf)
		if err != nil {
			return
		}
		if n > 0 {
			session.mu.Lock()
			session.WS.WriteMessage(websocket.BinaryMessage, buf[:n])
			session.mu.Unlock()
		}
	}
}

func (h *TerminalHandler) handleWSRead(session *TerminalSession, sessionID string) {
	defer func() {
		session.Stdin.Close()
		session.WS.Close()
		sessionMu.Lock()
		delete(terminalSessions, sessionID)
		sessionMu.Unlock()
	}()

	for {
		_, message, err := session.WS.ReadMessage()
		if err != nil {
			return
		}

		// 尝试解析 JSON
		var msg struct {
			Type string `json:"type"`
			Data string `json:"data"`
			Cols int    `json:"cols"`
			Rows int    `json:"rows"`
		}

		if err := json.Unmarshal(message, &msg); err != nil {
			// 不是 JSON，直接发送原始数据
			session.Stdin.Write(message)
			continue
		}

		// 是 JSON，根据类型处理
		switch msg.Type {
		case "input":
			session.Stdin.Write([]byte(msg.Data))
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 && session.Session != nil {
				session.Session.WindowChange(msg.Rows, msg.Cols)
			}
		}
	}
}

func (h *TerminalHandler) Disconnect(c *gin.Context) {
	sessionID := c.Param("id")

	sessionMu.Lock()
	session, exists := terminalSessions[sessionID]
	sessionMu.Unlock()

	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	if session.Session != nil {
		session.Session.Close()
	}
	if session.Stdin != nil {
		session.Stdin.Close()
	}
	if session.WS != nil {
		session.WS.Close()
	}

	sessionMu.Lock()
	delete(terminalSessions, sessionID)
	sessionMu.Unlock()

	c.JSON(http.StatusOK, gin.H{"message": "disconnected"})
}

func (h *TerminalHandler) ListSessions(c *gin.Context) {
	sessionMu.RLock()
	sessions := make([]gin.H, 0, len(terminalSessions))
	for id, session := range terminalSessions {
		sessions = append(sessions, gin.H{
			"session_id":  id,
			"server_id":   session.Server.ID,
			"server_name": session.Server.Name,
			"server_ip":   session.Server.IP,
		})
	}
	sessionMu.RUnlock()

	c.JSON(http.StatusOK, gin.H{"sessions": sessions})
}

var rdpSessions = make(map[uint]*rdpSessionInfo)
var rdpSessionMu sync.RWMutex

type rdpSessionInfo struct {
	Server    *model.Server
	LocalPort int
	SSHClient *sshPkg.Client
}

func (h *TerminalHandler) ConnectRDP(c *gin.Context) {
	id := c.Param("id")
	var server model.Server
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	log.Printf("[RDP] 请求连接: %s, jump=%v, proxy=%v, JumpIP=%s, ProxyHost=%s",
		server.IP, server.JumpEnabled, server.ProxyEnabled, server.JumpIP, server.ProxyHost)

	rdpSessionMu.Lock()
	if info, exists := rdpSessions[server.ID]; exists {
		rdpSessionMu.Unlock()
		c.JSON(http.StatusOK, gin.H{
			"host":    "127.0.0.1",
			"port":    info.LocalPort,
			"message": "复用现有会话",
		})
		return
	}

	var sshClient *sshPkg.Client

	if server.JumpEnabled && server.JumpIP != "" {
		log.Printf("[RDP] 使用跳板机: %s:%d", server.JumpIP, server.JumpPort)
		sshClient = sshPkg.NewClient(server.JumpIP, server.JumpPort, server.JumpUser, server.JumpPassword, server.JumpKey)
		sshClient.ProxyEnabled = server.ProxyEnabled
		sshClient.ProxyType = server.ProxyType
		sshClient.ProxyHost = server.ProxyHost
		sshClient.ProxyPort = server.ProxyPort
		if err := sshClient.Connect(); err != nil {
			rdpSessionMu.Unlock()
			c.JSON(http.StatusOK, gin.H{"error": "跳板机连接失败: " + err.Error()})
			return
		}
		localPort, err := sshClient.LocalPortForward(server.IP, 3389)
		if err != nil {
			sshClient.Close()
			rdpSessionMu.Unlock()
			c.JSON(http.StatusOK, gin.H{"error": "端口转发失败: " + err.Error()})
			return
		}
		log.Printf("[RDP] 跳板机隧道: localhost:%d -> %s:3389", localPort, server.IP)
		rdpSessions[server.ID] = &rdpSessionInfo{
			Server:    &server,
			LocalPort: localPort,
			SSHClient: sshClient,
		}
		rdpSessionMu.Unlock()
		c.JSON(http.StatusOK, gin.H{
			"host":    "127.0.0.1",
			"port":    localPort,
			"message": "使用跳板机连接",
		})
		return
	} else if server.ProxyEnabled && server.ProxyHost != "" {
		log.Printf("[RDP] 使用代理: %s:%d (type: %s)", server.ProxyHost, server.ProxyPort, server.ProxyType)
		sshClient = sshPkg.NewClient(server.IP, 3389, "", "", "")
		sshClient.ProxyEnabled = true
		sshClient.ProxyType = server.ProxyType
		sshClient.ProxyHost = server.ProxyHost
		sshClient.ProxyPort = server.ProxyPort
		localPort, err := sshClient.LocalPortForward("127.0.0.1", 3389)
		if err != nil {
			sshClient.Close()
			rdpSessionMu.Unlock()
			c.JSON(http.StatusOK, gin.H{"error": "代理连接失败: " + err.Error()})
			return
		}
		log.Printf("[RDP] 代理隧道: localhost:%d -> 127.0.0.1:3389", localPort)
		rdpSessions[server.ID] = &rdpSessionInfo{
			Server:    &server,
			LocalPort: localPort,
			SSHClient: sshClient,
		}
		rdpSessionMu.Unlock()
		c.JSON(http.StatusOK, gin.H{
			"host":    "127.0.0.1",
			"port":    localPort,
			"message": "使用代理连接",
		})
		return
	} else {
		log.Printf("[RDP] 直连: %s:3389", server.IP)
		rdpSessions[server.ID] = &rdpSessionInfo{
			Server:    &server,
			LocalPort: 3389,
			SSHClient: nil,
		}
		rdpSessionMu.Unlock()
		c.JSON(http.StatusOK, gin.H{
			"host":    server.IP,
			"port":    3389,
			"message": "直连 Windows 服务器",
		})
		return
	}
}

func (h *TerminalHandler) DisconnectRDP(c *gin.Context) {
	id := c.Param("id")
	serverID, _ := strconv.ParseUint(id, 10, 32)

	rdpSessionMu.Lock()
	if info, exists := rdpSessions[uint(serverID)]; exists {
		if info.SSHClient != nil {
			info.SSHClient.Close()
		}
		delete(rdpSessions, uint(serverID))
		log.Printf("[RDP] 断开连接: localhost:%d", info.LocalPort)
	}
	rdpSessionMu.Unlock()

	c.JSON(http.StatusOK, gin.H{"message": "RDP 会话已断开"})
}

type ContainerTerminalSession struct {
	WS      *websocket.Conn
	Stdin   io.WriteCloser
	Stdout  io.Reader
	Stderr  io.Reader
	Server  *model.Server
	Session *ssh.Session
	mu      sync.Mutex
}

var (
	containerTerminalSessions = make(map[string]*ContainerTerminalSession)
	containerSessionMu        sync.RWMutex
)

func (h *TerminalHandler) ConnectContainerTerminal(c *gin.Context) {
	serverID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server id"})
		return
	}

	containerID := c.Param("containerId")
	if containerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "container id is required"})
		return
	}

	cols, _ := strconv.Atoi(c.DefaultQuery("cols", "80"))
	rows, _ := strconv.Atoi(c.DefaultQuery("rows", "40"))

	var server model.Server
	if err := database.DB.First(&server, serverID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	sshClient := sshPkg.NewClient(server.IP, server.Port, server.Username, server.Password, server.SSHKey)
	sshClient.JumpEnabled = server.JumpEnabled
	sshClient.JumpHost = server.JumpIP
	sshClient.JumpPort = server.JumpPort
	sshClient.JumpUser = server.JumpUser
	sshClient.JumpPassword = server.JumpPassword
	sshClient.JumpKey = server.JumpKey
	sshClient.ProxyEnabled = server.ProxyEnabled
	sshClient.ProxyType = server.ProxyType
	sshClient.ProxyHost = server.ProxyHost
	sshClient.ProxyPort = server.ProxyPort
	if err := sshClient.Connect(); err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("SSH连接失败: "+err.Error()+"\r\n"))
		conn.Close()
		return
	}

	nativeClient, err := sshClient.GetNativeClient()
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("获取SSH客户端失败: "+err.Error()+"\r\n"))
		conn.Close()
		return
	}

	session, err := nativeClient.NewSession()
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("创建会话失败: "+err.Error()+"\r\n"))
		conn.Close()
		return
	}

	if err := session.RequestPty("xterm-256color", cols, rows, ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("请求PTY失败: "+err.Error()+"\r\n"))
		conn.Close()
		return
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Failed to get stdin: "+err.Error()))
		conn.Close()
		return
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Failed to get stdout: "+err.Error()))
		conn.Close()
		return
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Failed to get stderr: "+err.Error()))
		conn.Close()
		return
	}

	cmd := "docker exec -it " + containerID + " sh"
	if err := session.Start(cmd); err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("启动容器终端失败: "+err.Error()+"\r\n"))
		conn.Close()
		return
	}

	sessionID := strconv.FormatUint(serverID, 10) + "-" + containerID
	terminalSession := &ContainerTerminalSession{
		WS:      conn,
		Stdin:   stdin,
		Stdout:  stdout,
		Stderr:  stderr,
		Server:  &server,
		Session: session,
	}

	containerSessionMu.Lock()
	containerTerminalSessions[sessionID] = terminalSession
	containerSessionMu.Unlock()

	go h.handleContainerSSHOutput(terminalSession, sessionID)
	go h.handleContainerSSHErr(terminalSession, sessionID)
	go h.handleContainerWSRead(terminalSession, sessionID)

	c.JSON(http.StatusOK, gin.H{"session_id": sessionID})
}

func (h *TerminalHandler) handleContainerSSHOutput(session *ContainerTerminalSession, sessionID string) {
	defer func() {
		session.WS.Close()
		containerSessionMu.Lock()
		delete(containerTerminalSessions, sessionID)
		containerSessionMu.Unlock()
	}()

	buf := make([]byte, 1024)
	for {
		n, err := session.Stdout.Read(buf)
		if err != nil {
			return
		}
		if n > 0 {
			session.mu.Lock()
			session.WS.WriteMessage(websocket.BinaryMessage, buf[:n])
			session.mu.Unlock()
		}
	}
}

func (h *TerminalHandler) handleContainerSSHErr(session *ContainerTerminalSession, sessionID string) {
	buf := make([]byte, 1024)
	for {
		n, err := session.Stderr.Read(buf)
		if err != nil {
			return
		}
		if n > 0 {
			session.mu.Lock()
			session.WS.WriteMessage(websocket.BinaryMessage, buf[:n])
			session.mu.Unlock()
		}
	}
}

func (h *TerminalHandler) handleContainerWSRead(session *ContainerTerminalSession, sessionID string) {
	defer func() {
		session.Stdin.Close()
		session.WS.Close()
		containerSessionMu.Lock()
		delete(containerTerminalSessions, sessionID)
		containerSessionMu.Unlock()
	}()

	for {
		_, message, err := session.WS.ReadMessage()
		if err != nil {
			return
		}

		var msg struct {
			Type string `json:"type"`
			Data string `json:"data"`
			Cols int    `json:"cols"`
			Rows int    `json:"rows"`
		}

		if err := json.Unmarshal(message, &msg); err != nil {
			session.Stdin.Write(message)
			continue
		}

		switch msg.Type {
		case "input":
			session.Stdin.Write([]byte(msg.Data))
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 && session.Session != nil {
				session.Session.WindowChange(msg.Rows, msg.Cols)
			}
		}
	}
}

func (h *TerminalHandler) ListContainerFiles(c *gin.Context) {
	serverID := c.Query("server_id")
	containerID := c.Param("containerId")
	path := c.DefaultQuery("path", "/")

	if serverID == "" || containerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server_id and container_id are required"})
		return
	}

	id, err := strconv.ParseUint(serverID, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server_id"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	cmd := fmt.Sprintf("docker exec %s ls -la '%s'", containerID, path)
	output, err := service.SSHSvc.ExecuteCommand(&server, cmd, 15*time.Second)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list files: " + err.Error()})
		return
	}

	var files []map[string]interface{}
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 9 {
			name := strings.Join(parts[8:], " ")
			if name == "." || name == ".." {
				continue
			}
			isDir := strings.HasPrefix(parts[0], "d")
			size := int64(0)
			if !isDir {
				fmt.Sscanf(parts[4], "%d", &size)
			}
			fullPath := path
			if !strings.HasSuffix(fullPath, "/") {
				fullPath += "/"
			}
			files = append(files, map[string]interface{}{
				"name":    name,
				"size":    size,
				"is_dir":  isDir,
				"path":    fullPath + name,
				"mode":    parts[0],
				"modTime": strings.Join(parts[5:8], " "),
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"files": files, "path": path})
}

func (h *TerminalHandler) ReadContainerFile(c *gin.Context) {
	serverID := c.Query("server_id")
	containerID := c.Param("containerId")
	path := c.Query("path")

	if serverID == "" || containerID == "" || path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server_id, container_id and path are required"})
		return
	}

	id, err := strconv.ParseUint(serverID, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server_id"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	cmd := fmt.Sprintf("docker exec %s sh -c 'cat %s'", containerID, path)
	output, err := service.SSHSvc.ExecuteCommand(&server, cmd, 30*time.Second)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read file: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"content": output, "path": path})
}

func (h *TerminalHandler) WriteContainerFile(c *gin.Context) {
	serverID := c.Query("server_id")
	containerID := c.Param("containerId")
	path := c.Query("path")

	if serverID == "" || containerID == "" || path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server_id, container_id and path are required"})
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "content is required"})
		return
	}

	id, err := strconv.ParseUint(serverID, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server_id"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	tmpPath := "/tmp/upload_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	cmd := fmt.Sprintf("echo '%s' > %s", req.Content, tmpPath)
	_, err = service.SSHSvc.ExecuteCommand(&server, cmd, 30*time.Second)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write temp file: " + err.Error()})
		return
	}

	cmd = fmt.Sprintf("docker cp %s %s:%s", tmpPath, containerID, path)
	_, err = service.SSHSvc.ExecuteCommand(&server, cmd, 60*time.Second)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to copy file to container: " + err.Error()})
		return
	}

	cmd = fmt.Sprintf("rm %s", tmpPath)
	service.SSHSvc.ExecuteCommand(&server, cmd, 10*time.Second)

	c.JSON(http.StatusOK, gin.H{"message": "File saved successfully"})
}

func (h *TerminalHandler) UploadContainerFile(c *gin.Context) {
	serverID := c.PostForm("server_id")
	containerID := c.Param("containerId")
	path := c.PostForm("path")

	if serverID == "" || containerID == "" || path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server_id, container_id and path are required"})
		return
	}

	id, err := strconv.ParseUint(serverID, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server_id"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read file"})
		return
	}

	remoteTmpPath := "/tmp/upload_" + strconv.FormatInt(time.Now().UnixNano(), 10)

	chunkSize := 1024 * 50 // 50KB chunks to reduce load through jump server
	for i := 0; i < len(content); i += chunkSize {
		end := i + chunkSize
		if end > len(content) {
			end = len(content)
		}
		chunk := content[i:end]
		base64Chunk := base64.StdEncoding.EncodeToString(chunk)

		appendOp := ">>"
		if i == 0 {
			appendOp = ">"
		}
		cmd := fmt.Sprintf("echo '%s' | base64 -d %s %s", base64Chunk, appendOp, remoteTmpPath)
		_, err = service.SSHSvc.ExecuteCommand(&server, cmd, 60*time.Second)
		if err != nil {
			log.Printf("Upload chunk error: %v", err)
			service.SSHSvc.ExecuteCommand(&server, "rm -f "+remoteTmpPath, 10*time.Second)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write temp file on remote: " + err.Error()})
			return
		}
	}

	targetPath := path
	if !strings.HasSuffix(targetPath, "/") {
		targetPath = targetPath + "/"
	}

	cmd := fmt.Sprintf("docker cp %s %s:%s", remoteTmpPath, containerID, targetPath+header.Filename)
	_, err = service.SSHSvc.ExecuteCommand(&server, cmd, 300*time.Second)
	if err != nil {
		log.Printf("Docker cp error: %v", err)
		service.SSHSvc.ExecuteCommand(&server, "rm "+remoteTmpPath, 10*time.Second)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to copy to container: " + err.Error()})
		return
	}

	service.SSHSvc.ExecuteCommand(&server, "rm "+remoteTmpPath, 10*time.Second)
	c.JSON(http.StatusOK, gin.H{"message": "File uploaded successfully", "filename": header.Filename})
}

func (h *TerminalHandler) DownloadContainerFile(c *gin.Context) {
	serverID := c.Query("server_id")
	containerID := c.Param("containerId")
	path := c.Query("path")

	if serverID == "" || containerID == "" || path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server_id, container_id and path are required"})
		return
	}

	id, err := strconv.ParseUint(serverID, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server_id"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	downloadTmpPath := "/tmp/download_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	filename := filepath.Base(path)

	cmd := fmt.Sprintf("docker cp %s:%s %s", containerID, path, downloadTmpPath)
	_, err = service.SSHSvc.ExecuteCommand(&server, cmd, 300*time.Second)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to copy file: " + err.Error()})
		return
	}

	cmd = fmt.Sprintf("cat %s", downloadTmpPath)
	output, err := service.SSHSvc.ExecuteCommand(&server, cmd, 300*time.Second)
	if err != nil {
		service.SSHSvc.ExecuteCommand(&server, "rm "+downloadTmpPath, 10*time.Second)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read file: " + err.Error()})
		return
	}

	service.SSHSvc.ExecuteCommand(&server, "rm "+downloadTmpPath, 10*time.Second)

	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Data(http.StatusOK, "application/octet-stream", []byte(output))
}

func (h *TerminalHandler) CreateContainerDir(c *gin.Context) {
	serverID := c.Query("server_id")
	containerID := c.Param("containerId")

	if serverID == "" || containerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server_id and container_id are required"})
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
		return
	}

	id, err := strconv.ParseUint(serverID, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server_id"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	cmd := fmt.Sprintf("docker exec %s sh -c 'mkdir -p %s'", containerID, req.Path)
	_, err = service.SSHSvc.ExecuteCommand(&server, cmd, 30*time.Second)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create directory: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Directory created successfully"})
}

func (h *TerminalHandler) DeleteContainerFile(c *gin.Context) {
	serverID := c.Query("server_id")
	containerID := c.Param("containerId")
	path := c.Query("path")

	if serverID == "" || containerID == "" || path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server_id, container_id and path are required"})
		return
	}

	id, err := strconv.ParseUint(serverID, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server_id"})
		return
	}

	var server model.Server
	if err := database.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}

	cmd := fmt.Sprintf("docker exec %s sh -c 'rm -rf %s'", containerID, path)
	_, err = service.SSHSvc.ExecuteCommand(&server, cmd, 30*time.Second)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Deleted successfully"})
}
