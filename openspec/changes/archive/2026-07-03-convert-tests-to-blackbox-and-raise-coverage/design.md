## Context

onclaw has ~64 `_test.go` files. Almost all use the **internal (white-box)** package
(`package foo`), including `internal/secrets`, `internal/store/sqlite`, `internal/cli`, etc.
A few already follow the black-box pattern: `internal/browser/browser_test.go` (`browser_test`),
`internal/cli/app_test.go` (`cli_test`), `internal/browser/cdp/engine_test.go` (`cdp_test`).

Coverage today is **49.3% aggregate**; **15 of 27 packages are below 70%**. The gaps cluster
in the service layer (`api/service` 6.6%), the LLM adapter (`llm/adapter` 4.7%, mostly a stub),
and the browser/CDP transport (`browser/cdp` 18.8%, `agent/tools/browser` 20.4%).

## Goals / Non-Goals

**Goals:**
- Standardize on black-box `<pkg>_test` packages across `internal/...`.
- Establish and enforce a ≥70% per-package coverage floor.
- Keep the shipped binary identical to today (no production logic change).

**Non-Goals:**
- Changing production behavior.
- Refactoring production code beyond what new coverage tests reasonably require.
- Raising coverage above 70% where a package already clears the floor.

## Decisions

### D1 — Convert whole packages in lockstep
A `<pkg>_test` file cannot see an unexported identifier defined in a `package <pkg>` test file.
Therefore every `_test.go` in a directory converts to `<pkg>_test` together; partial conversion
breaks compilation. Mocks/helpers defined in `_test.go` move with the package and become
`<pkg>_test` locals.

### D2 — `export_test.go` bridges for genuine private access
Where a test legitimately needs an unexported symbol, add `export_test.go` (`package foo`) that
re-exports it via a type or var alias (e.g. `type AppState = appState`, `var BuildConfig =
buildConfig`). It compiles only under `go test`, so it never reaches the shipped binary.

_Alternative considered:_ exposing the symbol in the real package API (rejected — widens the
public surface for test convenience).

### D3 — Rework over bridge where private access is incidental
`internal/mcp`'s `manager_test.go` asserts `NewManager(...).(*manager)` on the unexported
concrete type. Rather than bridge the type, rework the test to assert behavior through the
exported `Manager` interface. Bridges are for genuine need, not convenience.

### D4 — Black-box conversion is coverage-neutral; uplift is separate work
Part A only repackages existing assertions (no logic change), so coverage should not move.
Part B authors new tests. We verify Part A caused no regression (each package ≥ its pre-change
value) before starting Part B.

### D5 — Per-package coverage floor, with documented exceptions
The floor is `go test -cover` per `internal/...` package ≥70%. Packages whose untested code
requires a live external system (browser/CDP) may not reach 70% under unit tests; any such
package is documented here with evidence and a remediation path (integration harness) rather
than silently failing the gate.

### D6 — Escape hatch
Go permits both `package foo` and `package foo_test` in one directory. A test that exists solely
to unit-test a private algorithm may stay white-box as `xxx_internal_test.go`. Used sparingly.

## Risks / Trade-offs

- **[Bridge surface growth]** `export_test.go` symbols widen the effective test surface, though
  they never ship. → kept minimal and clearly named.
- **[Browser/CDP ceiling]** `internal/browser/cdp` and `internal/agent/tools/browser` may resist
  70% without a real browser. → best-effort now; documented exception with a follow-up
  integration-harness proposal if blocked.
- **[Effort]** 15 packages of coverage uplift is substantial. → ordered easiest-first; each
  package is an independent commit for reviewable progress.

## Migration Plan

No data, config, or API migration. Conversion is mechanical and verified per package
(`go build`, `go vet`, `go test`). Production binary unchanged; only test packaging and new
test coverage move.

## Open Questions

- Per-package vs aggregate coverage floor → **decided: per-package ≥70%** (user choice).
- Handling browser/CDP ceiling → **decided: best-effort + documented exception.**
