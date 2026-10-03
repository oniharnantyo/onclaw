# Proposal

## Why

The memory intent gate classifies every turn through an LLM side-call. A new wave of small "decision models" (TypeSafe's hosted Jev, and encoder deciders like GLiNER2.5-Decide behind the same contract) performs typed classification in a single forward pass — cheaper, faster, and more predictable than an LLM call for exactly this shape of decision. Workspaces should be able to route the intent gate to one without giving up the LLM path.

## What Changes

- Add a workspace-level **decision backend** for the memory intent gate, configured on the existing memory settings record: `decision_provider_id` + `decision_model` (absence = today's LLM side-call behavior, unchanged as the default).
- Add a **TypeSafe `/v1/systemone` decision client** in `internal/memory`: one request per classified turn carrying noul questions (`needs_memory`, `bucket_notes`, `bucket_events`), thresholds defaulting to 0.5, one attempt inside the existing `gate_budget_ms` deadline, failing open exactly like the LLM gate.
- In decision mode the **associative route is unavailable** (TypeSafe has no extraction primitive): the decider request omits the associative question, and the intent gate routes only notes/events. Entity-associative routing remains an LLM-backend capability.
- Add the **`typesafe` provider type** to the provider catalog: key required, canonical base URL `https://api.typesafe.ai/v1/systemone` (overridable), verify probes the systemone endpoint, deletion blocked while a workspace's decision configuration references it.
- Web: the **Providers pane** gains internal tabs — "Language models" (default) and "Decision" — with `typesafe` configs visible only under Decision; the provider dialog's type select groups the new type under Decision; decision providers are **excluded from every model picker** (agent config, workspace default model, memory side-call selects).
- Web: the **Memory configuration pane** gains a "Decision backend" block: an unchecked-by-default checkbox ("Use decision backend") that reveals provider (Decision-tab providers only) and model (default `jev-latest`) fields; save-time validation mirrors the side-call "Specific model" flow.
- No schema migration: the decision fields ride the existing memory settings JSONB record. No privacy callout: the decider call is treated like any other provider call.

## Capabilities

### New Capabilities

- `agent-memory-decision-backend`: the workspace decision configuration (settings record fields, checkbox semantics, absence-is-defaults, save-time validation) and the TypeSafe decision-client contract (single systemone request, noul mapping, thresholds, budget deadline, fail-open).

### Modified Capabilities

- `agent-memory-retrieval`: the intent gate's classification source becomes configurable — LLM side-call (default) or decision backend; in decision mode the gate routes only notes/events and never proposes the associative route.
- `providers`: provider type catalog grows from six to seven built-in types with `typesafe` (decision provider); key required, canonical base URL default, systemone verify probe, and delete blocked while referenced by the workspace decision configuration.
- `web-app/settings`: Providers pane gains internal tab grouping (Language models | Decision) with decision providers excluded from model pickers; Memory configuration pane gains the Decision backend checkbox block.

## Impact

- **Backend:** `internal/memory` (new decision client + intent-gate backend seam), `internal/agents/runner.go` (wiring only — `composeMemoryDocs` unchanged), `internal/server/handlers/memory_notes.go` (settings validation), providers domain/registry/verify, `internal/cli/server.go` composition.
- **Frontend:** `ProvidersPane.tsx` (tabs), `ProviderFormDialog.tsx` (type group), `MemoryPane.tsx` (decision block), model-picker filtering.
- **API:** memory settings record gains `decision_provider_id`/`decision_model` keys; provider create/verify accept `type: "typesafe"`. No new endpoints, no migration.
- **Contracts:** TypeSafe `/v1/systemone` (external, hosted — the key leaves the instance like any provider call).
- **Verification:** `internal/memory/eval` fixture A/B (decider vs LLM gate on recall/overall) validates the mapping before a workspace flips the checkbox; new smoke sections for settings validation and provider CRUD.
