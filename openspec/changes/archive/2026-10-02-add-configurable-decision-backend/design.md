# Design

## Context

The intent gate (`internal/memory/intent.go`) classifies each turn through a cheap LLM side-call resolved via the shared side-call seam (agent override → workspace memory settings → the agent's own model), bounded by `gate_budget_ms` (default 4000ms, hard context deadline) and failing open to a self-contained verdict. The TypeSafe `/v1/systemone` contract (`POST /v1/systemone`, `{state, model, questions} → {answers, usage}`) offers noul/choice/score primitives over one authenticated endpoint — see proposal.md for motivation and the spec deltas for the pinned behavior.

## Goals / Non-Goals

**Goals:**
- A per-workspace opt-in decision backend for the intent gate, with the TypeSafe contract as the first (and only) wired client.
- The LLM side-call stays the default and the fail-open/budget contract stays byte-identical across both backends.

**Non-Goals:**
- Decision backends for the curation gate, gister, or consolidator (they generate content — encoders cannot serve them).
- Entity/associative routing in decision mode (no extraction primitive in the contract).
- Embedding GLiNER2.5-Decide or any local decider into the Go binary; a future local decider would arrive as a systemone-speaking endpoint behind a `typesafe` config with a `base_url` override.
- Retries/backoff on 429/529 (the budget deadline owns latency; a retry would spend it).

## Decisions

**D1 — Backend seam inside `IntentGate`, not a new port surface.** `IntentGate` gains an optional decision client (`typesetter: a small `decisionClient` struct with endpoint/key/model); `Classify` branches: decision config stored → one systemone request; else → today's LLM path. The runner (`composeMemoryDocs`) and the `IntentVerdict` type do not change. *Alternative considered:* a `DecisionCaller` interface with LLM/HTTP implementations — rejected for v1 as ceremony; one implementation exists and the branch is two lines. If a second contract (a native GLiNER shape) ever lands, extract the port then.

**D2 — The verdict mapping is deterministic Go, not prompt text.** The systemone request is built from three fixed noul questions with instructions/criteria expressing today's `intentSystemPrompt` rules as criteria text. Threshold read: ≥ threshold (default 0.5) = yes; `needs_memory` no → zero verdict; `needs_memory` yes with all buckets no → route notes+events (mirrors `parseIntent`'s unrouted-buckets rule). Constants live beside the client; thresholds are not workspace-configurable in v1 (a knob nobody asked for — revisit on evidence).

**D3 — One attempt, local deadline, no retry.** The client wraps the call in `context.WithTimeout` with the caller's budget (already resolved per turn by `gateBudget`). 429/529 (TypeSafe's documented backoff cases) are treated as any other failure: fail open. *Alternative:* their SDK-recommended exponential backoff — rejected; the budget exists precisely to bound this call, and memory recall is recoverable next turn.

**D4 — `typesafe` rides the provider catalog, not a settings-encrypted key.** A seventh registered provider type reuses AES-256-GCM-at-rest, workspace AAD, CRUD endpoints, the verify action, and `providers.ByID`. The chat model factory never consumes it (registered as decision-class; `resolveSideCallModel` and model-catalog resolution exclude it). *Alternative:* encrypting a key into the settings record — rejected; that builds a second credential path against the plugins-are-first-class rule. The user asked for UI separation: the Providers pane tabs (web delta) deliver it without a new storage concept.

**D5 — Delete-integrity via the existing 409 lane.** Provider delete already refuses 409 when agents reference the config; the handler additionally checks the memory settings record's `decision_provider_id`. Read-through the tool-settings store; no new join or referential machinery.

**D6 — Settings validation mirrors the sidecall pair.** `memory_notes.go` gains: both fields present together or both absent (half-set → 400), provider must exist and be of the decision class (mirrors the agent handler's `providers.ByID` pre-check at `agents.go:938`). Checkbox-on-remove clears both keys server-side when the UI omits them (PATCH tri-state already established for this record).

**D7 — No migration.** `tool_settings.config` is JSONB; the two keys ride it. Schema version unchanged; smoke sections cover settings save/validation and provider CRUD instead.

**D8 — Web: tab grouping is pane-local state.** `ProvidersPane` gains a two-tab segment (Language models | Decision) filtering the already-loaded list client-side (no second API call); the dialog's type `<select>` renders `<optgroup>` per class. Model pickers filter `type !== 'typesafe'` at the one shared provider-list helper. The Memory pane's checkbox block follows the ingestion-toggle + sidecall-specific-field patterns already in the file.

## Risks / Trade-offs

- [Hosted dependency: turn text leaves the instance] → Same class as every provider call (user decision: no special treatment). `base_url` override lets a privacy-sensitive deployment point at a self-hosted systemone-compatible endpoint later.
- [Rate limits on a shared hosted endpoint] → One call per turn, sub-second, no retry; a 429 costs one turn's recall, and the warn log makes a pattern visible.
- [Threshold 0.5 misclassifies borderline turns] → Fail direction is safe: needs_memory is recall-oriented (buckets default wide); eval fixture A/B validates before a workspace relies on it.
- [Two classification code paths drift] → The verdict type and the routing/fail-open tests are shared; both backends must satisfy the same `IntentVerdict` contract in `intent_test.go`.

## Migration Plan

No migration: no schema change, no backfill, default behavior byte-identical. Rollback = clear the two settings keys (or untick the checkbox). The change ships dark until a workspace enables it.

## Open Questions

None — all design questions were resolved during exploration (associative dropped in decision mode; provider catalog for the key; no privacy callout; checkbox UX; TypeSafe contract).
