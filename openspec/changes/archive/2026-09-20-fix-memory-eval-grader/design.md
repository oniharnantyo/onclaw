# Design: fix-memory-eval-grader

## Context

Live evidence, eval-20260919-215922 (scoreboard JSON recorded in `fix-memory-prefetch-matching/`):

- q-abstain-offsite answer: "Not in memory. No approved Q3 team offsite budget figure is recorded — I searched, and the only related entries are logs of this same question being asked before… Nothing captures an actual number, so I won't guess one." Graded notes: "answer does not state that nothing is recorded" AND "answer fabricates a specific amount".
- q-abstain-competitor answer: "Nothing usable is recorded. Memory confirms the talks existed but holds no outcome… what came out of them is not in memory." Graded note: "answer does not state that nothing is recorded".
- Both are correct abstentions by any human read. Root causes in `internal/memory/eval/score.go`:
  - `noRecordPhrases` (:30) — the answer's wordings ("not in memory", "nothing usable is recorded", "nothing captures") match none of the enumerated substrings, so `statesNothing` is false. The same miss fired on the prior run's offsite flip ("unexplained" until now).
  - `fabricatedLargeNumberRe` (:69, `(?:\d{1,3}(?:,\d{3}){2,}|\d{7,})`) runs unguarded on raw answer text; the offsite answer cites event id `…83350462298e`, whose segment contains an 11-digit run → fabrication false positive. Any cited UUID segment can do this ((10/16)⁸ ≈ 2% per all-digit segment; across a run's cited ids it recurs).
- The grader is pure (`ScoreQuestion(q, ans, evidence) QuestionScore`, no I/O) — fixes are unit-testable without a server, and stored per-question answers make recorded runs regradable.

Constraints: the grader stays deterministic, offline, and string-heuristic (no model judge — that is a deliberate property of the wave-0 harness); the abstention phrase inventory stays substring-over-normalized-text (generous by design); no change to question fixtures, scoring arms, or summary math.

## Goals / Non-Goals

Goals:
- A prose-correct abstention is never mis-graded, whichever reasonable wording the model used.
- Provenance ids cited under the citation lock can never register as invented specifics.
- The two recorded mis-gradings become permanent regression fixtures.
- A corrected live rerun recorded as the pre-wave-3 baseline.

Non-Goals:
- Model-judged grading (LLM-as-judge), semantic similarity, or embeddings.
- Changing the question fixtures, the four scoring arms, `BuildScoreboard` math, or the scope/leak checks (`privateLeakTokens`, question `Forbidden` tokens are separate mechanisms and stay).
- Re-driving ingestion or touching the fixture seed; the reuse path stands.

## Decisions

- **D1: Extend the phrase inventory with the recorded withhold family, and fold curly apostrophes in `normalizeText`.** Add the observed misses and their tight family ("not in memory", "nothing usable", "nothing captures", "nothing on record", "no figure", "no amount", "no number", "won't guess" and variants). Keep the mechanism a substring inventory — the spec's contract is "not one exact phrase", which a generous inventory satisfies. Also map `’` → `'` during normalization so curly-apostrophe phrasings ("don’t") match. Rejected: requiring a semantic "withhold" classifier (model call — breaks the determinism contract).
- **D2: Delimiter-guarded fabrication matching via a small matcher function, not regex lookarounds.** Go's RE2 has no lookaheads, so replace the raw `fabricatedLargeNumberRe.MatchString` (and the same call site for `fabricatedAmountRe`) with helpers that find candidate matches and require the characters immediately before and after each match to be non-alphanumeric. Effect: a digit run inside a longer token (`83350462298e`) is not a "bare large number"; a comma-group or digit run delimited by spaces/punctuation ("50,000,000", "1234567") still is. Currency-symbol amounts are unaffected by the guard (`$`, `€` are delimiters themselves). Rejected: stripping hex-shaped tokens before matching (pure-digit prose numbers like "12345678" are valid hex and would silently escape detection); keeping the unguarded regex (the recorded bug).
- **D3: Pin the two recorded answers as pure-function regression fixtures.** Copy the q-abstain-offsite and q-abstain-competitor answer texts from eval-20260919-215922's per-question records into `score_test.go` as table cases asserting `abstained_correct=true` (offsite additionally asserts the fabrication note is gone). This is the executable form of "recorded runs are regradable".
- **D4: One corrected live rerun closes the change.** After the grader fix, rerun `eval-memory` on the reused fixture (fresh run id, `gate_budget_ms` still 8000, model glm-5.3-flash), write the scoreboard JSON into the change folder, and record the delta vs eval-20260919-215922 — the corrected run supersedes it as the pre-wave-3 baseline. Also audit the new run's per-question notes for any remaining grader artifact (expect none; anything found is recorded, not silently absorbed).

## Risks / Trade-offs

- [Phrase inventory grows toward infinite paraphrase chasing] → the inventory only needs to cover the fixtures' observed space; the deterministic contract plus stored answers means a miss is diagnosable and cheaply fixable. An LLM judge is the real cure and is explicitly out of scope for wave-0.
- [Delimiter guard could miss a fabrication glued to prose] → a bare number glued to letters ("budget5000000") is not how models fabricate; the recorded fabrication shapes are delimited. The guard is asserted by table tests on both sides (ids never flag, delimited numbers always flag).
- [Rerun variance] → the live model may phrase future abstentions differently again; the corrected baseline records the arms honestly, and D3's fixtures keep the two known shapes permanently green.

## Migration Plan

1. Land the grader fix (phrases + guards + normalization) with the D3 regression fixtures — pure eval-package change.
2. Run the D4 corrected rerun; record JSON + delta in the change folder.
3. Rollback: single-package revert; no data, schema, or wire changes.
