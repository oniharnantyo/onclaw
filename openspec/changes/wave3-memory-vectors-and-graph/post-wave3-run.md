# Post-wave-3 scoreboard run — eval-20260920-155925

- Workspace: `memory-eval-harden-w3` (fresh slug, so the full corpus ingested THROUGH the wave-3 pipeline — raw index, row embeddings, and entity resolution all populated at ingestion time; final DB ground truth: `memory_embeddings` 99 raw / 89 note / 103 event rows all at dimension 2048, `memory_entities` 67, `memory_entity_edges` 313).
- Server: rebuilt with the wave-3 read/write path (`/tmp/onclaw-eval-server`, started from this tree).
- Embedding model: `nvidia/nemotron-3-embed-1b` @ 2048 dims via the workspace Nvidia provider (NIM OpenAI-compatible embeddings; Z.AI serves chat only). Connection test through onclaw's own probe: ok, discovered dimension 2048 = configured.
- Chat/side-calls: glm-5.3-flash via Z.AI (unchanged from baseline). `gate_budget_ms` 8000 mirrored. Raw-embedding toggle on (default).
- Raw scoreboard: `eval-scoreboard-post-wave3.json` (this folder). Baseline: `harden-memory-eval-multihop/eval-scoreboard-hardened-baseline.json` (eval-20260920-134948).

## Scoreboard delta

| category | baseline (lexical-only) | post-wave-3 | delta |
| --- | --- | --- | --- |
| multihop (associative) | 9/10 strict — 90% | 9/10 strict — 90% (10/10 lenient arms) | = (same question, now citation-traced) |
| recall | 10/12 strict — 83.3% (summary 88.0) | 11/12 per-question (summary 92.0; the JSON's arm-totals are internally inconsistent — see caveat) | +1, both baseline strict misses resolved |
| update | 1/1 | 1/1 | = |
| temporal | 2/2 | 2/2 | = |
| abstention | 1/2 — 66.7% | **2/2 — 100%** | **+1** — no fabricated specifics |
| scope | 1/1 + audit PASS | 1/1 + audit PASS | = |
| citation_valid | 100% | 100% | = |
| overall | 88.7% | **98.0%** | **+9.3** |

## Acceptance verdict (design D11 override)

**PASS — associative ≥ baseline AND no regression anywhere.**

- Associative/multihop: 90% = baseline (the rule's floor is ≥). The one persistent strict-arm miss, `q-multihop-nilam-beta`, changed character between runs: baseline answered correctly but with no opened-evidence-backed claim (a citation-discipline miss); post-wave-3 the answer cites its source notes/events explicitly and passes both lenient arms. The residual strict-arm miss is answer-shape judgment (the grader's expected fact set wants more than the date), not retrieval.
- The vector leg's two target misses from the baseline are resolved exactly as the miss taxonomy predicted: `q-recall-kickoff` — a paraphrase-shaped query the lexical channel could not surface (false "nothing recorded") — now returns the fact through the fused channel with a proper note+event citation; `q-recall-sla` no longer fails the strict criterion (its residual miss is in a lenient scoring arm — see the caveat below).
- Abstention recovered to 100%: `q-abstain-competitor` now answers "nothing recorded" without fabricating specifics — with richer injected memory, the model no longer papered over the gap.
- scope (structural), citation_valid, update, temporal: unchanged at their ceilings.

**Caveat (grader follow-up):** the post-run JSON's `totals.recall` arm-totals disagree with its own per-question rows — arm 0 reads 12/12 while `q-recall-sla` carries `recall: false` and the summary's 92% (= 11/12) tracks the per-question count. The baseline JSON was internally consistent. The D11 acceptance outcome is identical under either reading (11/12 or 12/12 vs baseline 10/12), but the grader's arm-aggregation deserves a look before the next run is treated as a standing gate record.

The completeness bet paid: three parallel stores + index-before-extract measured a +9.3% overall delta on the hardened fixture with zero regressions, and the associative category the graph targets held its ceiling.
