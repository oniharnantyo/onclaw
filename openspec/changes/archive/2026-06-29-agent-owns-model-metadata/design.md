## Context

Today the **provider** profile is overloaded with model concerns: `llm_providers.model` holds a model name and `llm_providers.settings` holds its `context_window`. Yet at runtime `ResolveAgentProfile` (`internal/llm/service.go`) already overrides the profile's model with the agent's, and `run.go`/`chat.go` source the context window from the *provider's* settings — a misplacement. The `agents` table already has `provider` + `model`, so the agent is the de-facto model owner; this change makes that explicit and corrects the metadata sourcing. Model selection is also blind today (free-text), with no discovery of what a provider offers or its limits.

Constraints: onclaw targets low-resource single-board machines; SQLite is pure-Go (`modernc.org/sqlite`, ≥3.35 → `DROP COLUMN` available); there is no migration framework (migrations are guarded, idempotent `CREATE`/`ALTER` in `Migrate()`); the `add-agent-edit-command` change is in progress and provides the agent `edit` surface + `UpdateAgent`.

## Goals / Non-Goals

**Goals:**
- Provider = connection only (drop `model`).
- Agent owns its model + discovered metadata (`context_window`, `thinking`, `input_modalities`).
- Evidence-based model selection via discovery (provider API + cached `models.dev` fallback).
- Runtime sources model + context window from the agent.

**Non-Goals:**
- Multiple models per agent (one model per agent; use multiple agents).
- Switching the persistent active model via a dedicated command (per-run `--model` override remains).
- Full partial-update `agent edit` machinery (the sibling change owns that; this change adds model/metadata to add/edit).
- Touching the adapter `Build` signature.

## Decisions

**D1 — Keep `Profile.Model` as a transient (non-DB) struct field.** The adapter and the effective-profile injection pattern (`effProfile.Model = ...`) read `p.Model`. Dropping the *column* but keeping the *field* (populated at runtime from the agent) minimizes churn across `service.go`, `run.go`, `chat.go`, and the adapter. *Alternative considered:* change the `Adapter.Build` signature to accept the model explicitly — rejected as a larger ripple through the registry/stub/tests for no behavioral gain.

**D2 — DROP the `llm_providers.model` column (not deprecate).** The provider must not own a model. Guarded `ALTER TABLE ... DROP COLUMN model` (existing DBs) + updated `CREATE TABLE` (fresh DBs). *Alternative:* keep the column nullable and ignore it — rejected as leaving dead data and a misleading NOT NULL that `AddProfile` would have to satisfy.

**D3 — Store agent metadata as one `model_metadata TEXT` JSON column.** Mirrors the existing `settings` JSON pattern; flexible and avoids column sprawl. *Alternative:* discrete columns (`context_window INT`, `thinking INT`, `modalities TEXT`) — rejected as less flexible for future fields, though `context_window` could be promoted later if querying is needed.

**D4 — Layered resolver: provider-native → models.dev cache → defaults.** Maximizes accuracy where the provider tells the truth (Ollama `/api/show`, OpenRouter-class `/v1/models` `context_length`), falls back to a maintained catalog, then sane defaults. The models.dev DTO MUST match the real catalog (verified against `docs/references/api.json`): the root is keyed directly by provider id with **no** `"providers"` wrapper, and model fields are read from `limit.context`, `reasoning`, and `modalities.input`. *Alternative:* catalog-only — rejected as inaccurate for local/self-hosted models whose real context differs from the catalog.

**D5 — models.dev cache: 12h TTL + sha256 checksum-diff + atomic write + stale-on-fail.** Avoids needless rewrites when the catalog is unchanged, is crash-safe, and degrades gracefully offline. Checksum stored as a sibling file (`api.json.sha256`).

**D6 — Model resolution chain: `--model` flag → agent.Model → `config.model` → error.** Dropping the provider fallback would break the builtin master and any agent created without an explicit model; `config.model` (already a config field) is the natural global default. *Alternative:* require every agent to carry a model — rejected as regressing the "runs out of the box" master guarantee.

**D7 — Reuse `store.ModelMetadata` as the resolver's output type** (modelmeta imports store) to avoid a parallel type and an extra mapping layer.

## Risks / Trade-offs

- [DROP COLUMN needs SQLite ≥3.35] → modernc.org/sqlite bundles it; migration is guarded by `PRAGMA table_info`, so on an older engine the DROP is skipped and the column is merely ignored (field is transient), not fatal.
- [BREAKING: provider `model` removal] → existing profiles lose their model column; runtime model comes from the agent/config, so the only behavior change is for callers who relied on a provider-supplied model. Documented in proposal.
- [models.dev schema drift] → defensive parsing; unknown keys ignored; global-search fallback; defaults as the floor.
- [models.dev DTO shape must match exactly] → the initial implementation wrongly assumed a `"providers"` wrapper and flat field names (`context_window`/`thinking`/`input_modalities`), which silently left the catalog empty so every model defaulted to context window 0 (observed with an NVIDIA NIM profile). Mitigation: parse the documented provider-keyed root with `limit.context`/`reasoning`/`modalities.input`, guarded by a real-shaped JSON-parse regression test.
- [Discovery needs network at add/edit time] → cache + stale-on-fail + `--model` non-interactive path keep add working offline against the cache.
- [Dependency on `add-agent-edit-command`'s `UpdateAgent`] → if that change has not landed, this change implements a model-focused `UpdateAgent` + `agent edit` itself; the sibling change reconciles later.
- [Old agents have no metadata] → runtime falls back to `config.max_context_tokens` then 64000 until the agent's model is re-picked.

## Migration Plan

`Migrate()` (idempotent, runs at startup):
1. `llm_providers`: fresh `CREATE TABLE` omits `model`; guarded `ALTER TABLE llm_providers DROP COLUMN model` for existing DBs (skip if column absent).
2. `agents`: fresh `CREATE TABLE` includes `model_metadata TEXT NOT NULL DEFAULT '{}'`; guarded `ALTER TABLE agents ADD COLUMN model_metadata TEXT NOT NULL DEFAULT '{}'` for existing DBs.

No data backfill: existing providers simply lose the model column; existing agents get `'{}'` metadata and resolve at runtime via the fallback chain. **Rollback:** column drops are irreversible; rollback is by reverting the code (providers would need re-adding a model). Acceptable given the breaking-change designation.

## Open Questions

- Force-refresh the models.dev cache on demand (e.g. `--refresh` flag on the picker)? Deferred; the 12h TTL + checksum covers normal use.
- Should `agent edit` (this change) also edit non-model fields, or stay model-focused until the sibling change lands? Decision: model-focused now (model + metadata + reasoning), to satisfy the "change/remove model" need without duplicating the sibling's partial-update design.