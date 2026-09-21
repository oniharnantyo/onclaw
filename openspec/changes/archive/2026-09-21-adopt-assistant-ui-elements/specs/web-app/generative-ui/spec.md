# web-app/generative-ui — Delta

## Purpose

A `$type`-keyed spec-renderer registry that turns structured tool output into first-class chat UI — chart, web preview, timeline, todo checklist, and web search source cards — rendered in OnClaw's design tokens and extensible by plugins without frontend edits.

## ADDED Requirements

### Requirement: Generative UI registry
The chat transcript SHALL render structured tool output through a spec registry: a tool result (or echoed tool arguments) carrying a recognized `$type` SHALL dispatch to the registered renderer for that type, with all remaining keys passed as the renderer's props and nested specs passed under `children`. Renderers SHALL use the app's design tokens (no framework-default styling). An unrecognized `$type` SHALL render nothing user-visible rather than raw JSON or an error. Registering a new renderer SHALL not require edits to the transcript renderer. The registry entry set SHALL be covered by a guard test so a removed entry cannot ship silently.

#### Scenario: Structured envelope renders as its element
- **WHEN** a tool result carries a recognized `$type` with props
- **THEN** the transcript renders the registered element in app tokens, not raw JSON

#### Scenario: Unknown type is silent
- **WHEN** a result carries a `$type` with no registered renderer
- **THEN** the card renders its standard collapsed one-liner and no raw JSON surfaces in the transcript body

### Requirement: Streaming honesty for one-by-one reveals
Because the wire delivers tool arguments and results complete, reveal-style elements (search sources, chart points) SHALL animate their reveal client-side after arrival (a staggered reveal) and MUST NOT claim live mid-stream parsing. A reveal animation SHALL complete without user interaction, and reloading the thread SHALL render the element fully revealed without replaying a fake arrival.

#### Scenario: Sources stagger after arrival
- **WHEN** a web search tool result lands with five sources
- **THEN** the card reveals sources in a short stagger and finishes fully revealed without any further stream events

#### Scenario: Hydrated history skips the stagger
- **WHEN** a thread containing a reveal element is reloaded from history
- **THEN** the element renders fully revealed immediately

### Requirement: Chart element
A `chart` envelope SHALL render as a compact stat chart: a label, a headline value, and an optional delta (falling values tinted red, rising tinted green) above a small SVG series supporting area, line, and bars variants. The series SHALL render even while only some points are designated visible, with at least one point always drawn and the newest point emphasized.

#### Scenario: Chart envelope renders
- **WHEN** the registry receives a chart envelope with a label, value, and point series
- **THEN** the transcript shows the labeled sparkline card with the delta tinted by sign

#### Scenario: Malformed series is not fatal
- **WHEN** a chart envelope carries no usable points
- **THEN** the card renders the label and value without a series and without breaking the transcript

### Requirement: Web preview element
A `preview` envelope SHALL render inside a browser-like chrome: a URL bar showing the content's host, a reload control, and an open-in-new-tab control. The content SHALL be mounted in a sandboxed frame that does not share the app's origin (no same-origin grant). While the producing tool call is still running, the element SHALL render nothing; on failure, the card SHALL show the failure without leaving a blank frame.

#### Scenario: Preview renders sandboxed
- **WHEN** a preview envelope lands with HTML content and a source URL
- **THEN** the transcript shows the chrome with the host in the URL bar and the content in a frame that cannot reach app cookies or storage

#### Scenario: Reload remounts the frame
- **WHEN** the user activates reload
- **THEN** the frame remounts and never shows a previous address's content under a new URL bar

### Requirement: Timeline element
A `timeline` envelope SHALL render as a vertical time axis of labeled events, past events settled and future/reference events visually distinct, ordered by the envelope's declared ordering.

#### Scenario: Timeline renders ordered events
- **WHEN** a timeline envelope lands with dated events
- **THEN** the card draws one axis with events in the envelope's order and readable labels

### Requirement: Todo checklist card
The newest `todo_write` call in a turn SHALL render as a checklist card: a header with an `n/m` ratio counting done items only (failed items counted in the denominator only) plus the revision, and one row per item keyed by its stable item id — done rows struck and dimmed, the active row spinning, pending rows dimmed, failed rows in the danger color with their reason. Rows whose item id persists across rewrites SHALL restyle in place rather than remount. Earlier `todo_write` cards in the same turn SHALL collapse to a one-line "plan updated" summary so a long turn does not stack full checklists. A turn that never calls `todo_write` SHALL render nothing todo-related (present-only).

#### Scenario: Checklist renders states
- **WHEN** a turn's latest todo_write carries two done, one active, and one pending item
- **THEN** the card shows `2/4`, a struck done pair, a spinner on the active row, and a dim pending row

#### Scenario: Rewrites collapse backwards
- **WHEN** a turn contains three todo_write calls
- **THEN** the first two render as one-line plan-updated summaries and only the last renders the full checklist

#### Scenario: Failed item shows its reason
- **WHEN** an item is marked failed with a reason string
- **THEN** the row renders in the danger color with the reason beneath its text and the ratio's denominator includes it while the numerator does not

### Requirement: Web search source card
A `web.search` tool call SHALL render as a dedicated card instead of the generic result list: the query in a pill, a status line ("searching" while the call runs, "Read N sources" once complete), and one row per result with a domain-mark avatar, the result title, and the source domain in monospace. The generic tool-card rendering SHALL remain the fallback for search calls whose results do not parse.

#### Scenario: Search card replaces the generic list
- **WHEN** a web.search call completes with parsed results
- **THEN** the transcript shows the query pill and source rows, and the generic result list does not also render

#### Scenario: Searching state while running
- **WHEN** the search call is still in flight
- **THEN** the card shows the query pill with a searching status and no result rows yet
