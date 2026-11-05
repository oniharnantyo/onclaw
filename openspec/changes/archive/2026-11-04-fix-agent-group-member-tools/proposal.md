# Proposal

## Why

Group member turns cannot open files. The live "Chit Chat" (Ngobrol) trajectory of 2026-11-03 shows every member route dead-ending: Joni's 14 mechanical attempts to read the knowledge corpus (`ls`, `cat` INDEX.md, `grep`) all returned "no execution tool in this session", including through a fresh subagent; Scout inventoried the surface as exactly `group_history`, `group_assign_task`, `group_task_result`, and `skill` — and the `knowledge-search` playbook the member can read demands `read_file`/`grep` that do not exist. Research work in a group is limited to what the packet itself carries.

The cause is an over-read of the archived design: `add-agent-groups` decided the in-channel tools "replace the session's generic **history** tools" (design.md:66), but the implementation withholds the member's entire ordinary tool catalog for the turn (`gate.go` `groupToolSurface` builds a bare surface with no workspace, so `runner.go` never mounts the filesystem capability, the knowledge corpus, or the uploads mirror). The synced spec never asked for this: "Member threads and context packets" requires the member's "persona, tools, memory, skills, and compaction behave as in any ordinary session", and "In-channel tools for group turns" bars only the ordinary session-history affordances. The reference implementation agrees: OpenBot's channel tools are an additive layer over the member's full provider toolset — that is how its channels run real work tasks.

## What Changes

- **Composite group-turn surface**: a member's group turn is offered its **ordinary tool surface** — the same catalog through the three-way gate (catalog × global enable × agent allowlist), the workspace-rooted filesystem tools, the knowledge corpus per its gate, the uploads mirror, and the agent's enabled capabilities — **with the three in-channel group tools appended**. The packet remains the turn's whole transcript input; the group transcript reader stays the only way to read the shared room.
- **Workspace rides the group turn**: the member's workspace jail mounts for the turn (its session-relative paths, truncated-output offload, subagent workspaces), replacing today's "no workspace this turn" mount.
- **Doc-comment correction**: the "replace the turn's ordinary catalog surface entirely" comments (`groupturn.go`, `gate.go`, `manager_impl.go`) are rewritten to the additive contract.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `agent-groups`: the "In-channel tools for group turns" requirement — the group tools ride **beside** the member's ordinary work surface instead of replacing it; the history-tool exclusion stays.

## Impact

- **Backend**: `internal/agents/runner.go` (step 6 branch resolves the ordinary surface for group turns too and appends the group tools; step 8 keeps the member's `sessionRel`, uploads mirror, filesystem/reduction/plantask capabilities for group turns), `internal/agents/gate.go` (`groupToolSurface` removed), `internal/agents/manager_impl.go` (dispatch-time comment), `internal/agents/groupturn.go` (seam doc comment). No schema, binding, or store changes.
- **Tests**: `groupturn_test.go` ("exactly the source's tools" assertion flips to catalog + group tools, workspace-rooted), the group-tool tests in `internal/groups` (unchanged — the group tools' behavior is untouched), and a new case pinning that a group turn can read a workspace file and the knowledge corpus per its gate.
