## Context

The agent runs on eino `adk.TypedChatModelAgent[*schema.AgenticMessage]`. Tools are injected
at assembly time via `compose.ToolsNodeConfig.Tools []tool.BaseTool` (`agent.go:173-180`),
built today only from the static registry in `internal/agent/tools/` (`tools.Builtin`).
MCP tools are dynamic — each server is a live client connection with a lifecycle
(Start → Initialize → … → Close) — so they cannot ride the static `init()`-registered `Tool`
factory (`tools.go:15`). They need a dedicated lifecycle owner.

## Goals / Non-Goals

**Goals**
- stdio + Streamable HTTP (the two MCP-standard transports, per spec 2025-06-18); legacy SSE
  fallback for older servers.
- Managed like providers/skills: SQLite store + CLI + hot-reload.
- MCP tools flow through the same redaction decorator and `tools` allowlist as builtins.

**Non-Goals (v1)**
- Secret-bearing env-var encryption (plaintext in v1, redacted in UI/logs; `${secret:…}`
  → `SecretStore` resolution is a fast-follow).
- Tool-name namespacing across servers (left as-is; revisit if collisions bite).
- API-server runtime reconnect when servers change mid-session.

## Decisions

- **Decouple the agent from MCP.** `AssembleAgent` takes pre-built `[]tool.BaseTool`, not a
  `*mcp.Manager`. `internal/agent` imports no MCP package.
- **Manager at `internal/mcp/`**, top-level like `internal/skill/`, `internal/secrets/`,
  matching the project's package topology.
- **Manager owns redaction + caching.** It returns ready `[]tool.BaseTool` (each
  `tools.WrapRedacted`-wrapped) after the first `Tools()` call; `AssembleAgent` only merges
  + filters. The static `Tool`/`Builtin` registry is untouched.
- **Transport enum** `stdio | http | sse(legacy)`. The client calls `Start` uniformly for all three transports as implemented in `client.go`, which is supported and correct for mcp-go client initialization.
- **Failure isolation.** A server that fails to start, fails to initialize, or returns zero
  tools is logged and skipped; it never aborts agent assembly or affects other servers.
- **Hot-reload** reuses `signalRunningProcess` (`skill_cmd.go:172`). Because the CLI
  re-assembles the agent per command, server-definition changes apply on the next run
  automatically.

## Risks

- 🔴 **Secret env vars** — see Non-Goals; redact now, encrypt via `SecretStore` later.
- 🟡 **stdio `Start` requirement** — verify against the installed mcp-go.
- 🟡 **SBC memory** — each stdio server is a long-lived subprocess; recommend remote servers.
- 🟡 **API-server agent lifetime** — confirm whether `internal/api` keeps a long-lived agent
  (manager = session, reconnect needed) or re-assembles per request (automatic, like CLI).
- 🟡 **SSE is deprecated** — the 2024-11-05 HTTP+SSE transport is superseded by Streamable
  HTTP; keep SSE support minimal and prefer `http`/`stdio`.