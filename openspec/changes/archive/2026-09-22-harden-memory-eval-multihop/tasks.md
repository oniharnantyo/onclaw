# Tasks: harden-memory-eval-multihop

## 1. Corpus expansion

- [x] 1.1 Grow the scripted corpus to ~30 sessions / ~70 turns across a simulated multi-week timeline in `fixture.go`, keeping every fact on closed-vocabulary values (dates, ports, named entities, amounts)
- [x] 1.2 Introduce three recurring entities (project, vendor, person), each appearing in 3+ separate sessions with cross-session state changes
- [x] 1.3 Seed decoy near-miss facts per entity (differing only in day, amount, or port) so unanchored recall produces wrong-but-confident answers
- [x] 1.4 Verify `seed --seed-only` posts the full corpus green against the live fixture workspace (memory-eval, reuse path) and ingestion drains within the wait budget

## 2. Question set

- [x] 2.1 Write 10 multihop questions: 4 associative-shaped (entity → events across ≥3 sessions → detail), 4 classic cross-session A→B chains, 2 chains composed with a superseding update
- [x] 2.2 Grow recall to ~12 questions (paraphrase + temporal variants spread across the new corpus), keeping abstention and scope questions intact
- [x] 2.3 Confirm every question's expected answer is asserted against a fixture constant (no hand-written expectations)

## 3. Determinism guard

- [x] 3.1 Re-grade the stored eval-20260920-042455 record with the new fixture loaded and assert identical verdicts (grader must be fixture-independent)
- [x] 3.2 `go test ./internal/memory/eval/...` green with the expanded fixture

## 4. Hardened run and verdict

- [x] 4.1 Run the hardened scoreboard (`go run . eval-memory --model glm-5.3-flash` on the reused memory-eval fixture, fresh run id) and record the JSON in the change folder
- [x] 4.2 Apply the pre-registered rule from design D4: ≥90% multihop → record wave-3b permanent close; <75% → offer the graph change capture; 75–89% → per-question miss analysis documented here before any recommendation
- [x] 4.3 Record the same run's recall verdict for wave-3a (vectors) under the identical rule
- [x] 4.4 Update `docs/memory-system.md` evaluation section with the standing hardened-fixture protocol and the recorded verdict
