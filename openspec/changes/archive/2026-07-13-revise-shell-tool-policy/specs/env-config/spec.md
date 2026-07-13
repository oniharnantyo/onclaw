## ADDED Requirements

### Requirement: Shell denylist configuration and default policy

The system SHALL support a `tools.shell.denylist` configuration field (env
`ONCLAW_TOOLS_SHELL_DENYLIST`) holding the catastrophic-command patterns used by the `denylist`
shell policy. The field SHALL parse as a comma-separated array using the same array-parsing rules
as the shell allowlist. The **default policy SHALL be `denylist`** (previously `allowlist`), and
the default denylist SHALL be seeded with the catastrophic floor defined in the `agent-tools`
specification; both are overridable through the standard layering
(`defaults < config file < ONCLAW_* env < CLI flags`). An empty policy value SHALL resolve to
`denylist` (not `deny`).

#### Scenario: The default policy is denylist

- **WHEN** no `tools.shell.policy` is set by any config layer
- **THEN** the resolved policy is `denylist`

#### Scenario: An empty policy resolves to denylist

- **WHEN** `tools.shell.policy` is set to an empty string
- **THEN** the resolved policy is `denylist`

#### Scenario: The denylist parses as a comma-separated array

- **WHEN** `ONCLAW_TOOLS_SHELL_DENYLIST=rm -rf /,curl|sh,/dev/tcp/`
- **THEN** the config parses as a three-element array
- **AND** the denylist field has type `[]string`

#### Scenario: The denylist overrides the default floor

- **WHEN** `tools.shell.denylist` is set by any config layer
- **THEN** that value replaces (or extends, per the documented merge rule) the default
  catastrophic floor used by the `denylist` policy
