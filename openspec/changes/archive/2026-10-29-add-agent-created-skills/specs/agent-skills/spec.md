# Spec Delta

## ADDED Requirements

### Requirement: Agent skill authoring

The system SHALL compose each agent's system prompt with a skills-authoring
contract that teaches the agent to save durable, reusable workflows as skills
in its own workspace skills directory — one directory per skill, named with a
DNS-label (lowercase letters, digits, hyphens), containing a `SKILL.md` whose
frontmatter declares a `description` — and that its skills are personal to it
until promoted to the global library. A skill written into an agent's
workspace skills directory SHALL be usable by that agent's next turn without
an app restart. The global skills library SHALL include a seeded `create-skill`
skill carrying the full authoring playbook: seeded when missing, never
overwriting an existing skill of that name.

#### Scenario: The authoring contract rides every turn

- **WHEN** an agent's turn is composed
- **THEN** its system prompt carries the skills-authoring contract, including
  the directory and frontmatter conventions and that authored skills are
  personal until promoted

#### Scenario: Authored skill is live next turn

- **WHEN** an agent writes a valid skill into its workspace skills directory
  during a turn
- **THEN** the agent's next turn can invoke that skill, including via
  `$name`, without an app restart

#### Scenario: Seed does not clobber

- **WHEN** the global skills library already contains a `create-skill` skill
- **THEN** the seeded copy is not written and the existing skill stands

### Requirement: Skill sharing boundary

A workspace-tier skill SHALL be offered only to its owning agent's turns until
it is promoted to the global skills library. Any other agent's turns MUST NOT
carry the skill's metadata, and `$`-invocation of its name MUST NOT load it
for them.

#### Scenario: Personal skill stays personal

- **WHEN** agent B sends a turn while only agent A's workspace contains the
  skill `deploy-runbook`
- **THEN** agent B's turn carries no `deploy-runbook` metadata and a
  `$deploy-runbook` token in B's input stays plain text

#### Scenario: Promotion makes it shared

- **WHEN** agent A's `deploy-runbook` skill is promoted to the global library
- **THEN** every other agent permitted by the eligibility gate sees its
  metadata and can invoke it

### Requirement: Skill promotion

The user SHALL be able to promote an agent's workspace-tier skill into the
global skills library. Promotion SHALL copy the skill into the library and
register it (name, description, enabled) without altering the agent's own
copy, which continues to shadow the promoted skill for that agent. Promoting
a name that already exists in the library SHALL be rejected with a clear
error and MUST NOT modify the existing library skill. A promoted skill is
thereafter an ordinary library skill subject to the existing eligibility
gate.

#### Scenario: Promote copies and the creator keeps its shadow

- **WHEN** the user promotes agent A's workspace skill `deploy-runbook`
- **THEN** the library contains `deploy-runbook`, agent A's turns still
  resolve `$deploy-runbook` to A's own copy, and the library version serves
  every other eligible agent

#### Scenario: Collision is rejected

- **WHEN** the user promotes `deploy-runbook` while the library already
  contains a `deploy-runbook` skill
- **THEN** the promotion fails with an error naming the collision and the
  existing library skill is unchanged

#### Scenario: Promoted skill follows the global toggle

- **WHEN** the user disables the promoted skill in the Skills pane
- **THEN** no agent's turns carry its metadata and `$`-invocation does not
  load it

#### Scenario: Delete removes only the workspace copy

- **WHEN** the user deletes agent A's workspace skill `deploy-runbook`
- **THEN** agent A's next turn no longer sees the skill and the global
  library is untouched
