## Why

The Workspace settings surface has four soft spots: it renders beside the icon rail with no explicit way back; the "Default model" field is a placebo (a static dropdown whose value never leaves the browser and nothing consumes); the provider dialog offers no connection test before saving a new credential; and the Memory pane opens on the Facts tab with a free-text dimension field even though OnClaw supports a fixed dimension set.

## What Changes

- **Settings full-screen takeover**: on any `/settings` route the icon rail is hidden and settings owns the whole viewport; a slim header carries a `← Back` control that returns to the route the user came from (fallback `/c`). Browser back still works; sections stay deep-linkable.
- **Default model becomes functional (full-stack)**: workspaces gain `default_model {provider_id, model}` (migration + PATCH/GET), the Workspace pane picks it provider-first over configured providers with the catalog model combobox, and agents may **inherit** it: agent create/patch accept an empty provider+model pair meaning "workspace default", resolved at run start. Inherit is blocked (UI hides it, API rejects it) until a workspace default exists, and clearing the default is blocked while inherit-agents exist. The channel template-spawn fallback reads the workspace default before falling back to the hardcoded model.
- **Provider dialog draft verification**: the provider create/edit dialog gains a "Verify connection" action that tests the *unsaved* form values (type, base URL, key, catalog mapping; the stored key when editing with a blank key field) via a new draft-verify endpoint and reports the outcome inline. The save button is never locked by it — verify is a test, not a gate. The per-row Verify action stays.
- **Memory pane defaults and dimension dropdown**: the Memory pane opens on the **Configuration** tab (today it opens on Facts), and the embedding dimension becomes a dropdown over OnClaw's supported set — Auto (detect on test) / 768 / 1024 / 1536 / 2048 / 3072 — preselecting the known dimension of a chosen embedding model; Test connection remains the source of truth and keeps its mismatch guard.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `web-app/shell`: Responsive navigation — the icon rail is no longer visible on `/settings` routes (full-screen settings takeover).
- `web-app/settings`: Settings navigation — header with `← Back` on the full-screen surface; Workspace pane — default model becomes an API-backed `{provider_id, model}` pair; Providers pane — create/edit dialog gains the draft "Verify connection" action; new Memory pane requirement covering the tabbed pane (Configuration default) and the embedding dimension dropdown.
- `tenancy`: Workspace settings — PATCH accepts `default_model` with both-or-neither validation and workspace payloads carry it; clearing is refused while inherit-agents exist.
- `agents`: Provider binding — provider and model become a both-empty-or-both-set pair; the empty pair means inherit-the-workspace-default and is rejected while no workspace default exists.
- `agent-runtime`: new requirement — run-start resolution of the effective provider/model (agent pair, else workspace default) and run-time enforcement of the max-tokens requirement that save-time validation can no longer guarantee for inherit agents.
- `web-app/agents`: Model and effort selection — Step 2 offers "Workspace default" when one is set, hiding the model combobox.
- `providers`: new requirement — draft credential verification endpoint testing unsaved provider values without persisting anything.

## Impact

- **Backend**: new migration (workspace `default_provider_id`/`default_model`, agent `provider_id`/`model` nullable); workspace PATCH/GET handlers; new `POST /workspaces/:ws/providers/verify-draft`; agent create/patch validation (both-or-neither, inherit requires default, clear-default guard); runner effective-model resolution + run-time `RequiresMaxTokens` enforcement; channels template-spawn fallback; audit of `agent.ProviderID` consumers (promptgen preview, heartbeat, scheduler) to route through one resolver.
- **Frontend**: `App.tsx`/`SettingsPage` (takeover + back), `WorkspaceSection` (provider-first default model), `AgentConfigModal` (inherit option), `ProviderFormDialog` (verify action), `MemoryPane` (default tab + dimension dropdown + known-model map in `lib/embedding.ts`).
- **No breaking wire changes**: existing agents all carry explicit pairs; `default_model` is additive; the draft-verify endpoint is new. Existing tests asserting the local-only default-model select and the settings rail visibility will be updated.
