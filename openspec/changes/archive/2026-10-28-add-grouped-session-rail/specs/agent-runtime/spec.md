# Spec Delta

## ADDED Requirements

### Requirement: Turn in-flight state observable app-wide

The runtime SHALL expose the set of sessions that currently have a turn in
flight — including turns parked on a pending approval — through a bound
call the UI can consult without subscribing to any session's event stream,
and SHALL announce every transition into and out of the in-flight state as
an application event naming the affected session. The query and the events
MUST cover turns the UI did not start or observe, including turns fired by
the scheduler or a gateway trigger into a session the UI has never opened.
The query alone MUST be sufficient to reconstruct the current busy set —
for example at startup or after a missed event — and reporting an empty set
when no turn is in flight is a normal state, not an error. The existing
per-session in-flight query keeps its meaning and is unaffected.

#### Scenario: Scheduler-fired turn lights the indicator

- **WHEN** a scheduled fire starts a turn in a session the UI has never
  opened
- **THEN** the transition event names that session, and a subsequent query
  reports the session as in flight

#### Scenario: Busy set reconstructs at startup

- **WHEN** the UI starts or reconnects while a turn is in flight anywhere
- **THEN** the bound query reports every in-flight session without any
  session subscription

#### Scenario: Terminal outcome clears the indicator

- **WHEN** an in-flight turn reaches a terminal outcome
- **THEN** the transition event names the session, and a subsequent query no
  longer reports it
