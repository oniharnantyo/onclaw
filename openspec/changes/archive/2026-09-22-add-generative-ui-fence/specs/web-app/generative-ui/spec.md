# web-app/generative-ui — Delta

## ADDED Requirements

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

## MODIFIED Requirements

### Requirement: Fence body validation
Each card tag SHALL define the body shape it accepts, and the renderer SHALL validate before mounting: `chart` requires `label`, `value`, and a non-empty `points` array with optional `delta`, `variant` (`area` | `line` | `bars`), and `visible`; `timeline` requires a non-empty `events` array whose entries carry `label` with optional `at`, `state` (`settled` | `reference`), and `detail`; `preview` requires an absolute `http(s)` `url` and loading text `html` with optional `title`; `table` requires `columns` and `rows`; `ticker` requires `label` and `value`; `activity` requires `title`, `start`, `end`, and a `data` array; `spec` requires `title` and a `rows` array; `compare` requires `traitLabels`, `options`, a `recommendedId` matching one option's `id`, and `reason`; `progress` requires `title`, `stages`, `stageIndex`, `stageProgress`, and `eta`; `score` requires `verdict`, `total`, `outOf`, and `criteria`; `flow` requires `nodes` (each with `id`, `label`, `column`, `row`, `state`) and `edges`; `math` requires a `steps` array; `mermaid` and `diagram` take raw diagram source as the body text (not JSON); `ui` requires a composition tree whose nodes each name a vocabulary component and satisfy that component's property schema, validated per the Composition tree fence requirement. A body violating its shape SHALL NOT mount.

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
