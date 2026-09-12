## Why

The session event log's ordering primitive silently collapses past 100 events: `ADKSessionAdapter.AppendEvents` stamps `nextSeq = len(existing)` from a load the store caps at 100 (`Limit <= 0` silently maps to a 100-row default), so every event after the 100th shares `seq = 100`. `LoadEvents` then orders by `seq` with `LIMIT 100`, and with ties the returned subset is arbitrary and unordered. Verified against production data (one 176-event session held 60 rows at seq=100 and 16 at seq=101; the History API returned 47 events missing every recent turn). The corruption is user-visible — scrambled, incomplete chat transcripts that persist across reloads — and latent: the same capped read also bounds the agent's own context replay, compaction, `/v1` response chaining, and the pending-approval scan.

## What Changes

- **Port contract fix**: `LoadSessionEventsParams.Limit <= 0` SHALL mean "no limit" (all events), honoring the documented `// all` intent at the adapter call site. Both the PostgreSQL store and the in-memory fake change semantics together.
- **Deterministic ordering**: both stores SHALL order by `(seq, occurred_at, event_id)` ascending (and the exact reverse for `Reverse`), so ties can never reorder results even if legacy tied rows remain.
- **Data repair migration**: one embedded SQL migration SHALL re-sequence every session's rows by `(occurred_at, event_id)` so existing tied sessions return to true chronological order and seq becomes strictly increasing again.
- **Append cost containment**: with the duplicate-detection pre-read now unlimited, `ADKSessionAdapter.AppendEvents` SHALL stop re-reading the whole session per appended event (O(n) per event, O(n²) per session); duplicate detection SHALL rely on the store's `(session_id, event_id)` primary key with a cheap existence check that still surfaces `adk.ErrDuplicateEventID`.
- **HTTP bounds unchanged in contract, now explicit**: the History endpoint keeps working as today; because the port default disappears, any handler that wants a bound passes an explicit `Limit`. (Chat hydration loads the full session — the web client already windows to the latest 80 messages, and the `after` cursor paginates older windows.)

Heals as a consequence: transcripts render complete and ordered (tool cards no longer flash as in-flight after a mid-run refresh because their finished events stop falling out of the subset), the agent's context replay sees the whole session again, compaction summarizes the full history instead of its first 100 events, and `/v1` chaining and `PendingApproval` read complete data.

## Capabilities

### New Capabilities

- `agent-session-events`: the durable, append-only session event log — seq allocation monotonicity, load ordering and limit semantics, and historical re-sequencing — as its own capability shared by the agent runtime, transcript history, and the `/v1` facade.

### Modified Capabilities

- `agent-runtime`: the durable-history requirement gains explicit ordering/completeness guarantees (history reload reflects every persisted event in true execution order, regardless of session length).

## Impact

- `internal/store/store.go` (port doc for `LoadSessionEventsParams.Limit`), `internal/store/postgres/session_events.go` (limit semantics + ORDER BY), `internal/store/fake/fake.go` (same semantics for parity).
- `internal/agents/session_adapter.go` (`AppendEvents` duplicate detection without the full pre-read; `LoadEvents` cursor math unchanged).
- New migration under `migrations/` (re-sequencing UPDATE, down migration restores nothing — the prior seq values for tied rows are garbage and intentionally unrecoverable).
- No wire-format changes; no client changes required.
- Risk: sessions with tied seqs get new seq values from the migration — any external system that persisted a seq cursor across the upgrade would need to re-sync; none exist today (cursors are `event_id`-based).
