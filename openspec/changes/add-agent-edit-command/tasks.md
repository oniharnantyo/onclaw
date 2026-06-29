## 0. Reconcile with shipped code

- [x] 0.1 `UpdateAgent(ctx, *Agent)` in `AgentStore` interface — shipped: `internal/store/store.go`
- [x] 0.2 `UpdateAgent` SQLite impl — shipped: `internal/store/sqlite/agent.go`
- [x] 0.3 `llm.Service.UpdateAgent` wrapping store + `reloadPending` — shipped: `internal/llm/service.go`
- [x] 0.4 `agent edit <name>` command with `--model` / `--reasoning` — shipped: `internal/cli/agent_cmd.go`
- [x] 0.5 Hot-reload signal after edit — shipped
- [x] 0.6 Model picker re-resolves metadata on model change — shipped: `internal/cli/agent_models.go`
- [x] 0.7 **Fix** the omitted `workspace` column in the UPDATE SET clause (`internal/store/sqlite/agent.go`) so `--workspace` edits take effect

> The original regex `validateModelName` plan (former §3.1-3.3 / §4.4) and the
> pointer-based `AgentUpdate` struct (former §1 / §4.8) are dropped — obsolete.

## 1. Parse `reasoning_options` from the catalog (model-discovery)

- [x] 1.1 Add `ReasoningOptionObj` and `ModelObj.ReasoningOptions` to `internal/modelmeta/types.go`
- [x] 1.2 Test the catalog parser against a real-shaped payload containing `effort`, `budget_tokens`, and `toggle` entries

## 2. Carry reasoning options in model metadata (agent-profiles)

- [x] 2.1 Add `ReasoningOption` and `ModelMetadata.ReasoningOptions` to `internal/store/types.go`
- [x] 2.2 Add `Agent.ReasoningBudgetTokens int` to `internal/store/types.go`
- [x] 2.3 Marshal/unmarshal coverage in `internal/store/models.go`
- [x] 2.4 Guarded migration `ALTER TABLE agents ADD COLUMN reasoning_budget_tokens INTEGER NOT NULL DEFAULT 0` in `internal/store/sqlite/db.go`
- [x] 2.5 Propagate `ReasoningOptions` from the catalog in `modelmeta.Resolve` (`internal/modelmeta/resolver.go`)

## 3. Interactive effort prompt (agent-update)

- [x] 3.1 Extend `pickModel` to return the chosen reasoning control alongside model + metadata
- [x] 3.2 Prompt for the model's primary control: effort enum choice / budget integer bounded by `[min,max]` / toggle on-off
- [x] 3.3 Skip the prompt for non-thinking models
- [x] 3.4 `agent add` / `agent edit` store the returned effort / budget value

## 4. Strict validation (agent-update)

- [x] 4.1 `--reasoning` accepts an effort enum value or `on`/`off`; reject on a non-thinking model
- [x] 4.2 New `--reasoning-budget <int>` flag; validate within the model's `[min,max]`
- [x] 4.3 Validation helper + unit tests (effort in/out of set, budget in/out of range, toggle, non-thinking rejection)
- [x] 4.4 Error messages list the model's valid options

## 5. Provider mapping at build time

- [x] 5.1 Extend `internal/llm/adapter/openai_compat.go` to map the full effort enum (`minimal`/`xhigh`/`max`) for `openai`/`openai-compatible`
- [x] 5.2 Map `budget_tokens` to the thinking budget for `anthropic`/`google` via eino-ext
- [x] 5.3 Map toggle to enable/disable; fail loudly on unmappable values
- [x] 5.4 Adapter unit tests

## 6. Documentation & verification

- [x] 6.1 Update `agent edit --help` / `agent add --help` for `--reasoning-budget`
- [x] 6.2 `make test`, `make vet`, `make lint`
- [x] 6.3 Smoke test: add a thinking agent via the picker (effort prompt), edit its reasoning, confirm with `agent show`
