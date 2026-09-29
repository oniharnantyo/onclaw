# Proposal

## Why

Agents store a tool **allowlist** (`agents.tools`), so every new built-in tool is invisible to existing agents until a human re-saves each agent's configuration, and newly created agents start with zero tools until every wanted tool is hand-checked. Tool selection should be a denylist (`disabled_tools`): a new tool registered into the catalog becomes available to every agent automatically, and opting out stays a one-toggle act.

## What Changes

- **BREAKING** — `agents.tools` (allowlist) is removed from schema, API, and runtime; replaced by `agents.disabled_tools` (denylist). A payload still carrying `tools` is accepted and ignored (managed-field convention, like the former `disabled_mcps`).
- Agent tool resolution inverts: effective tools = all catalog tools (registry built-ins, filesystem middleware tools, reserved `execute`, `browser` facade, reserved capability names) **minus** `disabled_tools`, then intersected with the workspace tool gate (the gate still wins).
- **No data backfill**: migration 000070 adds `disabled_tools text[] NOT NULL DEFAULT '{}'` and drops `tools`. Every existing agent starts with an empty denylist — all current catalog tools become exposed (the deliberate trade: old saved allowlists are discarded).
- A built-in tool registered after an agent is saved now **appears automatically** (reverses today's "hidden until the allowlist names it" rule). A name in `disabled_tools` that matches no registered tool stays inert.
- Reserved names flip from opt-in to opt-out: `execute` (shell), `browser` (facade alias — disables the whole browser set), `subagents`, and `background_shell` are enabled unless present in `disabled_tools`. The in-flight `add-agent-subagents-background` change's reserved-name semantics are superseded by this rule when both land.
- Skills "enable everywhere" force-enables by **removing** the skill's required tools from every agent's `disabled_tools` (instead of unioning into the allowlist).
- The per-turn allowed-tools override stays an explicit request-scoped allowlist (a narrowing mechanism); when absent, the turn resolves from the denylist. The workspace gate wins over both.
- Web Step 3 tool chips render **selected by default**; toggling off adds the key to `disabled_tools`; new catalog tools appear already-selected for every agent. Workspace-disabled tools stay greyed and unselectable.
- `enabled_mcps` remains an allowlist (workspace MCP servers stay opt-in per agent) — out of scope.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agents` — agent fields: `tools` allowlist replaced by `disabled_tools` denylist; create defaults; update replaces the denylist; legacy `tools` payload ignored.
- `agent-runtime` — tool surface resolution: denylist semantics, empty denylist exposes everything, new tools appear automatically, fs-middleware per-tool disable by denylist presence, reserved-name opt-out.
- `workspace-tools` — the gate intersects the denylist-resolved effective set; stale denylist keys inert; catalog key wording.
- `workspace-skills` — enable-everywhere force-enables by subtracting from agent denylists; unmet-dependency check reads the effective set.
- `web-app/agents` — Step 3 tool chips default selected; toggle-off stores the denylist; workspace-disabled greyed; skill-chip warning reads the effective set.
- `openresponses` — request-level `tools` intersect the agent's effective tool set, never extend it.
- `workspace-document-tools` — exposure condition reworded to the denylist + gate.
- `agent-memories` — exposure condition reworded (memory tool on unless disabled; scheduled runs still never mount it).

Reviewed, no change: `workspace-mcp` (`enabled_mcps` stays an allowlist), `workspace-connections` (connection attachment is its own per-agent opt-in), `scheduler` (anti-runaway strip wins regardless of agent tool config), `agent-todos` / `agent-channels` / `agent-hooks` (generic "exposed per agent" wording already holds).

## Impact

- **Schema**: migration 000070 (`ALTER TABLE agents ADD COLUMN disabled_tools text[] NOT NULL DEFAULT '{}', DROP COLUMN tools`), mirroring migration 000017's own flip in reverse; down migration restores an empty `tools` (data loss on down, same convention as 000017).
- **Backend**: `internal/domain/agent.go` (field), store ports + fake + postgres (`internal/store/...`), create/update handlers (`internal/server/handlers/agents.go`, `workspaces.go` starter agent), runner resolution (`internal/agents/runner.go` — `resolve()`, todo-exposure check), fs middleware disable computation, skills service (`internal/skills/service.go` merge→subtract), v1 narrowing (`internal/server/handlers/v1.go`), eval seed fixtures.
- **Web**: agent types, `AgentConfigModal` Step 3 toggle logic, skill unmet-dependency check, API payload shape.
- **Coordination**: the in-flight `add-agent-subagents-background` change (uncommitted) pins `subagents`/`background_shell` as opt-in reserved names — its delta specs must adopt the opt-out rule; land order should treat this change's denylist semantics as governing.
