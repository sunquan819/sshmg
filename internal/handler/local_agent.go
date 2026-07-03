package handler

import (
	"bufio"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type AgentInfo struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Command         string `json:"command"`
	Path            string `json:"path"`
	Installed       bool   `json:"installed"`
	SupportsHistory bool   `json:"supportsHistory"`
	SupportsLatest  bool   `json:"supportsLatest"`
	HistoryWarning  string `json:"historyWarning,omitempty"`
}

type AgentSessionInfo struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	ProjectPath string    `json:"projectPath,omitempty"`
	Updated     time.Time `json:"updated"`
}

type agentDefinition struct {
	ID              string
	Name            string
	Command         string
	SupportsHistory bool
	SupportsLatest  bool
}

var localAgentRegistry = []agentDefinition{
	{ID: "opencode", Name: "opencode", Command: "opencode", SupportsHistory: true, SupportsLatest: true},
	{ID: "codex", Name: "Codex", Command: "codex", SupportsHistory: true, SupportsLatest: true},
	{ID: "claude", Name: "Claude", Command: "claude"},
	{ID: "gemini", Name: "Gemini", Command: "gemini"},
	{ID: "aider", Name: "Aider", Command: "aider"},
	{ID: "cursor-agent", Name: "Cursor Agent", Command: "cursor-agent"},
}

func agentExecutableCandidates(name string) []string {
	if runtime.GOOS != "windows" {
		return []string{name}
	}
	return []string{name + ".exe", name + ".cmd", name + ".bat", name + ".ps1", name}
}

func findAgentExecutable(name string) string {
	for _, candidate := range agentExecutableCandidates(name) {
		if p, err := exec.LookPath(candidate); err == nil {
			return p
		}
	}
	return ""
}

func listDetectedAgents() []AgentInfo {
	agents := make([]AgentInfo, 0, len(localAgentRegistry))
	for _, def := range localAgentRegistry {
		path := findAgentExecutable(def.Command)
		if path == "" {
			continue
		}
		agents = append(agents, AgentInfo{
			ID:              def.ID,
			Name:            def.Name,
			Command:         def.Command,
			Path:            path,
			Installed:       true,
			SupportsHistory: def.SupportsHistory,
			SupportsLatest:  def.SupportsLatest,
		})
	}
	return agents
}

func resolveAgentCommand(agentID, mode, sessionID string) (string, []string, string, error) {
	if mode == "" {
		mode = "new"
	}
	for _, def := range localAgentRegistry {
		if def.ID != agentID {
			continue
		}
		cmd := findAgentExecutable(def.Command)
		if cmd == "" {
			return "", nil, "", errors.New("agent executable not found")
		}
		switch mode {
		case "new":
			return cmd, nil, def.Name, nil
		case "latest":
			if !def.SupportsLatest {
				return "", nil, "", errors.New("agent does not support latest mode")
			}
			if def.ID == "opencode" {
				return cmd, []string{"-c"}, def.Name, nil
			}
			if def.ID == "codex" {
				return cmd, []string{"resume", "--last"}, def.Name, nil
			}
		case "session":
			if strings.TrimSpace(sessionID) == "" {
				return "", nil, "", errors.New("sessionId is required")
			}
			if def.ID == "opencode" {
				return cmd, []string{"-s", sessionID}, def.Name, nil
			}
			if def.ID == "codex" {
				return cmd, []string{"resume", sessionID}, def.Name, nil
			}
			return "", nil, "", errors.New("agent does not support session mode")
		default:
			return "", nil, "", errors.New("unsupported agent mode")
		}
		return "", nil, "", errors.New("unsupported agent mode")
	}
	return "", nil, "", errors.New("unknown agent")
}

func powerShellQuoteSingle(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func posixQuoteSingle(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func buildPowerShellAgentInvocation(cmd string, args []string) string {
	parts := []string{"&", powerShellQuoteSingle(cmd)}
	for _, arg := range args {
		parts = append(parts, powerShellQuoteSingle(arg))
	}
	return strings.Join(parts, " ")
}

func buildPOSIXAgentInvocation(cmd string, args []string) string {
	parts := []string{posixQuoteSingle(cmd)}
	for _, arg := range args {
		parts = append(parts, posixQuoteSingle(arg))
	}
	return strings.Join(parts, " ")
}

func wrapAgentCommandInShell(cmd string, args []string) (string, []string) {
	if runtime.GOOS == "windows" {
		shellCmd, err := exec.LookPath("powershell.exe")
		if err != nil {
			shellCmd = "powershell.exe"
		}
		reset := "[Console]::Out.Write(\"`e[0m`e[?25h`e[?1000l`e[?1002l`e[?1003l`e[?1006l`e[?2004l`e[?1049l\")"
		invocation := buildPowerShellAgentInvocation(cmd, args)
		command := "try { " + invocation + " } finally { " + reset + "; Write-Host '' }"
		return shellCmd, []string{"-NoLogo", "-NoProfile", "-NoExit", "-Command", command}
	}

	shellCmd, _, _ := detectShell()
	reset := "printf '\\033[0m\\033[?25h\\033[?1000l\\033[?1002l\\033[?1003l\\033[?1006l\\033[?2004l\\033[?1049l\\n'"
	invocation := buildPOSIXAgentInvocation(cmd, args)
	command := invocation + "; " + reset + "; exec " + posixQuoteSingle(shellCmd)
	return shellCmd, []string{"-lc", command}
}

func parseOpencodeSessionList(out string) []AgentSessionInfo {
	var sessions []AgentSessionInfo
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(strings.ToLower(line), "id ") || strings.HasPrefix(line, "-") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 1 || !strings.HasPrefix(fields[0], "ses_") {
			continue
		}
		title := ""
		if len(fields) > 3 {
			title = strings.Join(fields[3:], " ")
		}
		sessions = append(sessions, AgentSessionInfo{ID: fields[0], Title: title})
	}
	return sessions
}

func readCodexSessionIndex(path string) ([]AgentSessionInfo, string) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err.Error()
	}
	defer f.Close()

	type row struct {
		ID        string    `json:"id"`
		Timestamp time.Time `json:"timestamp"`
		CWD       string    `json:"cwd"`
		Title     string    `json:"title"`
	}

	var sessions []AgentSessionInfo
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var r row
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil || r.ID == "" {
			continue
		}
		sessions = append(sessions, AgentSessionInfo{ID: r.ID, Title: r.Title, ProjectPath: r.CWD, Updated: r.Timestamp})
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Updated.After(sessions[j].Updated) })
	return sessions, ""
}

func (h *LocalTerminalHandler) ListAgents(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"agents": listDetectedAgents()})
}

func (h *LocalTerminalHandler) ListAgentSessions(c *gin.Context) {
	agentID := c.Param("id")
	switch agentID {
	case "opencode":
		cmd := findAgentExecutable("opencode")
		if cmd == "" {
			c.JSON(http.StatusNotFound, gin.H{"sessions": []AgentSessionInfo{}, "warning": "opencode not found"})
			return
		}
		historyCmd := exec.Command(cmd, "session", "list")
		hideLocalAgentCommandWindow(historyCmd)
		out, err := historyCmd.CombinedOutput()
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"sessions": []AgentSessionInfo{}, "warning": strings.TrimSpace(string(out))})
			return
		}
		c.JSON(http.StatusOK, gin.H{"sessions": parseOpencodeSessionList(string(out))})
	case "codex":
		home, _ := os.UserHomeDir()
		sessions, warning := readCodexSessionIndex(filepath.Join(home, ".codex", "session_index.jsonl"))
		c.JSON(http.StatusOK, gin.H{"sessions": sessions, "warning": warning})
	default:
		c.JSON(http.StatusOK, gin.H{"sessions": []AgentSessionInfo{}, "warning": "history is not supported for this agent"})
	}
}
