## 1. Port contract: unbounded reads

- [x] 1.1 Document `LoadSessionEventsParams.Limit` semantics in `internal/store/store.go`: `Limit <= 0` SHALL mean "no limit"; positive values bound the page
- [x] 1.2 PostgreSQL store (`internal/store/postgres/session_events.go` LoadEvents): remove the `limit <= 0 → 100` default; apply the SQL LIMIT clause only when `Limit > 0`
- [x] 1.3 Fake store (`internal/store/fake/fake.go` sessionEventStore.LoadEvents): same semantics change for parity
- [x] 1.4 Deterministic ordering in both stores: `ORDER BY seq ASC, occurred_at ASC, event_id ASC` (and the exact mirrored DESC ordering for `Reverse`)
- [x] 1.5 Audit cursor-suppression conditions tied to the old default (`req.Limit > 0` checks in `ADKSessionAdapter.LoadEvents` next-cursor computation and `Runner.History` next computation) so a positive-limit caller still gets `next`, and a no-limit caller correctly gets an empty `next`

## 2. Append path: monotonic seq without the full pre-read

- [x] 2.1 `ADKSessionAdapter.AppendEvents`: remove the whole-history LoadEvents pre-read; keep within-batch duplicate rejection
- [x] 2.2 Add a cheap cross-call duplicate probe (indexed existence check per candidate event id) that returns `adk.ErrDuplicateEventID` on a hit, preserving the adapter's documented contract
- [x] 2.3 Sequence allocation becomes the true count of prior events: either keep `len(existing)` over an unbounded load equivalent via a single `COUNT`/`MAX(seq)+1` query scoped to the session (workspace-scoped), or query max(seq) directly — implementation per design D4/D5
- [x] 2.4 Comment the per-session serialization assumption at the allocation site (design D5)
- [x] 2.5 Regression: appends into a >100-event session keep seq strictly increasing and append-ordered (fake store test)

## 3. Data repair migration

- [x] 3.1 Write migration `000045_resequence_session_events` (up): per `session_id`, re-sequence rows by `(occurred_at, event_id)` with `row_number() - 1` via UPDATE ... FROM CTE
- [x] 3.2 Down migration: documented no-op (pre-repair tied seq values are unrecoverable garbage)
- [x] 3.3 Integration test (`-tags=integration`): seed a session with 60 rows tied at one seq, run the store against the migrated schema, assert strictly increasing seq matching `(occurred_at, event_id)` order and unchanged payloads/event ids
- [x] 3.4 Verify no code writes `seq` outside the adapter append path (grep audit) so the repair stays authoritative

## 4. Caller audits

- [x] 4.1 `Runner.History` (internal/agents/history.go): confirm the unbounded default load now returns the full log and the transcript projection covers all turns; adjust any test fixture that relied on the 100 cap
- [x] 4.2 `PendingApproval` (internal/agents/history.go): confirm scans see the whole log (no early 100-row cutoff)
- [x] 4.3 Compaction (`internal/agents/compact.go`): confirm the summarizer replays the entire session, not the first 100 events
- [x] 4.4 `/v1` facade (internal/server/handlers/v1.go): confirm chaining reads are complete; add explicit limit only if a bound is semantically required there
- [x] 4.5 HTTP History endpoint (`internal/server/handlers/agents.go` ListSessionEvents): keep `after`/`limit` wire semantics; document that no limit means full log

## 5. Verification

- [x] 5.1 Fake-based tests: new suite covering >100-event sessions (append order, unbounded load, cursor pages, reversed pages, kind filter with unbounded load)
- [x] 5.2 Integration tests: parity of the four scenarios above against Postgres, including the legacy-tie seatbelt (two loads over tied rows return identical order)
- [x] 5.3 `go build ./...`, `go vet ./...`, `go test ./...` green; integration suite green against a migrated database
- [x] 5.4 Manual browser pass: the corrupted Research session (`sess_c5262664-19d5-49ba-945d-5d99b60f2685`) after repair — transcript renders every turn in true chronological order across reload, navigate-away-and-back, and mid-run refresh — VERIFIED 2026-09-11 after migration 000045: full transcript head-to-tail in true order (Summarize → lilianweng turns → attachment tests → 40-count), a NEW run completes on the previously poisoned session (pre-repair it failed every run with "anchor message not found for insertion" and /compact silently no-oped), order holds across navigate-away-and-back and reload; mid-run refresh ordering exercised by the same-day cross-tab pulse test
