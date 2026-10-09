# Proposal

## Why

Delegation runs dark. When an agent delegates to a subagent (or launches a background shell), the web shows a generic collapsed tool card and nothing else: no live trajectory, no list of what is in flight, no way to stop a background task. The backend already persists the full child trajectory line-by-line (`.tasks/<task_id>.output`), emits completion events the web drops on the floor, and holds a per-run task space with cancel — none of it is surfaced to users.

## What Changes

- **Session tasks API** (new, member-facing): list a session's background tasks (delegation + shell kinds) with status, description, kind, and output location; read a task's trajectory (live tail or replay from the persisted output file); cancel a running background task; subscribe to live progress for the session's running tasks. Endpoints reach the live run's task space through the same registry path the existing run-cancel uses.
- **Live progress side-channel** (new): a dedicated SSE stream on the session tasks API delivers task-keyed child execution records (text, tool calls, tool results) while tasks run — never persisted to the session transcript, never entering the parent's context, and never multiplexed onto the existing turn or session-event streams. Task-keyed and separately addressable, so the transcript still shows exactly one card per delegation.
- **Floating run card** (rework of the session-todos surface): the current header chip + popover is replaced by a floating card auto-surfaced over the transcript, hosting the Progress checklist (todos, unchanged data lane) plus a new Agents section — live rows for running delegations and shells, an "Ended N ›" entry. Same auto-surface machine (arm on run start, auto-open, sticky dismissal). No header chip.
- **Subagents panel** (new right-panel source): a list view (Running with live preview strip / Ended with status + snippet) and a trajectory detail view (back navigation, kind/elapsed/tool-call count, Stop button). Stop is enabled for background tasks only; foreground delegations show it disabled. Ended rows replay from the persisted output files, so history survives reload.
- **Rich delegation cards**: the `agent` and `background_shell` tool cards gain display entries (name, one-liner, live status) and a deep-link into the panel's trajectory detail.
- **Wire `task_completed` through the web translator** (today it is dropped).

## Capabilities

### New Capabilities
- `agent-runs-surface`: user-facing visibility and control over a session's background tasks — the session tasks API (list, trajectory read, cancel), the live task-keyed progress side-channel, and replay of ended tasks from persisted output files.

### Modified Capabilities
- `agent-subagents`: the Child transcript isolation requirement gains a scoped carve-out — a task-keyed progress side-channel for UI consumers is permitted, provided the parent transcript still shows exactly one card per delegation and the parent's context never carries child events.
- `agent-todos`: the Session todos chip and Todos popover requirements are replaced by the floating run card hosting the Progress checklist (same todo data lane and present-only rule, new host surface).
- `web-app/chat`: new requirements for the floating run card host, the Subagents panel source (list + trajectory detail), and rich `agent`/`background_shell` tool cards with deep-linking.
- `web-app/chat-runtime`: consuming task-keyed progress events into client state the card and panel read, and wiring the panel's Stop action to the task cancel API.

## Impact

- **Backend**: `internal/agents` (runner task-space exposure, progress drain on the task space — spike against eino's public API: Manager progress-entry cursor vs. custom `TranscriptFormat` closure), `internal/server/handlers` (session tasks endpoints + progress stream), event types (`internal/agents/events.go`).
- **Frontend**: `web/src/lib/livechat.ts` (fold the already-delivered `task_completed` event), `web/src/lib/panel/` (new source module), `web/src/components/chat/` (floating card replacing `SessionTodosSurface` header anchoring, tool-card display), `web/src/store` (panel + runs state).
- **Wire**: one new dedicated SSE endpoint (session tasks progress stream); the existing `/v1` and session-events streams are unchanged — no new persisted session-event kinds (the side-channel is live-only; history replays from output files; `x.task_completed` stays the durable terminal chip).
- **Known wart, accepted**: the call↔task join (which tool card maps to which task id) is resolved render-side by description match with order as tiebreak; upstream eino does not carry the call id on the task record.
