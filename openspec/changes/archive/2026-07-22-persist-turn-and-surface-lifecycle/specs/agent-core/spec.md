## ADDED Requirements

### Requirement: Event iterator exposes the terminal reason

The `EventIterator` SHALL distinguish terminal reasons via `Err()` using typed sentinels: clean completion returns a nil error; an interrupt returns a sentinel identifiable via `errors.Is`; context cancellation returns `context.Canceled`; other errors return the underlying error. The iterator SHALL NOT leave the error unset on an interrupt terminal.

#### Scenario: Caller distinguishes interrupt from clean finish

- **WHEN** an interrupt terminal occurs
- **THEN** `Next()` returns false and `errors.Is(it.Err(), ErrInterrupted)` is true

### Requirement: No agent event is dropped silently

The event iterator SHALL emit a structured log line for every framework event it does not surface to consumers (exit signals, non-summarization customized actions, empty message outputs, customized outputs, session-event variants), so dropped events are diagnosable rather than invisible. Logging SHALL NOT change iteration behavior.

#### Scenario: An empty message output is logged, not swallowed

- **WHEN** the framework yields a message output with neither a stream nor a buffered message
- **THEN** the iterator logs a warning and continues iteration without surfacing an event
