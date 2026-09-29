# Spec Delta

## ADDED Requirements

### Requirement: Document usage system skill
The runtime SHALL ship a system-tier skill named `document-read` that teaches agents the reference-document workflow: consulting the compose-time manifest before searching, discovering sections with `document.search`, reading with scoped `document.read` invocations (pages for PDF ranges, section for heading, slide, or sheet; a hit's own read hint when present), citing by document name plus locator linked to `references/<document name>`, delegating heavy multi-document research to the `agent` tool, and never locating or extracting documents through the shell. The skill SHALL be embedded with the binary and mirrored by the same boot-time sync as the other system skills, injected into every agent's execution per the system-tier rules, not disableable, and forkable to the workspace tier as the only customization path. The skill SHALL be content-accurate whether or not search hits carry read hints.

#### Scenario: Skill ships and syncs
- **WHEN** the server boots in a fresh instance
- **THEN** the system skills tree contains `document-read` beside the other embedded skills, mirrored from the binary

#### Scenario: Every agent sees the workflow
- **WHEN** any agent runs with document tools enabled
- **THEN** the skill's procedure is available to it as a system-tier skill, with no install or attach step

#### Scenario: Fork customizes, original locked
- **WHEN** a user forks the `document-read` system skill to the workspace tier
- **THEN** a workspace skill with source `fork` is created from the embedded content and the system skill itself remains locked and unchanged
