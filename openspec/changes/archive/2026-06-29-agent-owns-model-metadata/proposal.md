## Why

The provider profile is overloaded with model concerns it does not own. `llm_providers` stores a single `model` name and stashes its `context_window` in `settings`, yet at runtime `ResolveAgentProfile` already overrides the profile's model with the *agent's* model — so the agent is the de-facto model owner, while the context window is still mis-sourced from the provider's settings (`run.go`/`chat.go`), and model selection is blind free-text with no awareness of what the provider offers or its limits. This change completes the separation: **the provider is a connection, the agent owns the model and its discovered metadata**, and selecting a model becomes evidence-based.

## What Changes

- **BREAKING**: `llm_providers` no longer stores a `model`; the column is dropped via a guarded migration. Provider profiles are connection-only (`name`, `provider_type`, `api_base`, `enabled`).
- Agents gain a `model_metadata` column carrying `context_window`, `thinking`, and `input_modalities` for the agent's chosen model.
- New **model discovery**: enumerate available models from the provider's own API (`/v1/models`, Ollama `/api/tags`) and enrich each with metadata using provider-native fields first (Ollama `/api/show`, OpenRouter-class `context_length`), then a cached `models.dev` fallback, then defaults.
- `models.dev` data is cached at `~/.onclaw/cache/api.json` with a 12h TTL, refreshed only when the fetched content's checksum differs from the stored copy.
- `agent add` / `agent edit` gain an interactive model picker (one model + its metadata per agent); `provider add` becomes connection-only (no model prompt or `--model`/`--context-window` flags).
- **Runtime** (`run.go`/`chat.go`) sources both the model name and the context window from the agent; the `--model` per-run flag still overrides for one invocation. The provider's model is no longer a fallback source.
- Extends the in-progress `add-agent-edit-command` change (depends on its `UpdateAgent`).

## Capabilities

### New Capabilities

- `model-discovery`: Discover available models from a provider's API and enrich each with metadata (`context_window`, `thinking`, `input_modalities`) using provider-native fields, falling back to a checksum-cached `models.dev` catalog.

### Modified Capabilities

- `providers`: Provider profiles no longer store a `model`; they are connection-only.
- `agent-profiles`: The agent owns its model and the model's metadata; effective-profile resolution no longer falls back to a provider model, and the runtime context window is sourced from the agent's metadata. `agent add`/`edit` select models via discovery.

## Impact

- **Code**: `internal/store/types.go` (+ new `models.go`), `internal/store/sqlite/{db,profile,agent}.go`, new `internal/modelmeta/` package (resolver + cache + http), `internal/cli/{provider_cmd,agent_cmd}.go` (+ new `agent_models.go` picker), `internal/cli/{run,chat}.go`, `internal/llm/service.go` (resolution path).
- **Database**: guarded migrations — drop `llm_providers.model`, add `agents.model_metadata`.
- **Dependencies**: depends on `add-agent-edit-command` for the agent `edit` surface and `UpdateAgent`.
- **Compatibility**: BREAKING for callers relying on a provider profile's `model` column or the `provider add` model flags; existing agents keep working with config-default context window until their model is re-picked.
