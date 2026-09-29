# web-app/generative-ui Specification

## Purpose
A `$type`-keyed spec-renderer registry that turns structured tool output into first-class chat UI — chart, web preview, timeline, todo checklist, and web search source cards — rendered in OnClaw's design tokens and extensible by plugins without frontend edits.

## Requirements

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

### Requirement: Markdown fence mounting
The transcript renderer SHALL detect tagged code fences in assistant message text — `chart`, `timeline`, `preview`, `table`, `ticker`, `activity`, `spec`, `compare`, `progress`, `score`, `flow`, `math`, `mermaid`, `diagram` — and, when the fence body validates against that tag's shape, render the corresponding card element in place of the code block, inline at the fence's position in the message. A fence whose body fails validation SHALL render as an ordinary code block. An unrecognized tag SHALL render as an ordinary code block. While a fence is still streaming (unclosed), its partial text SHALL render as an ordinary code block until the closing fence arrives.

#### Scenario: Valid chart fence renders a card inline
- **WHEN** an assistant message contains a ` ```chart ` fence whose body is a valid chart object, between two paragraphs of prose
- **THEN** the chart card renders at the fence's position between the paragraphs, and no code block renders for it

#### Scenario: Malformed body degrades to a code block
- **WHEN** a ` ```chart ` fence body is not valid JSON, or fails the chart shape validation
- **THEN** the fence renders as an ordinary code block with its original text, and no card renders

#### Scenario: Unknown tag unchanged
- **WHEN** an assistant message contains a fence tagged with a language that is not a card tag (e.g. `go`)
- **THEN** it renders as an ordinary code block exactly as before this capability

#### Scenario: Streaming fence stays plain until closed
- **WHEN** a ` ```timeline ` fence has been opened but the closing fence has not yet arrived during a live turn
- **THEN** the partial body renders as an ordinary code block, and the timeline card mounts only once the fence closes

### Requirement: Fence body validation
Each card tag SHALL define the body shape it accepts, and the renderer SHALL validate before mounting: `chart` requires `label`, `value`, and a non-empty `points` array with optional `delta`, `variant` (`area` | `line` | `bars`), and `visible`; `timeline` requires a non-empty `events` array whose entries carry `label` with optional `at`, `state` (`settled` | `reference`), and `detail`; `preview` requires an absolute `http(s)` `url` and loading text `html` with optional `title`; `ticker` requires `label` and `value`; `activity` requires `title`, `start`, `end`, and a `data` array; `spec` requires `title` and a `rows` array; `compare` requires `traitLabels`, `options`, a `recommendedId` matching one option's `id`, and `reason`; `progress` requires `title`, `stages`, `stageIndex`, `stageProgress`, and `eta`; `score` requires `verdict`, `total`, `outOf`, and `criteria`; `flow` requires `nodes` (each with `id`, `label`, `column`, `row`, `state`) and `edges`; `mermaid` and `diagram` take raw diagram source as the body text (not JSON); `ui` requires a composition tree whose nodes each name a vocabulary component and satisfy that component's property schema, validated per the Composition tree fence requirement. A body violating its shape SHALL NOT mount.

#### Scenario: Preview rejects non-http URLs
- **WHEN** a ` ```preview ` fence carries a `javascript:` or relative `url`
- **THEN** the fence renders as an ordinary code block and no frame mounts

#### Scenario: Compare rejects an unknown recommendation
- **WHEN** a ` ```compare ` fence's `recommendedId` does not match any option `id`
- **THEN** the fence renders as an ordinary code block

#### Scenario: Mermaid renders raw source with fallback
- **WHEN** a ` ```mermaid ` fence carries diagram source that the diagram engine cannot parse
- **THEN** the raw source text renders instead of a broken or blank diagram

#### Scenario: A ui tree naming an unknown component fails closed
- **WHEN** a ` ```ui ` fence's root node names a `$type` outside the composition vocabulary
- **THEN** the fence renders as an ordinary code block and no partial composition mounts

#### Scenario: Removed tags are unknown
- **WHEN** a message carries a ` ```table ` or ` ```math ` fence
- **THEN** the tag is not in the fence universe and the block renders as an ordinary code block

### Requirement: Composition tree fence
The registry SHALL support a `ui` fence tag whose body is a JSON tree of `{$type, ...props}` nodes with nested `children`, rendered against the platform's composition vocabulary (24 components: Row, Col, Card, Divider, Spacer, Box, Header, Text, Caption, Markdown, Badge, Fact, Alert, Icon, Table, Chart, ListView, ListViewItem, Form, Input, Select, Checkbox, RadioGroup, Button).

Each node SHALL be validated against its component's declared property schema. A node whose props violate the schema SHALL be dropped while its sibling nodes render; a failing ROOT node SHALL reject the fence, which then renders as an ordinary code block. A `$type` that names no component in the vocabulary SHALL reject at the root and drop in the interior.

The vocabulary SHALL exclude `Image` (model-chosen image sources are a request-forging and exfiltration surface from the viewer's browser), `DatePicker`, and `Carousel`. A tree naming an excluded component SHALL be rejected at the root and dropped in the interior.

The validator SHALL accept a `type` key as an alias of `$type` on every node. Numeric `gap` and `padding` values SHALL be clamped to the vocabulary's 0–8 token range before validation. `Card` SHALL drop any `background` property before rendering: the property renders as an inline style that forces white card text, which is illegible on light surfaces, and the platform's card surfaces are owned by host styling rather than model styling.

#### Scenario: A composed digest renders
- **WHEN** a ` ```ui ` fence carries a Col tree containing a Row of three Cards (Caption plus Header each) and an Alert
- **THEN** the rendered output contains the component elements for col, row, card, caption, header, and alert, and no code block mounts

#### Scenario: Root failure degrades, interior failure drops
- **WHEN** the tree's root node violates its component schema
- **THEN** the fence renders as an ordinary code block
- **WHEN** an interior node violates its component schema while a sibling carries valid props
- **THEN** the invalid node renders nothing, the sibling renders, and the fence does NOT degrade to a code block

#### Scenario: `type` is accepted for `$type`
- **WHEN** a tree node carries its component name under `type` instead of `$type`
- **THEN** the node resolves to the named component and renders

#### Scenario: Token range is clamped, not rejected
- **WHEN** a node carries `gap: 12`
- **THEN** the value clamps to 8 and the node renders

#### Scenario: Card background is dropped
- **WHEN** a `Card` node carries a `background` value
- **THEN** the rendered card carries no inline background and its text renders in the theme's foreground color

#### Scenario: Excluded components are unknown
- **WHEN** a tree names `Image`, `DatePicker`, or `Carousel`
- **THEN** the node is rejected at the root, or dropped in the interior

### Requirement: Card element catalogue
Each mounted tag SHALL render its element: `chart` — headline label, value, optional delta, and a sparkline series with the newest point emphasized; `timeline` — an ordered event list distinguishing settled from reference events with timestamps and detail lines; `preview` — a URL bar with a reload control and a sandboxed frame (`allow-scripts`, never `allow-same-origin`) whose height is fixed by the card; `ticker` — a single labeled animated number; `activity` — a date-bucketed activity grid between `start` and `end` with a total; `spec` — a titled key/value sheet with optional row emphasis; `compare` — options against trait labels with the recommended option distinguished and a stated reason; `progress` — a weighted multi-stage progress bar with the current stage, stage fraction, and eta; `score` — a weighted criteria breakdown with a verdict and total; `flow` — nodes laid out on the fence's declared column/row grid joined by edges, states `done`/`active`/`pending`; `mermaid` — the diagram rendered as SVG; `diagram` — the diagram rendered inside zoom chrome (zoom in/out/reset, expand). Card elements SHALL follow the application's design tokens in both light and dark themes.

#### Scenario: Every catalogue tag mounts its element
- **WHEN** a message contains a valid fence for each catalogue tag
- **THEN** each tag renders its element and none renders a code block

#### Scenario: Preview frame is sandboxed
- **WHEN** a ` ```preview ` fence mounts
- **THEN** the frame runs sandboxed without `allow-same-origin`, shows the URL host in a bar with reload and open-in-new-tab controls, at the card's fixed height

### Requirement: Legacy envelope rendering
Tool calls whose stored result or arguments carry a recognized `$type` envelope (`chart`, `timeline`, `preview`) SHALL continue to render their card element, so transcripts produced before the echo tools' removal keep rendering. An unrecognized `$type` SHALL render nothing user-visible: the tool call keeps its generic rendering, with no raw JSON in the transcript body. A failed tool call SHALL keep the generic rendering.

#### Scenario: Hydrated echo-tool transcript still renders
- **WHEN** a transcript created while the echo tools existed is reloaded, containing a `ui.chart` tool call with a `$type: "chart"` envelope
- **THEN** the chart card renders from the envelope as before

#### Scenario: Unknown type stays silent
- **WHEN** a tool call carries a `$type` with no registered renderer
- **THEN** the tool call renders generically and no card or raw envelope JSON appears in the transcript body

### Requirement: Highlighted ordinary code blocks
An ordinary code block (non-card tag, or a card tag whose body failed validation) SHALL render syntax-highlighted once its fence is closed, with the highlight theme following the application's active light/dark theme, and SHALL offer a copy control that copies the block's text. While the fence streams, the partial text SHALL render plain without highlighting. When the highlighting engine is unavailable or the language is unknown, the block SHALL render as a plain styled code block — never blank, never an error.

#### Scenario: Highlighting applies after the fence closes
- **WHEN** a ` ```go ` fence closes during a live turn
- **THEN** the block renders with syntax highlighting for the language and a working copy control

#### Scenario: Unknown language renders plain
- **WHEN** a closed fence carries an unrecognized language tag
- **THEN** the block renders as a plain styled code block

### Requirement: Heavy engines load on demand
The diagram and highlighting engines SHALL load only when the transcript first needs them (a `mermaid`/`diagram` fence, or the first highlightable code block). Until an engine has loaded, the affected content SHALL render its degraded form — plain code block, or raw source for an unparsed diagram — and SHALL upgrade in place once the engine is ready. No engine SHALL load for a transcript that never uses it.

#### Scenario: Engines stay unloaded without use
- **WHEN** a session renders only prose and card fences that need no engine
- **THEN** neither the diagram engine nor the highlighting engine is fetched

#### Scenario: First diagram upgrades after load
- **WHEN** a ` ```mermaid ` fence mounts before the diagram engine has loaded
- **THEN** the raw source renders first and the rendered SVG replaces it once the engine is ready
