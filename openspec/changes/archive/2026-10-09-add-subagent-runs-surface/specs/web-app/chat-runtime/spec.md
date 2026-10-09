## ADDED Requirements

### Requirement: Background task state folding
The chat runtime SHALL fold background-task lifecycle into client run state: `task_completed` events — delivered on both the live turn stream and the hydrated session events — SHALL update the session's task state (status flips to completed, failed, or canceled) instead of being dropped. The same folded state SHALL drive the floating run card's Agents section, the Subagents panel list, and the transcript card status. Folding SHALL be present-only: sessions without task events carry no task state.

#### Scenario: Completion flips state live
- **WHEN** a `task_completed` event naming a running delegation arrives during a live turn
- **THEN** the card, the floating card's Agents section, and the panel list show the task completed without a reload

#### Scenario: Hydrated completion matches live
- **WHEN** a session with persisted `task_completed` events is reloaded
- **THEN** the task state after hydration matches what the live view showed

### Requirement: Task progress subscription
The chat runtime SHALL subscribe to the session tasks progress stream while a consumer needs live trajectories — an open Subagents panel or expanded live preview — and SHALL tear the subscription down when the consumer closes. Progress frames SHALL update the addressed task's trajectory buffer; terminal frames SHALL finalize it. The runtime SHALL NOT hold the subscription while no task surface is visible, and SHALL NOT fold progress records into the transcript or the parent message list.

#### Scenario: Subscription scoped to the panel
- **WHEN** the user opens the Subagents panel and closes it while tasks still run
- **THEN** the progress stream is subscribed during the open window and released on close

#### Scenario: Progress never touches the transcript
- **WHEN** progress frames arrive for a running task
- **THEN** the transcript's message list is unchanged; only the task's trajectory buffer grows
