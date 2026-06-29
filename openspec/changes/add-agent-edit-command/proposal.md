## Why

The `agent edit` command, the store-level `UpdateAgent`, and reasoning-effort
storage already shipped under the `agent-owns-model-metadata` change — but this
change's design never caught up to that reality, and it leaves a capability gap
the user has now hit. Two problems remain:

1. **Stale design.** This change still specifies regex model validation, which
   would reject valid current models like `claude-opus-4-7` and `gpt-5.2-pro`,
   and a pointer-based `AgentUpdate` partial-update that was simplified to a
   full-struct update at implementation time (leaving a `workspace`-column bug
   where `--workspace` edits silently no-op).
2. **Missing capability.** Setting a *thinking* model on an agent gives no way
   to choose its reasoning effort, and nothing validates the value. The model
   metadata types carry only a `Thinking bool`; they never read the
   `reasoning_options` array that `api.json` (the `models.dev` catalog) provides,
   so the system cannot know which effort levels a model supports.

## What Changes

- **Reconcile** the design and tasks with shipped code: replace regex validation
  with discovery-based validation; record the full-struct `UpdateAgent` decision;
  fix the omitted `workspace` column in the SQLite UPDATE statement.
- **Add a reasoning-effort capability** for thinking models, sourced from
  `api.json`'s `reasoning_options`:
  - Parse all three option types — `effort` (enum), `budget_tokens` (min/max),
    and `toggle` — from the catalog and carry them in model metadata.
  - When a thinking model is selected, the interactive picker prompts for the
    effort level appropriate to the model's supported control.
  - Strictly validate `--reasoning` / `--reasoning-budget` against the model's
    declared options; reject effort on non-thinking models.
  - Persist the chosen value (effort enum / budget tokens / toggle) on the agent
    and map it per provider at build time.

## Capabilities

### New Capabilities

- `agent-update`: CLI command and storage layer support for updating existing
  agent configurations. Largely shipped; this change reconciles the spec and
  adds reasoning-effort validation.

### Modified Capabilities

- `agent-profiles`: Completes the "U" (Update) CRUD operation; changes the
  reasoning-effort policy from lenient ("ignore unsupported values") to strict,
  model-aware validation; extends stored model metadata to include reasoning
  options.
- `model-discovery`: The catalog parser now also reads each model's
  `reasoning_options` array (effort enum, budget_tokens range, toggle).

## Impact

- **Code**: `internal/modelmeta/{types,resolver}.go`,
  `internal/store/{types,models}.go`, `internal/store/sqlite/{agent,db}.go`,
  `internal/cli/{agent_cmd,agent_models}.go`, `internal/llm/adapter/openai_compat.go`.
- **API**: `UpdateAgent` already exists; `ModelMetadata` and `Agent` gain
  fields (`ReasoningOptions`, `ReasoningBudgetTokens`).
- **CLI**: `agent edit`/`add` gain strict reasoning-effort validation and an
  interactive effort prompt for thinking models; a new `--reasoning-budget` flag.
- **Database**: Adds a `reasoning_budget_tokens` column via a guarded migration.
- **Compatibility**: Backward compatible. Existing agents with legacy
  `low|medium|high` effort keep validating; new metadata fields are optional.
