# Spec Delta

## MODIFIED Requirements

### Requirement: Skills pane
The Skills settings page SHALL present two top-level tabs: **Skills** and **Curation**. The **Skills** tab (the default view) SHALL present the workspace skill library backed by the skills API exactly as before: each row shows the skill's name, version chip, source badge (`authored` | `upload` | `git` | `fork`), dependency status chip when unmet, an enable master toggle, an edit action, and an uninstall action (confirm dialog). An **Install skill** wizard SHALL walk through source selection (Author / Upload / Git or URL; Fork when entered from a system skill), content (body editor for Author; archive drop with file-tree preview for Upload; URL with ref and optional token plus discovered-skill selection for Git/URL), and a dependency review step listing every declared or inferred dependency with its resolution status — tools (with the pre-checked "enable everywhere" option), binaries (per-platform install command with copy action and re-check), python packages (auto-provision checkbox). Installing with unmet dependencies SHALL be allowed and leave a persistent warning chip on the row. Disabling a skill SHALL toast that it was removed from every agent; enabling SHALL restore it everywhere. A **System skills** section SHALL list embedded skills as read-only locked entries marked always-on with a Fork-to-workspace action. Holders of `skills.read` without `skills.write` (Members) SHALL see the same lists with no action affordances. The **Curation** tab and everything it contains SHALL be specified by the `web-app/skill-curation` capability; this pane's contract is the tab shell: the pending-work badge, the run-curation-now control's placement, and the guarantee that the skill library's behavior is identical whether or not any curation exists.

#### Scenario: Install custom skill
- **WHEN** the user authors "Changelog sweeper" with a SKILL.md body and installs through the wizard
- **THEN** it appears with version `0.1.0`, an `authored` badge, `enabled` on, and a toast that it is live on all agents

#### Scenario: Edit custom skill
- **WHEN** the user edits "Changelog sweeper" and changes its description
- **THEN** the new description renders on the row after save

#### Scenario: Dependency review reports a missing binary
- **WHEN** the wizard's review step evaluates a skill declaring `pdftotext` absent from the server PATH
- **THEN** the step shows the binary as unmet with the per-platform install command, a copy button, and a re-check action, and install completes with a warning chip on the row

#### Scenario: Enable-everywhere option
- **WHEN** the user installs a skill declaring the `web.search` tool dependency with the pre-checked enable option left on
- **THEN** the skill installs and the agent wizard's tool chips show Web Search enabled for every agent

#### Scenario: Master toggle disables everywhere
- **WHEN** the user toggles "Changelog sweeper" off
- **THEN** the toast reads the skill was disabled and removed from every agent, and the row renders dimmed until re-enabled

#### Scenario: Upload with entry-point error
- **WHEN** the user drops a zip without a root SKILL.md
- **THEN** the content step shows the error state and nothing is installed

#### Scenario: Multi-skill repo selection
- **WHEN** a git URL resolves to a tree containing three skills
- **THEN** the wizard lists all three with descriptions and installs only the checked ones

#### Scenario: System skills locked with fork
- **WHEN** the user opens the System skills section
- **THEN** each entry renders locked and always-on, and its Fork action creates a workspace copy (`fork` badge) opened for editing

#### Scenario: Member sees read-only inventory
- **WHEN** a Member-role holder opens the Skills pane
- **THEN** the library and system lists render with no install, toggle, edit, or uninstall affordances

#### Scenario: Tab shell isolates curation from the library
- **WHEN** a workspace has the curation loop disabled or empty
- **THEN** the Skills tab presents the identical library UI as before this change, with the Curation tab present but its badge empty
