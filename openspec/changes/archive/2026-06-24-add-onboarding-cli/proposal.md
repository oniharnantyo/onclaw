## Why

onclaw's provider/secrets/crypto layer is complete, but a brand-new user must know to
run two granular, non-interactive commands — `onclaw provider add <name> --kind … --model …`
then `onclaw provider login <name>` — before `onclaw run` works. There is no guided
first-run experience; nothing in the codebase or specs defines onboarding. The result is
needless friction and a "what do I type first?" moment at the exact point a user is
deciding whether to keep using onclaw.

## What Changes

- Add a guided, interactive **onboarding** layer over the existing `llm.Service` building
  blocks — no storage, crypto, or provider-contract changes.
- **`onclaw init`** — a top-level command that runs an ordered, extensible set of setup
  steps (today: provider setup) with a welcome banner and outro. Re-runnable and additive.
- **`onclaw provider setup`** — a new `provider` subcommand: the reusable guided "set LLM
  providers" step, also runnable standalone to add more providers later.
- **Guided flow:** choose a provider kind from a numbered menu → profile name (with a
  default) → model name (always required, no pre-filled default) → base URL (only for
  openai-compatible/ollama) → API key (hidden input, skipped for keyless ollama) → loop to
  add more → set a default provider → summary.
- **Interactive but lightweight:** line-oriented prompts via the already-present
  `golang.org/x/term` (`term.IsTerminal`, `term.ReadPassword`) + stdlib. No full-TUI
  library (no bubbletea/lipgloss/huh) — keeps the binary lean, works on headless Pi /
  serial / piped stdin, and stays within the 2 GB RAM budget.

## Capabilities

### New Capabilities

- `onboarding`: guided first-run CLI flow (`onclaw init`, `onclaw provider setup`) that
  configures one or more LLM providers and a default, interactively, without adding
  storage or crypto surface.

### Modified Capabilities

_None — onboarding composes the existing `providers` capability (`llm.Service`) and does
not change its contract._

## Impact

- **New CLI surface:** `onclaw init` (top-level) and `onclaw provider setup` (subcommand).
- **New files:** `internal/cli/prompt.go`, `internal/cli/onboard_cmd.go`,
  `internal/cli/init_cmd.go` (+ tests).
- **Edits:** `internal/cli/provider_cmd.go` (wire `setup`), `internal/cli/app.go` (wire `init`).
- **Dependencies:** none added (`golang.org/x/term` already present).
- **Out of scope:** connectivity/smoke-test verification (adapters are stubs), `onclaw run`
  auto-detection of an unconfigured state, any setup step beyond providers.