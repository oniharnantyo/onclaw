## ADDED Requirements

### Requirement: The hook configuration dialog validates handler inputs before submission

The Configure New Hook dialog SHALL validate handler inputs in the browser before they are submitted and SHALL prevent the save when a hard validation error is present. The tool-name matcher SHALL be validated as a regex and the dialog SHALL warn when the pattern contains constructs that Go's RE2 `regexp` engine rejects (lookahead, look-behind, or backreferences), because the server validates matchers with RE2. The shell command SHALL be checked for non-emptiness and balanced quoting. The JavaScript source SHALL be checked for syntax and SHALL be checked to define a `handle(ctx)` function, matching the script-handler contract. Guidance shown in the dialog (placeholders, hints) SHALL reflect the real `function handle(ctx)` returning `{decision, reason}` contract and SHALL NOT reference functions or globals the sandbox does not provide.

#### Scenario: an invalid regex blocks save with an inline error

- **WHEN** the user enters an unbalanced regex matcher such as `(`
- **THEN** the dialog shows an inline error on the matcher field and the save action is disabled

#### Scenario: an RE2-incompatible regex warns the user

- **WHEN** the user enters a regex using lookahead such as `(?=foo)`
- **THEN** the dialog warns that the construct is not supported by the server's RE2 engine

#### Scenario: a script missing handle(ctx) blocks save

- **WHEN** the user enters JavaScript that does not define `handle(ctx)`
- **THEN** the dialog shows an inline error explaining the required contract and the save action is disabled

#### Scenario: the JavaScript placeholder reflects the real contract

- **WHEN** the user opens the JavaScript source field
- **THEN** the placeholder shows a `function handle(ctx)` example returning `{decision, reason}` and references only `ctx.*` event fields

### Requirement: The hook configuration dialog offers a dry-run test

The Configure New Hook dialog SHALL provide a Test action that submits the in-progress hook to the existing `POST /api/hooks/test` dry-run endpoint and displays the returned decision, and any error, inline. The Test action SHALL run client-side validation first and SHALL NOT call the endpoint when a hard validation error is present. The dry-run SHALL NOT persist the hook or write an audit row.

#### Scenario: a valid script tests successfully

- **WHEN** the user clicks Test with a syntactically valid script that defines `handle(ctx)`
- **THEN** the dialog displays the decision returned by the dry-run endpoint

#### Scenario: a failing script shows the server error

- **WHEN** the user clicks Test with a script that compiles but throws at runtime
- **THEN** the dialog displays the error returned by the dry-run endpoint without saving the hook

### Requirement: The hook configuration dialog provides per-field guidance

The Configure New Hook dialog SHALL present a tooltip on each field that explains its meaning and effect, including which lifecycle events are blocking, the fail-closed timeout policy, priority ordering, that the matcher is RE2 and applies only to tool events, the command exit-code semantics, the environment-variable allowlist baseline, and the JavaScript sandbox contract. The lifecycle-event and handler-type selects SHALL provide per-option explanations.

#### Scenario: a user can discover what each field does

- **WHEN** the user invokes the guidance control on any field (hover or keyboard focus)
- **THEN** an explanation of that field's meaning and effect is shown