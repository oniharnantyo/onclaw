# Spec Delta

## Purpose

Render each tool call in the transcript as a card that says what the tool did in plain words, shows its arguments and result on demand, and — for the tools the app ships — presents an interactive body shaped to the tool: a file diff for edits, code previews for files, a terminal block for shell commands, result cards for searches, element tables and screenshots for the browser, and a plan checklist, with an honest fallback for anything unknown.

## ADDED Requirements

### Requirement: Tool card anatomy

A tool call SHALL render as a card with a collapsed header and an expandable body. The header SHALL show the tool's icon and display name (falling back to the raw tool name), a one-line sentence for tools the app ships — stating intent while the call runs and outcome once it ends — the call's latency when a duration is known, and a disclosure control. The expanded body SHALL present labeled arguments and result sections; output longer than ten lines SHALL be clamped behind a show-all control; and a raw toggle SHALL show the arguments and result exactly as delivered. A card MUST NOT show a sentence for a tool it does not know, MUST NOT show a latency it does not have, and SHALL render legibly in both the light and dark themes.

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

### Requirement: Tool call states

A card SHALL reflect its call's state: while running it SHALL indicate activity on its label without a spinning icon, show a live elapsed time, and present the arguments already known; when completed it SHALL show the outcome and the result; when failed it SHALL carry failure styling with the error text available. A card SHALL update in place when the same call reaches its terminal state and MUST NOT be duplicated. Motion used to indicate activity MUST be disabled when the user prefers reduced motion.

#### Scenario: Running card completes in place

- **WHEN** a tool call that was rendering as running reaches completion
- **THEN** the same card switches to its completed outcome and result without a second card appearing

#### Scenario: Failed call

- **WHEN** a tool call ends with an error
- **THEN** the card carries failure styling, its header states the failure, and the error text is readable in the body

#### Scenario: Reduced motion

- **WHEN** the user prefers reduced motion and a tool call is running
- **THEN** the running indication renders as static text with no animation

### Requirement: Interactive tool bodies

For the tools the app ships, the expanded body SHALL be shaped to the tool rather than a generic text dump:

- An edit-file call SHALL show a unified line diff of the replaced text against its replacement, with line numbers, added and removed rows distinguished by color in both themes, unchanged runs collapsed to a few lines of context that can be expanded, and a marker when all occurrences were replaced.
- A write-file call SHALL show the written content as a code preview with its line count.
- A read-file call SHALL show the lines that were read as a numbered code preview.
- A shell call SHALL show the command as a terminal-styled block followed by its output, with a failed exit styled as a failure.
- A grep call SHALL list matches grouped by file with the pattern highlighted in each line.
- Glob and list-files calls SHALL show the returned paths as a file list.
- A web-search call SHALL show each result as a card with its title as a link, the site's hostname, and the snippet.
- A web-fetch call SHALL show the fetched address and the readable text, clamped.
- A browser navigation or action call SHALL show the page address and title with the action's outcome; a snapshot SHALL render the page's elements as a table of reference, role, and name.
- A browser screenshot call SHALL show the captured image as a thumbnail that can be enlarged.
- A plan-write call SHALL show the plan items with their pending, in-progress, and done states as a checklist snapshot.
- Other shipped tools SHALL render their result as key-value pairs when structured and as text otherwise.

Every file path shown in a body SHALL open that file's preview in the right panel under the same workspace restrictions as transcript links.

#### Scenario: Edit renders a diff

- **WHEN** an edit-file call completes
- **THEN** its body shows removed and added lines with line numbers and color, and unchanged surrounding lines are collapsed to brief context that the user can expand

#### Scenario: Written file preview

- **WHEN** a write-file call completes
- **THEN** its body shows the written content as a code preview with the number of lines written

#### Scenario: Shell command block

- **WHEN** a shell call completes
- **THEN** its body shows the command in a terminal-styled block followed by the command's output

#### Scenario: Grep matches grouped by file

- **WHEN** a grep call returns matching lines
- **THEN** the body groups the matches by file and highlights the searched pattern within each line

#### Scenario: Search results as cards

- **WHEN** a web-search call returns results
- **THEN** each result renders as a card with a linked title, the hostname, and the snippet

#### Scenario: Browser snapshot table

- **WHEN** a browser snapshot call completes
- **THEN** the body shows the page title and address and a table of element references, roles, and names

#### Scenario: Screenshot thumbnail enlarges

- **WHEN** the user activates a browser screenshot's thumbnail
- **THEN** the image opens enlarged over the transcript and closes with Escape or a click outside it, returning focus to the thumbnail

#### Scenario: Plan snapshot

- **WHEN** a plan-write call completes
- **THEN** the body lists the plan items with their states as a checklist

#### Scenario: Path in a body opens the preview

- **WHEN** the user activates a file path shown in a tool body
- **THEN** the right panel opens that file's preview, refusing paths outside the agent's workspace

### Requirement: Honest fallback

A tool the app does not ship, or a shipped tool whose arguments or result cannot be interpreted, SHALL render a fallback body: the display name or raw name, the arguments and result pretty-printed when they are structured and verbatim otherwise, and no generated sentence. A failure inside any tool body's rendering MUST degrade to the fallback body for that card and MUST NOT blank the card or the transcript.

#### Scenario: MCP tool falls back

- **WHEN** an MCP tool call completes
- **THEN** its card shows the tool's name with pretty-printed arguments and result and no sentence

#### Scenario: Unparseable arguments

- **WHEN** a shipped tool's arguments cannot be interpreted as structured data
- **THEN** the card shows the arguments verbatim in the fallback body instead of its shaped body

#### Scenario: Body rendering failure degrades

- **WHEN** rendering a tool's shaped body fails on a malformed payload
- **THEN** that card shows the fallback body and the rest of the transcript renders normally

### Requirement: Heavy-turn folding

When a turn produces four or more consecutive tool and task calls, the transcript SHALL fold them behind one summary row stating the number of steps and the number of distinct files changed by completed edit and write calls. While the turn is still running, the summary SHALL name the tool currently running. Activating the summary SHALL reveal the individual cards nested beneath it. A pending approval MUST NOT be folded — it splits the run so it stays actionable. A failed call inside a folded run SHALL tint the summary with failure styling and reveal the cards. Repeated plan-write calls within one turn SHALL collapse to a "plan updated" line except the last, which renders its snapshot.

#### Scenario: Four calls fold

- **WHEN** a turn completes with five consecutive tool calls, two of which edited distinct files
- **THEN** the transcript shows one summary row reading five steps and two files changed, and activating it reveals the five cards

#### Scenario: Running turn names its current tool

- **WHEN** a turn with four or more calls is still running
- **THEN** the summary row names the tool currently running

#### Scenario: Pending approval stays visible

- **WHEN** a run of tool calls contains a call awaiting approval
- **THEN** the approval card renders unfolded and actionable, with the other calls folded around it

#### Scenario: Failure reveals the run

- **WHEN** one call in a folded run fails
- **THEN** the summary carries failure styling and the cards are revealed

### Requirement: Collapse state persistence and auto-open

A card's open or closed state SHALL persist for the duration of the app session when the card re-renders, is scrolled out of and back into the rendered window, or is shown again after a variant switch. Cards SHALL start collapsed. Edit-file and write-file cards SHALL open automatically once when they complete; a user's explicit close MUST NOT be reversed by a later automatic open.

#### Scenario: Toggle survives a re-render

- **WHEN** the user opens a card and the transcript re-renders during streaming
- **THEN** the card remains open

#### Scenario: Edit auto-opens on completion

- **WHEN** an edit-file call completes while its card is in its initial collapsed state
- **THEN** the card opens to show the diff

#### Scenario: Explicit close wins

- **WHEN** the user closes an edit-file card and the same call later re-reports its terminal state
- **THEN** the card stays closed

### Requirement: Browser screenshot access

The webview SHALL obtain a browser screenshot's bytes only through a bound read addressed by the file name the browser tool wrote. The read MUST accept only names of that exact shape, MUST resolve them solely inside the browser tool's screenshots directory, and MUST refuse any name carrying directory components or not matching the shape before touching the filesystem. The on-disk path MUST NOT be exposed to the webview. A screenshot SHALL render in the card at the dimensions the tool reported so the transcript does not shift when the image arrives.

#### Scenario: Valid screenshot name served

- **WHEN** a card requests the screenshot named in its result
- **THEN** the image bytes are returned and the thumbnail renders at the reported dimensions

#### Scenario: Traversal refused

- **WHEN** a read names a path with directory components or a name outside the tool's naming shape
- **THEN** the read is refused and no file content is returned

#### Scenario: Missing screenshot

- **WHEN** the screenshot file no longer exists on disk
- **THEN** the card shows an unavailable state in place of the thumbnail and the rest of the card renders normally
