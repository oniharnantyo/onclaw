## 1. Episodic tier (L1)

- [x] 1.1 `internal/memory/episodic.go` — `EpisodicSummary` DTO + summarize: reuse compaction summary when present, else one LLM call; extractive `l0_abstract` (no LLM); `key_topics` extraction
- [x] 1.2 `internal/store/sqlite/episodic.go` — `episodic_summaries(summary, l0_abstract, key_topics, promoted_at, expires_at, source_id)`; `EpisodicStore{AppendEpisodic, ListUnpromoted, MarkPromoted, PruneExpired}`
- [x] 1.3 `internal/store/sqlite/episodic_test.go` — black-box: append/list-unpromoted/mark-promoted/prune; `source_id` dedup prevents double-summary
- [x] 1.4 Wire episodic write into the `EventStop` flush path from slice #1 (reuse compaction summary; do not re-summarize)

## 2. Dreaming consolidator

- [x] 2.1 `internal/memory/dream.go` — debounced (10 min per agent) + thresholded (≥ N, default 5) consolidator; reads `ListUnpromoted`; replays a digest (recent verbatim + older `l0_abstract`)
- [x] 2.2 Run synthesis on `memory.review.model` (configurable, eino-ext); extract preferences / project facts / recurring patterns / key decisions
- [x] 2.3 Promote via slice #1's `CoreStore.WriteCore` (consolidate/supersede, never duplicate; char cap + security scan inherited); `MarkPromoted` on success
- [x] 2.4 `dream_test.go` — below-threshold ⇒ no run; debounce skips a re-entry; promotion routes through `CoreStore` (cap/scan enforced); extractive fallback when review model fails

## 3. Review surface + approval

- [x] 3.1 `DREAMS.md` writer (and/or `dreams` table) — per sweep: promotions, supersessions, scores, timestamp
- [x] 3.2 `write_approval` gate: when on, dreaming promotions stage for review (`/memory pending` / approve / reject) instead of writing live; covers `memory`-tool writes too
- [x] 3.3 Web UI view (optional in this slice): list dreaming sweeps + pending approvals

## 4. Pruning + config

- [x] 4.1 Periodic pruner goroutine — delete `episodic_summaries` past `episodic_ttl_days` (default 90)
- [x] 4.2 Config: `memory.review.model`, `memory.dream.threshold`, `memory.episodic_ttl_days`, `memory.write_approval` — wired through the CLI assembly root

## 5. End-to-end verification

- [x] 5.1 `make fmt && make vet && make build` — `CGO_ENABLED=0` preserved; no new storage deps
- [x] 5.2 `go test ./internal/memory/... ./internal/store/sqlite/...` — episodic + dreaming tests pass; ≥ 70% coverage per package
- [ ] 5.3 Manual: run ≥ 5 sessions, confirm dreaming promotes a durable fact into `MEMORY.md` via the cap/scan-gated path, writes a `DREAMS` review record, and that an expired episodic row is pruned
