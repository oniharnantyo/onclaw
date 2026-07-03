## Why

onclaw's tests are almost universally **white-box** (`package foo`, same package as the code
under test), and coverage is low — **49.3% aggregate**, with **15 of 27 packages below 70%**
(`api/service` 6.6%, `llm/adapter` 4.7%, `browser/cdp` 18.8%, `agent/tools/browser` 20.4%,
`mcp` 49.4%, `agent/tools` 54.2%, `skill` 54.0%, `cli` 54.4%, `modelmeta` 59.3%, `agent`
61.1%, `agent/middlewares` 63.1%, `browser` 67.4%). White-box tests couple to implementation
detail, can form import cycles as the agent layer grows, and let the suite drift from the
public contract. There is no enforced coverage floor, so large gaps have accumulated in the
service, adapter, and browser layers.

This change standardizes the suite on **black-box** tests and establishes a **≥70% per-package
coverage floor**.

## What Changes

- **Black-box tests by default.** Every `_test.go` becomes `package <pkg>_test`, exercising
  only the exported API. Mocks/helpers defined in test files move into `<pkg>_test`. A
  precedent already exists: `internal/browser/browser_test.go` (`browser_test`),
  `internal/cli/app_test.go` (`cli_test`), `internal/browser/cdp/engine_test.go` (`cdp_test`).
- **`export_test.go` bridges** for the few packages whose tests genuinely need private access
  (`internal/cli` → `appState`, `internal/observability` → `buildConfig`). `internal/mcp` is
  **reworked** to drop its `NewManager(...).(*manager)` internal-type assertion instead of
  being bridged.
- **Coverage uplift to ≥70% per package** for all 15 packages below the floor, via new
  black-box tests through the exported API.
- **Convention codified** in `CLAUDE.md` and a new `testing-conventions` spec.

## Capabilities

### New Capabilities

- `testing-conventions`: the project's test-packaging and coverage rules — black-box `foo_test`
  by default, `export_test.go` bridges only for genuine private access, and a ≥70% per-package
  coverage floor.

### Modified Capabilities

<!-- None — this change touches build/test conventions only, not product behavior. -->

## Impact

- **Modified files:** all `*_test.go` across `internal/...` (package clause + import +
  reference qualification); `CLAUDE.md` (Conventions section).
- **New files:** bridge files `internal/cli/export_test.go`,
  `internal/observability/export_test.go`; new/expanded `*_test.go` for coverage; new spec
  `openspec/specs/testing-conventions/`.
- **No behavior change:** production code is untouched except for test-only bridges; the
  shipped binary is identical.
- **Out of scope:** production logic changes; raising coverage on packages already ≥70%;
  browser/CDP integration-test harness design (deferred if a package proves infeasible).
