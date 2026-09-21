# Design: harden-memory-eval-multihop

## Context

The corrected 100% scoreboard (eval-20260920-042455) cannot gate wave-3: 2 multihop questions over 18 seed turns pass trivially with lexical prefetch and search. D11 ("graph only on eval evidence") and the wave-3a vector gate both hang on this instrument, so the instrument needs widening before it renders a verdict either way. The eval machinery itself is proven — it caught three real defects and its grading contract is now spec'd (`agent-memory-eval`) — this change only grows the corpus and question set.

## Goals / Non-Goals

Goals:
- A fixture whose multihop category can legitimately fail (cross-session distance, associative shapes, decoy facts).
- A pre-registered wave-3 decision rule, written down before the hardened run exists.
- The hardened scoreboard recorded as the standing gate record.

Non-Goals:
- No new scoring categories (multihop already covers the associative shape; abstention/scope machinery untouched).
- No changes to the pipeline, store, retrieval, or runtime under test.
- No wave-3 implementation — this change only decides whether wave-3 earns a capture.

## Decisions

- **D1: Corpus scale ≈ 30 sessions / ~70 turns, multi-week simulated timeline.** Facts deliberately interlink: three recurring entities (a project, a vendor, a person) each touched in 3+ sessions, state changes across weeks (the update/temporal categories get harder for free), and decoy near-miss facts that differ only in a detail (day, amount, port) so unanchored recall fails. Rationale: enough distance that prefetch's ≤5-candidate window cannot accidentally carry both halves of a multihop pair, without making runs prohibitively expensive. Alternative considered: 100+ turns (rejected — 4× cost for marginal evidentiary gain at this stage).
- **D2: 10 multihop questions, at least 4 associative-shaped.** Associative shape = entity → events across ≥3 sessions → one event's detail (the graph's target query). The other 6 are classic A→B cross-session chains plus two with a superseding update in the chain (composing multihop with the update category). Rationale: the entity-event graph justifies itself only if THIS shape cracks; the composed variants probe whether chains survive belief updates. Alternatives considered: a new `associative` category (rejected — same machinery, would fragment the history of scores).
- **D3: Recall grows to ~12 questions spread across the corpus** (paraphrase + temporal variants on the new facts), so the vector leg's gate metric has the same evidentiary footing. Same category, more rows.
- **D4: The decision rule is pre-registered here, before the run:**
  - multihop ≥ 90% on the hardened fixture → **wave-3b (graph) closes permanently**; the verdict is recorded in the specs and the graph is not built unless a future recall regression reopens it;
  - multihop < 75% → **capture the graph change** (bipartite entity↔event store, gate-time entity resolution, scope-inheriting edges, traversal in the searcher);
  - 75–89% → judgment call, documented in the change folder with per-question miss analysis (extraction gap vs retrieval miss vs grader artifact) before any capture.
  - The same run's recall numbers arbitrate wave-3a (vectors) under the identical rule.
  Rationale: the whole point of wave-0-first sequencing was deciding on data; writing the thresholds down before seeing them is what keeps the decision honest.
- **D5: Deterministic scoring must survive the bigger fixture.** All new facts use closed-vocabulary values (dates, ports, named entities, amounts) so the grader's fabrication detection and fixture answer-matching stay exact; no free-prose answers introduced.

## Risks / Trade-offs

- [Run cost grows ~4×] → bounded: tens of minutes, not hours; the fixture agent stays on glm-5.3-flash; reruns are deterministic per the reuse path.
- [A bigger corpus may stress turn-end ingestion (queue, side-call volume)] → that is signal, not noise: if ingestion can't keep up at toy scale ×4, wave-3 would inherit a broken foundation; the scoreboard's citation/abstention categories will expose it.
- [Harder decoys could penalize the model, not the memory] → miss analysis (D4 middle branch) separates extraction gaps from retrieval misses before any verdict.
- [Pre-registered rule could close the graph leg permanently] → that is the rule working as designed; reopening requires new evidence (a recall regression on associative queries), which the now-standing hardened fixture makes cheap to detect.

## Migration Plan

1. Extend `fixture.go` (corpus, questions) — pure data, behind the existing fixture contract.
2. Verify grading determinism: re-score a stored run with the new fixture loaded — old recorded runs re-grade identically (the grader is fixture-independent by contract).
3. Run the hardened scoreboard once; record JSON + per-question notes in the change folder.
4. Apply D4's rule; the outcome is either a wave-3b capture, a permanent close recorded here, or a documented judgment call.
5. Rollback: revert the fixture file; no persisted state, no schema, no API surface.
