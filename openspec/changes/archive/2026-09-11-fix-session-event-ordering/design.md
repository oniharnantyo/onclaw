## Context

Every session event carries a `seq` used as the log's total order and the basis of event-id cursor pagination (`after` resolves an event id to its seq, then pages strictly after/before it). `ADKSessionAdapter.AppendEvents` (internal/agents/session_adapter.go) allocates `nextSeq = len(existing)` from a full-history load whose comment says `Limit: 0 // all` — but both stores map `Limit <= 0` to a 100-row default (internal/store/postgres/session_events.go, internal/store/fake/fake.go). Past 100 events, allocation collapses (production data: 60 rows at seq=100, 16 at seq=101 in one session) and `ORDER BY seq LIMIT 100` returns an arbitrary, unordered subset. Callers drinking from the same capped read: transcript History, the ADK context replay, compaction (internal/agents/compact.go), the `/v1` facade (internal/server/handlers/v1.go), and `PendingApproval`. See proposal.md — Why.

## Goals / Non-Goals

- Goals: seq monotonic for sessions of any length; deterministic load order; complete unbounded loads by default; one-time repair of already-corrupted logs; append cost stays sub-linear in log size.
- Non-Goals: no wire/DTO changes; no switch of the ordering primitive away from seq (it is the ADK cursor basis); no retention/TTL policy for event logs; no new HTTP pagination behavior (existing `after`/`limit` semantics unchanged).

## Decisions

### D1: Honor `Limit <= 0` as "no limit" in both stores (contract fix, not a new mechanism)

The adapter's call site already documents `Limit: 0 // all`; the stores are the deviation. Fixing the stores heals allocation (len(existing) becomes the true count again), History completeness, compaction, `/v1` chaining, and `PendingApproval` in one stroke. The fake store changes identically so fake-based tests exercise the same semantics. Callers that want a bound (cursor-paginated reads) already pass an explicit positive `Limit`.

### D2: Keep seq as the ordering authority; add `(seq, occurred_at, event_id)` as the deterministic order

Swapping to a timestamp keyset would churn the ADK cursor contract, the fake, and History for zero payoff once allocation is monotonic. The composite order is a seatbelt: even if tied rows existed, two loads can never disagree. `occurred_at` is trustworthy for repair because it is written once from the event timestamp (or append time) and never rewritten.

### D3: Repair migration re-sequences per session by `(occurred_at, event_id)`

One embedded SQL migration: per `session_id`, assign new seq = row_number over `(occurred_at, event_id)` minus 1, via a single UPDATE ... FROM CTE. Down migration is a no-op — pre-repair seq values for tied rows are garbage and intentionally unrecoverable. Safe to run on healthy logs (same relative order → same numbering). Repair ordering equals append order in practice: `occurred_at` derives from the event timestamp captured at append time.

### D4: Duplicate detection without the full pre-read

Today `AppendEvents` re-reads the whole session per event to build an id set (O(n) per event, O(n²) per session) — acceptable only while the read was accidentally capped. With unbounded reads this becomes the new hot spot, so: drop the pre-read; keep the within-batch duplicate check; add a cheap existence probe per candidate event (single indexed `SELECT 1` per event id, or equivalent) that returns `adk.ErrDuplicateEventID` on a hit; the store's `ON CONFLICT (session_id, event_id) DO NOTHING` remains the last-line idempotency guarantee.

### D5: Concurrency assumption recorded

Sequence allocation assumes per-session serialization of appends (turn execution holds a per-run lock; compaction is compact-as-a-turn). Two concurrent appends to one session would race the count. This is unreachable today; the assumption gets a comment at the allocation site rather than locking machinery.

### D6: History endpoint stays unbounded by default

The chat client windows to the latest 80 messages client-side and the `after` cursor pages older history, so a full-session read per chat open is correct-first and bounded in practice (sessions realistically hold low-thousands of rows). If a perf bound is ever needed, the endpoint can pass an explicit `Limit` without touching the port contract.

## Risks / Trade-offs

- Migration rewrites seq values on corrupted sessions; any consumer holding a seq (not event id) across the upgrade would desync — none exist (cursors are event-id-based).
- `[req.Limit > 0]` cursor conditions in the adapter and History must be revisited so "no limit" no longer suppresses the next-cursor computation where a caller asked for a bound.
- The fake store must change in the same commit as Postgres or parity tests diverge.

## Open Questions

- None — all decisions resolved during exploration (2026-09-11).
