## Why

Slice #1 (`add-agent-memory-core`) gave onclaw cross-session memory — a curated core, a searchable
archive, and flush-before-compaction. But memory only grows when compaction fires or the agent
explicitly writes: there is **no unattended consolidation**, so the curated core never distills the
archive into sharper long-term knowledge, and the episodic context of a conversation is lost once it
ends. "Smarter over time" needs the agent to review its own recent experience in the background and
promote only what is durable — without a per-turn model tax.

Reference systems converge here too: OpenClaw's **dreaming** (thresholded promotion into
`MEMORY.md` with a `DREAMS.md` review surface), Hermes' **background self-improvement review**
(optionally on a cheaper model), and GoClaw's `dreaming_worker` (≥5 unpromoted episodes). All run
offline, all are thresholded, all promote into the curated core.

## What Changes

- **Add L1 episodic memory** (`episodic_summaries` table). On `EventStop`, summarize the
  just-ended session into an episodic row — **reusing the compaction summary when one exists** (no
  second summarization), otherwise a single LLM call. Each row carries an extractive one-line
  `l0_abstract` (no LLM), `key_topics`, and a configurable TTL (default 90 days).
- **Add the dreaming consolidator** (`internal/memory/dream.go`): debounced and thresholded (≥ N
  unpromoted episodic entries per agent), it runs on the **cheaper review model** (configurable via
  eino-ext, e.g. a small/local model), replays a digest of recent episodes, and synthesizes durable
  facts — preferences, project facts, recurring patterns, key decisions.
- **Promotion goes through slice #1's curated-core write path** (`CoreStore.WriteCore`), so it
  inherits the char cap, the security scan, and supersession semantics — never duplicating.
- **Add a `DREAMS` review surface** (a `DREAMS.md` file and/or a `dreams` table + web view): each
  sweep writes a human-readable record of promotions, supersessions, and scores so the user can
  inspect what the system learned (and, with `write_approval`, veto it).
- **Add periodic pruning** of expired episodic rows (goroutine).
- **Reuse slice #1's substrate** — stores, `CoreStore`, security scan, extractive fallback. This
  slice adds the episodic tier and the consolidator, not new infrastructure.

## Capabilities

### Modified Capabilities

- `agent-memory`: adds L1 episodic session summaries and the dreaming consolidation pass (with the
  review surface and pruning).

## Impact

- **New code:** `internal/memory/episodic.go` (summarize + store), `internal/memory/dream.go`
  (consolidator + review writer), `internal/store/sqlite/episodic.go` (`episodic_summaries`), a
  periodic pruner; config `memory.review.model`, `memory.dream.threshold`, `memory.episodic_ttl_days`.
- **Modified:** the `EventStop` flush path from slice #1 also writes the episodic row; the dreaming
  loop promotes via slice #1's `CoreStore`.
- **Depends on:** `add-agent-memory-core` (slice #1) being landed.
- **No data migration** beyond the new `episodic_summaries` table.
