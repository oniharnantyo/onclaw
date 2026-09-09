## ADDED Requirements

### Requirement: Agent hooks configuration
The agent configuration modal SHALL offer a Hooks section with two parts: CRUD for hooks private to this agent (same editor contract as the workspace Hooks pane, minus the level controls), and a read-only list of the instance- and workspace-level hooks that reach this agent — each shown with its event, what it selects, whether it can block, and its level. The read-only list MUST NOT offer disable or exclusion controls for instance or workspace hooks.

#### Scenario: Agent-owner adds a private gate
- **WHEN** a user who can edit the agent creates an agent-level hook selecting only the shell tool
- **THEN** the hook applies to this agent's future runs immediately and appears under this agent's hooks, not the workspace list

#### Scenario: Visibility without control
- **WHEN** a user views an agent's Hooks section where a mandatory instance hook and a workspace hook apply
- **THEN** both are listed read-only with their level marked, and neither offers a disable control
