# ui-conventions Specification

## Purpose

Native input and motion conventions for the WebView UI: the behaviors that
make an embedded web UI read as a native app's UI — dialog keyboard
discipline, cursor and text-selection rules, and system reduced-motion
compliance — stated as observable behavior so both desktop and server-mode
web frontends can be held to them.

## Requirements

### Requirement: Dialog keyboard discipline

Every modal dialog in the app SHALL close on Escape as if its
cancel/dismiss affordance was activated, and SHALL place initial keyboard
focus on its primary input (or, for dialogs without an input, on the dialog
itself so Enter/Tab behave predictably). Non-destructive state changes
(dialog open/close) MUST NOT be triggered by any other unqualified key.

#### Scenario: Escape closes a dialog

- **WHEN** any modal dialog is open (form, confirm, provider editor) and the
  user presses Escape
- **THEN** the dialog closes without submitting, and no stray characters
  reach the page behind it

#### Scenario: Dialog opens with a focused primary input

- **WHEN** a dialog containing a primary text input opens (create agent,
  type-the-name delete confirmation, provider form)
- **THEN** that input has keyboard focus immediately and typing goes to it
  without a click

### Requirement: Native cursor conventions

Interactive controls (buttons, menu items, list rows, tabs, clickable
labels) SHALL NOT change the cursor to a pointer on hover; the cursor stays
the platform default arrow. Text inputs and textareas SHALL show the
I-beam text cursor.

#### Scenario: Hovering a control shows the default cursor

- **WHEN** the user moves the pointer over buttons, sidebar items, provider
  rows, or toggle labels
- **THEN** the cursor remains the default arrow

#### Scenario: Editing fields show the text cursor

- **WHEN** the user moves the pointer over a text input or textarea
- **THEN** the cursor is the I-beam

### Requirement: Text selection scope

Chrome (headings, labels, button text, menu items, sidebars) SHALL NOT be
text-selectable. User content — chat transcripts, message bodies, editable
fields — SHALL remain selectable.

#### Scenario: Chrome does not select

- **WHEN** the user drags across sidebar labels, menu items, or headings
- **THEN** no text selection highlight appears

#### Scenario: Content still selects

- **WHEN** the user selects text in a chat transcript and copies it
- **THEN** the selected text copies normally

### Requirement: Reduced-motion compliance

Under the system `prefers-reduced-motion: reduce` setting, all app motion
SHALL collapse to opacity-only transitions of at most 150ms — card hover
lifts, panel transitions, and component transitions alike. Focus rings
SHOULD appear instantly and MUST NOT be animated in any motion setting.

#### Scenario: Reduced motion disables hover lift

- **WHEN** the system is set to reduce motion and the user hovers an agent
  card
- **THEN** the card does not translate or animate; at most opacity changes

#### Scenario: Focus rings are instant

- **WHEN** the user tabs between controls with any motion setting
- **THEN** the focus ring appears immediately, without a transition

### Requirement: Interactive state semantics

Interactive rows and list items SHALL convey selection and expansion state
semantically, not only visually: the selected row SHALL set
`aria-current`, and an expandable row SHALL set `aria-expanded` to its open
state. Visual state alone MUST NOT be the only carrier of selection or
expansion. A standalone icon control SHALL present a pointer target of at
least 24 by 24 CSS pixels; where the glyph is smaller, the clickable target
extends beyond it.

#### Scenario: Selected row announces itself

- **WHEN** a screen reader lists the chat rail's session rows
- **THEN** the active session's row is exposed as the current one, and
  inactive rows are not

#### Scenario: Accordion header announces expansion

- **WHEN** the user expands or collapses a session group header
- **THEN** the header's `aria-expanded` flips between true and false

#### Scenario: Icon controls meet the target floor

- **WHEN** a row carries a 20px icon-only control such as delete
- **THEN** the clickable target is at least 24 by 24 CSS pixels

### Requirement: Structural transition motion

Structural chrome — the navigation rail's collapse and expand, the session
column's reveal, session-group accordion expand and collapse, and the right
bar's minimize, restore, and identity-preview swap — SHALL animate through
the design contract's motion tokens with one shared duration and easing
(the base motion duration, standard ease), so the window's panels move as
one system. Session-group expansion SHALL reveal its rows through a
grid-row transition rather than animating raw content height. Dragging a
panel's resize handle MUST NOT be transitioned: the panel tracks the
pointer 1:1 while dragging, and transitions re-arm on release. Under
reduced motion these transitions collapse per Reduced-motion compliance:
the move completes without spatial animation and the state change stays
legible.

#### Scenario: Chrome moves as one system

- **WHEN** the user collapses the navigation rail and then minimizes the
  right bar
- **THEN** both panels animate with the same duration and easing

#### Scenario: Accordion reveals through the grid

- **WHEN** a session group expands
- **THEN** its rows are revealed by a grid-row transition with no raw
  height animation, and the group's chevron rotation matches the reveal

#### Scenario: Drag stays glued to the pointer

- **WHEN** the user drags the session column's resize handle
- **THEN** the column follows the pointer with no transition lag, and
  transitions re-arm when the drag ends

#### Scenario: Reduced motion collapses the moves

- **WHEN** reduced motion is set and the user minimizes the right bar
- **THEN** the panel's state change completes without spatial animation,
  per Reduced-motion compliance

### Requirement: Status text contrast

Status and percentage text that color themselves by state — the
context-ring's percentage label and tier text among them — SHALL draw from
the theme's lifted text tiers, `accenttext`, `warntext`, and `dangertext`,
not from the raw `accent`, `warn`, and `danger` fills. Each lifted tier
SHALL meet at least 4.5:1 contrast against its surface in both the light
and dark themes, and each lifted tier SHALL be a defined color token in
both theme palettes — a tier MUST NOT resolve by inheriting the fill color
it lifts. Graphic strokes that mark the same state (a ring's arc, a
border) SHALL stay on the raw fill tokens.

#### Scenario: Dark-theme ring label receives the lifted accent tier

- **WHEN** the context ring renders its percentage label in the dark theme
- **THEN** the label draws from the lifted `accenttext` tier, not the raw
  `accent` fill, and meets at least 4.5:1 against the ring's surface

#### Scenario: The warn tier is a real lifted token

- **WHEN** status text renders in the warn state in either theme
- **THEN** `warntext` resolves to a defined lifted token in that theme's
  palette rather than inheriting the warn fill, and the text meets at
  least 4.5:1 against its surface

#### Scenario: Danger follows the same grammar

- **WHEN** status text renders in the danger state in either theme
- **THEN** it draws from the lifted `dangertext` tier, not the raw
  `danger` fill, while any accompanying stroke stays on the raw fill
