# AI Agent CLI Terminal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an AI Agent menu to the local terminal so users can launch detected CLI agents and resume supported historical sessions.

**Architecture:** Add a small backend registry for known agent CLIs, expose detection and history endpoints, and extend local terminal session creation with safe agent launch modes. The local terminal frontend fetches this registry, renders an `AI Agent` status bar menu, and starts sessions through the existing PTY/WebSocket flow.

**Tech Stack:** Go, Gin, existing local PTY abstraction, HTML/JavaScript local terminal template, Node/Playwright-style frontend regression tests.

---

## File Map

- Create: `internal/handler/local_agent.go`
  Agent registry, executable detection, command builders, history readers, and JSON DTOs.
- Create: `internal/handler/local_agent_test.go`
  Unit tests for command construction and session parsing.
- Modify: `internal/handler/local_terminal.go`
  Parse optional agent launch JSON in `CreateSession`; reuse existing shell path when absent.
- Modify: `pkg/server/server.go`
  Register `GET /api/local-terminal/agents` and `GET /api/local-terminal/agents/:id/sessions`.
- Modify: `pkg/assets/web/templates/local-terminal.html`
  Add status bar AI Agent button, menu rendering, history list, and agent session payload.
- Modify: `pkg/assets/web/templates/local-terminal-layout.test.js`
  Stub agent endpoints and verify frontend menu/payload behavior.

---

### Task 1: Backend Agent Registry

**Files:**
- Create: `internal/handler/local_agent.go`
- Test: `internal/handler/local_agent_test.go`

- [ ] **Step 1: Write failing registry tests**

Create `internal/handler/local_agent_test.go` with:

```go
package handler

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestResolveAgentCommand(t *testing.T) {
	cases := []struct {
		name      string
		agentID   string
		mode      string
		sessionID string
		wantArgs  []string
		wantErr   string
	}{
		{name: "opencode new", agentID: "opencode", mode: "new", wantArgs: nil},
		{name: "opencode latest", agentID: "opencode", mode: "latest", wantArgs: []string{"-c"}},
		{name: "opencode session", agentID: "opencode", mode: "session", sessionID: "ses_123", wantArgs: []string{"-s", "ses_123"}},
		{name: "codex new", agentID: "codex", mode: "new", wantArgs: nil},
		{name: "codex latest", agentID: "codex", mode: "latest", wantArgs: []string{"resume", "--last"}},
		{name: "codex session", agentID: "codex", mode: "session", sessionID: "abc", wantArgs: []string{"resume", "abc"}},
		{name: "missing id", agentID: "opencode", mode: "session", wantErr: "sessionId is required"},
		{name: "bad mode", agentID: "opencode", mode: "bad", wantErr: "unsupported agent mode"},
		{name: "bad agent", agentID: "nope", mode: "new", wantErr: "unknown agent"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, args, display, err := resolveAgentCommand(tc.agentID, tc.mode, tc.sessionID)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cmd == "" {
				t.Fatalf("expected command")
			}
			if display == "" {
				t.Fatalf("expected display name")
			}
			if strings.Join(args, "\x00") != strings.Join(tc.wantArgs, "\x00") {
				t.Fatalf("args = %#v, want %#v", args, tc.wantArgs)
			}
		})
	}
}

func TestParseOpencodeSessionList(t *testing.T) {
	out := "ID          Updated                 Title\nses_123     2026-07-03 10:11:12     Fix deploy\nses_456     2026-07-02 09:00:00     Add terminal\n"
	sessions := parseOpencodeSessionList(out)
	if len(sessions) != 2 {
		t.Fatalf("len = %d", len(sessions))
	}
	if sessions[0].ID != "ses_123" || sessions[0].Title != "Fix deploy" {
		t.Fatalf("first session = %#v", sessions[0])
	}
}

func TestReadCodexSessionIndex(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "session_index.jsonl")
	data := `{"id":"11111111-1111-1111-1111-111111111111","timestamp":"2026-07-03T10:00:00Z","cwd":"C:\\repo","title":"Deploy work"}` + "\n"
	if err := os.WriteFile(index, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	sessions, warning := readCodexSessionIndex(index)
	if warning != "" {
		t.Fatalf("warning = %q", warning)
	}
	if len(sessions) != 1 {
		t.Fatalf("len = %d", len(sessions))
	}
	if sessions[0].ID != "11111111-1111-1111-1111-111111111111" || sessions[0].ProjectPath == "" {
		t.Fatalf("session = %#v", sessions[0])
	}
	if sessions[0].Updated.After(time.Now().Add(24 * time.Hour)) {
		t.Fatalf("unexpected updated time: %v", sessions[0].Updated)
	}
}

func TestAgentCandidateNamesIncludeWindowsExtensions(t *testing.T) {
	names := agentExecutableCandidates("opencode")
	joined := strings.Join(names, "|")
	if runtime.GOOS == "windows" && !strings.Contains(joined, "opencode.ps1") {
		t.Fatalf("windows candidates = %v", names)
	}
}
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```powershell
go test ./internal/handler -run "TestResolveAgentCommand|TestParseOpencodeSessionList|TestReadCodexSessionIndex|TestAgentCandidateNamesIncludeWindowsExtensions"
```

Expected: fail with undefined functions such as `resolveAgentCommand`.

- [ ] **Step 3: Implement registry and parsers**

Create `internal/handler/local_agent.go` with types and functions:

```go
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
		out, err := exec.Command(cmd, "session", "list").CombinedOutput()
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
```

- [ ] **Step 4: Run tests and verify pass**

Run:

```powershell
go test ./internal/handler -run "TestResolveAgentCommand|TestParseOpencodeSessionList|TestReadCodexSessionIndex|TestAgentCandidateNamesIncludeWindowsExtensions"
```

Expected: PASS.

---

### Task 2: Agent-Aware Session Creation and Routes

**Files:**
- Modify: `internal/handler/local_terminal.go`
- Modify: `pkg/server/server.go`
- Test: `internal/handler/local_agent_test.go`

- [ ] **Step 1: Add request parsing test**

Append to `internal/handler/local_agent_test.go`:

```go
func TestResolveLocalSessionLaunchDefaultShell(t *testing.T) {
	req := localSessionCreateRequest{}
	cmd, args, name, isAgent, err := resolveLocalSessionLaunch(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd == "" || name == "" {
		t.Fatalf("cmd=%q name=%q", cmd, name)
	}
	if isAgent {
		t.Fatalf("default shell should not be an agent")
	}
	_ = args
}
```

- [ ] **Step 2: Run test and verify failure**

Run:

```powershell
go test ./internal/handler -run TestResolveLocalSessionLaunchDefaultShell
```

Expected: fail with undefined `localSessionCreateRequest` or `resolveLocalSessionLaunch`.

- [ ] **Step 3: Refactor local terminal launch resolution**

In `internal/handler/local_terminal.go`, add near `LocalTerminalHandler`:

```go
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
	return cmd, args, name, true, err
}
```

Replace the start of `CreateSession` with:

```go
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
```

Wrap shell integration injection with:

```go
if !isAgentSession {
	go func() {
		// existing shell integration body stays here
	}()
}
```

- [ ] **Step 4: Register routes**

In `pkg/server/server.go`, inside the existing `localTerminal := auth.Group("/local-terminal")` block, add:

```go
localTerminal.GET("/agents", localTerminalHandler.ListAgents)
localTerminal.GET("/agents/:id/sessions", localTerminalHandler.ListAgentSessions)
```

- [ ] **Step 5: Run backend tests**

Run:

```powershell
go test ./internal/handler
go test ./...
```

Expected: PASS.

---

### Task 3: Frontend AI Agent Menu

**Files:**
- Modify: `pkg/assets/web/templates/local-terminal.html`
- Test: `pkg/assets/web/templates/local-terminal-layout.test.js`

- [ ] **Step 1: Add failing frontend test coverage**

In `pkg/assets/web/templates/local-terminal-layout.test.js`, extend the `window.fetch` stub so `GET /agents` and `GET /agents/opencode/sessions` return deterministic data:

```js
if (url.indexOf('/api/local-terminal/agents/opencode/sessions') >= 0) {
  return Promise.resolve({ json: function() { return Promise.resolve({ sessions: [{ id: 'ses_123', title: 'Fix deploy', projectPath: 'C:/repo' }] }); } });
}
if (url.indexOf('/api/local-terminal/agents') >= 0) {
  return Promise.resolve({ json: function() { return Promise.resolve({ agents: [{ id: 'opencode', name: 'opencode', installed: true, supportsHistory: true, supportsLatest: true, path: 'C:/opencode.ps1' }] }); } });
}
```

Add assertions after terminal startup:

```js
await page.waitForFunction(() => document.querySelector('#agent-btn'));
await page.click('#agent-btn');
await page.waitForFunction(() => document.querySelector('#agent-menu').style.display === 'block');
assert.ok(await page.evaluate(() => document.querySelector('#agent-menu').textContent.includes('opencode')));
```

Add launch payload assertions:

```js
await page.evaluate(() => {
  window.__postedBodies = [];
  const originalFetch = window.fetch;
  window.fetch = function(url, opts) {
    if (opts && opts.method === 'POST') {
      window.__postedBodies.push(JSON.parse(opts.body || '{}'));
    }
    return originalFetch(url, opts);
  };
});
await page.click('[data-agent-action="opencode:new"]');
await page.waitForTimeout(50);
assert.deepStrictEqual(await page.evaluate(() => window.__postedBodies.pop()), { agentId: 'opencode', mode: 'new' });
```

- [ ] **Step 2: Run frontend test and verify failure**

Run:

```powershell
$env:NODE_PATH='C:\Users\BR\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\node_modules;C:\Users\BR\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\node_modules\.pnpm\node_modules'; node pkg/assets/web/templates/local-terminal-layout.test.js
```

Expected: fail because `#agent-btn` does not exist.

- [ ] **Step 3: Add menu markup**

In `pkg/assets/web/templates/local-terminal.html`, add this button before the reset button in the status bar:

```html
<button title="AI Agent" style="background:none;border:none;color:#999;cursor:pointer;padding:2px 6px;border-radius:3px;font-size:12px;" id="agent-btn">AI</button>
<span style="margin:0 4px;color:#444;">│</span>
```

Add menu container near `hist-menu`:

```html
<div id="agent-menu" class="popup-menu"></div>
```

- [ ] **Step 4: Add frontend behavior**

Inside the main script after history helpers, add:

```js
var agents = [];
var agentMenu = document.getElementById('agent-menu');
function fetchAgents() {
    return fetch('/api/local-terminal/agents', {headers:{'Authorization':'Bearer '+token}})
        .then(function(r){return r.json();})
        .then(function(data){agents=data.agents||[];return agents;})
        .catch(function(){agents=[];return agents;});
}
function startAgentSession(agentId, mode, sessionId) {
    createTerminalSession({agentId:agentId, mode:mode, sessionId:sessionId});
    agentMenu.style.display='none';
}
function renderAgentMenu() {
    agentMenu.innerHTML = '';
    if (!agents.length) {
        agentMenu.innerHTML = '<div class="menu-item" style="color:#888;">未检测到 AI Agent CLI</div>';
        return;
    }
    agents.forEach(function(agent){
        var header=document.createElement('div');
        header.className='menu-header';
        header.textContent=agent.name+'  '+(agent.path||'');
        agentMenu.appendChild(header);
        var fresh=document.createElement('div');
        fresh.className='menu-item';
        fresh.dataset.agentAction=agent.id+':new';
        fresh.textContent='新会话';
        fresh.onclick=function(){startAgentSession(agent.id,'new');};
        agentMenu.appendChild(fresh);
        if(agent.supportsLatest){
            var latest=document.createElement('div');
            latest.className='menu-item';
            latest.dataset.agentAction=agent.id+':latest';
            latest.textContent='继续最近一次';
            latest.onclick=function(){startAgentSession(agent.id,'latest');};
            agentMenu.appendChild(latest);
        }
        var history=document.createElement('div');
        history.className='menu-item';
        history.textContent=agent.supportsHistory?'历史 session':'历史 session 暂不支持';
        history.onclick=function(){ if(agent.supportsHistory) showAgentSessions(agent.id); };
        agentMenu.appendChild(history);
    });
}
function showAgentSessions(agentId) {
    agentMenu.innerHTML='<div class="menu-item" style="color:#888;">加载中...</div>';
    fetch('/api/local-terminal/agents/'+encodeURIComponent(agentId)+'/sessions', {headers:{'Authorization':'Bearer '+token}})
        .then(function(r){return r.json();})
        .then(function(data){
            agentMenu.innerHTML='';
            var sessions=data.sessions||[];
            if(data.warning){
                var warn=document.createElement('div');
                warn.className='menu-item';
                warn.style.color='#fbbf24';
                warn.textContent=data.warning;
                agentMenu.appendChild(warn);
            }
            if(!sessions.length){
                var empty=document.createElement('div');
                empty.className='menu-item';
                empty.style.color='#888';
                empty.textContent='暂无历史 session';
                agentMenu.appendChild(empty);
                return;
            }
            sessions.forEach(function(s){
                var item=document.createElement('div');
                item.className='menu-item';
                item.textContent=(s.title||s.id)+'  '+(s.projectPath||'');
                item.title=s.id;
                item.onclick=function(){startAgentSession(agentId,'session',s.id);};
                agentMenu.appendChild(item);
            });
        })
        .catch(function(){agentMenu.innerHTML='<div class="menu-item" style="color:#f87171;">加载历史失败</div>';});
}
document.getElementById('agent-btn').addEventListener('click', function(e) {
    e.stopPropagation();
    if (agentMenu.style.display==='block') { agentMenu.style.display='none'; return; }
    renderAgentMenu();
    agentMenu.style.display='block';
    agentMenu.style.right='12px';
    agentMenu.style.bottom='28px';
    agentMenu.style.left='auto';
    agentMenu.style.top='auto';
    setTimeout(function(){document.addEventListener('mousedown',function handler(ev){if(!agentMenu.contains(ev.target)&&ev.target.id!=='agent-btn'){agentMenu.style.display='none';document.removeEventListener('mousedown',handler);}});},50);
});
fetchAgents();
```

- [ ] **Step 5: Pass agent payload through session creation**

Change `createTerminalSession` POST options to include JSON when agent options exist:

```js
var body = opts.agentId ? JSON.stringify({agentId:opts.agentId,mode:opts.mode||'new',sessionId:opts.sessionId||''}) : null;
var postOpts = {method:'POST',headers:{'Authorization':'Bearer '+token}};
if (body) { postOpts.headers['Content-Type']='application/json'; postOpts.body=body; }
fetch('/api/local-terminal/sessions', postOpts)
```

- [ ] **Step 6: Run frontend test**

Run:

```powershell
$env:NODE_PATH='C:\Users\BR\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\node_modules;C:\Users\BR\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\node_modules\.pnpm\node_modules'; node pkg/assets/web/templates/local-terminal-layout.test.js
```

Expected: PASS.

---

### Task 4: Full Verification and Desktop Build

**Files:**
- No new source files.

- [ ] **Step 1: Run all targeted tests**

Run:

```powershell
$env:NODE_PATH='C:\Users\BR\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\node_modules;C:\Users\BR\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\node_modules\.pnpm\node_modules'; node pkg/assets/web/templates/local-terminal-layout.test.js
node pkg/assets/web/static/shell-integration.test.js
go test ./...
```

Expected: all pass.

- [ ] **Step 2: Build desktop executable**

Run:

```powershell
& 'C:\Users\BR\go\bin\wails.exe' build
```

Working directory: `cmd/desktop`.

Expected: build succeeds and writes `cmd/desktop/build/bin/deploy-manager-desktop.exe`.

- [ ] **Step 3: Start executable without auto-login**

Run:

```powershell
Get-Process -Name 'deploy-manager-desktop' -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Process -FilePath 'C:\Users\BR\Desktop\code\lmg\deploy-manager\cmd\desktop\build\bin\deploy-manager-desktop.exe'
```

Expected: login window opens. Do not fill credentials automatically.

