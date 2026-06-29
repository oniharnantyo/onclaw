## Context

The `agent edit` command and store-level `UpdateAgent` shipped under the
`agent-owns-model-metadata` change. This change (a) reconciles its own design
with that shipped reality and (b) adds a reasoning-effort capability for
thinking models.

Shipped state (reference, not work to redo):
- **Storage**: SQLite `agents` table — name, provider, model, model_metadata
  (JSON), reasoning_effort, system_prompt, workspace, tools, max_iterations,
  created_at, updated_at (`internal/store/sqlite/db.go`).
- **Store**: `AgentStore.UpdateAgent(ctx, *Agent)` — full-struct update
  (`internal/store/store.go`, `internal/store/sqlite/agent.go`).
- **Service**: `llm.Service.UpdateAgent` wraps the store and sets
  `reloadPending` for hot-reload.
- **CLI**: `agent edit <name>` with `--model` / `--reasoning`; loads the agent,
  re-resolves metadata when the model changes, persists, and signals running
  processes (`internal/cli/agent_cmd.go`).
- **Discovery**: `modelmeta.Enumerate` + `modelmeta.Resolve` enumerate provider
  models and enrich them from the `models.dev` catalog
  (`internal/modelmeta/resolver.go`).

The codebase follows interface/types/implementation separation and prefers
formal store methods over direct SQL.

## Goals / Non-Goals

**Goals:**
- Reconcile this change's design with shipped code (no regex validation;
  full-struct update; fix the `workspace` column bug).
- Let a user set reasoning effort when assigning a *thinking* model, with valid
  values sourced from the model's `reasoning_options` in `api.json`.
- Strictly validate reasoning effort against the selected model's capabilities.

**Non-Goals:**
- Re-architecting the edit command (it already exists).
- Changing how providers enumerate models.
- Per-run reasoning overrides beyond what `onclaw run`/`chat` already accept.

## Decisions

### 1. Store update is a full-struct `UpdateAgent` (revised from the original pointer design)

**Decision:** Keep the shipped `UpdateAgent(ctx context.Context, a *Agent) error`.
The CLI loads the current agent, mutates only the flagged fields, and writes the
whole struct back. Partial-update semantics are achieved at the CLI layer.

**Rationale:** The original proposal's pointer-based `AgentUpdate` struct
(nil = skip, empty = clear) was simplified away at implementation time. The
load-mutate-save pattern is already used by `agent edit`, is simpler, and avoids
a second type. The original design is recorded here only for traceability.

**Known bug to fix:** the SQLite UPDATE statement omits the `workspace` column
(`internal/store/sqlite/agent.go`), so `--workspace` edits silently no-op. Add
`workspace = ?` to the UPDATE SET clause and its bound argument.

### 2. Model validation is discovery-based, not regex (revised)

**Decision:** A model supplied via `--model` (or chosen in the picker) is
validated against the provider's enumerated models and the cached `models.dev`
catalog. A model found in either source is accepted with its discovered
metadata. A model found in neither is still accepted on explicit manual entry,
with metadata resolved to defaults (context window 0, thinking false, text-only).
The system SHALL NOT apply provider-specific name regexes.

**Rationale:** The original regex patterns (`^claude-[23]-[5-9]-...$`) are stale
and would reject current valid models (`claude-opus-4-7`, `gpt-5.2-pro`). The
system already has authoritative model data from the provider's `/v1/models` and
the catalog, so regex adds only false negatives. This matches shipped behavior.

### 3. Reasoning effort is modeled on api.json's three option types

`api.json` advertises, per model, `reasoning: true` plus a `reasoning_options`
array whose entries are one of three types:
- `effort` — `values: ["low","medium","high", ...]` (also `minimal`, `xhigh`,
  `max`, `none`).
- `budget_tokens` — `min`/`max` integer (Claude, Gemini).
- `toggle` — on/off, no value.

**Decision — parse and carry all three.** Extend the catalog DTO
(`internal/modelmeta/types.go`) and the persisted `store.ModelMetadata` with a
`ReasoningOptions` slice mirroring these shapes. A model may expose more than one
control type (e.g. Claude exposes `effort` *and* `budget_tokens`); define a
**primary control** precedence of `effort` → `budget_tokens` → `toggle`.

```go
// internal/modelmeta/types.go
type ReasoningOptionObj struct {
	Type   string   `json:"type"`            // "effort" | "budget_tokens" | "toggle"
	Values []string `json:"values,omitempty"` // effort
	Min    int      `json:"min,omitempty"`    // budget_tokens
	Max    int      `json:"max,omitempty"`    // budget_tokens
}
type ModelObj struct {
	Limit            LimitObj             `json:"limit"`
	Reasoning        bool                 `json:"reasoning"`
	ReasoningOptions []ReasoningOptionObj `json:"reasoning_options"`
	Modalities       ModalitiesObj        `json:"modalities"`
}

// internal/store/types.go
type ReasoningOption struct {
	Type   string   `json:"type"`
	Values []string `json:"values,omitempty"`
	Min    int      `json:"min,omitempty"`
	Max    int      `json:"max,omitempty"`
}
type ModelMetadata struct {
	ContextWindow    int               `json:"context_window"`
	Thinking         bool              `json:"thinking"`
	InputModalities  []string          `json:"input_modalities"`
	ReasoningOptions []ReasoningOption `json:"reasoning_options,omitempty"`
}
```

**Persistence:** keep `Agent.ReasoningEffort string` for the effort enum (and
`on`/`off` for toggle), and add `Agent.ReasoningBudgetTokens int` for the budget
case (0 = unset). Migration:

```sql
ALTER TABLE agents ADD COLUMN reasoning_budget_tokens INTEGER NOT NULL DEFAULT 0;
```

Guarded/idempotent, like the existing `model_metadata` migration in `db.go`.

### 4. The picker prompts for effort on thinking models (interactive)

**Decision:** `pickModel` (`internal/cli/agent_models.go`), after resolving a
model whose metadata has non-empty `ReasoningOptions`, prompts for the primary
control: an enum choice (effort), an integer within `[min,max]` (budget_tokens),
or an on/off confirm (toggle). Non-thinking models get no prompt. The chosen
value is returned alongside model + metadata so `agent add`/`edit` store it.

### 5. Strict validation of reasoning effort (CLI layer)

**Decision:** `--reasoning <value>` and `--reasoning-budget <int>` are validated
before saving, against the selected model's `ReasoningOptions`:
- If the model is not a reasoning model → reject any reasoning flag.
- `effort` value must be in the model's supported `values`.
- `budget_tokens` must be within `[min,max]`.
- `toggle` accepts `on`/`off`.

An unsupported value fails with a message listing the valid options. This
replaces the prior "ignore unsupported values" policy recorded in the
`agent-profiles` spec.

**Validation sequence** (in `agent edit`/`add`):
1. Agent existence (edit only) / provider existence.
2. Model resolution + metadata (re-resolve on model change).
3. Reasoning-effort strict validation against resolved metadata.
4. Persist via `UpdateAgent`/`AddAgent`; signal running processes.

### 6. Provider mapping at build time

**Decision:** The OpenAI-compatible adapter
(`internal/llm/adapter/openai_compat.go`) is extended to map the chosen control
to the provider's native field: the full effort enum (including
`minimal`/`xhigh`/`max`) for `openai`/`openai-compatible`; `budget_tokens`
(thinking budget) for `anthropic`/`google`; toggle to enable/disable. Unmapped
values fail loudly rather than silently dropping.

### 7. Hot-reload (implemented)

`signalRunningProcess` is called after every successful add/edit, reusing the
existing SIGHUP/fsnotify mechanism. No change.

## Risks / Trade-offs

- **Strict validation may reject values users relied on.** Mitigation: the
  legacy `low|medium|high` values are a subset of most effort models' supported
  sets, so existing agents keep validating; only genuinely unsupported values
  are rejected, and the valid set is printed in the error.
- **A model's `reasoning_options` may be absent from the catalog.** Mitigation:
  the resolver falls through to defaults (no reasoning control); `--reasoning`
  is then rejected with "model is not a reasoning model or its options are
  unknown".
- **Three control types add CLI surface.** Mitigation: `--reasoning` covers the
  common effort/toggle case; `--reasoning-budget` is only needed for
  budget-tokens models.

## Migration Plan

Spec-only at this stage. Implementation tasks — catalog parsing, resolver
propagation, metadata/agent fields, the `reasoning_budget_tokens` migration, the
picker prompt, strict validation, and adapter wiring — are tracked in
`tasks.md`.
