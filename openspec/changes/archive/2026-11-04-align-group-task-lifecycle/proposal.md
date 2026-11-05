# Proposal

## Why

The group task lifecycle parks a task into a permanent `waiting` state whenever a member's turn ends without a reported result, and the transcript stamps a live state row (with a Cancel) under every task-linked message. The reference implementation this feature ports (OpenBot channels) proves the aligned model in production data: turns without results simply complete, `waiting` is a transient dependency wait that auto-resolves, and task state never renders under messages. Buddies today leaves a delegating parent parked forever with its child's result unintegrated, and litters the transcript with Cancel-only waiting rows (the "Waiting · Scout · Cancel" noise under conversational replies).

## What Changes

- **Turn-end rule**: a member turn that completes without a reported result now **completes** its task instead of parking it. `waiting` is redefined as the **dependency wait** — the turn ended but open child tasks remain — and the parent **auto re-queues** (revision-bumped) when its last child completes, so its next turn integrates the child's result.
- **Branch resume**: Continue re-queues the paused task **and its descendants**; Reassign also restarts the paused/failed descendants under the moved task (a resumed root must not wait on a stopped child forever).
- **Cancel gate**: Cancel becomes the "abandon this stopped chain" verb, allowed only when the named task is paused or failed; Stop stays the one verb for active work. **BREAKING** (UI): the omnipresent per-row Cancel chip — including the waiting row's — is gone.
- **Surface map**: per-message task state rows are removed in every state (state never rides on messages). The group composer's stop targets the top of the active run (`StopTask` at the root). One stopped notice per paused/failed root run renders at the transcript tail with the reason and Continue/Cancel. The activity line (queued/running owners) is unchanged.
- **Routing consequence** (documented, no tier change): conversational turns no longer hold the sole-open-task follow-up tier — after completion, follow-ups fall to the mention/reply/lead tiers, matching OpenBot.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `agent-groups`: the "Group task lifecycle" requirement — waiting semantics and exit, turn-end default state, branch resume, cancel gate, and the task-state rendering surfaces.

## Impact

- **Backend**: `internal/groups/tasks.go` — `handleTurnEnd` (default → completed; open children → waiting; child completion re-queues a waiting parent), `ContinueTask`/`ReassignTask` (branch scope), `CancelTask` (stopped-chain gate). No schema or binding changes; existing bindings (`stopTask`/`continueTask`/`reassignTask`/`cancelTask`) suffice.
- **Frontend**: `frontend/src/features/groups/TaskControls.tsx` + `GroupAttribution.tsx` (row removal, stopped notice), composer stop wiring for the group surface, new stopped-notice component.
- **Tests**: `tasks_test.go` turn-end matrix, `TaskControls.test.tsx` (round-9 waiting-row tests flip), `GroupAttribution` tests, new notice + composer-stop tests; `routing_test.go` gains a post-completion follow-up case.
