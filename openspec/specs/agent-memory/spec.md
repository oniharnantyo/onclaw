# agent-memory Specification

## Purpose

Give each agent durable, cross-session memory it curates itself: plain Markdown notes in its workspace, composed into every turn within a budget, searched lexically with a tool, and consolidated by a deterministic background sweep — with no additional model call anywhere in the memory path.

## Requirements

### Requirement: File-backed memory notes

Each agent SHALL have a memory directory at `memory/` in its workspace holding a curated index `MEMORY.md`, atomic fact notes `memory/<name>.md`, daily working notes `memory/YYYY-MM-DD.md`, and an `archive/` subdirectory. A fact note SHALL carry a frontmatter block with `name` (slug, equal to the filename), `description` (one line), `type` (one of `user`, `feedback`, `project`, `reference`), and optional `concepts` (list), `relations` (list of `<relation> <name>` entries using `has_property`, `prefers`, `related_to`, `contradicts`, `causes`, `part_of`), `supersedes` (note name), and wiki-links `[[name]]` in the body. The memory directory SHALL be created on demand the first time a turn runs for the agent, so existing workspaces need no migration. Note content SHALL live only on disk; the database SHALL hold derived state only. A note whose frontmatter cannot be parsed SHALL be skipped by every reader with a logged reason and SHALL NOT fail a turn or a sweep.

#### Scenario: Memory directory created on demand

- **WHEN** a turn runs for an agent whose workspace has no `memory/` directory
- **THEN** `memory/` and `memory/archive/` exist afterwards and the turn completes normally

#### Scenario: Note content lives on disk only

- **WHEN** an agent's memory notes are read
- **THEN** the content comes from the workspace files and no note content column exists in the database

#### Scenario: Malformed note is skipped

- **WHEN** a note with unparseable frontmatter exists alongside valid notes
- **THEN** the valid notes are composed and searchable, the malformed note is skipped with a logged reason, and no turn or sweep fails because of it

### Requirement: Curated index with a managed section

`MEMORY.md` SHALL consist of a free section the agent or user may edit, followed by a managed section delimited by marker comments that the consolidation sweep regenerates: one line per active note (name and description) ordered by retention, and a promoted subsection of entries promoted from daily notes, each carrying its source reference and promotion date. The sweep SHALL preserve the free section byte-for-byte and SHALL leave the file byte-identical when its inputs have not changed. Archived and superseded notes SHALL NOT appear in the managed section.

#### Scenario: Managed section regenerated

- **WHEN** a new valid note is written and the sweep runs
- **THEN** the managed section lists the note with its description and the free section above the markers is unchanged

#### Scenario: Unchanged inputs leave the index identical

- **WHEN** the sweep runs twice with no change to notes, daily notes, or recall traces in between
- **THEN** `MEMORY.md` is byte-identical after the second run

#### Scenario: Superseded note leaves the index

- **WHEN** a note declares `supersedes: <name>` and the sweep runs
- **THEN** the superseded note is absent from the managed section and the superseding note is listed

### Requirement: Memory block in every turn

Every agent turn SHALL compose a memory block into the system context positioned after the operating contract and before the compaction summary. The block SHALL contain the curation guidance, the index with its injected copy truncated to 3,200 characters while the file on disk stays intact, and a notes section holding at most five active notes whose retention score meets the injection threshold, in retention order, limited to 6,000 characters in total. When no note meets the threshold the notes section SHALL be absent. When the memory directory is missing or empty the block SHALL still carry the guidance and state that memory is currently empty. A read failure SHALL degrade the block to guidance plus an unavailable notice and SHALL NOT fail the turn.

#### Scenario: Block is positioned after the contract

- **WHEN** a turn is composed for an agent with memory notes
- **THEN** the system context contains the memory block after the operating contract and before the compaction summary

#### Scenario: Empty memory still carries guidance

- **WHEN** a turn is composed for an agent with no notes
- **THEN** the memory block contains the curation guidance and states that memory is empty, and no notes section is present

#### Scenario: Oversized index is truncated in the prompt only

- **WHEN** `MEMORY.md` exceeds 3,200 characters
- **THEN** the composed block carries the truncated index with a truncation notice and the file on disk is unchanged

#### Scenario: Notes section is silent below threshold

- **WHEN** every active note's retention score is below the injection threshold
- **THEN** the block contains the index but no notes section

#### Scenario: Read failure does not fail the turn

- **WHEN** the memory directory cannot be read during composition
- **THEN** the block carries the guidance and an unavailable notice and the turn executes normally

### Requirement: In-turn curation without additional model calls

The memory block's guidance SHALL direct the model to record durable facts, user preferences, decisions, and corrections as fact notes using the workspace write and edit tools during its own turn, to put working observations in the day's daily note, to follow the frontmatter schema, and to supersede an outdated note by writing a new one that names it rather than editing the old one into a contradiction. The memory path SHALL NOT invoke any model call for extracting, summarizing, scoring, linking, or consolidating memory: a turn's only model calls remain the agent's own generation and the existing compaction summary.

#### Scenario: No extraction call after a turn

- **WHEN** a turn completes for an agent with memory enabled
- **THEN** the provider has received no call beyond the agent's own generation and any existing compaction call

#### Scenario: Guidance names the conventions

- **WHEN** the memory block is composed
- **THEN** it names the note types, the frontmatter fields, the daily-note purpose, and the supersede convention

### Requirement: Memory skill

The application SHALL ship a `memory` skill seeded into the global skills directory at startup when missing. Its body SHALL teach read-first recall — consult the index, run `memory_search`, read the matching note — and the write conventions above, and its description SHALL state that it applies whenever a request concerns the user's preferences, past decisions, or earlier work. The skill SHALL be discovered, gated, explicitly invocable as `$memory`, and shadowable by an agent-tier skill of the same name exactly like any global-tier skill, and the memory block's guidance SHALL direct the model to activate it for such requests.

#### Scenario: Skill is seeded on startup

- **WHEN** the app starts with no `memory` skill in the global skills directory
- **THEN** the skill is seeded and eligible to agents with skills enabled

#### Scenario: Agent tier shadows the playbook

- **WHEN** an agent's workspace skills directory holds its own `memory` skill
- **THEN** that agent's turns use the workspace version in place of the seeded one

### Requirement: Memory search tool

Agents SHALL have a `memory_search` tool taking a query and an optional result limit that performs lexical matching over fact-note frontmatter and bodies, daily notes, and the index, and returns results ranked by match strength, each with the note name, description, path, score, and a matched excerpt. The tool SHALL make no model call. An empty result SHALL be returned as a normal empty result, and a read failure SHALL be returned as a readable error result that lets the turn continue.

#### Scenario: Lexical hit ranks by match strength

- **WHEN** the tool is called with a query whose terms appear in two notes, one matching more terms than the other
- **THEN** both notes are returned with the stronger match first and each carries its excerpt

#### Scenario: No match is an empty result

- **WHEN** the tool is called with a query matching no memory file
- **THEN** it returns an empty result and the turn continues

### Requirement: Recall traces and origin gate

Every `memory_search` invocation SHALL record a recall trace carrying the session, turn, origin (`user`, `scheduler`, `subagent`, or `other`), query, hit paths with scores, and time. Only traces with origin `user` SHALL count toward promotion signals and retention reinforcement; traces of other origins SHALL be stored but ignored by consolidation. Traces SHALL survive restarts and SHALL be removed when their agent is deleted.

#### Scenario: User-origin trace counts

- **WHEN** a user-origin turn calls `memory_search` and a note is among the hits
- **THEN** the next sweep counts that recall toward the note's signals

#### Scenario: Scheduler-origin trace is ignored by consolidation

- **WHEN** a scheduled run calls `memory_search` and a note is among the hits
- **THEN** the trace is stored with origin `scheduler` and the next sweep's signals for that note are unchanged by it

### Requirement: Subagent turns receive no memory block

Subagent child turns SHALL NOT receive the memory block in their system context. A child turn MAY read memory files through the workspace tools and MAY call `memory_search`, and its traces SHALL carry origin `subagent`.

#### Scenario: Child turn carries no block

- **WHEN** a subagent child turn is composed for an agent with memory notes
- **THEN** the child's system context contains no memory block

### Requirement: Consolidation sweep

The application SHALL run a consolidation sweep per agent in-process while the gateway is Running, on a fixed interval of one hour and on demand, and SHALL NOT run it while the gateway is Paused. A sweep SHALL make no model call. It SHALL read notes, daily notes, and traces changed since its last watermark, merge near-duplicate candidates whose token-set similarity is at least 0.9 into one candidate, compute signals, apply promotion gates, update retention, archive, regenerate the managed index section, and record its outcome. A sweep SHALL be idempotent: running it again with no new input SHALL change no file and no derived row. A sweep failure SHALL be recorded with its reason and SHALL NOT affect turns or the gateway.

#### Scenario: Sweep does not run while paused

- **WHEN** the gateway is Paused and the sweep interval elapses
- **THEN** no sweep runs and the last-sweep state is unchanged

#### Scenario: Sweep makes no model call

- **WHEN** a sweep runs for an agent with notes, daily notes, and traces
- **THEN** no provider receives a call during the sweep

#### Scenario: Re-run without new input is a no-op

- **WHEN** a sweep completes and runs again with no new notes, daily notes, or traces
- **THEN** no memory file and no derived row changes

#### Scenario: Sweep failure is isolated

- **WHEN** a sweep fails partway for one agent
- **THEN** the failure is recorded with its reason, turns for that agent still run, and other agents' sweeps proceed

### Requirement: Promotion signals and gates

For each candidate — a daily-note line or a fact note — the sweep SHALL compute a score from user-origin traces as the weighted sum of relevance (mean hit score, weight 0.30), frequency (hit count, 0.24), query diversity (distinct queries, 0.15), recency (time-decayed with a 14-day half-life, 0.15), consolidation (distinct days recalled, 0.10), and concept density (concept count, 0.06), each normalized to the unit interval. A daily-note line SHALL be promoted into the index's promoted subsection only when its score is at least 0.8, it has at least three recalls, and at least three distinct queries — all three gates — and at most ten lines SHALL be promoted per sweep. A promoted line SHALL be appended verbatim with its source reference and SHALL never be promoted twice. A candidate that has not passed the gates within 30 days of first staging SHALL expire from staging.

#### Scenario: Passing all gates promotes

- **WHEN** a daily-note line has a score of 0.85 from four recalls across three distinct user queries
- **THEN** the sweep appends it to the promoted subsection with its source reference

#### Scenario: Failing one gate keeps it staged

- **WHEN** a daily-note line has a score of 0.9 from five recalls but only two distinct queries
- **THEN** it is not promoted and remains staged with its signals

#### Scenario: Promotion happens once

- **WHEN** an already-promoted line is recalled again and the sweep runs
- **THEN** the promoted subsection still carries it exactly once

#### Scenario: Stale candidate expires

- **WHEN** a staged line has not passed the gates 30 days after first staging
- **THEN** it is removed from staging and is no longer a candidate

### Requirement: Retention and archive

Each fact note SHALL carry a retention score that is reinforced by user-origin recalls, dampened when the note is near-duplicate of a stronger note, and decayed by a fixed amount per sweep when not recalled. Notes SHALL be ranked for injection by retention. A note whose retention falls below the archive floor SHALL be moved to `memory/archive/` — never deleted — and excluded from the index and injection; moving it back restores it. A note SHALL NOT be archived earlier than 30 days after its creation. A superseded note SHALL be archived on the sweep that observes the superseding note.

#### Scenario: Unrecalled note decays into the archive

- **WHEN** a note created more than 30 days ago is never recalled across enough sweeps for its retention to fall below the floor
- **THEN** it is moved to `memory/archive/` and no longer appears in the index or the memory block

#### Scenario: Recalled note is reinforced

- **WHEN** a note is among the hits of user-origin recalls between two sweeps
- **THEN** its retention after the second sweep is higher than it would have been without the recalls

#### Scenario: Fresh note is protected

- **WHEN** a note created ten days ago has never been recalled
- **THEN** it is not archived by any sweep in that period

#### Scenario: Restoring from the archive

- **WHEN** an archived note is moved back into `memory/`
- **THEN** the next sweep lists it in the index again

### Requirement: Sweep heartbeat and visibility

Each sweep SHALL record its start time, finish time, outcome, and counts of candidates, promotions, and archivals. While the gateway is Running, if no sweep has succeeded for an agent within twice the interval, that agent's memory block SHALL state that memory maintenance is stale. A sweep SHALL NOT crash the gateway, and its failures SHALL be logged with their reason.

#### Scenario: Stale maintenance is surfaced

- **WHEN** the gateway has been Running for over two hours and the agent's sweep has not succeeded in that time
- **THEN** the agent's next memory block states that memory maintenance is stale

#### Scenario: Healthy maintenance is silent

- **WHEN** the agent's last sweep succeeded within the interval
- **THEN** the memory block carries no maintenance notice
