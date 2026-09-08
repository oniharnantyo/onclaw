# agent-memory — Design

## Context

The repo carries a dormant memory stub: `agent_user_memories` (migration 000011, store + fake + postgres + GET/DELETE endpoints) that nothing in the runtime ever writes; the L1 base prompt (`promptdocs/AGENTS.md`) already references "user-specific memories (L5)"; and `BOOTSTRAP.md` already promises "it is removed" while no tool can delete a file. The instruction composer (`DefaultInstructionComposer`) builds six docs per execution, including virtual `# Workspace` / `# User` metadata docs. `ToolContext` currently carries workspace/agent/session but no user identity or timezone. Tool conventions are established: registry constructor + catalog entry, tool errors as JSON results (never run-fatal), structured config only for configurable tools. See proposal.md for motivation.

## Goals / Non-Goals

**Goals:**
- Three persistent memory scopes behind one tool and one store sub-interface
- Memory visible to the agent every turn without the model having to ask
- Structural scoping (workspace/agent/user bound at construction) over permission checks
- Human edit surfaces for the two shared scopes, sized to ride existing seams

**Non-Goals:**
- A browser UI for browsing past daily memories (transcript tool cards suffice for v1)
- Embedding/vector retrieval or memory summarization — append-only markdown docs with a cap
- Overwrite/rewrite operations for agents (corrections are appended, or done by humans via UI)
- Template versioning machinery to retrofit existing agents' AGENTS.md

## Decisions

**D1 — Storage shapes follow cardinality.** `user_memories` table with composite PK `(workspace_id, user_id)`; `workspaces.memory` column (one row per workspace already exists); `agent_daily_memories` table with composite PK `(workspace_id, agent_id, memory_date DATE)` — the PK is the upsert conflict target. Alternatives rejected: memory columns on `users` (makes USER.md global-per-user — crosses the tenant boundary, contradicts workspace scoping); one polymorphic memories table (implicit shapes; this codebase prefers explicit interfaces).

**D2 — One `store.MemoryStore` sub-interface over three shapes.** Six methods: `UserMemory(ws, userID) Get/Upsert`, `WorkspaceMemory(ws) Get/Upsert`, `AgentDailyMemory(ws, agentID, date) Get/Upsert`. Injected positionally into the tool constructor per the DI rules. Postgres impl notes: the workspace scope is a targeted `UPDATE workspaces SET memory = $1 WHERE id = $2` — memory never rides `WorkspaceStore.Update`'s column list, so settings saves and memory saves cannot clobber each other.

**D3 — Tool surface: `memory`, actions read/append only.** Catalog entry `Key: "memory"`, group `memory`, non-configurable (plain allowlist toggle like `read_file`). `write` was dropped as redundant with `append` (user decision); consequence accepted: agent-side correction is additive, and a cap-saturated USER.md/WORKSPACE.md can only be trimmed by a human via the UI. Daily memories cap out with no trim path in v1 — accepted.

**D4 — Path grammar resolved server-side.** Exact match on `USER.md` / `WORKSPACE.md`; `MEMORY-DD-MM-YYYY.md` parsed strictly (Go layout `02-01-2006`) into a `date` column value; `MEMORY-TODAY.md` resolved to the workspace-local date at call time. Unknown or malformed paths return a JSON tool error listing the accepted forms. Alternative rejected: relying on prompt-injected dates only — models cannot be trusted to compute today's date; TODAY gives determinism while strict parsing keeps stored dates normalized (the dd-mm-yyyy format exists only at the tool boundary).

**D5 — Scoping is structural, not checked.** `ToolContext` gains `UserID` and `WorkspaceTZ` (the runner already loads user, membership, and the workspace-scoped agent before construction — `runner.go` `load()`); the constructor binds exactly those, so no argument exists to address another principal. Data-layer hardening: composite FK `agent_daily_memories(workspace_id, agent_id) → agents(workspace_id, id)` backed by `UNIQUE (workspace_id, id)` on `agents` makes cross-workspace daily rows unwritable, not merely unqueried. No defensive re-checks inside the tool (nil-check policy).

**D6 — Atomic append with in-statement cap.** Cap constant `maxMemoryContentChars = 32000` (~8k tokens at ≈4 chars/token) owned by the domain. Appends execute as one statement — upsert with `ON CONFLICT … DO UPDATE SET content = content || $n WHERE length(content) + length($n) <= $cap` — so no read-modify-write race and no partial cap bypass. The same validator serves all three write paths (tool JSON error, HTTP 422, UI counter); endpoints return `max_chars` alongside content so clients hold zero constants.

**D7 — Injection via the composer, labeled apart from metadata.** Compose() gains two subsections: `## Shared memory` under `# Workspace`, `## Memory` under `# User`. Metadata stays free context; memory carries what the structured fields don't. Empty memory omits its subsection. Per-execution composition (existing behavior) delivers "every next prompt brings USER.md and WORKSPACE.md" and makes last turn's appends visible this turn — two tiny indexed reads per run. The L1 template explicitly instructs the model never to re-store free context.

**D8 — Human endpoints.** `GET/PUT /api/v1/workspaces/:ws/me/memory` (membership only; self-scoped) and `GET /api/v1/workspaces/:ws/memory` (members) / `PUT` (workspace settings-management permission — the same gate as Tools PATCH). Payloads `{content}` / `{content, max_chars, updated_at}`. Old routes `GET/DELETE /workspaces/:ws/agents/:agent/memory` are deleted with the handlers.

**D9 — L1 template update reaches new agents only.** `SeedWorkspace` deliberately never overwrites an existing AGENTS.md (agents may customize it), so the new Memory section lands in fresh agent dirs; existing agents learn the mechanics from the tool description + injected subsections. Template versioning machinery rejected as disproportionate.

**D10 — Birth completes via a jail-scoped `delete_file`.** Added to the fs toolset (os.Remove within the jail; deleting a missing file is a tool error). `BOOTSTRAP.md`'s closing line is updated to name the deletion explicitly. Server-side deletion rejected: there is no reliable signal the ritual finished, and the template already casts the agent as the actor.

**D11 — Tool description is the contract.** The `tool.Info` description enumerates the three scopes, the read/append actions, the TODAY alias, the append-only discipline, and the "don't re-store free context" rule — it is the only instruction surface guaranteed to reach *every* agent regardless of AGENTS.md vintage.

## Risks / Trade-offs

- [Memory bloat inflates every future prompt] → hard cap (D6) + template guidance to keep entries short; counter in both editors
- [Append-only lets USER.md/WORKSPACE.md sprawl toward the cap] → human editors are the trim path; accepted for v1
- [Per-turn DB reads add latency] → two indexed point reads per execution; negligible against model round-trips
- [Composite FK requires `UNIQUE (workspace_id, id)` on agents] → one cheap unique index; verified against existing indexes before adding
- [Existing agents never see the L1 Memory section] → tool description (D11) carries the contract universally
- [Dropping `agent_user_memories` is irreversible data loss] → impossible: no runtime or API writer ever existed, so content is provably empty; down migration recreates the empty table

## Migration Plan

`000025_agent_memory` (up: create `user_memories`, `agent_daily_memories`, add `workspaces.memory`, add agents unique index, drop `agent_user_memories`; down: reverse, recreating the old table empty). Deploy per repo pattern: `migrate up`, then restart the server binary. No data backfill exists or is needed.

## Open Questions

- Browser UI for past daily memories — deferred; transcript cards cover v1.
