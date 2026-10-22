---
description: "Testing conventions and coverage expectations"
---
# Testing conventions

## Coverage expectations (per vertical slice)
- `internal/domain/<feature>`: unit tests for entity behavior and domain errors.
- `internal/application/<feature>`: use-case tests with hand-written mocks for the ports.
- `internal/adapters/http`: handler tests with `net/http/httptest`.
- Storage adapters (`internal/adapters/repositories/`): keep DB-dependent tests behind a build/label pattern documented in `docs/architecture.md` — plain `make test` must run without external services.

## Style
- Table-driven tests (`t.Run(...)`) for normal/edge/error cases.
- Test files live next to the code they test (`handler_test.go` next to `handler.go`).
- Mocks are hand-written, small, and live inside `_test.go` of the consuming package — no mock framework, no generated mocks.
- Assert with `testify` (`require` in test bodies, `assert` only where meaningfully better).
- No tests-only packages, no testing code shipped in production binaries.

## Strictness
- A change without tests is incomplete: it does not count as done.
- Failure modes count as test-scenarios too (permission denied, not found, timeouts).
- `make test` includes the race detector; keep it race-free.
