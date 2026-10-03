# agent-session-events Specification

## Purpose
Governs the durable session event log's sequencing and load contract: strictly increasing per-session sequence numbers regardless of log length, limit semantics where an absent limit means the whole log, deterministic ordering with tie-breakers, and the one-time repair that re-sequences logs corrupted by the legacy bounded-read collapse.

## Requirements

### Requirement: Monotonic event sequencing
The session event log SHALL assign each event a session-scoped sequence number strictly greater than every existing sequence number in that session, regardless of session length. Sequence allocation SHALL NOT depend on a count of previously loaded rows when that load is bounded. Appends within one session are serialized by turn execution, and sequence allocation MAY assume that serialization.

#### Scenario: Session grows past the legacy collapse point
- **WHEN** a session accumulates more than 100 events across successive appends
- **THEN** every persisted event holds a distinct sequence number and the sequence order equals append order

#### Scenario: Late append after long history
- **WHEN** an event is appended to a session whose log already contains 500 events
- **THEN** the new event's sequence number is greater than all 500 existing sequence numbers

### Requirement: Load completeness and limit semantics
Loading a session's events with no explicit limit SHALL return every event in the session, however long the log is. An explicit positive limit SHALL bound the page; the `after` cursor (an event id) SHALL position the page strictly after (or strictly before, when reversed) the referenced event's sequence. The `before` cursor SHALL additionally select a backward window on the HTTP read path: the events immediately older than the cursor event. With `before` set and a positive `limit`, the response SHALL contain at most that many events — all older than the cursor, ordered oldest-to-newest — and SHALL carry a has-more indicator whose absence (or false value) means the beginning of the log was reached, together with the oldest returned event id as the next backward cursor. Unknown cursor event ids SHALL fail with the existing invalid-request error contract, not an empty page. Ordering authority, tie-breakers, and the translated-event contract are unchanged.

#### Scenario: Unbounded load returns the whole log
- **WHEN** a caller loads a 1,000-event session without a limit
- **THEN** all 1,000 events are returned

#### Scenario: Explicit limit pages from a cursor
- **WHEN** a caller loads with a limit of 50 and an `after` cursor pointing at the 100th event
- **THEN** the page contains exactly the 101st through 150th events in sequence order, and reversing the request yields the mirror page

#### Scenario: Backward window with limit
- **WHEN** the events endpoint is called with `before=<event id>` and `limit=N` where N older events exist
- **THEN** the response contains exactly N events, all older than the cursor, ordered oldest-to-newest, with a has-more indicator and the oldest returned event id as the next backward cursor

#### Scenario: Backward window at log start
- **WHEN** the events endpoint is called with `before` pointing at the oldest event and a positive limit
- **THEN** an empty page is returned with no has-more indicator, signaling the beginning of the log

#### Scenario: Unknown cursor event
- **WHEN** the endpoint is called with a `before` (or `after`) event id that does not exist in the session
- **THEN** the request fails with the existing invalid-request error contract, not an empty page

#### Scenario: Forward reads unchanged by the addition
- **WHEN** a client that predates this change issues the same forward requests it always has
- **THEN** every response is byte-identical to the pre-change behavior

### Requirement: Deterministic load ordering
Loading events SHALL return them ordered by sequence number, with the event timestamp and event id as deterministic tie-breakers within equal sequence numbers, so no two loads of the same data can disagree on order. Reversed loads SHALL apply the exact mirrored ordering.

#### Scenario: Tied sequence numbers cannot scramble order
- **WHEN** a session contains rows sharing one sequence number (legacy corruption)
- **THEN** two consecutive loads return those rows in the same, timestamp-then-id order

### Requirement: Historical re-sequencing
The system SHALL provide a one-time repair that re-sequences each existing session's rows into strict chronological order (by event timestamp, then event id), so logs corrupted before this requirement landed become strictly increasing. The repair SHALL be safe to run on already-correct logs (re-sequencing is a no-op ordering-wise) and SHALL NOT alter event payloads or identities.

#### Scenario: Corrupted session is repaired
- **WHEN** the repair runs on a session whose events share duplicated sequence numbers
- **THEN** the session's rows hold distinct, strictly increasing sequence numbers matching chronological order, with payloads and event ids unchanged

#### Scenario: Correct session is untouched in meaning
- **WHEN** the repair runs on a session with strictly increasing sequence numbers in chronological order
- **THEN** the load order and content are unchanged
