## ADDED Requirements

### Requirement: Computer-use trajectory card

A run of consecutive computer-use tool calls within a turn SHALL render as one trajectory card instead of separate tool cards: the latest observation screenshot as the backdrop, an animated cursor marking each completed coordinate action with a fading trail, and a step footer listing every call in order — including non-coordinate calls (keyboard, element-value, clipboard) as footer steps without cursor markers. The desktop frame SHALL render only when the run carries an observation image — a run with no capture shows the step footer alone, never an empty frame. The card SHALL reflect live state while calls stream (the running step highlighted) and persist the finished trajectory on the transcript. Typed-text arguments SHALL be masked in the footer (never shown verbatim), and approval-gated calls SHALL carry their approval state.

#### Scenario: Consecutive calls group into one card

- **WHEN** a turn runs observe, click, type, and click again through computer-use tools
- **THEN** the transcript shows one trajectory card whose footer lists the four steps and whose backdrop is the latest observation screenshot

#### Scenario: A run with no observation shows no frame

- **WHEN** a turn's computer-use calls all fail before any observation succeeded
- **THEN** the transcript shows the step footer alone and no desktop frame

#### Scenario: Typed text is masked

- **WHEN** a trajectory step typed text into a field
- **THEN** the footer shows a masked placeholder for that text, not its content

### Requirement: Computer screenshot bound read

The webview SHALL obtain a computer-use screenshot's bytes only through a bound read addressed by the file name the observation wrote plus the session the card belongs to. The read MUST accept only names matching the same strict shape the browser screenshot read enforces, and MUST resolve them only inside the calling session's computer screenshots directory — never an arbitrary filesystem path.

#### Scenario: Bound read serves the trajectory backdrop

- **WHEN** a trajectory card renders
- **THEN** the backdrop image loads through the bound read for that session's `sessions/<session id>/computer/` directory

#### Scenario: Path escape is refused

- **WHEN** a read is attempted with a name carrying a path separator or one outside the strict shape
- **THEN** the read fails and no file bytes leave the session jail
