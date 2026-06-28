## Context

onclaw is an on-device agent CLI for ~2 GB RAM single-board computers. `AssembleAgent`
currently carries an `onboardingActive` flag read from an `onboarding_completed` DB preference
on every `run`/`chat`, and injects an onboarding prompt sourced from a shipped `onboarding.md`
template that **no longer exists** — so `GetTemplate("onboarding.md")` is a broken, silently-
swallowed reference. Persona templates are lowercase on disk while their embedded output names
and the upstream reference repo are UPPERCASE. The eino summarization trigger is hardcoded to
6000 tokens and ignores the model's actual context window (a provider/model property).

Constraints: pure-Go static binary (`CGO_ENABLED=0`), conservative defaults, minimal per-turn
overhead. `Migrate()` supports only `CREATE TABLE IF NOT EXISTS` (no `ALTER TABLE`). The
agent's file tools today are `listdir`, `readfile`, `writefile`, `shell` — **no delete tool**.

## Goals / Non-Goals

**Goals**
- Remove the broken, per-turn onboarding-prompt machinery; replace with a master-only
  `BOOTSTRAP.md` signal where skip = defer and the agent self-clears on completion.
- Align template filenames with the UPPERCASE reference convention.
- Make summarization driven by a per-provider context window at 80%, with a 64000 fallback.

**Non-Goals**
- No new DB migration mechanism or schema change.
- No per-turn LLM timeout or response-length cap (documented as future gaps only).
- No agent-level context-window override (provider profile is the sole source).
- No removal of the `onclaw init` guided setup or `provider setup` flow.

## Decisions

### D1. Store `context_window` in the existing `settings` JSON, not a new column
`llm_providers.settings` is already parsed as `map[string]interface{}` in four places;
`openai_compat.go` already reads `settings["max_tokens"]`. A sibling `settings["context_window"]`
needs **no migration**, whereas a dedicated column would need an `ALTER TABLE` path `Migrate()`
doesn't support. *Alternative:* new `context_window INTEGER` column — rejected (no migration
mechanism; inconsistent with the settings-bag pattern).

### D2. BOOTSTRAP.md is master-only, seeded by a dedicated helper — not via generic SeedWorkspace
`SeedWorkspace` seeds persona files for **any** agent, so adding `BOOTSTRAP.md` there would
force every agent into onboarding. Instead add `agent.SeedBootstrap(workspace)` (writes
`BOOTSTRAP.md` from template if absent) and call it **only** from the `onclaw init`
master-agent path. `LoadPersonaContext` includes `BOOTSTRAP.md` whenever present, so the master
agent sees it on `run`/`chat` until it is removed. *Alternative:* inline write in `init_cmd.go`
— rejected (a named helper is testable and keeps the init path thin).

### D3. Onboarding is deferred to the first run/chat; completion is agent-driven
`onclaw init` runs **no** onboarding interview. The agent setup step seeds `BOOTSTRAP.md` into
the master workspace and leaves it there, so onboarding occurs on the first `onclaw run`/`chat`
(where the agent follows the BOOTSTRAP guidance). The CLI removes **nothing** and reads/writes
**no** `onboarding_completed` preference. The agent itself removes `BOOTSTRAP.md` when
onboarding concludes (per the template's own "delete this file" instruction). *Alternative:*
run an interview inside init — rejected (an LLM call during setup is heavyweight for the
low-resource target, and deferring keeps init fast and non-interactive beyond model selection).

### D4. Agent deletes BOOTSTRAP.md via the existing shell tool
No dedicated delete-file tool exists, and none is being added. The agent removes `BOOTSTRAP.md`
using the existing `shell` tool (e.g. `rm BOOTSTRAP.md`). This relies on shell policy: under
`allow` it runs directly; under `deny`/allowlist the user confirms at the shell-tool prompt
(acceptable during onboarding, where the user is present). The `BOOTSTRAP.md` template already
instructs the agent to delete the file. *Alternative considered:* add a workspace-scoped
`remove_file` tool (mirrors `writefile.go`) — rejected (user opted for shell to avoid new
surface area).

### D5. Swap the `AssembleAgent` parameter: `onboardingActive bool` → `contextWindow int`
Same arity, minimal call-site churn. The CLI resolves the effective window (D6) and passes it
in; `AssembleAgent` computes `int(0.8 * contextWindow)` for the trigger.

### D6. Effective window fallback chain → 64000; config default raised to 64000
`effective = provider.context_window` (>0) → else global `max_context_tokens` (>0) → else
**64000**. For "unset → 51200" to hold on a fresh install, the global `max_context_tokens`
default must be 64000 (was 8192), so `defaults.go` changes 8192 → 64000 and `config_test.go` is
updated. eino v0.9.9 `TriggerCondition` accepts only absolute `ContextTokens`, so the 80% is
computed upstream. Unset → 64000 → trigger 51200. Provider 128000 → 102400.

### D7. Case-only template rename via two-step `git mv`
macOS APFS is case-insensitive but case-preserving, so `git mv soul.md SOUL.md` can no-op. Use
`git mv soul.md _tmp.md && git mv _tmp.md SOUL.md`. `embed.FS` is case-sensitive, so the
runtime path must match exactly.

### D8. Init reordered to [Provider, Agent]; the agent step seeds the workspace and binds the model
`initSteps` becomes `[Provider Setup, Agent Setup]`. The standalone "Persona Setup" step is
removed — it wrote persona files to the wrong locations (flat `~/.onclaw` and the CWD) that
`LoadPersonaContext` never reads. The new agent setup step runs **after** provider setup, so it
can use `getOrSeedMasterAgent` (a provider now exists); it shows the master agent, binds its
model (auto if one profile, `promptChoice` if many, persisted via `UPDATE agents`), and seeds
the master workspace via the idempotent `SeedWorkspace`/`SeedBootstrap`/`SeedGlobalUser` helpers.
This also aligns init with the canonical onboarding spec ("begins with provider configuration").

## Risks / Trade-offs

- **[Stale `onboarding_completed` rows]** → Inert; never read or written again. No migration.
- **[Skip leaves BOOTSTRAP indefinitely if user never finishes onboarding]** → Acceptable and
  intended (defer); the agent will resume onboarding on the next run until it removes the file.
- **[Agent fails to delete BOOTSTRAP]** → Onboarding guidance persists; harmless (the agent
  keeps seeing first-run guidance). If shell policy denies `rm`, the user confirms at the
  shell-tool prompt; otherwise the file simply remains until a later turn.
- **[Config default 8192 → 64000]** → Only affects the now-used summarization fallback; behavior
  shift is intended. `config_test.go` updated.
- **[Summarization threshold shifts (6000 → 51200 default)]** → Intended; now configurable per
  provider.

## Migration Plan

- No database migration.
- Deploy: rename templates (two-step `git mv`), apply code + config-default changes, `make build`.
- Rollback: revert the commit; stale `onboarding_completed` rows remain inert.

## Open Questions

- Should `context_window` be settable on **existing** profiles? No `provider edit` today;
  deferred (`provider add --context-window` covers creation; optional `provider set` can follow).