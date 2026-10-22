# Design

## Context

The buddies runtime (`internal/agents/runner.go`) already streams turns, persists an event-sourced transcript, accounts context, and compacts — but it never executes a tool: `tool_activity` events are recorded with empty results and the turn ends. Skills, MCP servers, and hooks have full CRUD surfaces (tables since migration 000001, settings panes, wizard step) that nothing reads at turn time; subagents have no trace. onclaw-v2 runs this entire feature family on eino's ADK through a pure `Compose(Config)` middleware seam, and its `backend/`, `hooks/`, and `mcp/` packages depend only on domain types plus narrow store ports — they port with little rework. See proposal.md for motivation and the specs for behavior.

Standing constraints: `internal/agents` speaks no eino types across its package boundary (doc.go); Wails bindings must be regenerated after every `task build`; the Go gates are `gofmt -w .`, `go vet ./...`, `go test ./...`, the frontend gate is `vp check` scoped to `frontend/src`; every UI surface must work in both themes; features must also work headless in server mode (`bin/buddies-server`), which is how live smokes run (temp-HOME + mock provider recipe).

## Goals / Non-Goals

**Goals:**
- One turn engine, ported from onclaw-v2's ADK compose, behind the unchanged `Manager` interface and sessions event model (extended additively).
- Live tool execution with the jailed-workspace security posture (jail, shell timeout/output caps, scrubbed child env).
- The four feature families actually load-bearing at turn time, with their existing CRUD/UI surfaces upgraded only where behavior demands (MCP status, hooks assignment, task progress, search chain config).

**Non-Goals:**
- Skill curation, skill install pipeline (zip/git), MCP OAuth, browser tools, document tools, memory, gateway triggers (cron/webhooks/Telegram), autonomy/approval gating, onclaw's embedded system skills, and any assistant-ui migration (the config file's assistant-ui note is stale — the shipped frontend is the custom kit).
- Hooks handler types other than command and script; multi-tenant scoping of any kind.

## Decisions

**D1 — Adopt the ADK compose; port packages nearly intact.** Replace `runTurn`'s internals with the ported resolve → compose → execute pipeline: `Compose(Config)` builds the ADK agent (tool loop + middleware), existing buddies code keeps loading, compaction, and persistence. Alternatives: extending the hand-rolled loop (rejected — re-implements skill/subagent/hook middleware one feature at a time and drifts from the proven source), or porting onclaw's 3.9k-line runner wholesale (rejected — carries channels/memory/ingest inline). The porting seam is narrow store interfaces: ported subpackages accept small interfaces defined in their own package, satisfied by adapters over `store.Store`.

**D2 — Sessions and compaction stay buddies'.** The event-sourced transcript, checkpoints, and the 0.8 compaction gate are kept; only the event mapping is ported from onclaw's `drainAgentEvents` pattern. Additive event changes: `ToolActivity` gains `Status` (running/completed/failed) and a real `Result`; a new `task_activity` kind carries subagent task lifecycle; transcript replay must tolerate the new kinds.

**D3 — Allowlist semantics kept; subagents get a dedicated toggle.** Buddies' `Tools`/`Skills`/`MCPServers` columns stay allowlists (empty = all). MCP tools bypass the tool allowlist — their gate is the enabled server plus the agent's `MCPServers` list under that same reading: empty offers every enabled server, a non-empty list restricts the agent to the named ones (a 2026-10-20 correction — the first cut had read the column as a positive opt-in, empty = none, so a freshly connected server reached no agent until each one was edited). Because an empty list cannot mean "none", agent configuration pins the last server left on (toggle disabled, marked always on); pausing the server in Settings removes it from every agent. Subagents are a per-agent boolean column (`agents.subagents_enabled`, default on) rather than an allowlist trick: with empty-means-all allowlists, a reserved-name denylist cannot express "everything except subagents" cleanly. Iteration cap is a runtime constant, not a per-agent column.

**D4 — Hooks: one entity table plus an assignment table.** Keep the existing `hooks` table as the single hook definition store; add `agent_hooks` (agent_id, hook_id, position). Resolution per turn: hooks assigned to the agent (by position), then unassigned hooks (global tier) — assigning a hook removes it from the global tier for everyone, so an assignment means "only these agents". This differs from onclaw's standalone per-agent hook entities but is simpler in a single-tenant app and matches the specced assignment model. Existing rows (all `command` type from the current service) keep working. Audit lands in a `hook_executions` table.

**D5 — Secrets stay plain JSON in SQLite.** Hook configs, the search chain credentials, and MCP env/headers are stored as today — no AES-GCM envelopes. Rationale: single-user desktop, DB already holds provider API keys in the same posture. Revisit only if server-mode hardening demands it.

**D6 — Skills: two tiers, fresh per turn, no system tier.** Global `~/.buddies/skills/` plus per-agent `workspaces/<slug>/skills/` (agent wins on name conflict), read fresh each turn — no cache invalidation problem. The skill backend (metadata list → body on demand → `$name` explicit load) is ported from onclaw's `backend/skill_backend.go` against these two roots; onclaw's embedded system skills are dropped (document-read has no document tools to serve; web-research is redundant with the web tools). Per-agent skill management is file presence — no upload UI; agents can author skills into their own skills dir through the jailed file tools.

**D7 — MCP: port the manager, drop OAuth.** `MCPManager` (lazy per-server connections, connect timeout, negative-cache retry, janitor, invalidate-on-write) with stdio/streamable-HTTP/SSE via the eino-ext MCP tool component + mcp-go (onclaw's stack). Naming `mcp__<server>__<tool>` with sanitization and deterministic collision suffixes. Probe writes the existing `status`/`status_error`/`tool_count` columns; failures never mutate the definition.

**D8 — Web search: port the provider seam whole.** `SearchProvider` interface, the five providers (brave/exa/perplexity/firecrawl API-key, searxng base-URL), and the failover chain, configured under tool key `web_search` in a new `tool_settings` key–JSON table and edited in the Tools pane.

**D9 — Script hooks on goja; command hooks as today, generalized.** The dispatcher is ported minus the http/prompt/mcp_tool handlers. Script handlers run in-process with console-only bindings, compiled once per run, terminated via goja interrupts on timeout. The Hooks pane form offers only the two supported types; `TestHook` generalizes from its current `sh -c` special case into a real sample-event dry run for both types.

**D10 — Live tool cards without a transcript rewrite.** The running state is a live-subscription concern: the turn emits tool-activity start/finish on the live channel and persists only the terminal record (status completed/failed + result), so replayed transcripts show final states while an in-flight turn shows the spinner.

## Risks / Trade-offs

- [ADK is alpha and the compose rewrite touches the most critical path] → keep the `Manager` interface and event schema stable; W1 starts with an API spike + the ported compose behind unit tests using fake models before any UI work; live smoke after each wave.
- [Unguarded `execute` shell on a local machine] → jail + timeout + output cap + scrubbed env now; approval/autonomy gating is a scoped-out follow-up, noted here so the gap is visible.
- [goja sandbox escape or runaway script] → no host bindings, interrupt-based timeout, mirror onclaw's hardening; command/script handlers stay behind manager options so they can be kill-switched later.
- [MCP stdio servers run arbitrary user-configured commands] — same trust level as hook commands; no additional sandboxing in this change.
- [Event schema additions break old clients/transcripts] → additive kinds/fields only; replay tolerates unknown kinds (test added in W1).
- [Bindings staleness and migration-count tests rot across five waves] → standing gates in every wave: `task build` + bindings regen, schema-version test updates with each migration.
- [Plain-text secrets] → accepted for desktop posture (D5); flagged for the server-mode hardening backlog.

## Migration Plan

Forward-only migrations: `000007` adds `agents.subagents_enabled`, `agent_hooks`, `hook_executions`, and `tool_settings`. No data backfill — today's feature rows are inert, existing hooks (command type) remain valid, and MCP rows keep their status columns (now actually populated). Rollback is restore-from-backup; all changes are additive, so the previous binary still runs against the migrated schema.

## Open Questions

None blocking. The Tools-pane presentation of the search chain (labels, branding tiles) follows the existing pane conventions and can be settled during W1 UI work.
