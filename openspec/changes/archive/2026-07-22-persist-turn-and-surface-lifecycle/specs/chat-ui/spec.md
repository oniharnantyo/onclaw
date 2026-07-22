## ADDED Requirements

### Requirement: SSE stream distinguishes terminal states

The chat SSE stream SHALL emit distinct terminal events so the client can render the outcome: `interrupted` for an agent interrupt, `cancelled` for context cancellation, and `error` for other errors. Successful completion SHALL continue to terminate the stream with the existing completion event.

#### Scenario: Cancelled turn is signalled distinctly

- **WHEN** the turn is cancelled (context cancellation)
- **THEN** the stream emits `event: cancelled` rather than `event: error`

### Requirement: Web UI renders terminal states distinctly

The Web UI SHALL handle `interrupted` and `cancelled` SSE events and mark the in-progress assistant turn accordingly (e.g. a stopped/interrupted indicator), distinct from an error state, reusing the existing stopped-turn affordance.

#### Scenario: Interrupted turn is marked, not errored

- **WHEN** the stream delivers `event: interrupted`
- **THEN** the UI stops streaming and marks the partial assistant turn as interrupted without showing an error
