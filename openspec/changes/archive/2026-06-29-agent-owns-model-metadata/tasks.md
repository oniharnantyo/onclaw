# Tasks

## 1. Store data model & migrations

- [x] 1.1 `internal/store/types.go`: add `ModelMetadata` struct (ContextWindow, Thinking, InputModalities); mark `Profile.Model` as a transient runtime field (no longer DB-backed); add `ModelMetadata string` to `Agent`.
- [x] 1.2 `internal/store/models.go` (new): `MarshalModelMetadata` / `UnmarshalModelMetadata` helpers (keep `types.go` DTO-only).
- [x] 1.3 `internal/store/store.go`: add `UpdateAgent(ctx, a *Agent) error` to the `AgentStore` interface.
- [x] 1.4 `internal/store/sqlite/db.go`: guarded `ALTER TABLE llm_providers DROP COLUMN model` + guarded `ALTER TABLE agents ADD COLUMN model_metadata TEXT NOT NULL DEFAULT '{}'`; update `CREATE TABLE` for both (fresh DBs).
- [x] 1.5 `internal/store/sqlite/profile.go`: remove `model` from INSERT/SELECT; drop the model-required validation in `AddProfile`.
- [x] 1.6 `internal/store/sqlite/agent.go`: thread `model_metadata` into AddAgent INSERT and GetAgent/ListAgents SELECT; implement `UpdateAgent` (writes model + model_metadata + reasoning_effort + system_prompt + tools + max_iterations, preserves created_at/workspace).
- [x] 1.7 `internal/llm/service.go`: add `UpdateAgent` service wrapper with reload flag.

## 2. modelmeta package (discovery + cache)

- [x] 2.1 `internal/modelmeta/types.go`: `models.dev` JSON DTOs (`apiJSON`, `providerObj`, `modelObj`).
- [x] 2.2 `internal/modelmeta/http.go`: shared conservative-timeout `*http.Client`; GET models.dev, GET `{base}/v1/models`, GET `{base}/api/tags`, POST `{base}/api/show` (Bearer auth where required).
- [x] 2.3 `internal/modelmeta/cache.go`: `CacheDir()` (`~/.onclaw/cache`); load + 12h TTL refresh with sha256 checksum-diff, atomic temp+rename write, stale-on-network-failure.
- [x] 2.4 `internal/modelmeta/resolver.go`: `Enumerate(providerType, baseURL, apiKey)` + layered `Resolve(...)` (provider-native → models.dev cache, incl. global search → defaults), returning `store.ModelMetadata`.
- [x] 2.5 `internal/modelmeta/cache_test.go` and `resolver_test.go`: TTL expiry, checksum-equal short-circuit, atomic write, stale-on-fail; layering, global-search fallback, defaults.
- [x] 2.6 Fix the models.dev DTO to match the real catalog shape (verified against `docs/references/api.json`): root keyed directly by provider id — **no** `"providers"` wrapper; model context window at `limit.context`, thinking at `reasoning`, input modalities at `modalities.input`. Add a real-shaped JSON-parse regression test (the existing in-Go catalog test masked the mismatch, which silently left the catalog empty and made every model default to context window 0).

## 3. Provider CLI (connection-only)

- [x] 3.1 `internal/cli/provider_cmd.go`: rewrite `provider add` to interactive connection-only flow (name → kind → base_url → api_key); remove `--model`/`--context-window` flags.
- [x] 3.2 `internal/cli/provider_cmd.go`: simplify `provider list` to connection info only (drop model/context_window display).
- [x] 3.3 Verify `onboard_cmd.go` `runProviderSetup` no longer passes a model to `store.Profile` (it must still build providers without a model).

## 4. Agent CLI (model picker + edit)

- [x] 4.1 `internal/cli/agent_models.go` (new): `pickModel(ctx, mgr, providerName, in, out)` — enumerate + enrich + `promptChoice` single-select + optional context-window override; returns (modelID, store.ModelMetadata).
- [x] 4.2 `agent add`: run `pickModel` interactively when `--model` is absent; resolve metadata non-interactively via `modelmeta.Resolve` when `--model` is present; store `Model` + `ModelMetadata`.
- [x] 4.3 `agent edit`: add command (model re-picker via `mgr.UpdateAgent`; supports model/metadata/reasoning) — satisfies "change/remove model".
- [x] 4.4 `agent list` / `agent show`: render model + metadata (context window, thinking, modalities).

## 5. Runtime sourcing from agent

- [x] 5.1 `internal/cli/run.go`: model chain becomes `--model` → `agent.Model` → `config.Model` → error (drop `profile.Model` fallback); source context window from `agent.ModelMetadata` → `config.MaxContextTokens` → 64000.
- [x] 5.2 `internal/cli/chat.go`: apply the same model-chain and context-window sourcing changes.

## 6. Verification

- [x] 6.1 `make fmt && make vet && make lint`.
- [x] 6.2 `make test`; update existing tests that constructed profiles with a persisted `model` or relied on the provider model fallback; add runtime context-window-from-agent tests.
- [x] 6.3 Manual E2E against local Ollama: provider add (connection-only) → agent add (picker, metadata stored) → run (agent-sourced model + context window) → agent edit (re-pick) → cache TTL/checksum/offline paths → migration on an existing DB.