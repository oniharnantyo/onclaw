## ADDED Requirements

### Requirement: Completed sessions produce episodic summaries

The system SHALL, on session stop, summarize the session into an episodic memory row, reusing the
compaction summary when one exists so the session is not summarized twice. Each episodic row SHALL
carry an extractive one-line abstract produced without an LLM call and SHALL expire after a
configurable number of days.

#### Scenario: A session summary reuses the compaction summary

- **WHEN** a session that was compacted ends
- **THEN** the episodic summary is derived from the existing compaction summary without a new LLM call

#### Scenario: Episodic entries expire

- **WHEN** an episodic row is older than the configured TTL
- **THEN** a periodic pruner removes it

### Requirement: Dreaming promotes durable facts into the curated core

The system SHALL run a dreaming consolidation pass that, once a configurable threshold of
unpromoted episodic entries has accumulated for an agent, synthesizes durable facts on a
configurable review model and promotes them into the curated core through the core write path
(subject to the char cap and security scan). Promotion SHALL consolidate or supersede rather than
duplicate. The pass SHALL be debounced per agent and SHALL replay a digest, not the full transcript.

#### Scenario: Dreaming runs only after the threshold is met

- **WHEN** fewer than the threshold of unpromoted episodic entries exist for an agent
- **THEN** dreaming does not run

#### Scenario: Promoted facts enter the curated core under its cap

- **WHEN** dreaming synthesizes durable facts
- **THEN** they are written through the curated-core write path and respect the char cap and security scan

#### Scenario: A retried sweep does not double-promote

- **WHEN** the dreaming pass runs again over already-promoted entries
- **THEN** no duplicate facts are written

### Requirement: Dreaming is reviewable

The system SHALL write a human-readable review record of each dreaming sweep — promotions,
supersessions, and scores — so the user can inspect what the system learned. When write-approval is
enabled, dreaming promotions SHALL stage for review instead of writing live.

#### Scenario: A user can inspect a dreaming sweep

- **WHEN** a dreaming sweep completes
- **THEN** a review record of its promotions and supersessions is available for human inspection

#### Scenario: Approval gates a dreaming promotion

- **WHEN** write-approval is enabled and dreaming produces a promotion
- **THEN** the promotion is staged for review and is not applied until approved
