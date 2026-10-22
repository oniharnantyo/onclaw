# Spec Delta

## Purpose

Make skills a live capability: agents see skill metadata in their turns, load skill bodies on demand, and can invoke a skill explicitly via `$name` — across a global tier and a per-agent tier.

## ADDED Requirements

### Requirement: Two-tier skill discovery

Skills SHALL be discovered from two tiers: the global skills directory (`~/.buddies/skills/<name>/SKILL.md`) and the agent's own workspace skills directory (`~/.buddies/workspaces/<slug>/skills/<name>/SKILL.md`). Skill presence and content SHALL be read fresh for each turn. When both tiers hold a skill of the same name, the agent-tier skill SHALL take precedence.

#### Scenario: Agent tier shadows global

- **WHEN** an agent's workspace and the global directory both contain a skill named "triage"
- **THEN** the agent's turns use the workspace version

#### Scenario: Workspace skill is picked up live

- **WHEN** a valid skill directory is added to an agent's workspace skills directory
- **THEN** the agent's next turn can use that skill without an app restart

### Requirement: Progressive disclosure

A turn SHALL receive each eligible skill's metadata (name and description) rather than its full body; a skill's body SHALL be loaded only when the skill is used.

#### Scenario: Bodies stay out of the prompt until used

- **WHEN** an agent has eligible skills but uses none
- **THEN** the turn carries only their names and descriptions

### Requirement: Explicit invocation

A `$skill-name` token in the user's message SHALL explicitly invoke that skill for the turn when the skill exists and is eligible.

#### Scenario: Dollar invocation

- **WHEN** the user sends a message containing "$trip-planner" for an eligible skill
- **THEN** the turn loads and uses that skill's body

### Requirement: Skill eligibility gate

A skill SHALL be eligible to an agent's turn only when it is enabled globally and permitted by the agent's skill allowlist (an empty allowlist permits all discovered skills). Disabled skills MUST NOT be offered to agents.

#### Scenario: Disabled skill is not offered

- **WHEN** a skill is disabled globally
- **THEN** no agent's turns carry its metadata and `$`-invocation does not load it

#### Scenario: Allowlist narrows skills

- **WHEN** an agent's skill allowlist lists a subset of skills
- **THEN** only that subset is eligible on its turns
