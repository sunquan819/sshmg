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

func TestResolveLocalSessionLaunchAgentUsesInteractiveShell(t *testing.T) {
	if findAgentExecutable("opencode") == "" {
		t.Skip("opencode is not installed")
	}

	cmd, args, name, isAgent, err := resolveLocalSessionLaunch(localSessionCreateRequest{AgentID: "opencode", Mode: "latest"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isAgent {
		t.Fatalf("expected agent session")
	}
	if name != "opencode" {
		t.Fatalf("name = %q", name)
	}
	joined := strings.Join(args, "\x00")
	if runtime.GOOS == "windows" {
		if !strings.Contains(strings.ToLower(filepath.Base(cmd)), "powershell") {
			t.Fatalf("cmd = %q, want powershell wrapper", cmd)
		}
		if !strings.Contains(joined, "-NoExit") || !strings.Contains(joined, "opencode") || !strings.Contains(joined, "-c") {
			t.Fatalf("args = %#v, want interactive opencode wrapper", args)
		}
		if !strings.Contains(joined, "?1049l") || !strings.Contains(joined, "?2004l") {
			t.Fatalf("args = %#v, want terminal reset after agent exits", args)
		}
		return
	}
	if !strings.Contains(joined, "opencode") || !strings.Contains(joined, "-c") {
		t.Fatalf("args = %#v, want opencode shell invocation", args)
	}
}
