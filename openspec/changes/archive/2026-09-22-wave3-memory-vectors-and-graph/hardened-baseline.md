# Pre-wave-3 hardened baseline (task 7.1)

Recorded from `harden-memory-eval-multihop`'s hardened run **eval-20260920-134948** (full analysis and raw JSON live in that change folder: `hardened-run.md`, `eval-scoreboard-hardened-baseline.json`).

| category | baseline (lexical-only, no embedding model) |
| --- | --- |
| multihop (associative) | 9/10 — 90% |
| recall | 10/12 strict — 83.3% (misses: q-recall-kickoff retrieval-miss → false abstention; q-recall-sla decoy-adjacent wrong answer) |
| update | 1/1 |
| temporal | 2/2 |
| abstention | 1/2 |
| scope | 1/1 + audit PASS |
| citation_valid | 100% |
| overall | 88.7% |

Run conditions: workspace `memory-eval-harden`, glm-5.3-flash via Z.AI, `gate_budget_ms` 8000 mirrored from the corrected thin-fixture baseline, 30 sessions / 75 turns ingested green.

Wave-3 acceptance (design D11 override): the post-wave-3 run must score **associative (multihop) ≥ 90%** with **no regression** in recall / update / temporal / abstention / scope. The vector leg's specific target is the recall misses (`q-recall-kickoff`, `q-recall-sla`) — a paraphrase-shaped query the lexical channel could not surface.
