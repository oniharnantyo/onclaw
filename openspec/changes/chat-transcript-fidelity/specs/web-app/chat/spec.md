## MODIFIED Requirements

### Requirement: Transcript rendering
The transcript SHALL render user messages, agent messages, and teammate messages distinctly, with agent messages showing the agent's identity. Agent message text SHALL render as markdown — headings, lists, emphasis, links, inline code, and fenced code blocks — with `@mention` highlighting continuing to apply within rendered text. Agent tool invocations SHALL render as expandable cards exposing the tool's catalog display name (the raw tool id SHALL remain visible on the expanded card), the call's arguments, and the call's result; a completed card whose result is absent SHALL render an explicit no-output state and SHALL NOT display a fabricated result, row count, or latency. A tool card SHALL show a running state while its call is in flight and latency in milliseconds once completed. Messages produced by a scheduled run SHALL carry a visible cron-origin marker naming the schedule.

#### Scenario: Markdown rendering
- **WHEN** an agent reply contains a heading, a bullet list, and a fenced code block
- **THEN** the transcript renders them as formatted markdown, not as literal plain text

#### Scenario: Mention highlighting inside markdown
- **WHEN** an agent reply renders as markdown and contains `@Atlas`
- **THEN** the mention still renders with its highlight styling

#### Scenario: Tool call card
- **WHEN** an agent message includes a tool invocation that has completed
- **THEN** the transcript shows a card with the tool's display name, its arguments, its result, and the measured latency (e.g. `760ms`), with the raw tool id visible when expanded

#### Scenario: Tool card running state
- **WHEN** an agent message's tool invocation is still executing
- **THEN** the card shows a running state instead of a latency value

#### Scenario: Completed card with no result
- **WHEN** a tool call completes without producing result content
- **THEN** its expanded card shows an explicit no-output state — never a synthesized result line

#### Scenario: Cron-origin message
- **WHEN** a thread message was produced by schedule "morning-digest"
- **THEN** the message displays a marker naming that schedule

## ADDED Requirements

### Requirement: Reasoning display
An agent message SHALL render the reasoning its turn produced as a collapsible section positioned above the message text. While reasoning is streaming, the section SHALL be visible with its content growing, so the waiting state shows the agent's thought rather than silence; once the turn completes the section SHALL collapse. A turn that produced no reasoning SHALL render no section. A hydrated message whose completed assistant content carries persisted reasoning SHALL render the same section.

#### Scenario: Streaming reasoning is visible
- **WHEN** an agent turn is producing reasoning before its answer text
- **THEN** the message shows a growing reasoning section instead of an empty loading placeholder

#### Scenario: Completed reasoning collapses
- **WHEN** an agent turn with reasoning completes
- **THEN** the reasoning section collapses and can be re-expanded in place

#### Scenario: No reasoning renders nothing
- **WHEN** an agent turn produced no reasoning content
- **THEN** no reasoning section renders on the message

#### Scenario: Hydrated reasoning
- **WHEN** a transcript is hydrated and a completed assistant message carries persisted reasoning
- **THEN** the message renders the collapsible reasoning section from the persisted content

### Requirement: Turn error display
When an agent turn fails, the transcript SHALL render an error entry in the thread at the point of failure carrying the server-provided error message, styled distinctly from normal messages. The entry SHALL NOT depend on a toast for its existence; a toast may accompany it. Key/connection failures SHALL continue to surface the connect state with its retry path instead of an error entry.

#### Scenario: Failed turn shows an entry
- **WHEN** a live turn ends with a terminal provider error
- **THEN** an error entry carrying the error message appears in the thread after the partial content

#### Scenario: Connect state unchanged
- **WHEN** the workspace chat key is missing or a key exchange fails
- **THEN** the connect state with retry renders as today and no error entry is produced
