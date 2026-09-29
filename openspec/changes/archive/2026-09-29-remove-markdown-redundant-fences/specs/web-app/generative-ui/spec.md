# Spec Delta — web-app/generative-ui

## MODIFIED Requirements

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

### Requirement: Card element catalogue
Each mounted tag SHALL render its element: `chart` — headline label, value, optional delta, and a sparkline series with the newest point emphasized; `timeline` — an ordered event list distinguishing settled from reference events with timestamps and detail lines; `preview` — a URL bar with a reload control and a sandboxed frame (`allow-scripts`, never `allow-same-origin`) whose height is fixed by the card; `ticker` — a single labeled animated number; `activity` — a date-bucketed activity grid between `start` and `end` with a total; `spec` — a titled key/value sheet with optional row emphasis; `compare` — options against trait labels with the recommended option distinguished and a stated reason; `progress` — a weighted multi-stage progress bar with the current stage, stage fraction, and eta; `score` — a weighted criteria breakdown with a verdict and total; `flow` — nodes laid out on the fence's declared column/row grid joined by edges, states `done`/`active`/`pending`; `mermaid` — the diagram rendered as SVG; `diagram` — the diagram rendered inside zoom chrome (zoom in/out/reset, expand). Card elements SHALL follow the application's design tokens in both light and dark themes.

#### Scenario: Every catalogue tag mounts its element
- **WHEN** a message contains a valid fence for each catalogue tag
- **THEN** each tag renders its element and none renders a code block

#### Scenario: Preview frame is sandboxed
- **WHEN** a ` ```preview ` fence mounts
- **THEN** the frame runs sandboxed without `allow-same-origin`, shows the URL host in a bar with reload and open-in-new-tab controls, at the card's fixed height
