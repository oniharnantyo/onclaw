## Why

The maximum context window is a single **global** setting today: `config.max_context_tokens` (default `64000`), applied uniformly to every agent in `internal/cli/agent_session.go` (`if st.cfg.MaxContextTokens > 0 { contextWindow = … }`). There is no per-agent control — not in `store.Agent`, not in the `agents` table, not in `AgentInput`/`AgentView`, and not in the web form. Agents that target different models or providers (a small-context edge model vs. a 200k model) cannot be tuned individually; they all inherit the one global value.

This is asymmetric with `max_iterations`, which is already a **per-agent** field (`Agent.MaxIterations`) with a **global default** (`AgentConfig.MaxIterations = 20`). This change adds per-agent `max_context_tokens` in exactly that same shape: a per-agent value that, when unset, falls back to the global config default, which in turn falls back to the model's discovered context window.

(Note: the project `CLAUDE.md` states the default is `8192`; the actual default in `internal/config/defaults.go` is `64000`. The doc is stale and will be corrected as part of this change.)

## What Changes

- Add `MaxContextTokens int` to `store.Agent` (`internal/store/types.go`) and a guarded migration adding `agents.max_context_tokens INTEGER NOT NULL DEFAULT 0` (`internal/store/sqlite/db.go`), mirroring the existing `reasoning_budget_tokens` / `memory_config` guarded migrations. `0` means "inherit the global default."
- Read/write the column in the sqlite agent store (`AddAgent`, `GetAgent`, `ListAgents`, `UpdateAgent`).
- Add `MaxContextTokens` to `AgentInput` and `AgentView` (`internal/api/service/types.go`) and map it through `CreateAgent` / `UpdateAgent` / `GetAgent` / `ListAgents`.
- Resolve the effective context window in `agent_session.go` with the precedence **agent override > global config > model discovered default**: `if agent.MaxContextTokens > 0` use it; else if `cfg.MaxContextTokens > 0` use that; else use the model's discovered context window.
- Surface max context on the web agent-edit Overview form behind an **"Override max context" checkbox**. Unchecked (the default) → the agent inherits the global default and the number field is hidden/disabled; checked → the number field becomes enabled and the entered value is stored. The checkbox maps directly to the existing `0 = inherit` sentinel — unchecked stores `max_context_tokens = 0`, checked stores the entered value — so it is a pure UI affordance with **no extra schema column or storage field**. The number field has a label, tooltip, inline validation (positive integer), and a hint showing the global default; it MAY pre-fill with the selected model's discovered context window when first enabled. Saved as structured fields, never a raw-JSON textarea.
- Extend the `onclaw agent edit` partial-update surface with a `--max-context` flag (consistent with `--max-iterations`).
- Correct the stale `max_context_tokens: 8192` reference in `CLAUDE.md` to the real default (`64000`).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `agent-profiles`: the `agents` table stores a per-agent `max_context_tokens`; resolution precedence is agent override → global config → model discovered default.
- `agent-update`: `--max-context` is an updatable partial field; `0`/unset retains the inherited value.
- `web-ui`: the agent-edit form exposes max context as a checkbox-gated structured field ("Override max context" → enabled number input), where unchecked means inherit-global.

## Impact

- **Modified files**: `internal/store/types.go` (`Agent.MaxContextTokens`); `internal/store/sqlite/{db,agent}.go` (guarded migration + read/write); `internal/api/service/{types,agent}.go` (DTO + mapping); `internal/cli/agent_session.go` (resolution precedence); `internal/cli/agent.go` (or wherever `agent edit` flags live) for `--max-context`; `web/src/pages/AgentDetailPage.tsx` + the `Agent` type in `web/src/components/Agents.tsx`; `CLAUDE.md` (doc fix).
- **Schema migration** (guarded by an existence check): `agents.max_context_tokens INTEGER NOT NULL DEFAULT 0`.
- **No change** to the secrets layer, provider adapters, memory storage, or existing context-window semantics beyond adding the agent-level override ahead of the global value.
- **Backward compatible**: existing rows default to `0`, preserving today's global-driven behavior.
