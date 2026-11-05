# Design

## Context

`add-agent-groups` ported OpenBot's channel machinery (tasks, budgets, packets, deterministic routing) but kept two semantic divergences from the reference implementation: a member turn that ends without a reported result parks its task into a permanent `waiting` state with no automatic exit, and the transcript renders a live state row under every task-linked message. Source verification of OpenBot (`channel-service.ts` send/pump/assignment-terminal paths) plus its live channel data (Ngobrol: all tasks `completed`, zero `waiting` residues) fixes the target semantics; see proposal.md for the motivation and specs/agent-groups for the behavior contract.

Constraint: the `agent-groups` base spec does not exist under `openspec/specs/` yet — it ships with the complete-but-unarchived `add-agent-groups` change. **`add-agent-groups` must be archived before this change archives** (strict validate already warns; archive refuses MODIFIED deltas against a missing spec).

## Goals / Non-Goals

**Goals:**

- Turn-end semantics match OpenBot: no-result → `completed`; `waiting` = dependency wait with automatic parent re-queue.
- Stop/Continue/Reassign/Cancel operate on the run tree the way OpenBot's do (branch resume, stopped-descendant restart on reassign, cancel gated to stopped chains).
- Surface map: state leaves the message rows entirely; the composer stop becomes the run-level control; one stopped notice per stopped root run.

**Non-Goals:**

- A general `dependencies[]` column or arbitrary multi-dependency DAG: the implicit rule "a parent waits on every child it assigned" covers the only observed writer (delegation).
- Resource-conflict scheduling (browser/workspace reservations refining the 2-active budget).
- The AwaitingReplies composer block: children of a waiting parent are queued or running and already show on the activity line.
- A lead-routing LLM session (D5's lead-answers-directly stands), and any change to the routing tier order.

## Decisions

- **D1 — Turn-end default completes** (`handleTurnEnd` default branch → `GroupTaskStateCompleted`). Alternative (hide the parked row, keep the state) rejected: it strands delegation — today a delegating parent parks forever and nothing integrates the child's result; the park's only exit is a manual user follow-up.
- **D2 — waiting = open children, edge is implicit.** `waiting` is entered only when the turn ends with open child tasks (an `descendantTasks`-style child check over `ParentTaskID`). No schema change: `group_assign_task` is the sole child writer, so "waits on all assigned children" reproduces OpenBot's observed `dependencies` usage without a column.
- **D3 — Child completion re-queues the parent.** In the child's `handleTurnEnd`, after the terminal write: if the parent task is `waiting` and no open sibling remains, re-queue it (revision-bumped) and let the existing `drainGroup` dispatch it under the budget. The parent's second turn reads the child's result row from the packet transcript and integrates. The revision guard already stales in-flight dispatches.
- **D4 — Branch scope for Continue and Reassign.** `ContinueTask` re-queues the named task and every stopped descendant (the composer's continue text rides as the named task's follow-up; descendants re-dispatch on their standing instructions — the drain loop, not direct dispatch, starts them, so the budget holds). `ReassignTask` additionally re-queues the moved task's paused/failed descendants: a restarted root must not wait on a stopped child forever (OpenBot's stated reason, verbatim).
- **D5 — Cancel gated to stopped chains.** `CancelTask` returns an error unless the named task is `paused` or `failed`; it keeps its existing root-chain walk (every open task of the root ends). The frontend offers Cancel only from the stopped notice. Alternative (cancel anywhere) rejected: it doubles Stop's verb — the exact per-row noise this change removes.
- **D6 — Surface remap (full OpenBot map).** `GroupAttribution` stops rendering `GroupTaskRow` in every state. New `GroupStoppedNotice`: one row per paused/failed ROOT run (store-filtered `groupTasks`), rendered once at the transcript tail beside the activity line, with the stop reason, Continue (re-dispatch with "Continue", the existing flow), and Cancel (confirm dialog, existing). The group composer's stop control branches on the group surface: while the open group has a running task, stop ends that task's root chain (`groups.stopTask(rootID)`); the session-surface stop is unchanged. Activity line untouched (queued/running owners already render).
- **D7 — Recovery converts stranded parks.** Existing rows parked `waiting` by the old semantics have no children and would strand under the new rule; restart recovery marks childless `waiting` tasks `completed` on boot. Terminal under both code paths — rollback-safe.
- **D8 — Routing unchanged in code.** `openTasks` still feeds tier 5; `waiting` becomes transient so the sole-open-task window narrows naturally. The routing test gains the post-completion case (sole task completed → untargeted follow-up falls to the lead).

## Risks / Trade-offs

- [Re-queued parent turn double-reports a result] → `group_task_result` already rejects terminal tasks; the parent integrates rather than re-completes.
- [Crash mid-delegation re-queues parent and child independently] → parent's restarted turn re-reads the transcript and re-delegates only if needed; accepted (OpenBot's recovery has the same shape). D7 covers stranded parks.
- [Post-completion follow-ups reach the lead instead of the member] → spec'd behavior (scenario under the lifecycle delta); mention/reply tiers still target the member; matches OpenBot.
- [Composer stop with several active roots stops only the first] → store-order first active root; acceptable for a 2-active budget (at most two roots can run); revisit if budgets grow.
- [Archive ordering] → `add-agent-groups` archives first; validation already refuses the reverse order, so the failure mode is a clean refuse, not corruption.

## Migration Plan

No schema migration and no binding changes. Deploy order: backend semantics and frontend surfaces land together (the old UI's Cancel-only waiting rows assume the parked state). Boot-time: D7's recovery rule converts legacy parked rows. Rollback: revert; legacy rows are terminal (`completed`) and harmless under the old code path.
