# Tasks

## 1. Rename persona templates to UPPERCASE

- [x] 1.1 Rename the 7 files in `internal/agent/templates/` to UPPERCASE using the two-step `git mv` (agents→AGENTS, bootstrap→BOOTSTRAP, capabilities→CAPABILITIES, identity→IDENTITY, memory→MEMORY, soul→SOUL, user→USER)
- [x] 1.2 Update `internal/agent/embed.go` `SeedWorkspace` map values and `SeedGlobalUser` `GetTemplate("user.md")`→`"USER.md"` (and the doc-comment example). Do NOT add BOOTSTRAP to the `SeedWorkspace` map (see 2.3).
- [x] 1.3 Update `internal/cli/init_cmd.go` `GetTemplate` calls (`"user.md"`→`"USER.md"`, `"agents.md"`→`"AGENTS.md"`)
- [x] 1.4 Update `internal/agent/embed_test.go` `GetTemplate(...)` args to UPPERCASE
- [x] 1.5 Verify `go test ./internal/agent/ -run TestGetTemplate` resolves the UPPERCASE names

## 2. Remove onboarding prompt checker; master-only BOOTSTRAP signal (skip = defer; agent deletes)

- [x] 2.1 In `internal/agent/agent.go`, delete the onboarding ensure/read block (~lines 29-47) and the `onboardingActive` injection branch (~55-58); drop the `onboardingActive bool` param from `AssembleAgent`
- [x] 2.2 Add `BOOTSTRAP.md` to `workspaceFiles` in `LoadPersonaContext` (`internal/agent/context.go`), as the first entry
- [x] 2.3 Add a `SeedBootstrap(workspace string) error` helper in `internal/agent/embed.go` (writes `BOOTSTRAP.md` from template if absent); do NOT add BOOTSTRAP to the generic `SeedWorkspace` map
- [x] 2.4 In `internal/cli/init_cmd.go` master-agent path, call `agent.SeedBootstrap(masterAgent.Workspace)` so BOOTSTRAP is seeded for the master only
- [x] 2.5 In `internal/cli/init_cmd.go` `runAgentInterview`: keep the "run interview? (y/n)" prompt; on "no" leave `BOOTSTRAP.md` in place (defer). Remove ALL `onboarding_completed` reads/writes. Do NOT `os.Remove` BOOTSTRAP — deletion is the agent's responsibility.
- [x] 2.6 In `internal/cli/run.go` and `internal/cli/chat.go`, remove the `onboardingCompleted`/`onboardingActive` block and the `AssembleAgent` arg
- [x] 2.7 Agent deletes `BOOTSTRAP.md` via the existing `shell` tool (e.g. `rm BOOTSTRAP.md`) on completion; no new tool. Ensure the master agent's shell policy permits it (or that the user can confirm at the shell-tool prompt). The `BOOTSTRAP.md` template already instructs the agent to delete the file.
- [x] 2.8 Tests: delete `TestAssembleAgent_OnboardingPrompt` and the `onboarding.md` assertion in `TestGetTemplate`; drop the trailing bool from remaining `AssembleAgent(...)` calls; add a `SeedBootstrap` test and a `LoadPersonaContext`-includes-BOOTSTRAP test

## 3. Provider context window + 80% summarization (64k fallback)

- [x] 3.1 Add `--context-window` IntFlag to `provider add` (`internal/cli/provider_cmd.go`); marshal `{"context_window": N}` into `p.Settings` when > 0
- [x] 3.2 Change `AssembleAgent` last param to `contextWindow int`; compute `triggerTokens := int(float64(contextWindow) * 0.8)` and set `TriggerCondition{ContextTokens: triggerTokens}` (replacing hardcoded 6000)
- [x] 3.3 In `run.go`/`chat.go`/`init_cmd.go`, resolve effective window: provider `context_window` (>0) → else `st.cfg.MaxContextTokens` (>0) → else 64000; pass to `AssembleAgent`
- [x] 3.4 `internal/config/defaults.go`: change `MaxContextTokens` default 8192 → 64000; update `internal/config/config_test.go` assertion (8192 → 64000)
- [x] 3.5 `onclaw provider list` (`provider_cmd.go`): parse each profile's Settings and print `context_window: <n>` when set, else `context_window: (default)`
- [x] 3.6 Tests: `AssembleAgent(...)` calls pass an int; add a trigger-math test (128000 → 102400, unset/64000 → 51200); add a `provider list` shows-context-window test

## 4. Verification

- [x] 4.1 `make vet` and `make build` (`CGO_ENABLED=0 go build ./...`) succeed
- [x] 4.2 `make test` passes (esp. `./internal/agent/...`, `./internal/cli/...`, `./internal/config/...`)
- [x] 4.3 `openspec validate lightweight-agent-cleanup --strict` passes
- [x] 4.4 Manual: `onclaw provider add demo --kind openai --model gpt-4o --context-window 128000` then `onclaw provider list` shows `context_window: 128000`; `onclaw init` seeds master `BOOTSTRAP.md` (declining the interview leaves it; accepting lets the agent delete it on completion); `onclaw run`/`chat` load BOOTSTRAP until the agent removes it and issue no `onboarding_completed` query; summarization trigger is 51200 when the window is unset

## 5. Reorder `onclaw init`: Provider/Model first, then Agent step (no in-init interview)

- [x] 5.1 Reorder `initSteps` (`internal/cli/init_cmd.go`) to `[Provider Setup, Agent Setup]`; remove the standalone "Persona Setup" step.
- [x] 5.2 Replace `runAgentInterview` with `runAgentSetup`: `getOrSeedMasterAgent` → print the master agent (name, workspace, provider) → bind model via `mgr.ListProfiles` (auto if 1, `promptChoice` if >1, persist with the existing `UPDATE agents SET provider` pattern) → `agent.SeedWorkspace` + `agent.SeedBootstrap` + `agent.SeedGlobalUser`. No `BuildWithProfile`/`AssembleAgent`/`RunAgent`/interview.
- [x] 5.3 Delete `runPersonaSetup` (its seeding is absorbed into `runAgentSetup` at the correct `~/.onclaw/workspace/master` location).
- [x] 5.4 Update `TestInitCommand_Integration` (`init_cmd_test.go`): adjust the piped stdin (no "run interview? n"); with one provider the agent step auto-binds; keep the `workspace/master/BOOTSTRAP.md` assertion; assert the master agent's `Provider` is set.
- [x] 5.5 Remove `TestPersonaSetup`; optionally add a `runAgentSetup` seeding test (persona files under `~/.onclaw/workspace/master/`, none in flat `~/.onclaw/`).
- [x] 5.6 `make vet/build/test` pass; `openspec validate lightweight-agent-cleanup --strict`; manual `onclaw init` (provider first; agent step binds model + seeds; no LLM during init; first `run`/`chat` resumes onboarding).