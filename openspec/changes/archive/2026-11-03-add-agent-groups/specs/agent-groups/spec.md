# Spec Delta

## Purpose

Let the user put several existing agents into one shared conversation — a group — where messages carry their author, a single lead coordinates untargeted work, and each member does its own work in its own thread while its replies land back in the shared transcript.

## ADDED Requirements

### Requirement: Group entity and membership

The user SHALL be able to create an agent group with a name, an optional title, optional instructions describing what the group works on, and a roster of existing agents as members. The user SHALL be able to edit the name, title, and instructions at any time, and add or remove members at any time. A group MUST accept the same agent only once. Removing a member MUST NOT delete the group, the shared transcript, or the member's past contributions.

#### Scenario: Create a group with members

- **WHEN** the user creates a group named "Research" with agents Scout and Jono
- **THEN** the group exists with both agents as members and opens as a conversation

#### Scenario: Edit group fields

- **WHEN** the user edits the group's instructions
- **THEN** subsequent member turns receive the edited instructions in their context packet

#### Scenario: Remove a member

- **WHEN** the user removes a member from a group
- **THEN** the member no longer receives work, its past messages remain in the transcript, and the other members are unchanged

### Requirement: Single lead

A group SHALL have exactly one lead at all times while it has members. The first member added to a group without a lead SHALL become the lead. The user SHALL be able to assign the lead to any member. When the current lead is removed from the group, or the lead agent is deleted from the app, the lead SHALL fall to the first remaining member. Changing the lead MUST NOT move tasks that already have an owner.

#### Scenario: First member becomes lead

- **WHEN** the user adds the first member to a new group
- **THEN** that member is the lead

#### Scenario: Reassign the lead

- **WHEN** the user assigns the lead to another member
- **THEN** that member is the only lead, untargeted messages route to it, and tasks already owned by other members keep their owners

#### Scenario: Lead removal falls back

- **WHEN** the lead is removed from a group that still has other members
- **THEN** the first remaining member becomes the lead

### Requirement: Shared authored transcript

A group SHALL own one canonical transcript ordered by a per-group sequence, in which every message carries its author: the user, a member agent, or a coordinator line written by the system (such as an assignment note). Messages SHALL have stable identifiers, and the transcript SHALL be preserved across restarts. Coordinator assignment lines SHALL be written when routing targets a specific task or member; mention-routed and lead-default messages SHALL add no coordinator line.

#### Scenario: Member reply is authored

- **WHEN** a member finishes a group turn with a text reply
- **THEN** the reply appears in the shared transcript attributed to that member, not to the user or to a generic assistant

#### Scenario: Assignment is visible

- **WHEN** routing sends a message as a follow-up to an existing task
- **THEN** a coordinator line naming the task's owner appears in the transcript ahead of the member's work

#### Scenario: Transcript survives restart

- **WHEN** the app restarts after a group exchange
- **THEN** the full transcript with its authorship is shown again

### Requirement: Deterministic routing with a lead default

A group message SHALL be routed without a model call, in order: a leading mention routes to the named member(s), where a typed `@Name` is read case-insensitively with the longest matching member name winning and `@everyone` addresses every member; a reply to a message that belongs to a task routes to that task's owner as a follow-up; a reply with no task to a member's message routes to that member; a follow-up on the group's sole open task routes to its owner; a single-member group routes to its member; every other untargeted message routes to the lead, which answers directly in its own turn. A member hidden or otherwise unable to take work MUST NOT be selected by a mention that names it; the message routes to the lead instead.

#### Scenario: Mention routes directly

- **WHEN** the user sends "@Scout summarize the corpus" in a group whose members include Scout
- **THEN** Scout receives the message as its request without any other member being asked

#### Scenario: Everyone mention fans out

- **WHEN** the user sends "@everyone give your view" in a group with three members
- **THEN** each member receives the message as its own request and answers separately

#### Scenario: Untargeted message goes to the lead

- **WHEN** the user sends a message with no mention, no task reply, and multiple members exist
- **THEN** the lead receives the message and answers in its own turn

#### Scenario: Reply continues a task

- **WHEN** the user replies to a member's message that belongs to an open task
- **THEN** the reply is dispatched to that task's owner as a follow-up on the same task

### Requirement: Member threads and context packets

Each (group, member) pair SHALL execute member turns on a session bound to that member agent, so the member's persona, tools, memory, skills, and compaction behave as in any ordinary session. A member's group turn input SHALL be a bounded packet — the group's title, instructions, the member's responsibility line, the current request with its attachments, the transcript messages the member has not yet read, results of the tasks the request depends on, a bounded window of recent messages, and a versioned summary of older history — rather than a replay of the group transcript. The system SHALL record how far each member has read, and a member whose session is new or was compacted SHALL receive the full packet again rather than a delta.

#### Scenario: Member turn receives its packet

- **WHEN** a member is dispatched a group request
- **THEN** its turn is built from the group packet, and the member can answer without ever having seen the group transcript as chat history

#### Scenario: Resumed member gets only unread messages

- **WHEN** the same member is dispatched again in a later turn and its session continues
- **THEN** its packet contains only transcript messages past its recorded read position

#### Scenario: Compacted member gets the full packet

- **WHEN** a member's session was compacted since its last dispatch
- **THEN** the next packet covers the transcript from the beginning of the unread range and the read position is rebuilt

### Requirement: Member results copied back with attribution

A member's final assistant text and reported task results SHALL appear in the shared transcript as messages authored by that member. A regenerated or replaced turn SHALL replace its earlier copied message rather than duplicate it. The member's intermediate tool and reasoning activity MUST NOT be copied into the shared transcript as messages.

#### Scenario: Reply lands once

- **WHEN** a member completes a group turn with a final answer
- **THEN** exactly one authored message for that answer appears in the transcript

#### Scenario: Regenerate replaces the copy

- **WHEN** a member's turn is regenerated and produces a different final answer
- **THEN** the transcript holds the new answer attributed to the member and not the superseded one

### Requirement: In-channel tools for group turns

A member's group turn SHALL be offered group tools — reading the group transcript, assigning a child task to a named member, and reporting a task result — and SHALL NOT be offered the ordinary session history tools. A reported result SHALL appear in the transcript as a task result and complete the task. An assignment SHALL create a child task owned by the chosen member, chained to the assigning task's root.

#### Scenario: Lead delegates through a tool

- **WHEN** the lead's group turn assigns a child task to another member
- **THEN** a task owned by that member is created, and its completion lands in the transcript as the member's result

#### Scenario: Group turn cannot read session history

- **WHEN** a member's group turn requests the ordinary session history tools
- **THEN** those tools are absent, and only the group transcript reader is available

### Requirement: Group task lifecycle

A group SHALL track request-scoped tasks, each with an owner, an optional parent and root chain, an instruction, and a state among queued, running, waiting, paused, completed, failed, and cancelled. The user SHALL be able to stop a task (pausing it and its descendant tasks, with the reason visible), continue a stopped task, reassign an open task to another member, and cancel a task, which SHALL end every open task of the same root. Reassigning or correcting a task SHALL bump its revision so a dispatch from a stale revision cannot complete it. Each member's turn card and the transcript SHALL show the task's state transitions.

#### Scenario: Stop pauses the run

- **WHEN** the user stops a running task that has delegated child tasks
- **THEN** the task and its descendants pause, the reason is visible, and active member turns are interrupted

#### Scenario: Continue resumes

- **WHEN** the user continues a stopped task
- **THEN** the task keeps its instruction and owner and is dispatched again with the continue message as a follow-up

#### Scenario: Reassign moves ownership

- **WHEN** the user reassigns an open task to another member after the previous owner stopped
- **THEN** the task's owner changes, its revision advances, and the new owner receives the task

#### Scenario: Cancel ends the root chain

- **WHEN** the user cancels a failed root task with an open child task
- **THEN** the child task is cancelled too and no further member turns start for either

### Requirement: Group dispatch budgets

A group SHALL run at most two member assignments concurrently, and a root request SHALL be limited to at most eight automatic child assignments. Requests beyond a budget SHALL queue and drain as active work completes.

#### Scenario: Third concurrent assignment queues

- **WHEN** two member assignments are active and routing addresses a third member
- **THEN** the third dispatch waits in the queued state and starts when an active assignment finishes

### Requirement: Group surfaces in the chat menu

The chat menu SHALL list groups beside the agent session groups. Opening a group SHALL show its shared transcript in the conversation surface with per-message authorship. The group settings pane SHALL edit the name, title, and instructions, list the members with a control that assigns the lead, and add or remove members. The group composer SHALL offer a mention picker for members and `@everyone`. A group with any member turn in flight SHALL show a busy indicator.

#### Scenario: Open a group conversation

- **WHEN** the user selects a group in the chat menu
- **THEN** the group transcript opens with its authored messages and the composer targets the group

#### Scenario: Lead control in settings

- **WHEN** the user activates the lead control on a member row in the group settings pane
- **THEN** that member becomes the sole lead

#### Scenario: Mention picker

- **WHEN** the user opens the group composer's mention picker
- **THEN** the group's members and an everyone entry are offered, and choosing one addresses the message accordingly

### Requirement: Agent deletion cleanup

Deleting an agent SHALL remove its memberships from every group, fall the lead back where it was the lead, and pause its open group tasks with a visible reason. The group itself, its transcript, and other members' work MUST survive the deletion.

#### Scenario: Deleting a member agent

- **WHEN** an agent that is a group member and owns an open task is deleted
- **THEN** it disappears from the group's members, its open tasks are paused with a reason, and if it was the lead the lead falls to the first remaining member

### Requirement: Group events and restart recovery

The app SHALL push group roster changes and per-group transcript and task updates to the UI as they happen. Dispatches queued by the concurrency budgets or paused mid-run SHALL survive an app restart and resume draining when the app starts again.

#### Scenario: Live transcript update

- **WHEN** a member posts a reply while the group is open
- **THEN** the message appears without a manual refresh

#### Scenario: Queued dispatch survives restart

- **WHEN** the app restarts with a group task queued behind the member lane
- **THEN** the task drains once the agent's lane frees
