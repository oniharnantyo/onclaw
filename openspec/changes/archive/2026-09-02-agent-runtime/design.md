# Design: Agent Runtime

## Context

Agents are configuration + generated prompt documents (`internal/agents/` prompt generation, `AGENTS.md`/`IDENTITY.md`/`SOUL.md`/`BOOTSTRAP.md` on disk) with no execution path. Eino is already a dependency for prompt generation (classic `BaseChatModel` path); the runtime adopts Eino's ADK on the agentic path. Full architecture with diagrams lives in `docs/agent-runtime-architecture.md`; see proposal.md for motivation.

Constraints that shape the design: multi-tenancy is enforced at the data layer; injected dependencies are never nil (composition root resolves everything); no compatibility aliases (one name per concept); plugins target the same interfaces as built-ins.

## Goals / Non-Goals

**Goals:**
- Streaming-first execution: deltas out, durable completed messages in
- A domain-level `Engine` port that keeps all Eino types inside `internal/agents/`
- Tool surface grown by registration (denylist), never by editing core
- Model file access jailed to the agent workspace directory
- Long threads that survive their own context window (summarization) with a full audit trail (event log)

**Non-Goals** (later changes): chat/SSE HTTP endpoints and the web chat runtime wiring; approval-autonomy interrupts; MCP server configuration and tools; sub-agents; automemory middleware; channels; cron/background tasks; message editing UI.

## Decisions

### D1 — Eino v0.10.0-alpha.28, agentic path
`TypedChatModelAgent[*schema.AgenticMessage]` + `Runner`. Compile-proven in a scratch module against the three agentic adapters (`/tmp/eino-probe`). Alternative considered: classic `*schema.Message` path (works on today's pins, full middleware parity) — rejected because the user requires the agentic model and adapters exist; the `Engine` port keeps the message-type choice swappable regardless. Risk of alpha drift accepted; noted under Risks.

### D2 — Runtime lives in `internal/agents/`
Engine port, agentic factory, instruction composer, skills resolver, tool registry, session adapter all join the existing prompt-generation package. Alternative: a new `internal/runtime/` package — rejected; "agents" is the cohesive home and the package is not large.

### D3 — Engine port is Eino-free
`Engine.Run(ctx, ExecRequest) *EventStream` yielding domain `TranscriptEvent`s. The HTTP layer (later scope) and tests depend on domain types only. Consequence: the agentic-vs-classic choice, middleware composition, and event mapping are implementation details, replaceable without touching callers.

### D4 — Agentic model factory (6 types → 3 adapters)
openai/openrouter/openai-compatible → `agenticopenai`; anthropic/anthropic-compatible → `agenticclaude`; gemini → `agenticgemini`. All three support custom endpoints (gemini via `genai.ClientConfig`, the same pattern the classic factory uses). The base_url version-strip contract carries over unchanged. The classic prompt-generation factory stays as-is.

### D5 — Instruction composition (six documents, fixed order)
AGENTS.md (seeded template, user-editable, never overwritten) + IDENTITY.md + SOUL.md (generated) + WORKSPACE.md (virtual: workspace name, description) + USER.md (virtual: caller name, email, role via membership) + BOOTSTRAP.md (generated). Missing files skipped (agents with failed prompt generation still run). Composition is per-execution (USER.md varies by caller); WORKSPACE.md is cacheable per workspace, invalidated on update.

### D6 — Denylist capability model
`tools`/`skills`/`mcp` allowlists → `disabled_tools`/`disabled_skills`/`disabled_mcps`. Everything registered is on by default; unknown names are inert (no referential validation — there are no DB rows to validate against anymore). Consequence: the registry ships every built-in to every agent; `web.search` (DuckDuckGo, zero credentials) is the proving built-in. Alternative: keep allowlists — rejected by product decision (matches the "everything available, opt out" posture).

### D7 — Filesystem middleware jailed to the agent dir
ADK filesystem middleware with a local `Backend` rooted at `AgentWorkspaceDir`; path resolution cannot escape (symlink/`..` checked at the backend boundary). Shell/`execute` deliberately not attached: shell escapes the jail trivially and no sandbox exists in this scope. File tools are a suite (the middleware does not support per-tool toggles) — granularity is the whole suite, always attached. Agent-tier skills living inside the jail means agents can author skills with their own file tools.

### D8 — Three-tier skills, most-specific wins
System: embedded `//go:embed` assets mirrored into `<ONCLAW_DIR>/skills` at every server start (overwrite changed, remove extraneous — disk is a cache, self-healing). Workspace: `<ONCLAW_DIR>/workspaces/<tenant>/skills/`. Agent: inside the jail. Precedence agent > workspace > system; system tier exempt from `disabled_skills`. A multi-root skills resolver backs the skill middleware (list metadata / fetch body).

### D9 — Summarization on the agent's own model
ADK summarization middleware in the handler stack, ordered `patchtoolcalls → reduction → summarization → skill → filesystem` (patch repairs dangling tool calls that compaction would otherwise strand; reduction shrinks oversized tool results so the token counter sees realistic numbers). Trigger = resolved context window × server-configured margin. `transcript.md` offload written from the summarization callback; replacement recorded in the event log (`messages_replaced`), so the full history stays retrievable by replay. Internal events enabled to surface compaction markers.

### D10 — Context window resolution chain
Agent's stored `context_window` → models.dev catalog `limit.context` for provider/model (new: catalog parser must stop dropping the `limit` field) → **200,000** default. Auto-fill at create/update from the catalog; client value always wins; compatible provider types never match the catalog (fall through to default).

### D11 — History: append-only session event log in Postgres
`session_events` table (session_id, event_id, turn_id, seq, kind, payload, occurred_at; PK (session_id, event_id) → idempotent appends; per-session `seq` for stable cursor ordering; kind index). A `session_checkpoints` table backs interrupt checkpoints. The table layer stays Eino-free (opaque serialized payloads); the serialization adapter implementing the ADK session interfaces lives in `internal/agents/`. Eino ships a conformance suite (`adk/session`) the Postgres implementation must pass — idempotency, cursoring, and replay are verified against the framework's own contract, not an ad-hoc matrix. `workspace_id` rides on rows for tenant-scoped queries.

### D12 — Config: one knob
`ONCLAW_DIR` (default `$HOME/.onclaw`) replaces `ONCLAW_WORKSPACE_DIR` outright (no alias). Derivations: workspace root `<ONCLAW_DIR>/workspaces`, system skills `<ONCLAW_DIR>/skills`. Defaults resolve to byte-identical paths as today; absolute-path validation at startup moves to `ONCLAW_DIR`. Path algebra centralized in domain helpers.

### D13 — Streaming contract
`Run` returns an event stream; deltas (`TextDelta`/`ReasoningDelta`) are transient and unpersisted; the completed message persists exactly once; interrupted streams persist an incomplete marker; tool-call blocks arrive complete before execution; cancel uses safe-point modes. Full event vocabulary table in `docs/agent-runtime-architecture.md`.

## Risks / Trade-offs

- [eino v0.10.0-alpha.28 is a pre-release; API may shift before 0.10.0] → Pin exactly; the Engine port confines the blast radius to `internal/agents/`; upgrade is a tracked follow-up when 0.10.0 lands.
- [Classic-path prompt generation must compile against the alpha core] → Phase 0 compile check; bump classic adapter pins if required.
- [File tools are suite-granular (no per-tool enable/disable)] → Accepted; middleware has no per-tool toggle. Finer granularity would require a denying wrapper tool layer — deferred until a real need appears.
- [No shell] → Deliberate: the jail cannot contain shell. Revisit with a sandbox design; approval-autonomy interrupts are the prerequisite for any tool-execution gating.
- [Summarization quality depends on the agent's own model] → Accepted for v1 (no second credential set); a dedicated utility model is a later option behind the same factory seam.
- [Delta events double the in-memory event rate for long generations] → Deltas are never persisted and are cheap structs; persistence writes completed messages only.
- [Per-execution instruction composition reads up to 4 files + 3 lookups per run] → Negligible at current scale; WORKSPACE.md cacheable; revisit if profiling demands.

## Migration Plan

1. Phase 0: dependency upgrade + compile check (no behavior change).
2. Migration `agent-runtime`: add `agents.context_window`, replace `tools/skills/mcp` with `disabled_*` (contents dropped — denylist starts empty), add `workspaces.description`, drop `workspace_skills`, create `session_events` + `session_checkpoints`.
3. Rollback: down-migration restores columns (dropped allowlist data is not recoverable — accepted pre-GA; `workspace_skills` data re-creatable only from backups).
4. Deploy order: migration → server (system skills sync on start); default-path deployments see identical filesystem layout.

## Open Questions

None blocking. Margin constant for the summarization trigger and `seq` allocation strategy (bigserial per row vs per-session counter) are tuning choices delegated to implementation.
