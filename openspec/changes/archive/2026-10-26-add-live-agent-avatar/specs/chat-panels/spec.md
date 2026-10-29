# Spec Delta

## ADDED Requirements

### Requirement: Identity view live avatar

When no tab is open, the identity view's agent avatar SHALL render as a live face whose state is a pure derivation from the active session's transcript entries, resolved in precedence order: no turn in flight → idle; a message sent with no event yet → listening; a turn running with no output yet, or with reasoning streaming → thinking; text streaming → speaking; a tool running at the turn's tail → tool call; an unresolved approval → listening with a question badge; an inline error → error, held until the next user message. Terminal turn outcomes SHALL produce one-shot reactions on the face — a completed turn a nod with a sparkle, a cancelled turn a shake with a sweat drop, a failed turn a shake while the error face holds — and these reactions MUST fire only from the live event path, never from a replayed or reloaded transcript, and never from a background session's events while another session is viewed. The face SHALL idle with ambient life (blink, saccades, sway), its gaze MUST follow the pointer across the window while the identity view is shown, and it MUST doze off after idle inactivity, waking when a turn starts or the pointer returns. Under `prefers-reduced-motion: reduce` the identity avatar MUST render as a still picture — no live props and no injected keyframes — consistent with the ui-conventions "Reduced-motion compliance" requirement. After a transcript reload the face MUST rest on the same state the live view ended in — a failed last turn on the error face, a completed one on idle — and nothing MUST move on reload. Every other avatar surface — agent cards, the agent list, the configuration pane, scheduler rows, and the picker preview — MUST render static exactly as before, with no timers and no injected styles.

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
- **THEN** the reaction is dropped, not deferred, and reopening the identity view after the turn plays no gesture

#### Scenario: Non-identity surfaces stay still

- **WHEN** the user views agent cards, the agent list, the configuration pane, scheduler rows, or the picker preview
- **THEN** each avatar renders static exactly as before, with no timers and no injected styles
