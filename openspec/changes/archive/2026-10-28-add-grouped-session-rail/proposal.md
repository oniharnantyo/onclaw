# Proposal

## Why

The Sessions rail shows only the active agent's sessions. Other agents' conversations are invisible while you work, and reaching one takes two steps (composer's agent picker, then the session). As the agent count grows, the rail stops being a useful index of "what has every agent been doing" — and turns fired in sessions the UI never opened (scheduler, gateway triggers) show no running indication at all.

## What Changes

- The Sessions rail lists **all agents** as accordion groups: collapsed by default, the active agent's group expanded; any context move (session click, new session, restored session at boot) auto-expands that agent's group and collapses the rest.
- Each group row carries a **per-agent new-session "+"** that creates and immediately opens a session for that agent — replacing the rail header's single "+", which could only ever mean the active agent. The "+" works on collapsed rows.
- Collapsed group rows show the agent avatar, name, session count, and a group running dot when any of the agent's sessions has a turn in flight; expanded groups show per-session running dots as today.
- The per-group expander becomes a **progressive "Load more sessions"** control (initial window: 4 most recent + the active one; each activation appends the next 8; client-side over the already-fetched listing) — replacing today's binary show-older/show-recent toggle.
- Running indicators are fed by a new **backend turn-lifecycle push** (`turns:changed` app event) plus a `RunningSessions()` boot-time query, so a turn started outside the UI lights the dot. This also closes today's blind spot where the active agent's never-opened sessions show no dot.
- Search spans every agent's sessions, expands each group with matches regardless of collapse state, and restores the collapse grammar when the query clears.
- The chat store holds all agents' sessions (one all-sessions listing) instead of only the active agent's; cross-agent session activation swaps the whole context through the existing `selectAgent(agentID, sessionID)` path.
- Desktop-only: below `md` the rail is hidden today and the mobile surface is unchanged. The composer's agent picker stays.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `chat-view`: the "Sessions organized per agent" requirement is rewritten for the grouped grammar — all-agent accordion list, per-agent "+" with create-and-open, progressive load-more, group/per-session running dots, search across agents — and its scenarios are updated (new-session source is the group row, not the column header; cross-agent session switching joins the grammar).
- `agent-runtime`: a new requirement makes turn in-flight transitions observable app-wide — the runtime exposes the set of sessions with turns in flight (bound query) and announces turn start/end as an app event, so surfaces that do not subscribe to a session's event stream can still reflect its liveness.

## Impact

- **Backend**: `internal/services/chat.go` (all-sessions listing, `RunningSessions()`), the turns runtime (emit in-flight transitions), `internal/services/events.go` (new event name + payload), generated bindings via `task common:generate:bindings`.
- **Frontend**: `features/chat/store.ts` (all-sessions model, group state, running-set source), `features/chat/components/SessionRail.tsx` (grouped accordion grammar), new group-row/section components as needed; tests for store and rail.
- **Untouched**: mobile chat surface (rail is hidden below `md`), composer pickers, transcript, session delete/refusal semantics (already cross-agent safe).
