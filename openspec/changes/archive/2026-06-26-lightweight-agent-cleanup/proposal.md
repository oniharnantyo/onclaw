## Why

onclaw targets low-resource single-board computers (~2 GB RAM), so every unnecessary per-turn cost matters. Three issues work against that goal today: (1) the onboarding subsystem is **half-broken** — `agent.go` calls `GetTemplate("onboarding.md")` against a template that no longer exists (orphan deleted), and a first-run `onboardingActive` flag is plumbed through four files and read from a DB preference on every `run`/`chat`; (2) persona template filenames are lowercase on disk while the reference convention (`openclaw/openclaw` and `docs/references/goclaw_md_files.md`) is **UPPERCASE**, so source and output names disagree; (3) the summarization trigger is hardcoded to 6000 tokens and ignores the model's actual context window, which is genuinely a property of the provider/model. This change lightens the agent, aligns the template convention, and makes context-window-driven summarization real and per-provider.

## What Changes

- **Remove the onboarding prompt checker.** Delete the onboarding ensure/read/inject block in `AssembleAgent` (removes the broken `GetTemplate("onboarding.md")` references) and drop the `onboardingActive` parameter.
- **Reorder `onclaw init` to provider-first, then an agent step.** `init` runs Provider Setup first, then an Agent Setup step that shows the master agent, binds its model (auto if one profile, choose if many), and seeds the master workspace. The standalone Persona Setup step is removed (its seeding folds into the agent step at the correct location), and init no longer runs an onboarding interview (deferred to the first `run`/`chat`).
- **BOOTSTRAP.md becomes the onboarding signal — master workspace only.** A dedicated helper seeds `BOOTSTRAP.md` into the **master** agent's workspace from the `onclaw init` master path (NOT the generic `SeedWorkspace` used for all agents). `LoadPersonaContext` includes it whenever present. Onboarding is **deferred** to the first `run`/`chat` (init runs no interview), so the file stays and onboarding continues there. The **agent itself** removes `BOOTSTRAP.md` via its workspace tooling when onboarding completes — the CLI no longer deletes it and no longer reads or writes an `onboarding_completed` preference.
- **Rename all persona templates to UPPERCASE.** `internal/agent/templates/{agents,bootstrap,capabilities,identity,memory,soul,user}.md` → UPPERCASE, with all embed/`GetTemplate` references updated.
- **Provider profiles carry a context window.** Add an optional `context_window` setting (in the existing `Settings` JSON bag) set via `provider add --context-window`. `onclaw provider list` displays it (or the default) for each profile. **No DB migration.**
- **Summarization triggers at 80% of the effective context window.** Effective window = provider `context_window` (>0) → else global `max_context_tokens` (>0) → else **64000**. Default (unset) → 64000 → trigger **51200** (replaces hardcoded 6000). The global `max_context_tokens` default is raised 8192 → 64000 so the fallback math holds.
- **Document remaining cost-control gaps** (per-turn LLM timeout, uniform response-length cap) as report-only; no code in this change.

## Capabilities

### New Capabilities

_None._

### Modified Capabilities

- `onboarding`: First-run onboarding is signaled by a `BOOTSTRAP.md` seeded into the **master** workspace only. The `onboardingActive` prompt-injection path and the `onboarding_completed` preference are removed entirely. Skipping the interview defers onboarding (file remains). The agent removes `BOOTSTRAP.md` itself on completion. `LoadPersonaContext` loads the full persona set (including `BOOTSTRAP.md`) by default.
- `providers`: Provider profiles gain an optional `context_window` setting (in the `Settings` JSON bag) declaring the model's context window; `provider list` displays it. Summarization triggers at 80% of the effective window (provider → global → 64000 fallback).

## Impact

- **Code:** `internal/agent/{embed,context,agent}.go`; `internal/cli/{init_cmd,run,chat,provider_cmd}.go`; `internal/config/defaults.go`; tests in `internal/agent/{agent,embed}_test.go` and `internal/config/config_test.go`. (No new tool — the agent deletes `BOOTSTRAP.md` via the existing `shell` tool.)
- **Config default change:** `max_context_tokens` default 8192 → 64000 (`defaults.go`); `config_test.go` assertion updated to match. No config-schema change.
- **Internal API change:** `AssembleAgent` signature — drops `onboardingActive bool`, gains `contextWindow int`. Internal callers/tests only.
- **No DB migration.** `context_window` rides the existing `llm_providers.settings` JSON. The `onboarding_completed` preference key is no longer read or written; existing rows are inert.
- **Agent tooling:** the agent deletes `BOOTSTRAP.md` via the existing `shell` tool (no new tool); depends on shell policy permitting it (the user confirms at the prompt otherwise).
- **Templates rename** is a case-only rename on macOS APFS — requires the two-step `git mv` form.
- **Dependencies:** none added or changed.
