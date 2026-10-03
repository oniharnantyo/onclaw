# Tasks

All work is web + transcript-read-path server code; `bench/` and the harness surfaces under measurement are untouched.

## 1. Server: backward pagination on the session-events read path

- [x] 1.1 Add `Before string` to `HistoryRequest` (`internal/agents/history.go`) and thread it through to `store.LoadSessionEventsParams{AfterEventID: before, Reverse: true, Limit: n}`; extend `HistoryResult` with `HasMore bool` + `Oldest string` (next backward cursor).
- [x] 1.2 Extend the events endpoint (`internal/server/handlers/agents.go` ListSessionEvents): accept `before` query param, forward `limit` clamp (500) unchanged; on backward reads return the DESC window reversed to ASC plus `has_more` + `oldest`; unknown cursor event id surfaces the existing invalid-request error contract.
- [x] 1.3 Integration tests (Postgres tag): backward window returns the ≤N events immediately older than the cursor in ASC order; `has_more`+`oldest` chain walks the whole log to exhaustion; empty page at log start; unknown cursor → 4xx; forward `after` reads byte-identical to pre-change behavior.

## 2. Web: server-authoritative hydration + windowed initial load

- [x] 2.1 Remove the `isBoundSessionId` gate from `hydrateSession` (`web/src/lib/livechat.ts`) so any session id hydrates; keep the existing failure-soft contract (fetch failure → null → store untouched); delete or repurpose the predicate's remaining callers (grep all call sites).
- [x] 2.2 Window the initial hydration: request the most recent page (`limit` = client constant H=60, backward read), carry `has_more`/`oldest` into the thread store alongside `lastEventId`; catch-up stream attaches from the newest loaded event exactly as under full hydration.
- [x] 2.3 Unit tests: external-shaped session id hydrates (fetch fires, transcript applies); unknown session id leaves the local thread byte-identical; windowed open holds the older cursor and attaches the catch-up stream from the newest event.

## 3. Web: infinite scroll-up paging

- [x] 3.1 Top-sentinel on the transcript viewport: when visible and `has_more`, fetch the next older page (`before` = oldest loaded event), prepend through an extended server-transcript apply path that preserves the streamed-tail merge rules of `applyServerTranscript`.
- [x] 3.2 Scroll anchoring: measure first-visible-message id + offset before prepend, restore after insert; bottom auto-scroll behavior unchanged when the user is at the bottom.
- [x] 3.3 Fetch discipline: single in-flight older-page fetch (ref guard); terminal state when `has_more` is false — sentinel disarmed, no further fetches, no persistent spinner.
- [x] 3.4 Unit/component tests: scroll-up prepends the older page; reading position preserved across prepend; log-start terminates paging; mid-turn page boundary composes into one rendered turn.

## 4. Verification

- [x] 4.1 `go build ./... && go vet ./... && go test ./...` and integration suite green; web `tsc`/vitest suite green.
- [x] 4.2 After the bench dry cycle completes and the dev server restarts: open a bench session in the pane (original incident repro → transcript renders); page back through a compaction-thread session to log start; open a session mid-run and confirm the catch-up stream completes the turn.
  - DONE 2026-10-04, server restarted on the fixed tree. Bench pane renders the full compaction-session transcript incl. conversation-map "Context compacted" markers (the original incident repro, now green). Scroll-up pass: 46-message session sits inside the 60-event window so has_more=false and the sentinel correctly stayed silent (no fetches, terminal state — the "start of log terminates paging" behavior; no bench session exceeds 60 events, so the live older-page fetch remains exercised by the web/integration suites only). Mid-run catch-up leg not live-reproducible (no run active post-dry-cycle); covered by ChatRoute/runtime tests.
- [x] 4.3 Smoke suite (`scripts/smoke.sh`) green — events-endpoint additions are additive and default reads unchanged.
