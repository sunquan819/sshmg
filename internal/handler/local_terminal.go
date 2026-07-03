package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"deploy-manager/internal/config"
	"deploy-manager/pkg/assets"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type LocalSession struct {
	ID        string    `json:"id"`
	Created   time.Time `json:"created"`
	PID       int       `json:"pid"`
	ShellName string    `json:"shellName"`
	pty       *pty
	ws        *websocket.Conn
	done      chan struct{}
	cols      uint16
	rows      uint16
	mu        sync.Mutex
}

type LocalTerminalManager struct {
	sessions map[string]*LocalSession
	mu       sync.RWMutex
	counter  atomic.Uint64
}

var localManager = &LocalTerminalManager{
	sessions: make(map[string]*LocalSession),
}

func newLocalSessionID() string {
	n := localManager.counter.Add(1)
	var b [4]byte
	rand.Read(b[:])
	return "local-" + strconv.FormatUint(n, 10) + "-" + hex.EncodeToString(b[:])
}

type LocalTerminalHandler struct{}

func NewLocalTerminalHandler() *LocalTerminalHandler {
	return &LocalTerminalHandler{}
}

type localSessionCreateRequest struct {
	AgentID   string `json:"agentId"`
	Mode      string `json:"mode"`
	SessionID string `json:"sessionId"`
}

func resolveLocalSessionLaunch(req localSessionCreateRequest) (string, []string, string, bool, error) {
	if strings.TrimSpace(req.AgentID) == "" {
		cmd, args, name := detectShell()
		return cmd, args, name, false, nil
	}
	cmd, args, name, err := resolveAgentCommand(req.AgentID, req.Mode, req.SessionID)
	if err != nil {
		return "", nil, "", true, err
	}
	shellCmd, shellArgs := wrapAgentCommandInShell(cmd, args)
	return shellCmd, shellArgs, name, true, nil
}

func (h *LocalTerminalHandler) ListSessions(c *gin.Context) {
	localManager.mu.RLock()
	sessions := make([]*LocalSession, 0, len(localManager.sessions))
	for _, s := range localManager.sessions {
		sessions = append(sessions, s)
	}
	localManager.mu.RUnlock()
	c.JSON(http.StatusOK, gin.H{"sessions": sessions})
}

func (h *LocalTerminalHandler) ListDirs(c *gin.Context) {
	dirPath := c.Query("path")
	if dirPath == "" {
		if runtime.GOOS == "windows" {
			dirPath = os.Getenv("USERPROFILE")
		} else {
			dirPath = os.Getenv("HOME")
		}
	}
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	type dirEntry struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		IsDir bool   `json:"isDir"`
	}
	var dirs []dirEntry
	// Add parent directory
	parent := filepath.Dir(dirPath)
	if parent != dirPath {
		dirs = append(dirs, dirEntry{Name: "..", Path: parent, IsDir: true})
	}
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, dirEntry{Name: e.Name(), Path: filepath.Join(dirPath, e.Name()), IsDir: true})
		}
	}
	c.JSON(http.StatusOK, gin.H{"path": dirPath, "dirs": dirs})
}

func (h *LocalTerminalHandler) CreateSession(c *gin.Context) {
	var req localSessionCreateRequest
	if c.Request.Body != nil {
		_ = c.ShouldBindJSON(&req)
	}
	shellCmd, shellArgs, shellName, isAgentSession, err := resolveLocalSessionLaunch(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	log.Printf("[LocalTerminal] creating session, shell=%s args=%v name=%s agent=%v", shellCmd, shellArgs, shellName, isAgentSession)
	cmd := exec.Command(shellCmd, shellArgs...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	if runtime.GOOS == "windows" && strings.Contains(shellCmd, "bash") {
		cmd.Env = append(cmd.Env, "CHERE_INVOKING=1")
	}

	pty, err := openPTY(cmd, 80, 24)
	if err != nil {
		log.Printf("[LocalTerminal] openPTY failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "openPTY: " + err.Error()})
		return
	}

	done := make(chan struct{})
	session := &LocalSession{
		ID:        newLocalSessionID(),
		Created:   time.Now(),
		PID:       pty.Pid(),
		ShellName: shellName,
		pty:       pty,
		done:      done,
		cols:      80,
		rows:      24,
	}

	localManager.mu.Lock()
	localManager.sessions[session.ID] = session
	localManager.mu.Unlock()

	if !isAgentSession {
		// Inject shell integration script after shell initializes
		go func() {
			time.Sleep(500 * time.Millisecond)
			if runtime.GOOS == "windows" {
				if strings.Contains(shellName, "PowerShell") || strings.Contains(shellCmd, "powershell") {
					// Write script to temp file and source it (encoding set in script)
					tmpFile := filepath.Join(os.TempDir(), "dm-shell-integration.ps1")
					os.WriteFile(tmpFile, assets.ShellIntegrationPowershell, 0644)
					pty.Write([]byte(". '" + tmpFile + "'\r"))
				} else if strings.Contains(shellCmd, "bash") {
					tmpFile := filepath.Join(os.TempDir(), "dm-shell-integration.sh")
					os.WriteFile(tmpFile, assets.ShellIntegrationBash, 0644)
					pty.Write([]byte("source '" + tmpFile + "'\r"))
				}
			} else {
				tmpFile := filepath.Join(os.TempDir(), "dm-shell-integration.sh")
				os.WriteFile(tmpFile, assets.ShellIntegrationBash, 0644)
				pty.Write([]byte("source '" + tmpFile + "'\r"))
			}
		}()
	}

	go func() {
		// ponytail: use library Wait with a long-lived context instead of exec.Cmd.Wait().
		// exec.Cmd.Wait() needs a valid OS process handle, which fails after Close() in cleanup paths.
		// The conpty library has its own wait using the create-process handle.
		// For now, we just block on done — process exit is signaled by PTY read error in Connect().
		<-session.done
		localManager.mu.Lock()
		delete(localManager.sessions, session.ID)
		localManager.mu.Unlock()
		log.Printf("[LocalTerminal] session %s cleaned up", session.ID)
	}()

	log.Printf("[LocalTerminal] session %s created (pid=%d)", session.ID, session.PID)
	c.JSON(http.StatusOK, gin.H{"id": session.ID, "pid": session.PID, "created": session.Created})
}

func (h *LocalTerminalHandler) CloseSession(c *gin.Context) {
	id := c.Param("id")
	localManager.mu.Lock()
	session, ok := localManager.sessions[id]
	if ok {
		delete(localManager.sessions, id)
	}
	localManager.mu.Unlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	closeSession(session)
	c.JSON(http.StatusOK, gin.H{"status": "closed"})
}

func closeSession(session *LocalSession) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.ws != nil {
		session.ws.Close()
		session.ws = nil
	}
	if session.pty != nil {
		session.pty.close()
	}
	select {
	case <-session.done:
	default:
		close(session.done)
	}
}

func (h *LocalTerminalHandler) Connect(c *gin.Context) {
	id := c.Param("id")

	localManager.mu.RLock()
	session, ok := localManager.sessions[id]
	localManager.mu.RUnlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[LocalTerminal] WS upgrade failed: %v", err)
		return
	}

	session.mu.Lock()
	session.ws = conn
	session.mu.Unlock()

	// Send shell metadata to frontend
	conn.WriteJSON(map[string]interface{}{
		"type":      "meta",
		"shell":     session.ShellName,
		"pid":       session.PID,
		"sessionId": session.ID,
	})

	defer func() {
		session.mu.Lock()
		session.ws = nil
		session.mu.Unlock()
		conn.Close()
		closeSession(session)
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := session.pty.Read(buf)
			if n > 0 {
				session.mu.Lock()
				ws := session.ws
				session.mu.Unlock()
				if ws != nil {
					if writeErr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
						return
					}
				}
			}
			if err != nil {
				log.Printf("[LocalTerminal] session %s PTY read ended: %v", session.ID, err)
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		for {
			_, message, err := conn.ReadMessage()
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
				session.pty.Write(message)
				continue
			}
			switch msg.Type {
			case "input":
				session.pty.Write([]byte(msg.Data))
			case "resize":
				if msg.Cols > 0 && msg.Rows > 0 {
					session.mu.Lock()
					session.cols = uint16(msg.Cols)
					session.rows = uint16(msg.Rows)
					session.mu.Unlock()
					session.pty.resize(uint16(msg.Cols), uint16(msg.Rows))
				}
			}
		}
	}()

	wg.Wait()
}

func getLocalShell() string {
	cmd, _, _ := detectShell()
	return cmd
}

// detectShell returns (command, args, displayName) for the best available shell.
// On Windows, priority: config override > PowerShell > Git Bash > CMD.
// On Unix, priority: config override > zsh > bash.
func detectShell() (cmd string, args []string, name string) {
	if cfg := config.GlobalConfig; cfg != nil && cfg.LocalShell.Command != "" {
		return cfg.LocalShell.Command, cfg.LocalShell.Args, filepath.Base(cfg.LocalShell.Command)
	}

	if runtime.GOOS != "windows" {
		if runtime.GOOS == "darwin" {
			return "/bin/zsh", nil, "zsh"
		}
		return "/bin/bash", nil, "bash"
	}

	// Windows: PowerShell (default in Windows Terminal)
	if psPath, err := exec.LookPath("powershell.exe"); err == nil {
		return psPath, []string{"-NoLogo", "-NoProfile"}, "PowerShell"
	}

	// Git Bash
	if gitBash := findGitBash(); gitBash != "" {
		return gitBash, []string{"--login", "-i"}, "Git Bash"
	}

	// CMD
	if cmdPath, err := exec.LookPath("cmd.exe"); err == nil {
		return cmdPath, nil, "CMD"
	}

	return "cmd.exe", nil, "CMD"
}

// findGitBash looks for bash.exe from a Git for Windows installation.
func findGitBash() string {
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "bash.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Git", "bin", "bash.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Git", "bin", "bash.exe"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Try resolving from git.exe in PATH
	if gitPath, err := exec.LookPath("git.exe"); err == nil {
		bashPath := filepath.Join(filepath.Dir(gitPath), "..", "bin", "bash.exe")
		if _, err := os.Stat(bashPath); err == nil {
			return bashPath
		}
	}
	return ""
}

// extractBusyBox writes the embedded BusyBox binary to a cache dir and returns its path.
// The binary is only written once; subsequent calls return the cached path.
var busyBoxPath string
var busyBoxOnce sync.Once

func extractBusyBox() (string, error) {
	var extractErr error
	busyBoxOnce.Do(func() {
		cacheDir := filepath.Join(os.TempDir(), "deploy-manager-busybox")
		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			extractErr = err
			return
		}
		path := filepath.Join(cacheDir, "busybox.exe")

		// Check if already extracted and size matches
		if info, err := os.Stat(path); err == nil && info.Size() == int64(len(assets.BusyBoxExe)) {
			busyBoxPath = path
			return
		}

		if err := os.WriteFile(path, assets.BusyBoxExe, 0755); err != nil {
			extractErr = err
			return
		}
		busyBoxPath = path
		log.Printf("[LocalTerminal] BusyBox extracted to %s", path)
	})
	if busyBoxPath == "" {
		return "", extractErr
	}
	return busyBoxPath, nil
}
