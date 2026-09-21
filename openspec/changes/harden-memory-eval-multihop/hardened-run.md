# Hardened scoreboard run — eval-20260920-134948

- Fixture: hardened corpus, 30 sessions / 75 turns, 28 questions (10 multihop, 12 recall, 1 update, 2 temporal, 2 abstention, 1 scope).
- Workspace: `memory-eval-harden` (fresh slug — a re-run on `memory-eval` would have double-seeded the corpus; the seeder re-posts sessions under fresh run-scoped ids by design).
- Model: glm-5.3-flash via the workspace Z.AI provider (the same provider/model as the corrected thin-fixture baseline eval-20260920-042455).
- Retrieval settings mirrored from the corrected baseline's workspace: `gate_budget_ms` 8000, posture narrow. No embedding model configured — this run measures the pre-wave-3 lexical-only read path.
- Seed: 30 sessions / 75 turns posted green; ingestion drained inside the wait budget; notes API live; scope audit PASS (third-party sees 0 private rows, owner sees 1).
- Raw scoreboard: `eval-scoreboard-hardened-baseline.json` (this folder).

## Scoreboard

| category | strict arm | result |
| --- | --- | --- |
| multihop | 9/10 | **90%** |
| recall | 10/12 | 83.3% (summary prints 88.0 with the grader's arm weighting) |
| update | 1/1 | 100% |
| temporal | 2/2 | 100% |
| abstention | 1/2 | 66.7% |
| scope | 1/1 + audit | 100% |
| citation_valid | — | 100% |
| overall | — | 88.7% |

## Pre-registered verdict (design D4, written before the run)

- **multihop 90% → the "≥ 90% closes wave-3b (graph) permanently" band.** Per-question miss analysis for the record: `q-multihop-nilam-beta` answered the correct date (24 March 2026) and opened 25 evidence items, but the grader detected no memory-backed claim traced to opened evidence — a citation-discipline miss, not a retrieval miss; the associative query shape itself was answered.
- **recall 83.3% (strict) → the 75–89% judgment band for wave-3a (vectors).** The two misses are exactly the vector leg's target taxonomy: `q-recall-kickoff` was a retrieval miss that produced a false "nothing recorded" (lexical search found nothing for the phrasing), and `q-recall-sla` was a wrong-but-confident answer adjacent to a seeded decoy.
- **abstention 66.7%** — `q-abstain-competitor` fabricated a specific amount on the adversarial near-miss probe. Orthogonal to wave-3 storage (a prompt/gate concern), recorded for the standing fixture.

## Standing decision

The pre-registered rule's multihop verdict (close wave-3b permanently) is **overridden by the recorded D11 product decision**: wave 3 (`wave3-memory-vectors-and-graph`) proceeds as a completeness milestone regardless. This run stands as the **pre-wave-3 hardened baseline** and wave-3's acceptance yardstick: associative ≥ this baseline AND no regression in recall / update / temporal / abstention / scope (wave-3 design D11-override).
