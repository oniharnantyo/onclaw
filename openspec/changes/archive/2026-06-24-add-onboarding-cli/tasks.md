# Implementation Tasks

## 1. Interactive prompt helpers (`internal/cli/prompt.go`)

- [x] 1.1 `promptString(prompt, def, r, w)` — line read, bracketed default on empty, re-prompt on empty-when-no-default
- [x] 1.2 `promptSecret(prompt, r, w)` — hidden input via `term.ReadPassword` in a TTY, line read when piped
- [x] 1.3 `promptChoice(prompt, choices, r, w)` — numbered menu, 1-based index, re-prompt on out-of-range/non-numeric
- [x] 1.4 `promptConfirm(prompt, defYes, r, w)` — y/N parsing (case-insensitive)
- [x] 1.5 Split parse/validate logic from IO; unit-testable without a TTY

## 2. Provider setup flow (`internal/cli/onboard_cmd.go`)

- [x] 2.1 `providerConfigs` catalog (kind → default base URL, promptBaseURL, keyless; no model defaults)
- [x] 2.2 `runProviderSetup(ctx, mgr, db, in, out)` loop: pick kind → name → model → base URL (conditional) → key (conditional) → add → "add another?"
- [x] 2.3 Name-collision re-prompt (via `GetProfile`); model is always required (empty → re-prompt for all kinds)
- [x] 2.4 `setDefaultProvider` — prompt when >1 provider and no default; write `default_provider` preference (mirror `provider use`)
- [x] 2.5 Summary output + `signalRunningProcess`
- [x] 2.6 Wire `setup` subcommand into `providerCommand` (`provider_cmd.go`)

## 3. Init command (`internal/cli/init_cmd.go`)

- [x] 3.1 `initStep` type + `initSteps` slice (today: one step calling `runProviderSetup`)
- [x] 3.2 `runInit` — welcome banner → steps → outro
- [x] 3.3 `initCommand(st)` builder; wire into root `Commands` (`app.go`)

## 4. Tests

- [x] 4.1 `prompt_test.go` — string/secret/choice/confirm (valid, empty, out-of-range, EOF, y/N variants)
- [x] 4.2 `onboard_cmd_test.go` — keyful happy path (profile+secret stored), keyless ollama (no secret), openai-compatible prompts base URL, name collision re-prompts, empty model rejected, add-another loop (2 providers), set-default writes preference, EOF mid-flow commits last completed
- [x] 4.3 `init_cmd_test.go` — welcome→setup→outro output; idempotent re-run
- [x] 4.4 Reuse repo patterns: `New()`/`app.Run`, `t.Setenv("ONCLAW_DB_PATH", …)`, `captureLocalStdout`, stdin-pipe override

## 5. Hardening, docs, validation

- [x] 5.1 `gofmt -l` clean; `go vet` clean; `CGO_ENABLED=0 go build ./...` green
- [x] 5.2 `go test ./internal/cli/...` green; coverage ≥ 80% for new files
- [x] 5.3 Update `README.md` onboarding / getting-started section
- [x] 5.4 `openspec validate add-onboarding-cli` passes