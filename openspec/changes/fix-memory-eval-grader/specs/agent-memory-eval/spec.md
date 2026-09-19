# agent-memory-eval Delta

## ADDED Requirements

### Requirement: Deterministic grading

The memory scoreboard SHALL score every (question, answer, evidence) triple with pure functions — no model call and no server dependency — and each per-question row SHALL carry the grader's notes so a mis-grading is diagnosable from the scoreboard JSON alone. Recorded runs SHALL be regradable: re-scoring a run's stored answers with a fixed grader SHALL reproduce the same per-question verdicts.

#### Scenario: A recorded answer re-grades identically

- **WHEN** the grader is run against an answer stored in a previous run's per-question record, with no server running
- **THEN** the verdicts and notes are reproducible deterministically

### Requirement: Paraphrase-tolerant abstention detection

An answer that withholds — communicates that the requested fact is not recorded — SHALL be credited as a correct abstention when it fabricates no specifics, regardless of which reasonable withhold phrasing the model chose. Detection SHALL NOT require one exact enumerated phrase. An answer that invents the asked-for specific (a money amount or a bare large number in prose) SHALL NOT be credited.

#### Scenario: Withhold wordings outside the phrase inventory are credited

- **WHEN** an abstention answer withholds using phrasings like "Not in memory", "Nothing usable is recorded", or "Nothing captures an actual number"
- **THEN** the grader credits the abstention and the fabricated-specifics check alone decides the verdict

#### Scenario: An invented specific still fails

- **WHEN** an abstention answer states a money amount or bare large number as if recorded
- **THEN** the abstention is not credited and the row notes the fabrication

### Requirement: Identifier-aware fabrication detection

Provenance identifiers the citation lock requires — event ids, note ids, and other hex-shaped tokens the system emits — SHALL NOT register as fabricated specifics, in any surface form the model might cite them (hyphenated UUIDs, short hex prefixes, `event://` links). Invented bare large numbers and amounts in prose SHALL still be detected.

#### Scenario: Cited event ids do not read as invented numbers

- **WHEN** an answer cites a source event id whose digit segments contain long digit runs (e.g. an 11-digit run inside a UUID segment)
- **THEN** the grader does not flag the answer for fabricating a specific amount

#### Scenario: A bare invented number is still flagged

- **WHEN** an abstention or recall answer asserts a bare multi-digit number in prose, delimited by whitespace or punctuation
- **THEN** the fabrication detector flags it
