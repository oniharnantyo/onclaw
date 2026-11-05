# Spec Delta

## ADDED Requirements

### Requirement: One turn lane per agent

An agent SHALL run at most one turn at a time across all of its conversations — its ordinary sessions and every group thread it takes part in. Work addressed to an agent while its lane is busy SHALL queue in arrival order and drain when the lane frees, and a queued group dispatch SHALL persist so it survives an application restart. Queued group work SHALL surface its waiting state rather than appearing lost.

#### Scenario: Group dispatch waits behind a chat turn

- **WHEN** a group dispatch addresses an agent whose ordinary session has a turn in flight
- **THEN** the dispatch queues and starts only after the chat turn reaches a terminal outcome

#### Scenario: Lane drains in arrival order

- **WHEN** two dispatches address the same busy agent, one from its own chat and one from a group
- **THEN** they start in the order they arrived, never concurrently

#### Scenario: Queued lane work survives restart

- **WHEN** the app restarts while a dispatch sits queued on a busy agent's lane
- **THEN** the dispatch drains after restart once the lane is free
