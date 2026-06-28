# onboarding

## Purpose

Provide a guided, interactive onboarding experience for first-time users of onclaw to set up their LLM providers.

## Requirements

### Requirement: A guided first-run command configures providers interactively

The system SHALL provide `onclaw init`, a top-level command that runs an ordered set of
guided setup steps beginning with provider configuration. The flow SHALL be interactive
and line-oriented, requiring no full-screen terminal. Re-running `onclaw init` SHALL be
additive and SHALL NOT modify or remove existing providers.

#### Scenario: A new user reaches a configured default provider in one command

- **WHEN** a user runs `onclaw init` on a fresh database and follows the prompts
- **THEN** at least one provider profile is created, its API key (if required) is stored, and a default provider is set

#### Scenario: Re-running init does not disturb existing providers

- **WHEN** `onclaw init` is run again after providers already exist
- **THEN** existing profiles remain unchanged and the user may add more

### Requirement: The init command is structured for future steps

The system SHALL structure `onclaw init` as an ordered sequence of setup steps. Adding a
new setup step SHALL require only appending to that sequence, not editing the flow's core.

#### Scenario: A future step is added by appending

- **WHEN** a new setup step is appended to the init step sequence
- **THEN** `onclaw init` runs it after the existing steps with no change to the provider step

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
