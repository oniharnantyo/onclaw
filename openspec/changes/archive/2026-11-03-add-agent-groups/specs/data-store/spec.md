# Spec Delta

## ADDED Requirements

### Requirement: Agent group persistence

Agent groups SHALL be persisted in the SQLite database: each group's name,
title, instructions, lead, archived flag, and revision; its membership rows;
its shared transcript as an ordered, authored message log with stable
identifiers; each member's read position and summary version for packet
delivery; its tasks with owner, chain, state, and revision; and its
versioned transcript summary. Group state changes SHALL be applied
atomically and durably, and a task's state SHALL be persisted before it is
dispatched so a crash never double-dispatches it.

#### Scenario: Group survives restart

- **WHEN** the user creates a group, exchanges messages in it, and restarts the app
- **THEN** the group, its members, its transcript with authorship, and its task states are all restored

#### Scenario: Crash between persist and dispatch

- **WHEN** the app crashes after a task was persisted as queued but before its member turn started
- **THEN** the task is not running twice after restart, and the queued dispatch drains once

## MODIFIED Requirements

### Requirement: Agent workspace lifecycle

Each agent SHALL have exactly one workspace directory named by its slug. The directory SHALL be created when the agent is created and removed when the agent is deleted; deleting an agent MUST NOT remove its session history, which stays in the database. Deleting an agent MUST also clean up its group memberships: the agent is removed from every group's roster, a group whose lead it was falls the lead to the first remaining member, and its open group tasks are paused with a visible reason while the groups, their transcripts, and other members' work remain.

#### Scenario: Workspace removed on delete

- **WHEN** the user deletes an agent after generating its documents
- **THEN** the agent's workspace directory is gone while its past sessions remain listed in the data store

#### Scenario: Group membership cleaned on delete

- **WHEN** the user deletes an agent that belongs to groups
- **THEN** the agent is no longer a member of any group, groups it led have fallen back to their first remaining member, and those groups' transcripts remain intact
