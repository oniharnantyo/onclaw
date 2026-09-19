# agent-memory-pipeline Specification

## Purpose
Background ingestion of workspace conversation history into two durable memory stores — an episodic events timeline and a curated semantic notes store (HDM) — with provenance and visibility stamped at birth, non-destructive update semantics, and a nightly consolidation pass. Ingestion runs off the conversational hot path and never affects run outcomes.

## Requirements

### Requirement: Turn-end ingestion trigger
The pipeline SHALL enqueue each turn for extraction when the run finishes, for both completed and failed runs. Enqueued work SHALL be processed asynchronously off the conversational hot path; a failure anywhere in the pipeline SHALL leave the raw session events untouched and SHALL NOT affect the run's outcome or response.

#### Scenario: Completed run ingests
- **WHEN** a run finishes successfully
- **THEN** its turn is queued for extraction and processed in the background

#### Scenario: Failed run still ingests the user turn
- **WHEN** a run finishes in a failed state after the user's message was accepted
- **THEN** the turn is still queued for extraction; the gate filters unusable material as noise

#### Scenario: Pipeline failure never fails a run
- **WHEN** the extraction model is unavailable or the pipeline errors while processing a turn
- **THEN** the run that produced the turn is unaffected and the raw session events remain intact for later reprocessing

### Requirement: Curation gate
Extraction SHALL route through a single cheap-model curation call per turn that receives the turn's material plus the most similar existing notes, and SHALL emit at most one operation per candidate fact: ADD, UPDATE, SUPERSEDE, or NOOP, with dedupe decided against the existing notes before writing. Operations on facts the model marks as explicitly requested by a user ("remember this") SHALL be flagged high-importance and pin-eligible. Unusable or trivial material SHALL yield NOOP.

#### Scenario: Near-duplicate collapses into supersede
- **WHEN** a turn yields a fact that substantially duplicates an existing note with newer information
- **THEN** the gate emits SUPERSEDE (or UPDATE) against the existing note rather than adding a near-duplicate

#### Scenario: Explicit request is flagged
- **WHEN** the transcript contains an explicit user request to remember something
- **THEN** the resulting note carries high importance and is pin-eligible in the memory UI

### Requirement: Per-run windowed gister
Because sessions are long-lived threads, the episodic timeline SHALL be fed by a windowed gister that runs at run end and summarizes everything in the session since the last gist into one event row containing a concise description, outcome, participants, and agent. Gisting SHALL be incremental: no window is reprocessed.

#### Scenario: Long session produces incremental events
- **WHEN** a session spans twenty turns across one day
- **THEN** the timeline holds one gist event per processed window, each covering only the turns since the previous gist

### Requirement: Provenance at birth
Every stored event and note SHALL carry its provenance tuple at insert time: an origin (manual, dialogue, infer, doc), an event timestamp (when it was true), a learned timestamp (when it was stored), and an evidence pointer resolving to the raw session event(s) it came from. Rows without a complete tuple SHALL NOT be written. Every read path SHALL be able to surface this provenance.

#### Scenario: Every note traces to evidence
- **WHEN** a user inspects any extracted note in the memory UI
- **THEN** the note shows its origin, both timestamps, and a working link to the raw session event(s) it was extracted from

### Requirement: Visibility stamping
Memory rows SHALL carry a two-axis scope: a workspace partition (every row belongs to exactly one workspace, and no query runs without it) and a visibility tier — shared (tenant-wide), user (owner = one member), or agent (owner = one agent) — stamped at insert time. The visibility ceiling of material extracted from a session SHALL be the session's own shape: a direct chat can birth at most user-visibility facts, a scheduled run at most agent-visibility, a multi-human channel at most shared. The gate SHALL propose visibility per fact within that ceiling and SHALL default to the narrowest tier when ambiguous. Only humans SHALL widen visibility after birth (promotion, audited); the pipeline SHALL never widen it, and narrowing SHALL NOT happen silently.

#### Scenario: DM cannot birth shared facts
- **WHEN** the gate extracts a fact from a direct chat, even one phrased as "share this with everyone"
- **THEN** the stored note is at most user-visibility with the chatting member as owner

#### Scenario: Participant rule for events
- **WHEN** the gister processes a scheduled run with no human participant, a direct chat with one human, and a channel with multiple humans
- **THEN** the resulting events are stamped agent, user, and shared respectively

#### Scenario: Human promotion widens
- **WHEN** a member promotes one of their user-visibility notes to shared in the memory UI
- **THEN** the note becomes tenant-visible and the promotion is recorded in its provenance

### Requirement: Non-destructive updates and tombstones
Corrections SHALL be recorded as new rows superseding old ones — nothing is overwritten or erased, so both current-state and what-changed questions stay answerable. User-initiated deletion SHALL be a recorded tombstone that removes the row from every read path while preserving the audit trail.

#### Scenario: Correction keeps history
- **WHEN** a fact changes ("provider moved from Stripe to Midtrans")
- **THEN** a new note is stored superseding the old one, and the old note remains queryable as history

#### Scenario: User deletes a note
- **WHEN** a user deletes a note about themselves from the memory UI
- **THEN** the note disappears from all retrieval and injection, and the deletion is recorded

### Requirement: The pipeline never edits the general documents
The ingestion pipeline SHALL never write to USER.md or WORKSPACE.md. An extracted fact that conflicts with document content SHALL be surfaced as a review flag; the nightly consolidator SHALL NOT modify the documents either.

#### Scenario: Consolidator leaves documents alone
- **WHEN** the nightly consolidator merges duplicates and folds topics
- **THEN** USER.md and WORKSPACE.md contents are byte-identical before and after

### Requirement: Nightly consolidator and morning report
A per-workspace consolidation pass SHALL run nightly (and on demand): merge near-duplicate notes into canonical ones with multi-evidence links, fold notes into browsable topics, and produce a morning report listing conflict flags and extraction-failure counts. Consolidation SHALL copy out of raw material and never delete it.

#### Scenario: Triplicated fact merges
- **WHEN** three near-identical facts about one incident accumulated during the day
- **THEN** the morning holds one canonical note whose evidence links to all three sources, and the report lists the merge

#### Scenario: Report names what failed
- **WHEN** extraction failed for some turns overnight
- **THEN** the morning report counts them so silence is never mistaken for success

### Requirement: Ingested-chip event
After the gate commits operations for a turn, the pipeline SHALL append a memory-ingested event to that session's event stream carrying the count and visibility breakdown of what was stored (never the content), so chat and channel transcripts render a post-turn chip. The chip SHALL hydrate from history like any session event, link to a provenance view of the stored facts, and expose per-fact deletion. Scheduled runs SHALL NOT render chips.

#### Scenario: Chip appears after the turn
- **WHEN** the gate stores two shared facts from a channel turn
- **THEN** the channel transcript shows a chip on that turn (two shared, zero private) that opens the facts' provenance and offers deletion

#### Scenario: Private extraction is disclosed, not revealed
- **WHEN** a channel turn produces a fact stamped user-visibility
- **THEN** the chip counts it as private without displaying its content
