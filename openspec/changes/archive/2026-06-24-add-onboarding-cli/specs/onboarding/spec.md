## ADDED Requirements

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

### Requirement: A reusable guided step sets LLM providers

The system SHALL provide `onclaw provider setup`, a subcommand that interactively collects
provider kind, profile name, model name (no default), optional base URL, and (for keyful kinds) an
API key, then persists the profile and secret through the existing provider service. The
step SHALL loop to add multiple providers per invocation.

#### Scenario: A keyful provider is added with a hidden API key

- **WHEN** the user selects `anthropic`, enters a model, and enters an API key
- **THEN** a profile is stored and the key is encrypted at rest, with the key input hidden during entry

#### Scenario: A keyless provider skips the API key prompt

- **WHEN** the user selects `ollama`
- **THEN** no API key is prompted, no secret row is created, and the profile is stored with its base URL

#### Scenario: An openai-compatible provider prompts for a required base URL

- **WHEN** the user selects `openai-compatible` and supplies a base URL, a model, and an API key
- **THEN** the flow prompts for a base URL (with no assumed default), and a profile is stored with that base URL and an encrypted API key

### Requirement: Profile name and local base URL offer defaults; model is always entered

The system SHALL offer a bracketed default for the profile name (the chosen kind) and, for
local providers, a base URL, each accepted on Enter. The system SHALL NOT pre-fill a model
for any kind; the model SHALL always be entered by the user and SHALL be non-empty. A
keyless kind SHALL NOT prompt for an API key.

#### Scenario: The common path accepts defaults

- **WHEN** the user selects a kind, presses Enter to accept the default profile name, and types a model
- **THEN** the default profile name is used and the typed model is stored

#### Scenario: An empty required model re-prompts

- **WHEN** the user submits an empty model for any kind
- **THEN** the system re-prompts instead of creating a profile with an empty model

### Requirement: The flow guides a default provider selection

When more than one provider is configured and no default is set, the flow SHALL prompt the
user to choose a default and SHALL persist that choice as the `default_provider`
preference. With a single provider, the default SHALL be set without prompting.

#### Scenario: Multiple providers prompt for a default

- **WHEN** two providers are added in one flow and no default exists
- **THEN** the user is asked to pick one and that choice is persisted

#### Scenario: A single provider becomes the default silently

- **WHEN** exactly one provider is added and no default exists
- **THEN** that provider is set as the default without an additional prompt

### Requirement: The interactive flow is lightweight and portable

The system SHALL implement interactive prompts using `golang.org/x/term` and the standard
library only. The system SHALL NOT depend on a full-screen TUI library. The flow SHALL
detect whether input is a terminal and SHALL remain drivable from piped stdin.

#### Scenario: No TUI library is added

- **WHEN** the dependency graph is inspected
- **THEN** no full-screen TUI framework (e.g. bubbletea/lipgloss/huh) is present, and `golang.org/x/term` is the only terminal dependency

#### Scenario: The flow is drivable over a pipe

- **WHEN** answers are piped to `onclaw init` via stdin
- **THEN** the flow completes using the piped input rather than requiring an interactive terminal

### Requirement: Interruption preserves completed providers

The system SHALL commit each provider immediately as it is entered. An interruption
(EOF or cancellation) mid-flow SHALL leave all previously completed providers persisted and
SHALL lose only the provider in progress.

#### Scenario: EOF after one provider keeps that provider

- **WHEN** the user adds one provider and the input stream ends before the second is committed
- **THEN** the first provider and its secret remain stored

### Requirement: The init command is structured for future steps

The system SHALL structure `onclaw init` as an ordered sequence of setup steps. Adding a
new setup step SHALL require only appending to that sequence, not editing the flow's core.

#### Scenario: A future step is added by appending

- **WHEN** a new setup step is appended to the init step sequence
- **THEN** `onclaw init` runs it after the existing steps with no change to the provider step