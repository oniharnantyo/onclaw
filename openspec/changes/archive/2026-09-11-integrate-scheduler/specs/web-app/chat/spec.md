## MODIFIED Requirements

### Requirement: Transcript rendering
The transcript SHALL render user messages, agent messages, and teammate messages distinctly, with agent messages showing the agent's identity, and agent tool invocations rendered as expandable cards. A card's collapsed header SHALL show the tool's display name and icon, a human-readable one-line summary of the call (not raw argument JSON), and the latency in milliseconds once completed. A tool card SHALL show a running state while its turn is in flight, an outcome summary when completed, and an error state — the intent form of the summary styled as an error — when the invocation failed. Messages produced by a scheduler run SHALL carry a visible scheduler-origin marker naming the schedule.

#### Scenario: Tool call card
- **WHEN** an agent message includes a tool invocation that has completed
- **THEN** the collapsed card header shows the tool's display name, a human-readable outcome summary (e.g. "Appended to USER.md" for a memory append; the command verbatim for a shell call), and the latency (e.g. `760ms`) — not truncated raw JSON

#### Scenario: Tool card running state
- **WHEN** an agent message's tool invocation is still executing
- **THEN** the card shows the intent form of the summary (e.g. "Appending to USER.md") with a running indicator instead of a latency value

#### Scenario: Tool card error state
- **WHEN** a tool invocation completes with an error
- **THEN** the card shows the intent form of the summary in an error style with the failure latency, and the expanded view shows the error text

#### Scenario: Summary facts from results
- **WHEN** a completed call's result provides a derivable fact (e.g. a web search returning 8 results)
- **THEN** the outcome summary appends the fact (e.g. "Searched for 'onclaw agent' — 8 results")

#### Scenario: Cron-origin message
- **WHEN** a thread message was produced by schedule "morning-digest"
- **THEN** the message displays a scheduler-origin marker naming that schedule
