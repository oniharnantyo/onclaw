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
