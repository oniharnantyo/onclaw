# Design

## Context

Two coupled defects, surfaced by the 2026-10-03 add-harness-bench live run (50+ externally-created sessions unreadable in the pane):

1. **Hydration gate by id shape.** `hydrateSession` (`web/src/lib/livechat.ts:623`) returns null unless `isBoundSessionId` (`livechat.ts:130`) matches `sess_`/`sched_` prefixes; both open paths (`ChatRoute.tsx:133`, `runtime.tsx:451`) route through it, so no `/events` fetch ever fires for foreign ids. The server accepts any `metadata.onclaw_session` value and indexes it (session list shows titles; events endpoint serves the full transcript — verified via curl as the fixture actor).
2. **Unbounded initial load.** Hydration omits `limit`, and the handler (`internal/server/handlers/agents.go` ListSessionEvents) treats omitted limit as "no limit" (fix-session-event-ordering D6 contract).

Enabling facts: the event store already implements reverse keyset pagination (`store.LoadSessionEventsParams.Reverse`, `internal/store/store.go:534` — `seq < cursor ORDER BY seq DESC LIMIT n`); `History` (`internal/agents/history.go:91`) does not expose it; `History`'s translator is explicitly pagination-aware (turn-closing at page end only, `history.go:383`). The client-side replace machinery (`applyServerTranscript`, `livechat.ts:546`) already preserves in-flight optimistic messages and streamed-turn tails, and tolerates unknown sessions (fetch failure → null → store untouched).

The bench dry cycle is live during capture: source changes here do not affect the running server process, but restart must wait until the run completes.

## Goals / Non-Goals

**Goals:**
- Every server-known session hydrates its transcript on open, regardless of which client minted the id.
- Open cost bounded: the recent window only; older history on demand.
- Infinite scroll-up paging with stable reading position (user-decided UX: infinite scroll, not a button).
- Additive API evolution — no breaking change to the existing events contract.

**Non-Goals:**
- No change to the SSE/live-stream protocol; catch-up attach semantics stay as-is.
- No cross-user transcript access changes (the endpoint's authorization and per-user session index scoping are untouched).
- No rewrite of the transcript translator or the threads store; the localStorage cache keeps its role (optimistic/streaming display cache, replaced on open by the server transcript).
- No retro-import of legacy unbound numeric-id sessions (they hydrate nothing and stay local, as today).

## Decisions

**D1 — Gate removal by behavior, not by broader prefix list.** Drop `isBoundSessionId` as the hydration predicate entirely: hydrate whenever a session id exists on the opened chat. Unknown/foreign ids fail soft through the existing catch → null path (local thread untouched, spec scenario "Unknown session leaves local thread intact"). Alternative considered — adding more prefixes — rejected: id-shape matching is the bug; the server already answers "do you know this session" cheaply.

**D2 — Backward window: `before` cursor + `limit`, ASC response.** Extend `HistoryRequest` with `Before string` and thread it to `LoadSessionEventsParams{AfterEventID: before, Reverse: true, Limit: n}`. The handler fetches the DESC window then reverses before returning, so clients always receive oldest→newest order (one ordering contract, no client-side sort). Response gains `has_more bool` + `oldest string` (next backward cursor = first row of the DESC fetch); existing `next` (forward cursor) semantics untouched. Unknown cursor event id → the store's cursor-resolution error → existing 4xx contract (spec scenario). Alternative — an `offset` parameter — rejected: keyset is race-free on an append-only log and the store already implements it.

**D3 — Windowed initial hydration.** Hydration requests `limit = H (default 60, client-side constant)`. The client takes the LAST H events of the response window: implemented as a backward read (`before` = none, reverse + limit H) returning the most recent H events in ASC order with `has_more` telling the client whether older pages exist. `lastEventId` for catch-up attach = newest event of the page — identical to full hydration. Token-meter note: `turn_usage` totals reflect loaded turns only (usage rides `turn_completed` events); accepted limitation, the meter already re-reads on each new turn.

**D4 — Infinite scroll-up (user-decided over "load earlier" button).** A scroll-position sentinel at the top of the transcript viewport triggers the older-page fetch when visible: `before = oldest loaded event id`, prepend via the same server-transcript apply path extended for prepend (merge rules of `applyServerTranscript` preserved — streamed-tail map only strengthens). Scroll anchoring: record the viewport's first visible message id + offset before prepend, restore after DOM insert (the transcript renders from a flat message list; one measurement/restore pair). Guard rails: single in-flight fetch (ref guard, same pattern as `catchUpRef`), terminal state when `has_more` is false (sentinel disarmed — no repeated fetches at log start). Page boundary mid-turn composes because the translator's turn state is per-request and pages are contiguous — verified by the existing limited-page turn-closing logic (`history.go:383`).

**D5 — Cache policy unchanged.** `onclaw.threads.v1` keeps its current role: optimistic sends, live-stream deltas, and last-known transcript for instant paint. The server transcript replaces it on every open (already true for hydrated sessions). This satisfies "always from server" for rendering truth without losing the live-stream UX.

**D6 — Cosmetic limitations accepted.** (a) An approval interrupt exactly at the oldest page boundary renders pending until the next page loads (its resolution event is below the window; the existing pending-interrupt poll heals it). (b) Meter/popover usage reflect loaded turns only (D3). Both documented in the spec deltas' acceptance frames, not worked around.

## Risks / Trade-offs

- [Prepend scroll-anchor jitter on slow devices] → measurement/restore is one synchronous pair around a single DOM insert; if jitter proves real in practice, fall back to hiding the sentinel until prepend completes (contained in the paging component).
- [Rapid scroll-to-top fires many fetches] → single in-flight guard + terminal `has_more` disarm; sequential pages only.
- [Foreign sessions with huge single events (bench compaction threads: multi-KB tool outputs)] → page size bounds event count, not bytes; H=60 of worst-case bench events is acceptable (~hundreds of KB); revisit only if real sessions show pathological sizes.
- [Hydrating sessions that will 404 on every open (legacy numeric ids)] → one cheap failing request per open, silent by contract; acceptable volume; no probe cache added.
- [Server restart coordination] → implementation must not restart the dev server until the bench dry cycle completes (running process unaffected by source edits).

## Migration Plan

1. Server: `HistoryRequest.Before` + handler params + reverse window plumbing + integration tests (store reverse path is already covered; add handler-level tests for cursor/has-more/unknown-cursor).
2. Web: gate removal + windowed initial hydration (D1/D3) + tests; then scroll-up paging (D4) + tests.
3. Manual pass after the bench dry cycle finishes and the server restarts: open bench sessions in the pane (the original incident), page back through a compaction-thread session, confirm catch-up attach mid-run.

Rollback: the API addition is additive (ignored by old clients); the web changes revert cleanly (gate was one predicate; paging is isolated in the transcript component).

## Open Questions

- Page size H=60: reasonable default; confirm feel during the manual pass (tunable constant, no spec impact).
- Exact sentinel placement/overscan in the transcript viewport is an implementation detail left to the apply phase (assistant-ui viewport already manages auto-scroll-to-bottom; the top sentinel must not fight it — bottom auto-scroll wins when the user is at the bottom, top sentinel only when scrolled up).
