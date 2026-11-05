# Spec Delta

## MODIFIED Requirements

### Requirement: Group task lifecycle

A group SHALL track request-scoped tasks, each with an owner, an optional parent and root chain, an instruction, and a state among queued, running, waiting, paused, completed, failed, and cancelled. A member turn that ends without error and without a reported result SHALL complete its task. The waiting state SHALL mean the turn ended while open child tasks remain, and a waiting task SHALL be re-queued automatically, with its revision advanced, when its last open child task reaches a terminal state, so the owner's next turn integrates the children's results. The user SHALL be able to stop a task (pausing it and its descendant tasks, with the reason visible), continue a stopped task (re-queuing it and its descendant tasks with it), reassign an open task to another member (restarting that task's own stopped descendants with it), and cancel a stopped task — a task whose named task is paused or failed — which SHALL end every open task of the same root. Cancel SHALL NOT end active work: a queued, running, or waiting task is stopped, not cancelled. Reassigning or correcting a task SHALL bump its revision so a dispatch from a stale revision cannot complete it. Task state SHALL NOT render beneath transcript messages: live work (queued and running tasks) surfaces on the transcript-tail activity line, the group composer's stop control ends the active run of the open group, and each stopped root run (a paused or failed root task) SHALL render one notice at the transcript tail with the stop reason and Continue and Cancel controls.

#### Scenario: Stop pauses the run

- **WHEN** the user stops a running task that has delegated child tasks
- **THEN** the task and its descendants pause, the reason is visible, and active member turns are interrupted

#### Scenario: Continue resumes

- **WHEN** the user continues a stopped task that has stopped descendant tasks
- **THEN** the task keeps its instruction and owner, its descendants re-queue with it, and they are dispatched again with the continue message as a follow-up

#### Scenario: Reassign moves ownership

- **WHEN** the user reassigns an open task to another member after the previous owner stopped
- **THEN** the task's owner changes, its revision advances, the new owner receives the task, and the task's stopped descendants restart with it

#### Scenario: Cancel ends the root chain

- **WHEN** the user cancels a failed root task with an open child task
- **THEN** the child task is cancelled too and no further member turns start for either

#### Scenario: Cancel refuses active work

- **WHEN** the user attempts to cancel a running task
- **THEN** the cancel is refused, the task keeps running, and Stop remains the control that pauses it

#### Scenario: Turn without a result completes

- **WHEN** a member's turn ends without error and without a reported result
- **THEN** the task completes and no pending task state remains in the group

#### Scenario: Waiting parent resumes after its children

- **WHEN** a member's turn ends while child tasks it assigned are still open, and the last child later completes
- **THEN** the task waited in the interim and is re-queued on the child's completion, and its next turn can integrate the child's result

#### Scenario: Composer stop ends the active run

- **WHEN** the user activates the group composer's stop control while a task of the open group is running
- **THEN** the active task's root chain pauses with a visible reason and the owners' active member turns are interrupted

#### Scenario: State stays off messages

- **WHEN** the group transcript renders messages linked to a task in any state
- **THEN** no task state row or lifecycle control renders beneath any message, live work shows on the activity line, and a stopped root run shows exactly one notice with its reason and Continue and Cancel controls
