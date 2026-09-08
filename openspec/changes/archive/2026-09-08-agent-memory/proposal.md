# agent-memory

## Why

Agents amnesia-reset every conversation: nothing persists about the user they serve, the team conventions they operate under, or what they themselves did today. The repo already carries a dormant memory stub (`agent_user_memories`, spec `agent-memories`) that nothing ever writes to, and the L1 base prompt already promises "user-specific memories (L5)" and the birth ritual already promises BOOTSTRAP.md's removal — neither promise is implemented.

## What Changes

- **Three memory scopes, one store**: `USER.md` (per workspace+user, `user_memories` table), `WORKSPACE.md` (per workspace, `memory` column on `workspaces`), `MEMORY-DD-MM-YYYY.md` (per agent per day, `agent_daily_memories` table with composite PK). The dormant `agent_user_memories` table and its GET/DELETE endpoints are **BREAKING**-dropped (replaced; never writable, so no data loss).
- **One `memory` tool** (registry built-in, allowlist opt-in, no config): actions **read / append only**; paths `USER.md` | `WORKSPACE.md` | `MEMORY-DD-MM-YYYY.md` (strict dd-mm-yyyy parse) | `MEMORY-TODAY.md` (server-resolved in workspace timezone). Append is atomic concat with an in-statement size-cap check; errors surface as JSON tool results. Structurally scoped to the run's workspace/agent/user; composite FK `agent_daily_memories(workspace_id, agent_id) → agents(workspace_id, id)` hardens agent-in-workspace at the data layer.
- **Every turn carries memory**: the instruction composer injects USER.md content under `# User → ## Memory` and WORKSPACE.md content under `# Workspace → ## Shared memory` (metadata docs stay as-is — free context; memory holds preferences and info the structured context doesn't capture; empty → omitted).
- **Shared size cap**: char cap as token proxy (~4 chars/token), one domain validator enforcing it on all three write paths: tool error, HTTP 422, UI counter.
- **Human edit surfaces**: USER.md editable in the user info menu (self); WORKSPACE.md in Settings → Workspace pane (Owner/Admin); daily memories surface as transcript tool cards only.
- **L1 template gains a Memory section** instructing how and when to save memory (new agents only — existing AGENTS.md is never overwritten; existing agents learn mechanics from the tool description).
- **Birth ritual completes**: jail-scoped `delete_file` fs tool lets the agent delete BOOTSTRAP.md after the birth beats, as the template already promises.

## Capabilities

### New Capabilities

- (none — `agent-memories` is the existing home for this capability and is reshaped below)

### Modified Capabilities

- `agent-memories`: full reshape — three scopes and their storage, the `memory` tool contract (read/append, path grammar, TODAY alias, size cap), per-scope human edit endpoints, removal of the dormant per-agent view/reset endpoints and table.
- `agent-runtime`: instruction composition injects per-turn memory subsections into the two virtual docs; filesystem jail toolset gains `delete_file` (serves BOOTSTRAP.md removal).
- `web-app/shell`: user info menu gains a memory entry opening the user's own USER.md editor.
- `web-app/settings`: Workspace pane gains a WORKSPACE.md memory editor gated to Owner/Admin.

## Impact

- **Migrations**: new `000025_agent_memory` — create `user_memories` + `agent_daily_memories`, add `workspaces.memory`, drop `agent_user_memories`; down reverses (recreates the old table empty).
- **Backend**: `internal/store` (+`MemoryStore` sub-interface, fake, postgres), `internal/agents` (`ToolContext` gains `UserID` + workspace TZ; registry/catalog registration; composer injection), `internal/server/handlers` (user-memory and workspace-memory endpoints; remove old agent-memory routes), `internal/promptdocs` (AGENTS.md + BOOTSTRAP.md templates), fs toolset (`delete_file`).
- **Frontend**: `web/src/components/nav/UserMenu.tsx` (+ memory modal), `web/src/screens/settings/WorkspaceSection.tsx` (+ memory editor with token counter), API client additions.
- **Permissions**: workspace-memory writes ride the existing workspace settings-management permission (Owner/Admin); user-memory writes are self-only; no catalog permission additions expected (checked against backfill rule 000022 pattern).
