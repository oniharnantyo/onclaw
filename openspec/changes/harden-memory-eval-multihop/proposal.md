# Proposal: harden-memory-eval-multihop

## Why

The corrected scoreboard (eval-20260920-042455) reads 100% across the board, and design D11 gates wave-3 (entity-event graph, pgvector) on eval evidence — but the current fixture cannot legitimately produce that verdict: 2 multihop questions over an 18-turn corpus cannot distinguish "associative retrieval solved" from "fixture too easy to fail". Before wave-3 is built or permanently closed, the gate needs a fixture at corpus scale with enough multi-hop and associative-shaped questions for the numbers to mean something.

## What Changes

- The scripted corpus grows from 9 sessions / 18 turns to a multi-week simulated history (~30 sessions / ~70 turns) whose facts interlink (shared projects, recurring people, cross-session state changes), giving multi-hop queries real distance to traverse.
- Multihop coverage grows from 2 to 10 questions, including associative-shaped queries (entity → linked events → detail) that represent the entity-event graph's target scenario.
- Recall coverage grows proportionally (paraphrase and temporal variants spread across the larger corpus), keeping recall the vector leg's decision metric.
- The wave-3 decision rule is pre-registered in the change design BEFORE the hardened run: multihop ≥ 90% → wave-3b (graph) closes permanently; < 75% → the graph change is captured; in between → a documented judgment call.
- A hardened scoreboard run is recorded in the change folder and becomes the standing wave-3 gate record.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agent-memory-eval`: adds an evidentiary-sufficiency requirement — the fixture must exercise multi-hop recall at corpus scale large enough to gate the wave-3 storage investments.

## Impact

- `internal/memory/eval/fixture.go` (corpus + questions), `internal/memory/eval/seed.go` (seeding scales unchanged, corpus-driven), `internal/memory/eval/score.go` (no scoring changes — existing multihop/recall machinery reused).
- Run cost grows (roughly 4× seed turns, 10 more questions — all live model calls on the fixture agent; bounded, minutes not hours).
- No runtime, store, or API changes — this change only widens the measurement instrument.
