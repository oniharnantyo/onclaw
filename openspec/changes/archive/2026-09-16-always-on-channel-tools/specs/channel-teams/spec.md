## MODIFIED Requirements

### Requirement: Facilitator role
Each channel MAY designate exactly one member — user or agent — as **facilitator**. The facilitator SHALL be summoned on kickoff (to post the opening plan), on budget exhaustion (to post a status), and by the stall watchdog; its runs' hops SHALL count against the session budget like any agent's. Only the facilitator SHALL be able to close a session, via a `session.close` tool whose summary is stored on the session and posted to the feed; the session MUST NOT accept new summons after closing. `session.close` SHALL be always active for a facilitator running inside an open work session: workspace tool settings SHALL NOT disable it, and a stored disabled row SHALL NOT remove it from the facilitator's toolset.

#### Scenario: Facilitator closes with a summary
- **WHEN** the facilitator calls `session.close` with a summary inside an open session
- **THEN** the session becomes `closed`, the summary is stored and visible in the feed, and further in-session summons are suppressed

#### Scenario: Only the facilitator closes
- **WHEN** a non-facilitator agent attempts `session.close`
- **THEN** the tool is unavailable to it (not in its toolset) or refuses

#### Scenario: Workspace settings cannot strip the closer
- **WHEN** the workspace holds a disabled settings row for `session.close` and the facilitator runs inside an open work session
- **THEN** the facilitator's toolset still includes `session.close`
