# agent-memory-eval Delta

## ADDED Requirements

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
