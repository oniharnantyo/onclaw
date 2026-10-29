# Spec Delta

## MODIFIED Requirements

### Requirement: Right panel shell

The chat view SHALL provide a single right bar that presents one preview at a time as tabs. When no tab is open the bar SHALL show the session's identity view (the agent avatar, name, and effective model); opening a preview SHALL replace that identity in place — the identity and the preview MUST NOT render as separate columns — and closing the last open tab SHALL return the bar to its identity view. While one or more tabs are open, the tab strip SHALL lead with a pinned identity tab — labelled by the agent's avatar, with the agent's name as its accessible label — that MUST NOT carry a close affordance and MUST NOT be removed or reordered; activating it SHALL show the identity view in the same bar while every open tab stays open in the strip, and opening or re-activating a preview from there SHALL activate that preview. Closing a tab that is not the active one while the identity view is shown SHALL keep the identity view showing. The bar docks as a resizable column at wide widths and presents previews as a full-height overlay sheet at narrow widths, remembers its width across sessions, and opens or closes without interrupting an in-flight turn. Each preview tab SHALL carry its own close affordance, and switching chat sessions SHALL reset the panel to the identity view.

#### Scenario: Open panel during a turn

- **WHEN** the user opens a preview while the agent is responding
- **THEN** the preview replaces the identity view in the same bar beside the transcript and the turn continues uninterrupted

#### Scenario: Preview replaces the identity view

- **WHEN** the user opens a file preview while the bar shows the agent identity
- **THEN** the same bar shows the preview in place of the identity, and no second column appears

#### Scenario: Closing the last tab restores identity

- **WHEN** the user closes the only open preview tab
- **THEN** the bar shows the session's identity view again

#### Scenario: Panel state resets on session switch

- **WHEN** the user switches to a different session
- **THEN** the panel is reset to the identity view, ready to be reopened for the new session's content

#### Scenario: Pinned identity tab returns to the identity view with tabs open

- **WHEN** the user activates the pinned identity tab while one or more preview tabs are open
- **THEN** the bar shows the session's identity view and the strip still lists every open preview tab

#### Scenario: Identity tab cannot be closed

- **WHEN** one or more preview tabs are open
- **THEN** the strip's first entry is the identity tab, it offers no close control, and no tab strip shows at all once every preview tab is closed

#### Scenario: Preview opened from the identity tab takes over

- **WHEN** the user opens a preview, or re-activates an already open one, while the identity tab is active
- **THEN** that preview becomes the active tab and replaces the identity view in place

#### Scenario: Closing a background tab keeps the identity showing

- **WHEN** the user closes a preview tab that is not the active one while the identity tab is active
- **THEN** the identity view stays shown and the remaining preview tabs stay listed

#### Scenario: Identity reachable in the narrow overlay

- **WHEN** the view is narrower than the docked breakpoint and the user activates the pinned identity tab in the overlay sheet
- **THEN** the sheet shows the session's identity view

### Requirement: Identity view live avatar

Whenever the identity view is shown — with no tab open, or with the pinned identity tab active while tabs are open — the identity view's agent avatar SHALL render as a live face whose state is a pure derivation from the active session's transcript entries, resolved in precedence order: no turn in flight → idle; a message sent with no event yet → listening; a turn running with no output yet, or with reasoning streaming → thinking; text streaming → speaking; a tool running at the turn's tail → tool call; an unresolved approval → listening with a question badge; an inline error → error, held until the next user message. Terminal turn outcomes SHALL produce one-shot reactions on the face — a completed turn a nod with a sparkle, a cancelled turn a shake with a sweat drop, a failed turn a shake while the error face holds — and these reactions MUST fire only from the live event path, never from a replayed or reloaded transcript, and never from a background session's events while another session is viewed. The face SHALL idle with ambient life (blink, saccades, sway), its gaze MUST follow the pointer across the window while the identity view is shown, and it MUST doze off after idle inactivity, waking when a turn starts or the pointer returns. Under `prefers-reduced-motion: reduce` the identity avatar MUST render as a still picture — no live props and no injected keyframes — consistent with the ui-conventions "Reduced-motion compliance" requirement. After a transcript reload the face MUST rest on the same state the live view ended in — a failed last turn on the error face, a completed one on idle — and nothing MUST move on reload. At most one live avatar instance SHALL exist at a time. Every other avatar surface — agent cards, the agent list, the configuration pane, scheduler rows, the picker preview, and the pinned identity tab's label avatar — MUST render static exactly as before, with no timers and no injected styles.

#### Scenario: Message sent before the first event

- **WHEN** the user sends a message and the agent has not yet emitted an event
- **THEN** the identity avatar shows the listening state

#### Scenario: Reasoning streams as thinking

- **WHEN** the in-flight turn is streaming reasoning with no assistant text output yet
- **THEN** the identity avatar shows the thinking state

#### Scenario: Tool runs at the turn's tail

- **WHEN** a tool is running as the last activity of the in-flight turn
- **THEN** the identity avatar shows the tool-call state

#### Scenario: Text streams as speaking

- **WHEN** the agent's reply is streaming text
- **THEN** the identity avatar shows the speaking state

#### Scenario: Unresolved approval shows a question badge

- **WHEN** the turn stops on an approval the user has not resolved
- **THEN** the identity avatar shows the listening state with a question badge

#### Scenario: Completed turn nods

- **WHEN** a turn completes on the live event path
- **THEN** the avatar nods once with a sparkle as a one-shot reaction

#### Scenario: Cancelled turn shakes

- **WHEN** the user cancels the in-flight turn
- **THEN** the avatar shakes once with a sweat drop as a one-shot reaction

#### Scenario: Failed turn holds the error face

- **WHEN** a turn fails with an inline error
- **THEN** the avatar shakes once, rests on the error face, and keeps it until the user sends the next message

#### Scenario: Reload of a failed turn rests on the error face

- **WHEN** a session whose last turn failed is reloaded
- **THEN** the avatar rests on the error face derived from the replayed entries and nothing animates on reload

#### Scenario: Reload of a completed turn rests idle

- **WHEN** a session whose last turn completed is reloaded
- **THEN** the avatar rests on the idle state and no completion reaction plays

#### Scenario: Background session's outcome does not move the viewed face

- **WHEN** a session other than the viewed one receives its terminal turn event
- **THEN** the viewed session's avatar shows no reaction

#### Scenario: Reduced motion renders a still picture

- **WHEN** the system requests reduced motion
- **THEN** the identity avatar renders as a still picture with no live props and no injected keyframes

#### Scenario: Reaction dropped while the identity view is replaced

- **WHEN** a turn reaches its terminal outcome while a preview tab has replaced the identity view
- **THEN** the reaction is dropped, not deferred, and reopening the identity view after the turn — by closing the last tab or by activating the pinned identity tab — plays no gesture

#### Scenario: Identity tab shows the live face

- **WHEN** the user activates the pinned identity tab while a turn is in flight
- **THEN** the identity view's avatar shows the live state derived for that turn while the strip's label avatar stays still

#### Scenario: Non-identity surfaces stay still

- **WHEN** the user views agent cards, the agent list, the configuration pane, scheduler rows, the picker preview, or the pinned identity tab's label avatar
- **THEN** each avatar renders static exactly as before, with no timers and no injected styles
