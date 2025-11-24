<!-- Copilot / AI agent instructions for the MCP-Hub repository -->
# MCP-Hub — Quick Instructions for AI coding agents

Target: get an AI agent productive quickly when editing or adding code in this repo.

Big picture
- Purpose: the hub aggregates multiple MCP (Model Context Protocol) servers and exposes their tools via an HTTP API. It can start local processes (stdio), Docker containers, or talk to remote HTTP/SSE servers.
- Language: Go. Primary entrypoint: `cmd/mcp-hub/main.go`.

Key components (what to open first)
- `cmd/mcp-hub/main.go` — program startup, graceful shutdown.
- `internal/plugin/manager.go` — starts/stops MCP servers, chooses transports (`stdio`, `docker`, `http`, `sse`), registers tools with the registry.
- `internal/transport/` — transport implementations: `stdio.go`, `http.go`, `sse.go`, `docker.go` (construction happens from `manager.go`).
- `internal/mcp/protocol.go` — MCP JSON-RPC shapes and helpers; use these when composing requests/responses.
- `internal/registry/registry.go` — tool registration and listing.
- `internal/server/server.go` — HTTP endpoints (`/mcp/tools`, `/mcp/execute`, `/mcp/servers`, `/mcp/stream`).
- `internal/config/config.go` + `config.example.json` — config parsing, env expansion, `mcpServers` entries.

Essential data flow (short)
- Startup: `config.Load()` → `plugin.Manager.LoadFromConfig()` → for each server: start transport → `session.Connect()` → `session.ListTools()` → `registry.RegisterTools()`.
- Runtime: HTTP API calls `registry` or `plugin.Manager.Execute()` → `session.CallTool()` → tool response → forward to client.

Concurrency & lifecycle notes
- `plugin.Manager` uses a mutex to guard its `servers` map; each `MCPServer` has its own `mu` protecting session state. Keep the same locking discipline when touching manager/server state.
- Register tools only after successful connect + list-tools; unregister before closing the session to avoid race conditions.

Transport & Docker specifics
- `stdio` transport: executed via `exec.Command(cfg.Command, cfg.Args...)`; ensure `env` mapping uses the repo's `envMapToSlice` pattern.
- `docker` usage: manager spawns containers with `-i --rm` and mounts volumes/headers from config. Containers must use stdin/stdout for MCP and produce unbuffered stdout (e.g., `python -u`) — MCP payloads must never go to stderr.

Logging conventions (important to preserve)
- The manager emits structured tokens like `connect:attempt`, `connect:ok`, `exec:start`, `exec:done`, `exec:fail` with key=value fields. Keep these tokens and the 200-byte truncation style for arg logging when adding instrumentation.

How to add a new transport (practical steps)
1. Implement or extend a type under `internal/transport/` that satisfies the transport interface (`transport.go`).
2. In `internal/plugin/manager.go` add a case for the transport type in the transport selection switch and construct the transport with config fields.
3. Ensure the start → connect → list-tools → register sequence remains unchanged.

Build / run / debug commands
- Build binary: `cd cmd/mcp-hub && go build -o ../../mcp-hub`
- Run (default): `./mcp-hub`
- Run with config: `./mcp-hub --config /path/to/config.json`
- Quick checks: `curl http://localhost:8080/mcp/tools` and `curl http://localhost:8080/mcp/servers`

Examples and useful files
- Example plugin: `examples/plugins/mcp_echo.py` (simple echo MCP server to test transports).
- Docker hints: see `DOCKER_DEPLOYMENT.md` and `Dockerfile*` for how images are expected to behave (stdin/stdout, no MCP JSON on stderr).

Tests & verification
- No repository-wide tests detected. For iterative verification run: `go vet ./...` and `go build ./...` (or `go test ./...` if tests are added).

Safe editing rules (do not change without care)
- Preserve logging tokens and their formats.
- Do not register tools before `session.ListTools()` returns successful discovery.
- Never send secrets in cleartext when composing config or logs; config values may use `${VAR}` expansion — use `internal/config` helpers.

If you need more
- If you'd like a sample transport implementation, unit test, or a short checklist for making a Docker-backed MCP server, tell me which and I'll add it.

-- End of file (concise guidance)
