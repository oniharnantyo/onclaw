# Spec Delta — agent-session-events

## MODIFIED Requirements

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
