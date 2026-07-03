# Desktop Client Design — Wails Wrapper

## Goal

Add a desktop GUI to the existing web-based deploy-manager, reusing all existing frontend (Alpine.js + html/template) and backend (Gin) code. The web version remains fully functional alongside the desktop version.

## Architecture

```
Wails Desktop App
├── System Tray / Native Menu / File Dialog
├── WebView (loads http://localhost:<port>)
└── Embedded Gin Server (goroutine)
    ├── internal/handler/ (18 handlers, unchanged)
    ├── internal/service/ (SSH, tunnel, Docker, etc.)
    └── pkg/rdp/ (extracted from rdp-agent, called directly)

Standalone Web Server (cmd/server)
└── Embedded Gin Server (same pkg/server)
```

- `pkg/server/` — extracted server library (Gin setup, route registration, config)
- `cmd/server/main.go` — thin wrapper calling `pkg/server.Start()`
- `cmd/desktop/` — Wails v2 application entry point
- `pkg/rdp/` — RDP agent logic extracted from `cmd/rdp-agent`, callable directly as a Go library

## Key Design Decisions

### Webview loads from Gin (localhost)

The Wails webview opens `http://127.0.0.1:<random-port>` which is the embedded Gin server. All existing frontend code (Alpine.js, xterm.js, CodeMirror) works unchanged. API calls and WebSocket connections go to the same local Gin server.

### Random port allocation

Desktop mode auto-assigns a random available port (port 0). No user configuration needed.

### RDP integration

`pkg/rdp/` package exposes a direct Go API:

```go
type ConnectRequest struct {
    Host, Username, Password string
    Port, JumpPort, ProxyPort int
    JumpEnabled, ProxyEnabled bool
    JumpIP, JumpUser, JumpPassword, JumpKey string
    ProxyType, ProxyHost string
}

func (a *Agent) Connect(req ConnectRequest) error
```

- Windows: `mstsc` + `.rdp` file (same as current rdp-agent)
- macOS: `open` with Microsoft Remote Desktop protocol
- Linux: `xfreerdp` / `rdesktop`

The Wails desktop app uses this directly — no separate agent download, no HTTP loopback.

## File Changes

| File | Action |
|------|--------|
| `pkg/server/options.go` | New — server config struct (Port, Desktop mode flag, etc.) |
| `pkg/server/server.go` | New — Gin engine creation, route registration extracted from cmd/server |
| `pkg/rdp/agent.go` | New — RDP connect logic from cmd/rdp-agent |
| `pkg/rdp/agent_windows.go` | New — platform-specific mstsc launching |
| `pkg/rdp/agent_darwin.go` | New — macOS RDP client |
| `pkg/rdp/agent_linux.go` | New — Linux freerdp/rdesktop |
| `cmd/server/main.go` | Rewrite — thin wrapper calling pkg/server |
| `cmd/desktop/main.go` | New — Wails app entry |
| `cmd/desktop/app.go` | New — App struct with lifecycle |
| `cmd/desktop/tray.go` | New — System tray setup |
| `cmd/desktop/wails.json` | New — Wails project config |
| `go.mod` | Add Wails dependency |
| `Makefile` | Add `desktop` target |

## Out of Scope (for this version)

- Native SQLite GUI editor
- Offline-first mode (app already requires server connectivity for SSH/terminal operations)
- Auto-update mechanism
- Windows installer / macOS .dmg / Linux .deb packaging

## Testing

- `pkg/server/` — unit tests for server options and route setup
- `pkg/rdp/` — unit tests for request validation, integration tests need RDP client installed

## Build

```makefile
desktop:  ## Build desktop version with Wails
	wails build -clean -o deploy-manager-desktop

server:   ## Build standalone web server (unchanged)
	go build -o deploy-manager ./cmd/server
```
