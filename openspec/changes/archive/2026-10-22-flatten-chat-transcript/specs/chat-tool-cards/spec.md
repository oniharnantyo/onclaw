# Spec Delta

## MODIFIED Requirements

### Requirement: Tool card anatomy

A tool call SHALL render as a card with a collapsed header and an expandable body. The header SHALL show the tool's icon and display name (falling back to the raw tool name), a one-line sentence for tools the app ships — stating intent while the call runs and outcome once it ends — the call's latency when a duration is known, and a disclosure control. The expanded body SHALL present labeled arguments and result sections; output longer than ten lines SHALL be clamped behind a show-all control; and a raw toggle SHALL show the arguments and result exactly as delivered. A card MUST NOT show a sentence for a tool it does not know, MUST NOT show a latency it does not have, and SHALL render legibly in both the light and dark themes.

The card's header SHALL carry no bordered or filled surface of its own: it renders as a quiet line whose tool name recedes behind the sentence, so the payload the sentence names is the brightest thing on the line. The expanded body SHALL be revealed beneath the header under a hairline rail on the column's left edge, with the body's content inset from that rail, and the card as a whole SHALL NOT be enclosed in a rounded, bordered, or filled panel. The header SHALL remain a disclosure control that opens and closes the body, and the body SHALL NOT be mounted while the card is collapsed.

#### Scenario: Collapsed card for a known tool

- **WHEN** a completed read-file call is in the transcript
- **THEN** its collapsed header shows the file icon, "Read File", a sentence naming the file that was read, and the call's latency

#### Scenario: Expanding a card

- **WHEN** the user activates the card's disclosure control
- **THEN** the body opens with labeled arguments and result sections, clamping any section longer than ten lines behind a show-all control

#### Scenario: Raw toggle shows delivered text

- **WHEN** the user activates the raw toggle on an expanded card
- **THEN** the arguments and result render verbatim as the strings the runtime delivered, replacing the formatted body until toggled back

#### Scenario: Unknown tool carries no sentence

- **WHEN** a tool the app does not ship (such as an MCP tool) is called
- **THEN** its header shows the display name or raw name and the status word, with no generated sentence

#### Scenario: Record without a duration

- **WHEN** a tool record persisted before durations were recorded is displayed
- **THEN** the header shows no latency

#### Scenario: Card header is a quiet line

- **WHEN** a tool call is displayed in the transcript
- **THEN** its header carries no bordered or filled surface, its tool name recedes behind the sentence naming the target, and the body is absent until the disclosure control is activated

### Requirement: Tool call states

A card SHALL reflect its call's state: while running it SHALL indicate activity on its label without a spinning icon, show a live elapsed time, and present the arguments already known; when completed it SHALL show the outcome and the result; when failed it SHALL carry failure styling with the error text available. A card SHALL update in place when the same call reaches its terminal state and MUST NOT be duplicated. Motion used to indicate activity MUST be disabled when the user prefers reduced motion.

A failed call's failure styling SHALL be carried by the header's failure chip and by the danger treatment on the line, not by a bordered surface around the card.

#### Scenario: Running card completes in place

- **WHEN** a tool call that was rendering as running reaches completion
- **THEN** the same card switches to its completed outcome and result without a second card appearing

#### Scenario: Failed call

- **WHEN** a tool call ends with an error
- **THEN** the card carries failure styling, its header states the failure, and the error text is readable in the body

#### Scenario: Reduced motion

- **WHEN** the user prefers reduced motion and a tool call is running
- **THEN** the running indication renders as static text with no animation
