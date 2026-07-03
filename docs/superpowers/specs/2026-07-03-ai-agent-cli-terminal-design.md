# AI Agent CLI Terminal Integration Design

## Goal

Add first-class AI agent CLI launching to the desktop local terminal. Users should be able to see which supported AI agent CLIs are installed, start a new agent session quickly, continue the latest session, or choose a known historical session to resume.

The first implementation focuses on `opencode` and `codex`. Other known agent CLIs may be detected and displayed, but historical session browsing can be marked unsupported until a parser is added.

## User Experience

The local terminal status bar gets an `AI Agent` control. Opening it shows detected agents with their install path and support level. Each supported agent exposes:

- `New Session`
- `Continue Latest`
- `History`

`History` opens a compact list of sessions with enough context to choose one: session id, title or summary when available, project/cwd when available, and last updated time. Selecting a session starts that agent in the terminal using its resume command.

Launching an agent creates a new local terminal session. If the active pane is already busy, the UI should open the agent in a new pane or tab instead of overwriting the current process.

## Supported Agents

`opencode`

- Detection: find `opencode` / `opencode.ps1` / `opencode.cmd` on `PATH`.
- New session: `opencode`.
- Continue latest: `opencode -c`.
- Resume selected: `opencode -s <sessionID>`.
- History: use `opencode session list` and parse its output. If parsing fails, return an empty list with an explanatory warning.

`codex`

- Detection: find `codex` / `codex.exe` on `PATH`.
- New session: `codex`.
- Continue latest: `codex resume --last`.
- Resume selected: `codex resume <sessionID>`.
- History: prefer `~/.codex/session_index.jsonl` when present. If that cannot be parsed, fall back to a supported minimal result or mark history unavailable while still allowing `codex resume` picker.

Other agents

- Candidate names include `claude`, `gemini`, `aider`, and `cursor-agent`.
- First version may show them as detected with `New Session` only when executable discovery succeeds.

## Backend API

Extend the local terminal handler with agent-aware endpoints:

- `GET /api/local-terminal/agents`
  Returns installed/detected agents, command path, version when available, and capability flags.

- `GET /api/local-terminal/agents/:id/sessions`
  Returns historical sessions for agents with a supported history reader.

- `POST /api/local-terminal/sessions`
  Existing endpoint gains optional JSON fields:
  - `agentId`
  - `mode`: `new`, `latest`, or `session`
  - `sessionId`

When no agent fields are supplied, behavior remains the current default shell session.

## Backend Structure

Add a small agent registry near the local terminal handler. Each agent definition should provide:

- stable id and display name
- executable candidates
- command builder for `new`, `latest`, and `session`
- optional session history reader

The local terminal session creation path should accept a resolved command and args, then reuse the existing PTY, WebSocket, resize, and shell integration flow where appropriate. Shell integration should not be injected into agent sessions unless it is known to be harmless; the agent TUI should own the terminal.

## Frontend Structure

Keep changes inside the local terminal page:

- Fetch agents on load.
- Add an `AI Agent` status bar menu.
- Add a history submenu/panel using existing menu styling.
- Start agent sessions through the same pane/session creation flow, passing `agentId`, `mode`, and optional `sessionId`.

The frontend should display unavailable history clearly without blocking new session launch.

## Error Handling

- Missing CLI: do not show as installed; optionally show a disabled entry under a secondary section later.
- Version or history command failure: agent remains launchable; history displays an error row.
- Invalid session id: backend returns `400`.
- PTY launch failure: reuse existing session creation error handling.

## Security

Only built-in registry definitions may construct commands in the first version. The frontend sends an agent id and mode, not an arbitrary executable or argument string. This avoids turning the endpoint into a generic command execution API.

## Testing

Backend tests should cover:

- agent detection with mocked executable lookup
- command construction for each supported mode
- `opencode` session list parsing
- Codex `session_index.jsonl` parsing
- session creation still works without agent fields

Frontend tests should cover:

- detected agents render in the menu
- launching new/latest/session sends the expected JSON payload
- unsupported history shows an unavailable state

