## Why

The web **Agent detail page** (`web/src/pages/AgentDetailPage.tsx`) silently loses or omits agent data on save. The backend and schema are correct — `UpdateAgent` persists `reasoning_effort`, `reasoning_budget_tokens`, `workspace`, and `model_metadata`, and all columns exist. Three distinct data losses happen entirely on the frontend, and two of them already violate requirements in the `agent-update` and `agent-workspace` specs:

1. **Reasoning effort and budget are wiped on edit-load.** A `useEffect` clears `reasoning_effort` and `reasoning_budget_tokens` whenever it decides the selected model "does not support reasoning." Until the async `/api/providers/{name}/models` response arrives, the effect falls back to a hardcoded `o1`/`o3` model-name prefix check that is `false` for every other reasoning model (Claude, Gemini, DeepSeek, Qwen, GLM, …). So the loaded value is erased before the user touches anything, and the field is also hidden (`reasoningSupported && …`), so the user cannot even see it to re-enter. Saving from the **Memory tab** (`saveMemoryConfig`) re-spreads `agentForm`, so it persists the wiped value too. The name-prefix heuristic directly violates `agent-update`'s "The system SHALL NOT apply provider-specific name regexes," and the wipe violates the requirement that unspecified fields retain their values.

2. **`model_metadata` goes stale and is never authored.** The form loads `model_metadata` once from the stored row and round-trips it in the payload, but when the user changes the model — or creates an agent fresh — the selected entry's discovered metadata is never written back. The Agents list card derives its "Reasoning supported" badge from `JSON.parse(model_metadata).thinking`, so a UI-created reasoning agent never shows the badge. This violates `agent-update`'s "When the model changes, the system SHALL re-resolve and store the model's discovered metadata."

3. **Workspace has never been editable from the UI.** There is no input bound to `workspace` anywhere in the form (`git log -S 'agent-workspace'` is empty), so agents created via the UI always have an empty workspace and an existing workspace cannot be viewed or changed. `workspace` is a first-class editable property in `onclaw agent edit` and in the `agent-workspace` spec, and `web-ui` requires that "Field semantics SHALL match the `onclaw agent` subcommands."

This change brings the web agent-edit surface into conformance with the existing `agent-update`, `agent-workspace`, and `web-ui` specs. It is **frontend-only** — no schema, store, service, or handler change is required, because those layers already persist every field correctly.

## What Changes

- **Stop the reasoning wipe.** Compute `reasoningSupported` as a render-time value used only to show/hide and label the reasoning controls; it SHALL NOT mutate loaded form state. A stored `reasoning_effort`/`reasoning_budget_tokens` is preserved across an edit session regardless of whether model metadata has loaded yet.
- **Drop the model-name regex heuristic.** Reasoning capability is determined solely from the live `/api/providers/{name}/models` metadata (`thinking` flag + `reasoningOptions`), mirroring the CLI's `modelmeta` resolution. A custom/free-text model that is absent from enumeration is treated as the CLI treats an unknown model — accepted, non-reasoning by default, with its metadata resolved to defaults — never via a name-prefix guess.
- **Re-resolve `model_metadata` on model change.** When the user selects a model from the picker (or enters a custom one), the form writes that entry's discovered metadata (context window, thinking, modalities, reasoning options) into `model_metadata` so it is persisted on save and on create. On create, `model_metadata` is seeded from the picked model instead of `{}`.
- **Add a structured Workspace field** to the Overview form: a text input bound to `workspace`, with a tooltip and hint explaining that empty resolves to the agent default (`~/.onclaw/workspace/<agent>/`) per the `agent-workspace` resolution precedence. It is saved like any other structured field (no raw-JSON textarea).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `web-ui`: the agent-edit form preserves all persisted agent fields across a save (reasoning effort/budget not wiped, model metadata re-resolved on change) and exposes workspace as a structured editable field — bringing the web surface into conformance with the existing `agent-update`, `agent-workspace`, and `web-ui` (structured-fields, field-semantics-match-CLI) requirements.

## Impact

- **Modified files**: `web/src/pages/AgentDetailPage.tsx` (remove the reasoning-wipe `useEffect` and the `o1`/`o3` fallback; re-resolve `model_metadata` on model change; add the Workspace field); possibly `web/src/components/Agents.tsx` if the reasoning badge's fallback heuristic needs to stop guessing from model names.
- **No backend, store, schema, or config changes** — `AgentInput`, `AgentView`, `store.Agent`, the `agents` table, and `UpdateAgent` already carry and persist all of `reasoning_effort`, `reasoning_budget_tokens`, `workspace`, and `model_metadata`.
- **No migrations.**
- **No change** to CLI behavior, the provider-adapter surface, or the secrets layer.
