# Tasks: extract-agent-runner

## 1. Runner extraction

- [x] 1.1 Create `internal/agents/runner.go` with the engine half of `agent.go` moved verbatim (L226–743): `Runner` struct (was `Agent`), `RunnerOption`s (was `AgentOption`; `With*` names unchanged), `NewRunner` (was `NewAgent`), `resolvedContextWindow`, `load`, `resolve`, `composeAgent`, `execute`, `Run`, `streamRun` — receiver `e` → `r`, no logic edits
- [x] 1.2 Truncate `internal/agents/agent.go` to the composition half: `Config`, `validateConfig`, `buildMiddlewares`, `Compose`, `skillBackend`, `offloadTranscript`; drop both section banners; prune unused imports (`io`, `log/slog`, `time`, `providers`, `secrets`, `store`, `domain`)
- [x] 1.3 Update `internal/agents/doc.go`: key type `[Runner]`, architecture wording — agent.go composes (pure build step), runner.go loads/resolves/composes-via-`Compose`/executes
- [x] 1.4 Move engine tests from `agent_test.go` to a new `runner_test.go` renamed: `TestNewAgent_DefaultsAndOptions` → `TestNewRunner_DefaultsAndOptions`, `TestAgent_Run_Validation` → `TestRunner_Run_Validation`, `TestResolvedContextWindow` (tests the moved `resolvedContextWindow`); composition tests (`TestCompose_*`) and their helpers stay in `agent_test.go`
- [x] 1.5 Update `internal/agents/history_test.go` (`setupHistoryTest` → `NewRunner`) and `internal/server/smoke_history_test.go` (`NewRunner` + `WithAgenticModelFactory` unchanged, `RouterOptions{Runner: runner}`, variable renames)

## 2. Wiring

- [x] 2.1 `internal/cli/server.go`: `agents.NewAgent(...)` → `agents.NewRunner(...)`; `RouterOptions` field `Engine: engine` → `Runner: runner`; update the construction comment
- [x] 2.2 `internal/server/router.go`: `RouterOptions.Engine *agents.Agent` → `Runner *agents.Runner`; fallback construction (:91–105) calls `agents.NewRunner`; pass through to `NewAgentHandlers`
- [x] 2.3 `internal/server/handlers/agents.go`: field `engine AgentHistoryReader` → `runner AgentHistoryReader` (interface unchanged); delete the `if h.engine == nil` empty-result branch in `ListSessionEvents`

## 3. Nil-guard cleanup

- [x] 3.1 `internal/agents/history.go`: receiver `(e *Agent)` → `(r *Runner)`; delete the `r.agents != nil` guard (keep the `req.AgentID != ""` optional-payload check) and the `r.sessionEvents == nil` early return

## 4. Verification

- [x] 4.1 `go build ./... && go vet ./... && go test ./...` green (export PATH="/usr/local/go/bin:$PATH") — no behavioral change
- [x] 4.2 Focused suites: `TestCompose_*` (composition untouched), `TestNewRunner_DefaultsAndOptions` / `TestRunner_Run_Validation`, all `TestHistory_*`, `TestAgents_ListSessionEvents`, `TestSmoke_ManualEngineRunAndHistoryEndpoint` (full Run → history pipeline)

## 5. Package cleanup

- [x] 5.1 Inline the five middleware constructor bodies from `internal/agents/middlewares/` into `buildMiddlewares` in `agent.go` (patchtoolcalls, reduction, summarization, skill, filesystem); delete the `middlewares` subpackage and its `Middleware`/`OffloadFunc` aliases
- [x] 5.2 Delete dead symbols: `ToolFilter.All()` + `denylistFilter.All()`, `agentConfig.Tools` (write-only field), `TranscriptEventKind.IsTerminal()`, `TranscriptEventToolCallRequested` and `TranscriptEventRetrying` constants; sort `Names()`/`Available()` via `sort.Strings` (fixes the `Names()` sorted-doc vs unsorted-impl mismatch)
- [x] 5.3 Dissolve `config.go`: `agentConfig` + `validateAgentConfig` + `DefaultSummarizationMargin` → `runner.go`; `FilesystemConfig`/`SkillsConfig`/`SummarizationConfig` + `DefaultMaxIterations` → `agent.go`; delete `config.go`
- [x] 5.4 Merge `instruction_composer.go` into `runner.go` (single caller `composeAgent`, spec-fixed document order, no extension axis); keep its test file; delete `instruction_composer.go`
- [x] 5.5 Update `doc.go` wording for the final layout (10 source files; middlewares inlined; extension seams and adapters standing alone)

## 6. Final verification

- [x] 6.1 `go build ./... && go vet ./... && go test ./...` green after cleanup — no behavioral change
- [x] 6.2 Confirm no references to the deleted package/symbols remain (grep `agents/middlewares`, `IsTerminal`, `\.All()`, `ToolCallRequested`, `EventRetrying`, `instruction_composer.go`, `config.go` symbols)
