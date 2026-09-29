# Design

## Context

The transcript renders `todo_write` calls as inline cards from **event args, client-side** — the store is never read by the web (`TodoChecklistCard` header comment; generative-ui `registerToolRenderer('todo_write', …)`). Every call is a full-replace list, so the newest call's args in the loaded session events ARE the current plan, and the same tool-call event stream that feeds the cards live can feed any other projection. The `agent_todos` Postgres store exists but has exactly two consumers, both server-side (`tools/todo.go`, the runner's compaction summary at `runner.go:2046`); no HTTP surface reads it. The chat view has a tabbed right panel and popover primitives (mention/slash menus) already.

Decisions below resolve the exploration recorded in `session-todos-surface-decided` (memory): surface shape chosen over strip/PiP/right-panel-tab; storage question settled — Postgres `agent_todos` stays exactly as is.

## Goals / Non-Goals

Goals:

- One persistent, present-only projection of the session's current todo plan in the direct-chat view: chip (ambient) + popover (full).
- Zero backend changes: the surface rides data the client already has.

Non-Goals:

- No `agent_todos` store, tool, API, or schema changes; the server-side compaction summary keeps reading the store.
- No task-queue semantics (dependencies, owner, metadata) — that is the eino-deep-todo explore's deferred thread, not this change.
- No channel-room support (aggregates several agents' sessions — needs per-agent grouping; deferred).
- No server endpoint over `TodoStore.GetBySession` yet (see D1 trigger).

## Decisions

### D1: Client-side data lane — event selector, not a new endpoint

A client selector derives the current plan from the loaded session events: the newest `todo_write` call's args, parsed by the already-exported tolerant `parseTodoPlan`. Live updates ride the existing tool-call event stream (the same subscription the inline cards use); the selector lives in the client store so chip and popover share one projection.

- Alternatives: (a) new `GET …/sessions/:id/todos` over `TodoStore.GetBySession` — rejected for v1: no consumer outside the session exists, and the event stream already delivers updates; the endpoint is the right move the moment a surface *outside* the session (agents-page chip, scheduler views) needs todos — that trigger is recorded here deliberately; (b) server-computed context injection — already exists for compaction re-grounding, different concern.

### D2: Chip lives in the chat header

The chip renders in the chat header, not the composer edge: it survives composer focus states and stays visible while typing. The chip is the collapsed always-visible state — active-item text in spinner state, done/total count — and renders nothing when the session has no plan or the agent does not expose `todo_write` (present-only, per the existing spec rule).

### D3: Anchored popover, bottom sheet at the responsive floor

The popover opens anchored to the chip using the existing popover primitives (mention/slash-menu precedent — an idiom the design prototype sanctions; the PiP floating card was rejected precisely because no such pattern exists in the contract). Below the breakpoint where the popover cannot fit (360×800 floor), it renders as a bottom sheet with identical content; no horizontal overflow at any viewport.

### D4: Auto-surface — presence is an event, not a layout tax

The popover opens itself on the run's **first** `todo_write` (the moment a plan is declared is the moment it matters) and collapses to the chip when the run finishes or the list reaches no-open-items. A user dismissal is sticky for the rest of that run: later rewrites update the chip and any open popover but never force-reopen. Next run re-arms auto-open.

### D5: Transcript relaxes to turn-final cards

Only each turn's final `todo_write` keeps a full inline card; earlier calls — same-turn (already the case) now extended cross-turn — collapse to the existing one-line `TodoUpdatedSummary`. The persistent surface owns current state; the transcript keeps the narrative without restating it. Row states in remaining cards are unchanged and reused by the popover.

### D6: Direct agent chats only in v1

The surface mounts in the direct-chat view only. Channel rooms aggregate several member agents' sessions, so "the session's todos" is ambiguous there; per-agent grouping is deferred with the rest of the channel scope.

## Risks / Trade-offs

- [Transcript windowing/virtualization later trims older events, breaking the newest-call selector] → The selector reads whatever events are loaded; if windowing lands, implement the D1 endpoint and seed from it. Trigger documented, cost contained.
- [Popover must layer over streaming tool cards] → Reuse the existing popover layering (mention menu precedent) rather than a new z-index scheme.
- [Auto-open feels intrusive on rewrite-heavy runs] → Once per run, first write only; sticky dismissal (D4) bounds it to one forced appearance per run.
- [Chip and inline card both visible for the same turn's final call] → Accepted: the card marks the plan *at that point in the narrative*; the chip shows *now*. D5 keeps this to one card per turn.

## Migration Plan

Web-only; no schema, API, or prompt changes — nothing to migrate. Present-only rules mean agents and sessions without todos see byte-identical UI. Rollback is a revert of the web change.

## Open Questions

None. The two flagged sub-decisions from exploration (chip placement, transcript collapse depth) are resolved here as D2 and D5 with the defaults stated to the user before capture.
