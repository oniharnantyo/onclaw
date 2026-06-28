# providers (delta)

## Purpose

Delta for the `providers` capability: provider profiles gain an optional `context_window`
setting declaring the model's context-window size, surfaced in `onclaw provider list`, and the
agent uses 80% of the effective context window as the conversation-summarization trigger
(replacing a hardcoded threshold). The window rides the existing `settings` JSON document, so
no schema migration is required. The effective window falls back to the global
`max_context_tokens` (default 64000) and then to 64000.

## MODIFIED Requirements

### Requirement: Provider profiles carry a settings document and an enabled flag

Each profile SHALL have a `settings` JSON field (default `{}`) for provider-specific extras
and an `enabled` flag (default true). The `settings` document SHALL recognize a
`context_window` integer expressing the model's context-window size in tokens, settable via
`onclaw provider add --context-window`. A disabled profile SHALL NOT be selectable as a
provider until re-enabled.

#### Scenario: Provider-specific settings are stored and surfaced

- **WHEN** a profile is created with extra settings (e.g. custom headers)
- **THEN** those settings persist in the `settings` field and are available when the adapter is built

#### Scenario: A provider's context window is stored in settings

- **WHEN** a profile is created with `--context-window 128000`
- **THEN** the `settings` document contains `context_window: 128000` and no database migration is performed

#### Scenario: A disabled profile is not selectable

- **WHEN** a profile is disabled and the user runs `onclaw run` without naming another provider
- **THEN** the disabled profile is skipped and not used

## ADDED Requirements

### Requirement: Conversation summarization triggers at 80% of the effective context window

The system SHALL trigger conversation summarization when the running token count reaches
`int(0.8 * effectiveContextWindow)`. The effective context window SHALL be the resolved
provider profile's `context_window` setting when it is greater than 0, else the global
`max_context_tokens` when it is greater than 0, else 64000. The system SHALL NOT use a
hardcoded token threshold for summarization.

#### Scenario: A provider window drives the trigger

- **WHEN** the resolved provider profile sets `context_window` to 128000
- **THEN** summarization is configured to trigger at 102400 tokens (80% of 128000)

#### Scenario: An unset window falls back to the default

- **WHEN** the resolved provider profile does not set `context_window` and the global default applies
- **THEN** the effective window is 64000 and summarization is configured to trigger at 51200 tokens

### Requirement: Provider list displays the configured context window

`onclaw provider list` SHALL include each profile's `context_window` when it is set, and SHALL
indicate the default/fallback clearly when it is unset.

#### Scenario: A set context window is shown

- **WHEN** a profile was created with `--context-window 128000` and `onclaw provider list` is run
- **THEN** the output includes `context_window: 128000` for that profile

#### Scenario: An unset context window is shown as the default

- **WHEN** a profile has no `context_window` and `onclaw provider list` is run
- **THEN** the output indicates the context window is unset/default (e.g. `context_window: (default)`)