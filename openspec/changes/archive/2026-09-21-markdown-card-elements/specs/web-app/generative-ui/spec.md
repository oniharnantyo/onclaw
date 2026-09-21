## Purpose

Rich card rendering in the chat transcript: assistant messages carry tagged code fences that the transcript mounts as card elements, giving agents a prompt-guided way to render charts, timelines, tables, diagrams, and similar structured visuals without tool calls.

## ADDED Requirements

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
Each card tag SHALL define the body shape it accepts, and the renderer SHALL validate before mounting: `chart` requires `label`, `value`, and a non-empty `points` array with optional `delta`, `variant` (`area` | `line` | `bars`), and `visible`; `timeline` requires a non-empty `events` array whose entries carry `label` with optional `at`, `state` (`settled` | `reference`), and `detail`; `preview` requires an absolute `http(s)` `url` and loading text `html` with optional `title`; `table` requires `columns` and `rows`; `ticker` requires `label` and `value`; `activity` requires `title`, `start`, `end`, and a `data` array; `spec` requires `title` and a `rows` array; `compare` requires `traitLabels`, `options`, a `recommendedId` matching one option's `id`, and `reason`; `progress` requires `title`, `stages`, `stageIndex`, `stageProgress`, and `eta`; `score` requires `verdict`, `total`, `outOf`, and `criteria`; `flow` requires `nodes` (each with `id`, `label`, `column`, `row`, `state`) and `edges`; `math` requires a `steps` array; `mermaid` and `diagram` take raw diagram source as the body text (not JSON). A body violating its shape SHALL NOT mount.

#### Scenario: Preview rejects non-http URLs
- **WHEN** a ` ```preview ` fence carries a `javascript:` or relative `url`
- **THEN** the fence renders as an ordinary code block and no frame mounts

#### Scenario: Compare rejects an unknown recommendation
- **WHEN** a ` ```compare ` fence's `recommendedId` does not match any option `id`
- **THEN** the fence renders as an ordinary code block

#### Scenario: Mermaid renders raw source with fallback
- **WHEN** a ` ```mermaid ` fence carries diagram source that the diagram engine cannot parse
- **THEN** the raw source text renders instead of a broken or blank diagram

### Requirement: Card element catalogue
Each mounted tag SHALL render its element: `chart` — headline label, value, optional delta, and a sparkline series with the newest point emphasized; `timeline` — an ordered event list distinguishing settled from reference events with timestamps and detail lines; `preview` — a URL bar with a reload control and a sandboxed frame (`allow-scripts`, never `allow-same-origin`) whose height is fixed by the card; `table` — a column/row table from the fence's `columns` and `rows`; `ticker` — a single labeled animated number; `activity` — a date-bucketed activity grid between `start` and `end` with a total; `spec` — a titled key/value sheet with optional row emphasis; `compare` — options against trait labels with the recommended option distinguished and a stated reason; `progress` — a weighted multi-stage progress bar with the current stage, stage fraction, and eta; `score` — a weighted criteria breakdown with a verdict and total; `flow` — nodes laid out on the fence's declared column/row grid joined by edges, states `done`/`active`/`pending`; `math` — a titled list of LaTeX-rendered steps with optional notes; `mermaid` — the diagram rendered as SVG; `diagram` — the diagram rendered inside zoom chrome (zoom in/out/reset, expand). Card elements SHALL follow the application's design tokens in both light and dark themes.

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
