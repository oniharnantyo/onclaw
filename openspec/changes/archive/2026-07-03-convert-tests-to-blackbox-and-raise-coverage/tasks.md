# Tasks

## 1. OpenSpec change

- [x] 1.1 Create `proposal.md` / `tasks.md` / `design.md` / `specs/testing-conventions/spec.md`.
- [x] 1.2 `openspec validate` passes.

## 2. Part A — black-box conversion (clean packages)

- [x] 2.1 Pilot `internal/version` to validate the recipe + build/test loop end-to-end.
- [x] 2.2 Convert Group A1 packages in lockstep (one or few per commit): `version`, `store`,
      `render`, `logging`, `config`, `llm`, `hooks`, `skill`, `modelmeta`, `secrets`,
      `store/sqlite`, `agent`, `agent/tools`, `agent/tools/browser`, `browser`, `browser/cdp`,
      `api`, `api/service`, `workspace`, `agent/middlewares`, `llm/adapter`.
- [x] 2.3 After sweep: `CGO_ENABLED=0 go build ./...`, `go vet ./...`, `go test ./...` green;
      no package's coverage below its pre-change value.

## 3. Part A — bridges / rework

- [x] 3.1 `internal/cli`: add `export_test.go` (`type AppState = appState`, `SetupTestDB`
      wrapper); convert tests to `cli_test`.
- [x] 3.2 `internal/mcp`: rework `manager_test.go` to drop the `.(*manager)` assertion; convert
      to `mcp_test`.
- [x] 3.3 `internal/observability`: add `export_test.go` (`var BuildConfig = buildConfig`);
      convert to `observability_test`.
- [x] 3.4 `internal/agent/middlewares`: convert; add a bridge only if the compiler requires.

## 4. Part B — coverage uplift to ≥70% (small gaps)

- [x] 4.1 `internal/modelmeta` 59.3→≥70, `internal/agent/middlewares` 63.1→≥70,
      `internal/browser` 67.4→≥70, `internal/agent` 61.1→≥70.

## 5. Part B — coverage uplift (medium gaps)

- [x] 5.1 `internal/mcp` 49.4→≥70, `internal/agent/tools` 54.2→≥70, `internal/skill`
      54.0→≥70, `internal/cli` 54.4→≥70.

## 6. Part B — coverage uplift (large gaps)

- [x] 6.1 `internal/api/service` 6.6→≥70 (DTO mapping/validation: `mapSkillToView`,
      `validateMCPServerInput`, `redactEnv`, `toMCPServerView`).
- [x] 6.2 `internal/llm/adapter` 4.7→≥70 (verify what is testable beyond the stub).
- [x] 6.3 `internal/browser/cdp` 18.8→≥70 (mock/loopback CDP harness; document blocker if
      infeasible).
- [x] 6.4 `internal/agent/tools/browser` 20.4→≥70 (document blocker if infeasible).

## 7. Final verification + convention

- [x] 7.1 `gofmt -l .` empty; `CGO_ENABLED=0 go build ./...`; `go vet ./...`; `go test ./...`.
- [x] 7.2 Every `internal/...` package reports ≥70.0% coverage (or a documented exception).
- [x] 7.3 `openspec validate` passes.
- [x] 7.4 Add the black-box + coverage-floor rule to `CLAUDE.md` (Conventions).
