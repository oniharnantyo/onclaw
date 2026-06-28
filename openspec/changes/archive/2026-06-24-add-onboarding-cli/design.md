## Context

onclaw is a remote-brain agent: the LLM runs off-device; the on-device static Go binary
(`CGO_ENABLED=0`, 2 GB RAM target, headless) holds everything else. The provider/secrets/
crypto layer (`add-provider-secrets-storage`, now archived) is complete: `llm.Service`
exposes `AddProfile`, `SetSecret`, `ListProfiles`, `GetProfile`, `RemoveProfile`; a
`provider_type`-keyed adapter registry exists (kinds: anthropic, openai, openai-compatible,
ollama); `getProviderManager` (`internal/cli/context.go`) auto-initializes keyfile mode on
first run, so encrypted storage works out-of-the-box with no key step.

Current first-run reality: a user must discover and run `provider add` (3 flags) then
`provider login` (hidden key) before `run` does anything useful. No guided path exists.

Hard constraints: 2 GB RAM, headless (serial/SSH, no keychain), static CGO-free binary,
existing convention that adding a provider hot-reloads a running session without restart.

## Goals / Non-Goals

**Goals:**
- Collapse `provider add` + `provider login` into one guided experience so a new user
  reaches a configured default provider in a single command.
- Keep it interactive without compromising the lightweight-agent constraint (no TUI dep,
  works on serial/piped terminals, kilobytes of RAM).
- Make the provider step reusable (`provider setup`) and the orchestrator extensible
  (`init`) so future steps drop in.

**Non-Goals:**
- Verifying a provider works (smoke test) — blocked until adapters stop being stubs.
- `onclaw run` detecting an unconfigured state and auto-launching onboarding.
- Any setup step beyond providers (key-mode choice, workspace/config, etc.).
- Changing the `providers` capability contract, storage, or crypto.

## Decisions

### D1 — Two commands: `init` orchestrates, `provider setup` is the reusable step
`onclaw init` is the discoverable entry point; `onclaw provider setup` is the guided
"set providers" step that `init` invokes and that can also run standalone to add more
providers later. This separates an orchestrator from composable pieces and keeps
`provider setup` useful beyond first run.

_Alternatives considered:_ a single `onclaw init` that inlines everything (rejected — the
provider step is valuable standalone); a guided mode bolted onto `provider add` (rejected —
`add` stays the non-interactive/scripting surface; `setup` is its interactive counterpart).

### D2 — `init` is an ordered slice of step functions (extensible, not over-built)
`init` runs `[]initStep`; today a single step (provider setup). New steps (key mode,
workspace, smoke test) append to the slice without redesign. This is a trivial scaffold
(truthful to the "current step is providers" framing) without a heavy plugin framework.

### D3 — Line-oriented interactive, via existing `golang.org/x/term` (NO TUI library)
Prompts, defaults, numbered menus, hidden secrets, and y/N confirms are built from `fmt`
+ stdlib + the already-present `golang.org/x/term` (`term.IsTerminal` for TTY detection,
`term.ReadPassword` for hidden input — the same idiom `provider login` and
`resolvePassphrase` already use). No bubbletea/lipgloss/huh/etc.

This is a deliberate lightweight-agent decision: a full TUI adds 1–3 MB + a render loop,
assumes a rich terminal (alternate screen, cursor addressing) that flickers or breaks on a
headless serial console or minimal SSH session, and cannot be piped for automation.
Line-oriented prompts work on serial/SSH/tmux/pipe, cost kilobytes of RAM, and stay
scriptable (`printf … | onclaw init`). The only thing given up is arrow-key navigation and
spinners, which are undesirable on a 2 GB headless box anyway.

### D4 — A small CLI catalog carries base-URL/keyless flags; models are never pre-filled (adapters carry no metadata)
The adapter registry's `DefaultAdapters` registers bare stubs with no default-model,
keyless, or base-URL metadata. So the guided flow uses a small bounded catalog in the CLI
(`providerConfigs`): whether to prompt for a base URL, a default base URL for local
providers, and whether the kind is keyless. The model is **never** pre-filled for any kind
— the user always types it — so onboarding ships no stale model defaults. When adapters
grow real metadata later, the catalog can defer to them.

Catalog (current as of 2026-06): anthropic — prompt base URL: no, keyless: no; openai —
prompt base URL: no, keyless: no; openai-compatible — prompt base URL: yes, keyless: no;
ollama — base URL `http://localhost:11434`, keyless: yes. Model: always required, always
user-entered (no defaults).

### D5 — Defaults in `[brackets]` where they exist; the model is always entered
The profile name defaults to the chosen kind, and ollama's base URL defaults to
`http://localhost:11434`, each shown in brackets and accepted on Enter. The model has no
default and is always required, so empty input re-prompts. The common path (anthropic) is
`1↵ ↵ <type model>↵ <paste key>↵ N↵`.

### D6 — Loop to add multiple providers, then set a default
After each provider the flow asks "Add another? [y/N]". When more than one provider exists
and no default is set, the flow prompts to choose one (writing the `default_provider`
preference, exactly as `provider use` does). With a single provider the default is set
silently.

### D7 — Commit per provider; interruption loses only the in-progress one
Each provider is committed immediately (`AddProfile` + `SetSecret`). EOF/Ctrl-C mid-flow
leaves all previously completed providers persisted and loses only the one in progress —
no transaction spans multiple providers, no partial row.

### D8 — IO-injected flow for testability
The flow functions take `io.Reader`/`io.Writer`; the commands pass `os.Stdin`/`os.Stdout`.
Tests inject pipes. Prompt parse/validate logic is split from IO so it is unit-testable
without a TTY, and integration tests reuse the repo's existing stdin-pipe-override pattern
(`cli_test.go` `TestResolvePassphraseInteractive`).

## Risks / Trade-offs

- **[Defaults age]** Catalog model defaults drift as providers release models → documented
  as UX hints only; trivially editable; adapters will eventually own this.
- **[Pipable means no strict terminal validation]** → intentional; users who need
  flag-based scripting already have `provider add`.
- **[No verification]** A user can enter a wrong key/model and only learn at first `run` →
  accepted for v1 (friction-reducer scope); verification deferred until adapters are real.
- **[Discovery]** A new user must still learn `onclaw init` exists → out of scope for v1;
  future `run`-aware hinting is the natural follow-up.

## Migration Plan

Greenfield CLI surface — no data migration. `init`/`setup` are additive over the existing
provider store; existing profiles are never modified. First run of `onclaw init` on a fresh
DB triggers `getProviderManager`'s existing keyfile auto-init (no new key logic).

## Open Questions

- Catalog default models → **decided: no model pre-fill for any kind; the user always
  enters the model.**
- `init` re-run behavior → **decided: always additive** (re-running adds more providers,
  never modifies existing ones).