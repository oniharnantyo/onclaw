# Design

## Context

Today the rail renders `store.sessions`, which `selectAgent` fills from
`client.chat.sessions(agentID)` — one agent's list only. Agent switching
lives in the composer's picker; the rail's "+" reads `activeAgentID`.
Running indicators come from per-session subscriptions created only in
`selectSession` (store.ts:662 is the sole subscribe site), so a turn fired
by the scheduler into a never-opened session shows no dot even for the
active agent. The backend already has an app-level named-event channel
(`internal/services/events.go`: `gateway:state`, `schedules:changed`,
`knowledge:changed`) pushed to both the desktop webview and the web ws
bridge, and the turns runtime already answers per-session `TurnState`.
`Session.AgentID` is on every row. See proposal.md — Why for motivation.

## Goals / Non-Goals

Goals:

- One rail that reaches every conversation: any agent's session, one click.
- Running state that is true regardless of what the UI has subscribed to.
- No new per-session subscription load; no backend pagination.

Non-Goals:

- Mobile (below `md`) — the rail is hidden there today; unchanged.
- The composer's agent picker — stays as-is; removal would be separate
  cleanup.
- Collapse-state persistence across restarts — launch state is always
  collapsed-except-active.
- Virtualization or server-side session paging — local SQLite, modest row
  counts; the existing max-height scroll region bounds the column.

## Decisions

**D1 — One all-sessions listing; the store's `sessions` becomes every
agent's sessions.** New bound call `ChatService.AllSessions()` returning
every session row (most recent activity first), replacing the per-agent
fetch in `selectAgent` and `refreshSessions`; grouping, sorting, and caps
are client-side. Alternative considered: keep `Sessions(agentID)` and issue
N calls (one per agent) — rejected: N round-trips, N failure modes, and a
staleness window between them, all to avoid one trivial query over an
indexed table. Because consumers already filter (`ChatHeader`,
`selectSession` hydration, `deleteSession`'s remaining-filter all select by
session ID), widening `sessions` to all agents is transparent to them;
group-scoped reads are new rail-local derivations.

**D2 — Running indicators come from the backend, not from subscriptions.**
The turns runtime emits `turns:changed` (payload `{session_id, busy}`,
agent derivable client-side from the session row) on each transition into
and out of the in-flight state — emitted from the runtime core where every
entry point (UI dispatch, scheduler fire, gateway trigger) converges, not
from the chat service handler. A new `RunningSessions()` bound call returns
the current busy set for boot-time and reconnect sync. The rail's
`runningSessions` set is fed by query-then-events; the existing ingest path
keeps updating the same set (idempotent with the push). Park-on-approval
changes nothing — a parked turn is still in flight, so it produces no
transition. Alternative considered: subscribe to every listed session —
rejected: hundreds of live feeds to learn what one map lookup already
knows, and still blind to nothing but heavier.

**D3 — Subscription behavior stays untouched.** Subscriptions remain a
transcript-liveness mechanism (open a session, its feed persists until its
agent leaves). The `selectAgent` teardown that drops other agents'
subscriptions loses its rail meaning but keeps its hygiene value; dots no
longer depend on it. This isolates the change: one removal of concern, no
rework of the subscribe/ingest machinery.

**D4 — Accordion state is rail-local, anchored to `activeAgentID`.**
`SessionRail` holds `expandedAgentID` (init: `activeAgentID`), the search
query, and a per-agent `visibleCount` map (initial 4 most recent + the
agent's active session; each "Load more" +8) as component state. A context
move (`activeAgentID` change) overwrites `expandedAgentID`; a collapsed-row
click sets it without touching the chat store. Per-group load-more counts
reset when the search query becomes non-empty (search lists every match)
and survive otherwise. Alternatives: persist expansion in localStorage
(rejected — makes "collapsed by default" muddy for near-zero value);
accordion in the store (rejected — it is view state; the store needs
nothing from it).

**D5 — Group grammar.** Group order: the active agent first, then agents by
their most recent session activity, session-less agents last (name-sorted
among themselves). A collapsed row renders avatar + name + session count +
the per-agent "+" (+ group dot when `runningSessions` ∩ agent's sessions ≠
∅). The expanded group renders today's `SideRow` entries, per-session
spinner/pulse and hover-delete unchanged, plus the progressive
"Load more sessions" control retiring on exhaustion. Zero-session agents
render a normal collapsed row (count 0; "+" seeds the first session).
Search matches span all agents and expand every group with ≥1 match,
hiding the load-more control while searching (every match is listed).

**D6 — Cross-agent activation rides the existing path.** A session click
under any agent calls `selectAgent(agent.ID, session.ID)` — the signature
already accepts the target session, reloads models, and swaps context; the
per-agent "+" calls a parameterized `newSession(agentID)` (today it reads
`activeAgentID`) and then opens the created session through the same
select path.

## Risks / Trade-offs

- [Emission point misses a turn entry path] → Emit from the turns runtime's
  start/terminal convergence, exercised by scheduler and trigger turns in
  tests; the boot/reconnect requery heals any missed event.
- [Event dropped while webview hidden] → `RunningSessions()` re-queried on
  window focus/reconnect; dots self-heal.
- [`sessions` widening surprises a consumer that assumed one agent] →
  Audit all `sessions` readers (few, all ID-keyed); regression tests on
  header title, override hydration, delete-remaining across agents.
- [Many agents make the collapsed stack tall] → Rows are 26px-class and
  the column already scrolls; accordion bounds the expanded case.
- [Title changes on non-subscribed sessions lag] → Pre-existing today for
  other agents (no listing refetch); unchanged behavior, noted as
  accepted.

## Migration Plan

No schema or persisted-state migration. Backend: add the query and event,
regenerate bindings (`task common:generate:bindings`). Frontend: store
model swap + rail rewrite land together (the rail is the only consumer of
the grouped shape). Rollback: revert the commit; nothing persists.

## Open Questions

None blocking — remaining choices (row paddings, dot placement) are
implementation details under the existing theming/ui-conventions specs.
