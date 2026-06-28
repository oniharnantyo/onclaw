# onboarding (delta)

## Purpose

Delta for the `onboarding` capability: reorder `onclaw init` to **provider setup first, then
an agent setup step**, and replace the file-based onboarding *prompt* mechanism (a seeded
`onboarding.md` plus an `onboarding_completed` database preference) with a lightweight,
**master-only** `BOOTSTRAP.md` signal. The agent setup step shows the master agent, binds its
model, and seeds the master workspace; it runs **no** onboarding interview — onboarding is
deferred to the first `onclaw run`/`chat`, where `BOOTSTRAP.md` is loaded as persona context
and removed by the agent when onboarding concludes. This removes a per-turn DB query and a
prompt-injection branch from every `run`/`chat`, drops the broken `GetTemplate("onboarding.md")`
reference, and aligns init with the canonical requirement that it begin with provider
configuration.

## REMOVED Requirements

### Requirement: The onboarding prompt is a user-editable markdown file

- **Reason:** Onboarding guidance now comes from a master-workspace `BOOTSTRAP.md` (see ADDED requirements), not from a global `~/.onclaw/onboarding.md` sourced from a shipped `onboarding.md` template. The shipped `onboarding.md` template does not exist (orphan removed), so this requirement described non-functional behavior.
- **Migration:** Delete any `~/.onclaw/onboarding.md`; onboarding guidance is the workspace-local `BOOTSTRAP.md` going forward.

### Requirement: An agent receives the onboarding prompt until onboarding is completed

- **Reason:** Onboarding state is now signaled by the presence of `BOOTSTRAP.md` in the master workspace, not by an `onboarding_completed` preference, and no prompt is conditionally prepended to the system prompt.
- **Migration:** The `onboarding_completed` preference key is no longer read or written and is ignored; no data migration is required. Existing users (no `BOOTSTRAP.md` present) are treated as already onboarded.

### Requirement: The default onboarding prompt guides a short interview and maps answers to files

- **Reason:** With the `onboarding.md` prompt removed, there is no shipped default prompt content. First-run guidance is the `BOOTSTRAP.md` template instead.
- **Migration:** None; the `BOOTSTRAP.md` template provides equivalent first-run guidance.

## ADDED Requirements

### Requirement: BOOTSTRAP.md signals first-run onboarding state for the master agent

The system SHALL seed a `BOOTSTRAP.md` into the **master** agent's workspace during first-run
setup via a dedicated bootstrap-seeding path, and SHALL NOT seed it through the generic
workspace-seeding routine used for all agents. The file's content SHALL be included in the
master agent's persona context whenever it is present. The presence of `BOOTSTRAP.md` SHALL
indicate onboarding is in progress. The system SHALL NOT use a database preference to track
onboarding completion, the CLI SHALL NOT delete `BOOTSTRAP.md`, and the agent itself SHALL
remove the file via its workspace tooling when onboarding concludes.

#### Scenario: First-run setup seeds BOOTSTRAP into the master workspace only

- **WHEN** first-run setup seeds the master workspace
- **THEN** `BOOTSTRAP.md` is created in the master workspace and other agents' workspaces are unaffected

#### Scenario: init defers onboarding (no interview during setup)

- **WHEN** `onclaw init` completes
- **THEN** no onboarding interview ran, `BOOTSTRAP.md` remains in the master workspace, and onboarding occurs on the first `onclaw run`/`chat`

#### Scenario: A deferred onboarding resumes on the next run

- **WHEN** the master agent runs via `onclaw run` or `onclaw chat` while `BOOTSTRAP.md` is still present
- **THEN** its persona context includes the BOOTSTRAP guidance and onboarding can resume

#### Scenario: The agent removes BOOTSTRAP when onboarding completes

- **WHEN** the master agent finishes onboarding
- **THEN** it removes `BOOTSTRAP.md` from its workspace via its tooling, and subsequent runs no longer include it

#### Scenario: No preference tracks onboarding state

- **WHEN** any `onclaw run` or `onclaw chat` turn executes
- **THEN** no `onboarding_completed` preference is read or written

### Requirement: The agent loads all persona files by default

The system SHALL assemble the agent's persona context from the full set of persona files
(`IDENTITY.md`, `SOUL.md`, `CAPABILITIES.md`, `USER.md`, `MEMORY.md`, `AGENTS.md`, and
`BOOTSTRAP.md` when present), loading each that exists and skipping those absent. The system
SHALL NOT conditionally inject an onboarding prompt; first-run guidance comes solely from
`BOOTSTRAP.md` content when present.

#### Scenario: All present persona files are loaded

- **WHEN** the workspace and user config dir contain persona files
- **THEN** each non-empty file's content is included in the assembled persona context

#### Scenario: Missing persona files are skipped

- **WHEN** a persona file is absent
- **THEN** it is skipped without error and the remaining files still load

### Requirement: The agent setup step seeds the master workspace

The `onclaw init` agent setup step SHALL seed the master agent's persona files (`IDENTITY.md`, `SOUL.md`,
`CAPABILITIES.md`, `USER.md`, `MEMORY.md`, `AGENTS.md`) into the master workspace
(`~/.onclaw/workspace/master`), and the global `USER.md` into the user config dir
(`~/.onclaw`). The system SHALL NOT seed persona files into the flat user config directory or
the current working directory, since `LoadPersonaContext` only reads them from the workspace
(and the global `USER.md` from the config dir).

#### Scenario: init seeds persona files into the master workspace

- **WHEN** `onclaw init` runs to completion
- **THEN** `IDENTITY.md`, `SOUL.md`, `CAPABILITIES.md`, `USER.md`, `MEMORY.md`, and `AGENTS.md` exist under `~/.onclaw/workspace/master/`

#### Scenario: init does not litter the flat config dir or the working directory

- **WHEN** `onclaw init` runs
- **THEN** no persona `.md` files are created directly under `~/.onclaw/` (other than the global `USER.md`), and none are created in the current working directory

### Requirement: The init agent setup step binds a model to the master agent

`onclaw init` SHALL run provider setup first, then an agent setup step. The agent setup step
SHALL ensure the master agent exists, display it to the user, and bind a model (provider
profile) to it: when exactly one provider profile exists it SHALL bind it automatically, and
when more than one exists it SHALL prompt the user to choose. The selected profile SHALL be
persisted as the master agent's provider.

#### Scenario: A single provider is auto-bound to the master agent

- **WHEN** exactly one provider profile exists when the agent setup step runs
- **THEN** the master agent is bound to that provider without a prompt

#### Scenario: Multiple providers prompt for the master's model

- **WHEN** more than one provider profile exists when the agent setup step runs
- **THEN** the user is prompted to choose one and that choice is persisted as the master agent's provider