# Proposal

## Why

Buddies agents compose only a persona (IDENTITY/SOUL/BOOTSTRAP — who the agent is) into their turns; nothing tells them how to work. There is no guidance for denied tool calls, hook feedback, outcome reporting, or autonomy — yet agents run from scheduler, webhook, and channel origins where no user is present to answer questions. An analysis of the ZCode harness's system-prompt layer (sections, contract, subagent notes) found the missing *behavioral* layer and supplied proven content to adapt.

## What Changes

- Add a runtime-owned **operating contract** composed into every agent turn as a virtual `AGENTS.md` document: harness rules (denied calls, hook feedback, parallel tool calls, outcome reporting), communication guidance, workspace rules (inspect-before-delete, faithful reporting, no report-file litter), two operating modes (live chat vs autonomous run), and context-management guidance. Virtual means a Go constant in the runtime — no file on disk, no seeding, no migration, promptgen untouched.
- Add a per-turn **environment block** after the compaction summary stating the effective provider/model, the current date, the turn's origin, and whether the user is present — this is what lets the contract's static mode descriptions select the right mode per turn.
- Compose a **reduced operating contract** into subagent child turns: findings belong in the child's final reply (never report/summary files), workspace-safe absolute paths. The full contract's user-facing mode guidance stays out of child prompts.
- Turn composition order becomes: persona documents → operating contract → compaction summary → environment block.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agent-prompts`: adds three requirements — the virtual operating contract composed on every turn (present even when persona documents are absent), the per-turn environment block, and the reduced contract composed into subagent child turns. Existing persona-document requirements are unchanged.

## Impact

- `internal/agents`: new contract constant (new `contract.go`), one-line composition in `persona.text()` (`runner.go`), environment block in `assembleTurnInput` (`turninput.go`), origin already available on `turnRequest` (design D6 of the scheduler/integrate-agent-chat work).
- `internal/agents/subagents`: child compose gains the reduced notes constant.
- `internal/workspaces`, `internal/promptgen`: untouched — the contract is virtual, so no doc family, seeding, or generation changes.
- No frontend, database, migration, or API changes. Tests pin the contract's section order and boundary strings, mirroring ZCode's prompt-contract test coverage.
