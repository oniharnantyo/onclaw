# workspace-skills Specification

## Purpose
Workspace-scoped SKILL.md storage: named, per-workspace skill documents (name, description, body, enabled) that agents subscribe to by name and that the future runtime consumes through progressive disclosure (list metadata, fetch body on demand).

## Requirements

### Requirement: Skills CRUD
Tenant-scoped endpoints under `/workspaces/:ws/skills` SHALL create, list, get, update, and delete skills. Routes SHALL address skills by ID; requests require `skills.read` for list/get and `skills.write` for create/update/delete. A skill SHALL have name (required), description (shown in disclosure lists), body (SKILL.md content), and enabled (default true). Name SHALL be unique per workspace (409 on duplicate). Deletion is immediate; agents referencing the name keep the reference (runtime skips missing/disabled skills).

#### Scenario: Create and duplicate
- **WHEN** an Admin creates a skill named "incident-runbook", then re-creates the same name
- **THEN** first create is 201; duplicate is 409 conflict

#### Scenario: Member reads, cannot write
- **WHEN** a Member-role holder lists skills, then attempts to create one
- **THEN** 200 on list (skills.read for every built-in role); 403 on create

### Requirement: Progressive disclosure contract
Listing skills SHALL return name, description, and enabled (no body). Getting one skill SHALL additionally return the body. This is the disclosure contract the future runtime consumes: List gives metadata, Get gives the full SKILL.md body on demand.

#### Scenario: List omits the body
- **INE-BODY WHEN** any member lists skills
- **THEN** rows show name/description/enabled without body

#### Scenario: Get returns the body
- **WHEN** any member gets a skill
- **THEN** the response includes the SKILL.md body

### Requirement: Agent subscription validated
Saving an agent whose skills array names an unknown skill SHALL be rejected (see agents capability). A skill row with matching name in the same workspace — enabled or disabled — satisfies validation.

#### Scenario: Unknown skill reference rejected
- **WHEN** an agent references a skill name with no row in this workspace
- **THEN** 400 invalid_request

#### Scenario: Disabled skill satisfies validation
- **WHEN** an agent references a disabled skill
- **THEN** save succeeds; runtime skips disabled skills
