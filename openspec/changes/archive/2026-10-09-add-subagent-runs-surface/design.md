# Design

## Context

Every delegation already writes the child's full trajectory — one JSONL line per child message (`{agent_name, message}`) — to `<agentDir>/.tasks/<task_id>.output` through an unbuffered append opener, live as the child runs, for both lanes (delegation and background shell), foreground and background alike. The per-run `BackgroundTaskSpace` (in-memory store + Manager + local Runner) owns task lifecycle; its notification pump already emits `task_completed` transcript events (persisted as `x.task_completed` session events) with `{task_id, kind, outcome, output_path, summary}`. Eino's agent tool mirrors every child record into the task record via `runtime.EmitProgress` — the same channel `task_output` reads. The web currently drops `task_completed`, has no display entry for the `agent`/`background_shell` tools, and never reads the output files. Run cancel reaches live runs through a runner-owned registry; `CancelRun` is the precedent for the tasks endpoints.

Constraints that shape this design: the `agent-subagents` spec pins child transcript isolation (one card on the parent transcript, child events never in the parent's context or live stream); the task space is process-local (honest-lifetime pin); tasks are not created with their tool-call id, so call↔task correlation is not authoritative upstream.

## Goals / Non-Goals

**Goals:**
- Users see what delegations and background shells are doing, live, and can stop background ones.
- History keeps working: ended tasks replay after reload from persisted output files.
- Parent transcript and context stay exactly as pinned today (one card per delegation).

**Non-Goals:**
- No durable persistence of child events (no new session-event kinds, no DB table for tasks).
- No per-task live editing/re-execution, no subagent permission changes, no recursion.
- No change to foreground delegation semantics (still blocks the parent; not stoppable from the UI).
- No new transcript elements for `task_completed` — it updates run/card state, the transcript keeps one card.

## Decisions

- **D1 — Progress tap: drain the task space behind a dedicated SSE endpoint (pending one spike).** The pump already ticks at 250ms per run and the agent tool already mirrors each child record into the task record via `EmitProgress` — the same store `task_output` reads. A drain (the pump or a sibling ticker) reads each live task's progress entries past a per-task cursor and fans them to subscribers of the session tasks progress stream. Shell tasks emit through the same `EmitProgress` path, so one drain covers both lanes. Alternative rejected: a custom `TranscriptFormat` closure emitting from inside eino's file writer — it couples emission to the formatter, misses the shell lane's symmetry, and burfs if eino changes write batching. **Spike (task 1.1) verifies** the Manager/store exposes progress entries with a readable cursor (List/Get shape); if not, the fallback is the `TranscriptFormat` closure (in our config, fully in our control).
- **D2 — Dedicated stream, live-only; history = output files.** Progress rides a new SSE endpoint on the session tasks API (`GET …/sessions/:sid/tasks/stream`) — task-keyed frames for the session's running tasks, subscribed while the card/panel wants them. The existing `/v1` and session-events streams are untouched, so transcript isolation needs no carve-out on those contracts and no stream schema grows. Nothing new persists. Ended/reloaded tasks replay by reading `.tasks/<id>.output` from disk — the file outlives the run. `x.task_completed` remains the durable terminal chip.
- **D3 — Session tasks API, session-scoped, registry-routed.** Member-facing endpoints under the existing agent-session family: `GET …/sessions/:sid/tasks` (list), `GET …/sessions/:sid/tasks/stream` (live progress, D2), `GET …/sessions/:sid/tasks/:tid/output` (trajectory), `POST …/sessions/:sid/tasks/:tid/cancel`. The handler resolves the live run via the runner registry (same path `CancelRun` uses): list = live store ⋈ persisted `x.eino.background_task.created`/`x.task_completed` session events (so ended tasks from earlier turns appear); output = live progress read, else file read from `output_path` when the run is gone; cancel = `Manager.Cancel`, refused (409) for anything not running. Foreground delegations never become tasks, so they are invisible to cancel by construction — the UI gate (D4) mirrors the backend's reality.
- **D4 — Stop = background tasks only.** The panel's Stop renders for background delegations and shell tasks; foreground delegation cards show it disabled ("background only"). The parent-run stop button remains the only way to halt a foreground child.
- **D5 — Call↔task join: render-side, by description.** The web joins `agent` tool cards to tasks on `args.description` (order as tiebreak); the join is display-only. Accepted wart: two same-description concurrent delegations can cross-join — pathological, and the upstream fix (call id on the task record) is not ours to make.
- **D6 — Floating card rework replaces the header chip.** The current `SessionTodosSurface` chip + anchored popover is rebuilt as a floating card over the transcript (top-right; bottom sheet <640px) hosting the Progress checklist and the new Agents section (running rows + "Ended N ›" opening the panel). Same data lane and auto-surface machine (arm on run start, auto-open once per run, sticky dismissal per run, chat-switch reset); present-only rule extends: no card when the session has neither a todo plan nor any agent/shell activity. The `agent-todos` chip/popover requirements are replaced, not extended.
- **D7 — One panel source, two views.** A single `subagents` panel-source module owns a list view (Running with a live preview strip / Ended with status, snippet, relative time) and a trajectory detail view (back navigation, kind/elapsed/tool-call count, Stop, streamed-or-replayed transcript). Internal view state lives in the module; the registry candidate matches `agent` and `background_shell` cards so deep-links open detail directly.
- **D8 — Rich delegation cards.** `agent` and `background_shell` gain tool-display entries (icon, one-liner from `args.description`, live status, deep-link button) in the web tool display/catalog tables.
- **D9 — `task_completed` folds into run state.** The translator (live SSE + hydration) maps `task_completed` onto the runs store — status flips in card, floating card, and panel list. No transcript element is added; the existing hydrated-render promise is satisfied by the state flip, not a chip.

## Risks / Trade-offs

- [Spike outcome: progress entries may not be cursor-readable from the Manager] → Task 1.1 verifies before any wiring; fallback `TranscriptFormat` closure is specified and isolated behind the same event-emitter seam.
- [Progress flood on chatty children] → Events are per-record but bounded by the child's own iteration cap; the panel renders a tail window, and the card/panel stores keep only recent records per task.
- [Description-join cross-talk] → Display-only; worst case a panel opens on the sibling task's trajectory. Accepted.
- [Output files grow unbounded in the jail] → Pre-existing condition (files are written today); no change in this plan. Retention is the workspace-files concern, out of scope.
- [Ended tasks' files may be pruned while session events persist] → Output endpoint returns a clear not-found; the list still shows the task with its summary/outcome from persisted events.

## Migration Plan

Additive: new endpoints, new event kinds, new web module. The only removal is the header chip (D6) — same-session UI rework, no data migration, no schema change. Rollback = revert; nothing persists that a revert cannot ignore.

## Open Questions

None blocking; the D1 spike decides between two specified taps, not between approaches.
