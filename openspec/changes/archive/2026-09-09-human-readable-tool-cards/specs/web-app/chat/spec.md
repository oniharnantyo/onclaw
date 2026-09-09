## MODIFIED Requirements

### Requirement: Transcript rendering
The transcript SHALL render user messages, agent messages, and teammate messages distinctly, with agent messages showing the agent's identity, and agent tool invocations rendered as expandable cards. A card's collapsed header SHALL show the tool's display name and icon, a human-readable one-line summary of the call (not raw argument JSON), and the latency in milliseconds once completed. A tool card SHALL show a running state while its turn is in flight, an outcome summary when completed, and an error state — the intent form of the summary styled as an error — when the invocation failed. Messages produced by a scheduled run SHALL carry a visible cron-origin marker naming the schedule.

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
- **THEN** the message displays a marker naming that schedule

## ADDED Requirements

### Requirement: Tool card expanded detail
Expanding a tool card SHALL show labeled fields for the call's arguments — only arguments actually present — with values shaped by kind: monospace chips for paths, selectors, element refs, and URLs; quoted literals for queries and patterns; clamped content blocks with a "show all" affordance for long text. A file-edit call SHALL render the replaced and replacement text as a stacked before/after diff. The expanded view SHALL render the result shaped by its kind (search results as a titled list, text output as a clamped block) and SHALL include the wall-clock time the call started. A raw toggle SHALL expose the full, unmodified argument and result JSON of any card.

#### Scenario: Labeled argument fields
- **WHEN** a memory append card is expanded
- **THEN** the view shows labeled fields for the action and path, and the appended content as a content block — with no fields for absent arguments

#### Scenario: File edit diff
- **WHEN** a file edit card is expanded
- **THEN** the replaced and replacement text render as a stacked before/after diff, clamped like other long content

#### Scenario: Raw JSON toggle
- **WHEN** the raw toggle on any expanded card is activated
- **THEN** the card shows the full unmodified argument and result JSON exactly as delivered by the events

#### Scenario: Unknown-tool fallback
- **WHEN** a tool outside the curated catalog (e.g. an MCP tool) renders a card
- **THEN** the header shows only the tool's display name with running/latency indicators and no fabricated summary sentence, and the expanded view renders parsed JSON as humanized labeled key-value rows — or the raw text when it does not parse
