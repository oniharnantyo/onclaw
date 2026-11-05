# Design

## Context

The live Chit Chat trajectory (2026-11-03, seq 85–104) exposed that group member turns run with exactly the three in-channel tools plus the skill capability: `groupToolSurface` (`internal/agents/gate.go:209-218`) returns a bare `resolvedTools` with no `agentDir`, so `runner.go` skips the uploads mirror (`surface.agentDir == ""`), mounts no filesystem capability, no reduction, no plantask, no subagent workspace, and zero-clears `composer.sessionRel` ("a group turn mounts no workspace this turn", runner.go:336-341). `GroupTurnTools` (`internal/groups/packet.go:360`) supplies only `group_history`/`group_assign_task`/`group_task_result`. Members therefore cannot read files, run shell, or touch the knowledge corpus — while the skills capability still rides, so members read playbooks that demand the absent tools. The archived design (`add-agent-groups/design.md:66`) decided the group tools "replace the session's generic **history** tools"; the implementation extended the replacement to the whole catalog. The synced spec (`openspec/specs/agent-groups/spec.md`) states the intended contract in "Member threads and context packets" ("the member's persona, tools, memory, skills, and compaction behave as in any ordinary session") and bars only the session-history affordances in "In-channel tools for group turns". OpenBot's channel tools are additive over the member's full provider toolset.

## Goals / Non-Goals

**Goals:**

- A group turn's tool surface = the member's ordinary surface (same three-way gate, workspace jail, knowledge gate, capability middlewares) + the three group tools.
- The packet stays the turn's whole transcript input; group history reading stays exclusively through `group_history`.

**Non-Goals:**

- Any change to the group tools' behavior, the packet builder, cursors, copy-back, or the task lifecycle (align-group-task-lifecycle owns that).
- A session-history replay tool: none exists today, and none is added.
- Touching the ordinary (non-group) turn path.

## Decisions

- **D1 — Composite surface at the runner's step-6 branch.** `runner.go` resolves the ordinary surface with `m.resolveTurnTools(ctx, agent, rt.sessionID)` for group turns exactly as for session turns, then appends `rt.group.tools` to `surface.tools`. `groupToolSurface` (`gate.go`) is deleted. The group tool names (`group_history`, `group_assign_task`, `group_task_result`) collide with nothing in the catalog, so no dedup is needed. The three-way gate now governs the work tools in groups — an allowlist that excludes `read_file` excludes it in groups too (spec scenario "Agent allowlist still governs the work tools"); the group tools themselves are source-provided and never catalog-gated.
- **D2 — Workspace mounts for the group turn.** With a real `agentDir`, the existing capability blocks apply unchanged: filesystem jail rooted at the member's workspace (with the shell gate, extra skills root, knowledge mount per gate), reduction/offload under the member's session directory, plantask, and — `agent.SubagentsEnabled` permitting — subagent delegation with a real `tasksDir`. `composer.sessionRel` keeps the member's session-relative paths (the zero-clear at runner.go:336 is removed), so the environment block names the workspace and the uploads mirror copies the request's attachments (the packet's `Attachments` already ride `req.attachments`) into `uploads/` for the fs tools.
- **D3 — Input assembly stays packet-shaped.** `assembleGroupTurnInput` is otherwise untouched: the packet (request, unread, task results, recent window, summary) remains the model input; the only change is the environment block now carrying the ordinary workspace paths. `group_history` remains the sole transcript reader; no session-log replay tool exists or is added.
- **D4 — Comments corrected to the additive contract.** `groupturn.go:145-150` ("They replace the turn's ordinary catalog surface entirely"), `manager_impl.go:640-646`, and the deleted `gate.go` doc are rewritten: the source-provided group tools ride **beside** the ordinary surface.

## Risks / Trade-offs

- [Harness tests pin "exactly the source's tools"] → `groupturn_test.go:265-268` flips to catalog+group-tools assertions; the fixture needs the workspace tempdir wiring the runner tests already use so the fs tools root somewhere real.
- [A member agent with a narrow allowlist loses work tools in groups too] → spec'd behavior (the gate is ordinary); the Chit Chat members have full surfaces, and the group tools always remain.
- [Skill playbooks now demand tools the surface still lacks] → the skills capability already rides; with fs tools mounted, `knowledge-search`'s `read_file`/`grep` demands are finally answerable.
- [Double registration of a group tool name in some future catalog] → the merge asserts no name collision (cheap guard in the append) rather than silently shadowing.

## Migration Plan

Backend-only, no schema, no bindings, no store changes. Deploy with the align-group-task-lifecycle work (same uncommitted tree). Rollback: revert; the group surface returns to the three-tool restriction with no data effects.
