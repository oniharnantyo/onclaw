# Capability: Testing Conventions

## Purpose
Define the testing standards, packaging conventions, and statement coverage requirements for the project.

## Requirements

### Requirement: Tests are black-box by default

The project SHALL place test files in an external `<pkg>_test` package, exercising only the
exported API of the package under test. All test files in a directory SHALL declare the
`<pkg>_test` package and be converted together as a unit, so that mocks and helpers defined in
test files remain visible across the package's tests.

#### Scenario: A test references an exported symbol

- **WHEN** a test file in `internal/foo` is authored
- **THEN** it declares `package foo_test` and qualifies references as `foo.<Symbol>`

#### Scenario: A test-only mock stays within the test package

- **WHEN** a mock or helper is defined in a `_test.go` file
- **THEN** it is part of `<pkg>_test` and is visible to every test file in that directory

### Requirement: Private access only via export_test.go bridges

The project SHALL reach unexported symbols from black-box tests only through an
`export_test.go` file in the package under test (`package <pkg>`), which compiles exclusively
under `go test` and never reaches the shipped binary. A test SHALL be reworked to remove
incidental private access before a bridge is introduced.

#### Scenario: A test genuinely needs an unexported symbol

- **WHEN** a black-box test requires access to an unexported symbol
- **THEN** an `export_test.go` file exposes it via a type or var alias

#### Scenario: A bridge never ships

- **WHEN** the project is built with `go build`
- **THEN** symbols declared only in `export_test.go` are absent from the binary

### Requirement: Per-package coverage floor of 70 percent

The project SHALL maintain a minimum of 70% statement coverage in every `internal/...`
package, measured by `go test -cover`. A package unable to meet the floor because its untested
code requires a live external-system dependency SHALL carry a documented exception with
evidence and a remediation path.

#### Scenario: The coverage gate runs in verification

- **WHEN** verification runs `go test -cover ./internal/...`
- **THEN** every package reports coverage of at least 70.0%, or a documented exception

#### Scenario: A repackaging does not regress coverage

- **WHEN** tests are converted to black-box packaging
- **THEN** no package's coverage falls below its pre-change value

## Documented Exceptions

The following packages are exempt from the strict 70% coverage floor under unit tests due to hard dependencies on a live browser or other external system.

### Exception: internal/browser/cdp
- **Current Coverage**: 18.8%
- **Evidence**: The package manages connection and communication with chromium, lightpanda, and remote browser instances using `github.com/go-rod/rod`. Instantiating pages, navigating, injecting scripts, and retrieving accessibility trees cannot be tested without a live browser running in the test environment.
- **Remediation Path**: Implement a loopback / mock CDP server or integration test harness in a future phase (e.g. using a headless chromium container in CI/CD).

### Exception: internal/agent/tools/browser
- **Current Coverage**: 20.4%
- **Evidence**: This package implements the tool executors for browser operations (starting, navigating, snapshotting, taking screenshots, clicking/acting on elements). These tools depend directly on `internal/browser/cdp` and require a running browser context to execute.
- **Remediation Path**: Integrate with the browser/cdp integration test harness when available.
