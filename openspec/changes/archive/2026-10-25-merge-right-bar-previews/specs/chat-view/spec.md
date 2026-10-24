# Spec Delta

## MODIFIED Requirements

### Requirement: Chat right sidebar

The chat view SHALL provide a single right bar showing the current session's identity — the agent (with its avatar) and the session's effective model: its override when set, otherwise the agent's configured model, with its effort when set — whenever the bar holds no preview. Opening a preview SHALL replace the identity view in place: the identity and the preview MUST NOT render as separate columns, and closing the last preview SHALL restore the identity. The context-window usage indicator SHALL live on the composer's rail, not in the bar. Collapsing the bar MUST NOT interrupt an in-flight turn, and the docked bar SHALL be drag-resizable by its left edge with one width persisted across restarts.

Activating the context indicator SHALL open a breakdown popover above the
composer showing: the used total against the limit, a stacked bar and legend
of the used context's composition — instructions, tools and skills, files,
conversation, and the remainder attributable to server-side context — the
headroom to the limit, and the last turn's input and output tokens. Rows
whose values are client estimates SHALL be labeled as estimates and MUST NOT
be scaled to match the reported total; the total SHALL always be the
reported used value. The popover SHALL render in both themes, its opening
animation MUST be disabled under reduced motion, and it SHALL NOT open when
no usage has been reported.

#### Scenario: Context indicator

- **WHEN** the user views a session with accumulated turns
- **THEN** the composer's rail shows context usage relative to the model's
  limit

#### Scenario: Sidebar reflects the session override

- **WHEN** the user picks a session-level model different from the agent's
  default
- **THEN** the bar shows the session's effective model, and the context
  indicator uses that model's context limit

#### Scenario: Context breakdown popover

- **WHEN** the user activates the context indicator on a session with
  accumulated turns
- **THEN** a popover opens showing the used total against the limit, the
  stacked composition bar with its legend, the headroom, and the last turn's
  input and output tokens

#### Scenario: Estimates are labeled

- **WHEN** the popover shows a composition row derived from a client estimate
- **THEN** the row carries an "estimated" caption and the rows together never
  exceed or get stretched to the reported used total

#### Scenario: No popover before the first turn

- **WHEN** the session has no reported usage yet
- **THEN** no context indicator is shown and no popover can open
