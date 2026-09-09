# web-app/chat delta

## ADDED Requirements

### Requirement: Work session affordances
In channels, the composer SHALL offer a kickoff affordance that flags the composed message as a work-session kickoff. The room SHALL surface the active work session's state — a banner showing goal and status (`open` with hops remaining, `paused: awaiting-human`, `paused: budget exhausted`, `closed`) and, once closed, the facilitator's summary. An `awaiting-human` session SHALL mark the channel as awaiting the mentioned human member; posting a message as that (or any) human member resumes the session through the feed API.

#### Scenario: Kickoff starts a session
- **WHEN** the user composes "Add dark mode to settings" with the kickoff affordance and sends
- **THEN** the message posts flagged as a kickoff and the session banner appears in `open` state

#### Scenario: Awaiting-human is visible
- **WHEN** an in-session agent reply mentions a human member
- **THEN** the banner shows `paused: awaiting-human` and the channel presents the awaiting state to that member

#### Scenario: Closed summary readable
- **WHEN** the facilitator closes a session
- **THEN** the banner shows the closed state with the facilitator's summary
