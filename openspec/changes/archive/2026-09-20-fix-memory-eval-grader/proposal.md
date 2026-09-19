# Proposal: fix-memory-eval-grader

## Why

The `fix-memory-prefetch-matching` rerun (eval-20260919-215922, recall 100%, overall 83.3%) recorded abstention at 33.3% — but both flagged answers are prose-correct abstentions. The deterministic grader in `internal/memory/eval/score.go` misfires in two ways: (1) `noRecordPhrases` misses the live model's actual withhold wordings ("Not in memory", "Nothing usable is recorded", "Nothing captures…"), so `statesNothing` goes false on correct refusals; (2) `fabricatedLargeNumberRe` (`\d{7,}`, unguarded) matches the digit runs inside the cited provenance ids themselves (e.g. the event UUID segment `83350462298e` → an 11-digit run → "answer fabricates a specific amount"). The same defects root-cause the previous run's unexplained q-abstain-offsite flip. The abstention arm is one of the four arms gating wave-3, so a grader that cannot recognize real abstentions makes the gate number a lie in both directions.

## What Changes

- Fabrication detection becomes identifier-aware: bare large numbers and invented amounts are flagged only when delimited (not embedded in a longer alphanumeric token), so the event/note ids the citation lock requires the model to cite can never register as fabricated specifics. Invented prose numbers ("50,000,000", "1234567") still flag.
- The abstention phrase inventory covers the recorded withhold wordings (and normalizes curly apostrophes), so a correct abstention is credited regardless of which reasonable phrasing the model chose.
- The two recorded mis-graded answers from eval-20260919-215922 (q-abstain-offsite, q-abstain-competitor) are pinned as pure-function regression fixtures — the grader scores them `abstained_correct=true` with no server.
- The scoreboard is rerun once live after the fix; that corrected run is recorded in the change folder and supersedes eval-20260919-215922 as the pre-wave-3 baseline.

## Capabilities

### New Capabilities

- `agent-memory-eval`: the LongMemEval-protocol scoreboard harness's grading contract — deterministic pure-function scoring (no model call) with per-question grader notes, paraphrase-tolerant abstention detection, and identifier-aware fabrication detection. The harness has driven the wave gates since integrate-agent-zero-memory D15 but has never had a capability spec; the two recorded mis-gradings make the contract worth pinning.

### Modified Capabilities

(none)

## Impact

- `internal/memory/eval/score.go` (phrase inventory, fabrication detectors, normalizeText) and `internal/memory/eval/score_test.go` (regression fixtures + detector tables).
- One live rerun of `eval-memory` on the reused `memory-eval` fixture; new scoreboard JSON in the change folder and a delta note against eval-20260919-215922.
- No production (non-eval) code, no wire, schema, or agent-runtime changes. The abstention arm of the recorded baseline is expected to move; recall/citation/scope arms should hold.
