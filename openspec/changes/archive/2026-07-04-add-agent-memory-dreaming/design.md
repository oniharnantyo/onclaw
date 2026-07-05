## Context

Slice #1 established the memory substrate — curated core, searchable archive, hybrid search,
flush-before-compaction, and the `CoreStore` write path with its char cap and security scan. This
slice adds the two pieces that make memory *improve unattended*: an episodic tier that captures
each completed session, and a dreaming pass that distills many episodes into durable curated facts.

The prior art is again convergent. OpenClaw's dreaming is opt-in, cron-scheduled, thresholded
(score/recall/diversity gates), promotes into `MEMORY.md`, and writes `DREAMS.md` for review.
Hermes runs a background self-improvement review after each turn, optionally on a cheaper model
that replays a digest rather than the full transcript. GoClaw's `dreaming_worker` fires on
`episodic.created`, debounces 10 minutes, and requires ≥ 5 unpromoted entries.

Constraints unchanged: pure-Go single binary, ~2 GB RAM SBC, no per-turn background model call,
black-box tests ≥ 70%.

## Goals / Non-Goals

**Goals:**
- Capture each completed session as an episodic summary without double-summarizing.
- Unattended consolidation that promotes only durable, threshold-qualified facts into the curated core.
- Run consolidation on a cheaper model, replaying a digest — not the main model on the full transcript.
- Keep the human in the loop: a review surface and the existing `write_approval` gate.

**Non-Goals:**
- The L2 knowledge graph (slice #3, `add-agent-memory-graph`).
- A full scheduler/cron framework — dreaming is triggered in-process from the session-stop path,
  thresholded and debounced.
- Changing the curated-core write contract from slice #1 (dreaming reuses it as-is).
- Promoting raw transcripts — only synthesized durable facts are promoted.

## Decisions

**D1 — Episodic on `EventStop`, reusing the compaction summary.** When a session ends, the
episodic row is derived from the compaction summary slice #1 already produced if present; only when
no compaction happened does the path make its own (single) LLM call. The `l0_abstract` is a
one-line extractive summary computed without an LLM. *Why:* no second summarization of the same
content; the cheap extractive abstract serves fast context injection later. *Rejected:* always
summarize from scratch — wastes a model call when compaction already summarized.

**D2 — Dreaming is thresholded + debounced, on a cheaper review model.** The consolidator runs only
when ≥ N unpromoted episodic entries exist for an agent (default N = 5, matching GoClaw), debounced
per agent (10 min). It runs on `memory.review.model` (configurable; defaults to a cheaper model than
the main chat model) and replays a **digest** — recent episodes verbatim, older ones as their
`l0_abstract` — not the full transcript. *Why:* this is the Pi-cost-survival pattern from Hermes'
`auxiliary.background_review` (~3–5× cheaper, near-identical capture). *Rejected:* run dreaming on
the main model over the full transcript — the cost the footprint rules forbid.

**D3 — Promotion goes through slice #1's `CoreStore.WriteCore`.** Dreaming does not write
`MEMORY.md` directly; it calls the same curated-core write path the `memory` tool uses, so the char
cap, the security scan, the error-on-overflow, and supersession all apply. Synthesis consolidates
rather than appends. *Why:* one write path, one set of safeguards, no bypass. *Rejected:* a
separate dreaming-only write — would duplicate the cap/scan logic and could drift past the cap.

**D4 — A `DREAMS` review surface, optional `write_approval`.** Each sweep writes a human-readable
record (promotions, supersessions, scores) to `DREAMS.md` and/or a `dreams` table surfaced in the
web UI. When `write_approval` is on, dreaming promotions stage for review instead of writing live.
*Why:* the user must be able to see and veto what the system learned (Sally's trust point, done
properly this time). *Rejected:* silent background promotion — the "agent saved a wrong assumption"
failure mode Hermes built `write_approval` to prevent.

**D5 — TTL-based pruning.** A periodic goroutine deletes episodic rows past `episodic_ttl_days`
(default 90). Promoted facts already live in the curated core/archive, so expiring the episodic
source loses nothing durable. *Why:* bounded storage growth; matches OpenClaw/GoClaw.

**D6 — No new event bus.** Dreaming is triggered in-process from the `EventStop` flush path (slice
#1's hook), not from a new pub/sub. The threshold + debounce gate the actual consolidation work.
*Why:* keeps the "one middleware, no event spine" shape from slice #1. *Rejected:* a dedicated
`episodic.created` event bus — reopens the worker-topology fork slice #1 closed.

## Risks / Trade-offs

- **[Cheaper review model may capture less]** A small model might miss durable facts the main model
  would catch. *Mitigation:* Hermes reports near-identical capture on a digest; the threshold gates
  on quantity, and the review surface makes misses visible.
- **[Dreaming promotion could clobber a hand-edited core entry]** If the user wrote a fact and
  dreaming supersedes it. *Mitigation:* promotion consolidates and the review surface + `write_approval`
  make every change inspectable/vetoable.
- **[Review model unavailable offline]** No network ⇒ no consolidation. *Mitigation:* dreaming
  skips and retries next session; memory still works via slice #1's flush + search.

## Migration Plan

Additive — new `episodic_summaries` table via migration. Requires slice #1 landed. Rollback =
revert; the table is unused by slice #1.

## Open Questions

- **Review surface shape:** `DREAMS.md` file, a `dreams` table + web view, or both. Lean both
  (file for portability, table for UI) — finalize in tasks.
- **Threshold default:** N = 5 matches GoClaw; tune once real session cadence is observed.
- **`write_approval` scope:** whether the gate covers dreaming promotions, `memory`-tool writes, or
  both (lean both, single switch).
