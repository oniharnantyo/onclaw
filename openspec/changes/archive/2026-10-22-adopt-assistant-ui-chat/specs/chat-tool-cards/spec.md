# Spec Delta

## MODIFIED Requirements

### Requirement: Interactive tool bodies

For the tools the app ships, the expanded body SHALL be shaped to the tool rather than a generic text dump:

- An edit-file call SHALL show a header naming the edited file with the counts of added and removed lines, followed by a unified line diff of the replaced text against its replacement, with line numbers, added and removed rows distinguished by color in both themes, unchanged runs collapsed to a few lines of context that can be expanded, and a marker when all occurrences were replaced.
- A write-file call SHALL show the written content as a code preview with its line count.
- A read-file call SHALL show the lines that were read as a numbered code preview.
- A shell call SHALL show the command and its output as a terminal block that states the exit status once the call ends — success, a failed exit with its code, a timeout, or truncated output — and the call's duration when known; the block SHALL offer a copy control for the output and SHALL show a cursor at the output's tail while the call is still running. A failed exit, a timeout, and truncation SHALL each be styled as a distinct state; a call that produced no output SHALL say so rather than show an empty block.
- A grep call SHALL list matches grouped by file with the pattern highlighted in each line.
- Glob and list-files calls SHALL show the returned paths as a file list.
- A web-search call SHALL show the query as a pill, a searching state while the call runs, a line stating how many sources were read once results arrive, and each result as a card with its title as a link, the site's hostname, and the snippet, with results landing one after another rather than all at once; staggered arrival MUST be disabled under reduced motion.
- A web-fetch call SHALL show the fetched address and the readable text, clamped.
- A browser navigation or action call SHALL show the page address and title with the action's outcome; a snapshot SHALL render the page's elements as a table of reference, role, and name.
- A browser screenshot call SHALL show the captured image as a thumbnail that can be enlarged.
- A plan-write call SHALL show the plan items with their pending, in-progress, and done states as a checklist snapshot.
- Other shipped tools SHALL render their result as key-value pairs when structured and as text otherwise.

Every file path shown in a body SHALL open that file's preview in the right panel under the same workspace restrictions as transcript links.

#### Scenario: Edit renders a diff

- **WHEN** an edit-file call completes
- **THEN** its body shows a header with the file name and the added and removed counts, then removed and added lines with line numbers and color, with unchanged surrounding lines collapsed to brief context that the user can expand

#### Scenario: Written file preview

- **WHEN** a write-file call completes
- **THEN** its body shows the written content as a code preview with the number of lines written

#### Scenario: Shell command block

- **WHEN** a shell call completes successfully
- **THEN** its body shows the command in a terminal block followed by the command's output, a success status, and the call's duration

#### Scenario: Shell failure shows the exit code

- **WHEN** a shell call ends with a non-zero exit
- **THEN** the terminal block states the exit code in a failure state and the output remains readable

#### Scenario: Shell timeout and truncation are distinct states

- **WHEN** a shell call was stopped for exceeding its time limit, or its output was cut for size
- **THEN** the terminal block states the timeout or the truncation as its own status, distinct from a failed exit

#### Scenario: Shell block while running

- **WHEN** a shell call is still running
- **THEN** the terminal block shows the command with a cursor at the output's tail and no exit status yet

#### Scenario: Grep matches grouped by file

- **WHEN** a grep call returns matching lines
- **THEN** the body groups the matches by file and highlights the searched pattern within each line

#### Scenario: Search results as cards

- **WHEN** a web-search call returns results
- **THEN** the body shows the query pill, a line stating how many sources were read, and each result as a card with a linked title, the hostname, and the snippet

#### Scenario: Search shows a searching state

- **WHEN** a web-search call is still running
- **THEN** the body shows the query pill with a searching state and no results yet

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

### Requirement: Heavy-turn folding

When a turn produces four or more consecutive tool and task calls, the transcript SHALL fold them behind one summary row stating the number of steps and the number of distinct files changed by completed edit and write calls. While the turn is still running, the summary SHALL name the tool currently running. Activating the summary SHALL reveal the run as compact trace rows — one per call, each with the tool's icon, its verb, and a chip naming its target — followed by a trailing row of per-file change statistics from the completed edit and write calls; while the turn is running the last trace row SHALL shimmer, disabled under reduced motion. Activating a trace row SHALL open that call's full card beneath it. A pending approval MUST NOT be folded — it splits the run so it stays actionable. A failed call inside a folded run SHALL tint the summary with failure styling and reveal the rows. Repeated plan-write calls within one turn SHALL collapse to a "plan updated" line except the last, which renders its snapshot.

#### Scenario: Four calls fold

- **WHEN** a turn completes with five consecutive tool calls, two of which edited distinct files
- **THEN** the transcript shows one summary row reading five steps and two files changed, and activating it reveals five trace rows and a change-statistics row naming the two files

#### Scenario: Trace row opens its card

- **WHEN** the user activates a trace row in a revealed run
- **THEN** that call's full card opens beneath the row with its arguments and result

#### Scenario: Running turn names its current tool

- **WHEN** a turn with four or more calls is still running
- **THEN** the summary row names the tool currently running, and when revealed the last trace row shimmers

#### Scenario: Pending approval stays visible

- **WHEN** a run of tool calls contains a call awaiting approval
- **THEN** the approval card renders unfolded and actionable, with the other calls folded around it

#### Scenario: Failure reveals the run

- **WHEN** one call in a folded run fails
- **THEN** the summary carries failure styling and the trace rows are revealed with the failed row tinted
