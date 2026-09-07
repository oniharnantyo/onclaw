# Agent Runtime

## Why

Agents exist today only as configuration and generated prompt documents — the backend can store, validate, and generate personas, but cannot execute them. The product's spine (direct chats with tool calls, streamed responses, long-lived threads) needs an execution engine, and the Eino ADK (v0.10.0-alpha.28) provides it: compile-proven agentic adapters for all six provider types, typed middleware (filesystem, skills, summarization), and a session event store we can back with Postgres.

## What Changes

- Add the agent execution engine inside `internal/agents/`: an Eino-free `Engine` port whose implementation composes a `TypedChatModelAgent[*schema.AgenticMessage]` + `Runner` per execution, mapping ADK events to domain transcript events. Streaming-first: responses stream as delta events; only completed messages persist.
- Add an agentic model factory covering all six provider types via three eino-ext adapters (`agenticopenai`, `agenticclaude`, `agenticgemini`), honoring the existing base_url version-strip contract. Upgrade Eino to v0.10.0-alpha.28.
- Compose the agent instruction per execution from six documents in fixed order: `AGENTS.md`, `IDENTITY.md`, `SOUL.md`, `WORKSPACE.md` (virtual: workspace name + description), `USER.md` (virtual: caller name + email + role), `BOOTSTRAP.md`.
- Add a tool registry (port + built-ins) with a **denylist** model: every registered tool/skill/MCP server is enabled by default; agents store what is turned off. Ships `web.search` (DuckDuckGo, no API key) as the proving built-in.
- Attach the ADK filesystem middleware jailed to each agent's workspace dir (`ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`; no shell in this scope).
- Add directory-based skills with three tiers: system (`$ONCLAW_DIR/skills`, embedded in the binary, mirrored on every server start, cannot be disabled), workspace (`<tenant>/skills`), agent (`<tenant>/agents/<slug>/skills`, inside the jail — agents can author their own skills). Precedence: agent > workspace > system.
- Add summarization middleware using the agent's own model, triggered when token count exceeds the resolved context window × margin; offloads full history to `transcript.md` inside the agent jail.
- Persist conversation history as an append-only, replayed event log (`session_events` table) implementing Eino's `SessionEventStore` + checkpoint interfaces; the eino-free Postgres table is wrapped by a serialization adapter in `internal/agents/`.
- Replace the single `ONCLAW_WORKSPACE_DIR` knob with `ONCLAW_DIR` (default `~/.onclaw`); workspace root, system skills dir derive from it. Paths for default deployments are byte-identical.
- **BREAKING** — Agent config model: `tools`/`skills`/`mcp` allowlist fields replaced by `disabled_tools`/`disabled_skills`/`disabled_mcps`; new nullable `context_window` column (tokens) with resolution chain agent → models.dev catalog → 200k default; auto-filled on create/update from the catalog and user-overridable.
- **BREAKING** — Add `description` to workspaces (rendered into `WORKSPACE.md`).
- **BREAKING** — Drop the `workspace_skills` capability: table, domain entity, store port, fake, postgres impl, and CRUD endpoints. Skills become directories on disk.
- Deferred to later changes (explicitly out of scope): chat/SSE HTTP endpoints, approval-autonomy interrupts, MCP server config + tools, sub-agents, automemory middleware, channels, cron/background tasks.

## Capabilities

### New Capabilities

- `agent-runtime`: agent execution — streaming runs over the Eino ADK agentic path, instruction composition (six docs), tool registry + denylist, jailed filesystem tools, three-tier skills, summarization with context-window trigger, and the session event history (append-only log, cursor pagination, checkpoints).

### Modified Capabilities

- `agents`: field model changes — `context_window` (nullable, validated > 0, catalog auto-fill on create/update), `disabled_tools`/`disabled_skills`/`disabled_mcps` replacing `tools`/`skills`/`mcp`; validation updated accordingly.
- `tenancy`: workspace creation/settings gain a `description` field (stored, rendered into `WORKSPACE.md`).
- `model-catalog`: catalog models expose the models.dev context limit (`limit.context`) used to auto-fill `context_window`.
- `workspace-skills`: capability removed — skills live on the filesystem (system/workspace/agent tiers), the `workspace_skills` table and its CRUD are dropped.

## Impact

- **Dependencies (go.mod):** `github.com/cloudwego/eino` → v0.10.0-alpha.28; add `eino-ext/components/model/agenticopenai`, `agenticclaude`, `agenticgemini`. Classic-path prompt generation and its pinned classic adapters must keep compiling (compile check required).
- **Code:** `internal/agents/` (engine, factory, instruction composer, skills resolver, session adapter, tool registry); `internal/domain` (Agent fields, Workspace.Description); `internal/config` (ONCLAW_DIR); `internal/store` + `internal/store/postgres` (+ fake) for agent/session changes; `internal/server/handlers` (agents payloads, workspace skills removal); `migrations/` (agent columns, workspaces.description, drop workspace_skills, session tables); `web/` (Advanced model section: context-window field + disable toggles).
- **Config:** `ONCLAW_DIR` replaces `ONCLAW_WORKSPACE_DIR` (no compat alias; default resolves to identical paths).
- **Risk:** eino v0.10.0-alpha.28 is a pre-release — API may shift before 0.10.0; compatibility with the three adapters is compile-proven (see `docs/agent-runtime-architecture.md`).
- **Design reference:** `docs/agent-runtime-architecture.md` (component + sequence diagrams, decision log, streaming contract, history subsystem detail).
