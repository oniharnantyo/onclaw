# agent-memory-eval Specification

## Purpose
The memory scoreboard's grading contract: deterministic, model-free scoring of recorded runs so verdicts are reproducible, abstention credit tolerates reasonable withhold phrasings without crediting fabrication, and provenance identifiers never register as invented specifics.

## Requirements

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

### Requirement: Evidentiary sufficiency for wave-3 gating

The fixture SHALL exercise multi-hop recall at corpus scale — a scripted history large enough that multi-hop questions require linking facts stored in separate sessions, with enough multi-hop questions (at least 10) that per-category scores are meaningful rather than anecdotal. The fixture SHALL include associative-shaped queries (an entity, its linked events across sessions, and a detail of one of those events) representing the scenario an entity-event store would target, so a wave-3 storage investment can only be justified or denied by evidence that actually exercises it.

#### Scenario: Multi-hop requires cross-session linkage

- **WHEN** a multi-hop question chains two facts that the corpus places in different scripted sessions
- **THEN** answering correctly requires the retrieval path to surface both facts, and the run records which evidence was opened

#### Scenario: Associative shape is represented

- **WHEN** the fixture's associative-shaped questions run against a workspace whose corpus links a recurring entity to events in at least three separate sessions
- **THEN** the questions resolve only when retrieval connects the entity to the right event before extracting the detail

#### Scenario: Category scores are meaningful

- **WHEN** a scoreboard run completes on the hardened fixture
- **THEN** the multihop and recall categories each report at least 10 scored questions, making a category-level pass or fail statistically interpretable
