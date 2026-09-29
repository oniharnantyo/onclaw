# Proposal

## Why

Agent todo lists are visible only as inline transcript cards at the moment of each `todo_write` call. Once the agent moves past the plan, there is no persistent place to see where things stand: a user scrolling a long transcript, returning to an active session, or joining mid-run must archaeologize the event log. The current plan already exists as queryable state (the newest `todo_write` call's args); nothing surfaces it continuously.

## What Changes

- Add a persistent session todos surface to the direct-chat view: a **header chip** (collapsed always-visible state — spinner while an item is active, done/total progress) that opens an **anchored popover** with the full current checklist.
- **Auto-surface**: the popover opens itself on the run's first `todo_write` call and collapses back to the chip when the run finishes or the list completes. Presence follows events, not a permanent layout tax.
- **Present-only**: no chip and no popover when the session has no todo plan or the agent does not expose `todo_write` (extends the existing no-empty-state rule to the new surface).
- **Client-side data lane**: the current plan derives from the newest `todo_write` call args already present in loaded session events, updated live by the same tool-call event stream the inline cards use. No backend, API, or schema changes.
- **Transcript relaxation**: only each turn's final `todo_write` keeps a full inline card; earlier calls — cross-turn now, not just same-turn — collapse to the existing one-line "plan updated" summary. The persistent surface owns current state; the transcript keeps the narrative.
- **Mobile degradation**: at small viewports (360×800 floor) the popover renders as a bottom sheet; the chip is unchanged.

## Capabilities

### New Capabilities

<!-- none — this change extends an existing capability -->

### Modified Capabilities

- `agent-todos`: add requirements for the persistent session todos surface — chip + popover presence and states, auto-surface behavior, mobile degradation — and the transcript collapse rule (turn-final card only).

## Impact

- **Web frontend only**: chat view header, a new chip + popover component pair, the client todo selector over session events, and the generative-ui todo renderer's collapse rule. `TodoChecklistCard`'s row states and tolerant `todo_write` parser are reused, not replaced.
- **No backend changes**: `agent_todos` store, tools, and APIs are untouched. The existing compaction summary injection keeps reading the store server-side.
- **Out of scope (v1)**: channel rooms (they aggregate several agents' sessions — per-agent grouping deferred), server endpoint over `TodoStore.GetBySession` (added when a surface outside the session needs todos), any task-queue semantics (dependencies/owner — tracked by the eino-deep-todo explore, not this change).
