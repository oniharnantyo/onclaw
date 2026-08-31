# Add Tenant Providers Management

## Why

Agents need LLM credentials, and today there is nowhere to put them: the backend has no provider concept at all, and the web app's agent config modal offers a hardcoded provider/model list (`web/src/lib/constants.ts`). Tenants must be able to register their own provider credentials — their OpenAI key, their vLLM endpoint — so the upcoming chat/runs domains can execute agents with tenant-supplied keys. This is the first block of the AI-provider domain: a tenant-scoped credential vault plus the provider extension point (Go interface + registry) the rest of the platform builds on.

## What Changes

- **New backend capability `providers`**: tenant-scoped provider configs — CRUD under `/workspaces/:ws/providers`, six built-in provider types (`openai`, `anthropic`, `gemini`, `openrouter`, `openai-compatible`, `anthropic-compatible`), AES-256-GCM key encryption with tenant binding, live connection verification per type, and two new permissions in the closed catalog (`providers.read`, `providers.write`, granted to Owner and Admin).
- **New instance master key**: `ONCLAW_ENCRYPTION_KEY` (32-byte hex or base64), required by `server` at startup with strict refusal; `migrate`/`user`/`superadmin` commands are key-free. Rotation is out of scope: changing the key renders stored keys undecryptable, surfaced as a clear per-row error state, not a 500.
- **Web: Providers pane** in the workspace settings modal — an API-backed pane listing the tenant's provider configs with type, name, base URL, key-set state, key hint (last 4), enabled toggle, create/edit/delete, and a per-row verify action whose result is shown transiently (never persisted server-side).
- **Web: agent config modal provider options become API-driven** — the modal lists the workspace's configured providers; unconfigured types appear as disabled entries pointing at Settings → Providers, replacing the hardcoded `PROVIDERS` constant.

## Capabilities

### New Capabilities
- `providers`: Tenant-scoped provider credential management — provider type catalog (code, registry of built-ins), per-tenant provider configs with encrypted keys, live verify, and the `providers.*` permissions.

### Modified Capabilities
- `web-app/settings`: Settings navigation gains a Providers pane (eight sections now); new Providers pane requirement covering list/create/edit/delete/verify UX and the one-time key-entry discipline.
- `web-app/agents`: Provider options in the structured configuration modal SHALL come from the workspace's configured provider configs (API-driven), with unconfigured types shown as disabled entries; cascade behavior preserved.

## Impact

- **Backend code**: `internal/secrets` (new), `internal/providers` (new), `internal/domain` (entity + permissions + catalog), `internal/store` port + `store/fake`, `store/postgres`, `internal/server` (router, middleware reuse), `internal/server/handlers/providers.go` (new), `internal/cli` + `internal/config` (key validation), `.env.example`, `scripts/smoke.sh`.
- **Frontend code**: `web/src/modals/SettingsModal.tsx`, new `web/src/components/settings/ProvidersPane.tsx` (or equivalent), `web/src/lib/api.ts`, `web/src/lib/constants.ts` (PROVIDERS removed), `web/src/modals/AgentConfigModal.tsx`, `web/src/data/seed.ts`.
- **API surface**: new tenant-scoped REST resources `GET/POST /workspaces/:ws/providers`, `PATCH/DELETE /workspaces/providers/:id`, `POST /workspaces/:ws/providers/:id/verify` under existing auth + `RequireWorkspace` + `RequirePermission` middleware.
- **Ops**: `server` now fails fast without `ONCLAW_ENCRYPTION_KEY`; smoke script and integration tests supply a key. `.env.example` documents generation via `openssl rand -hex 32`.
- **Out of scope**: agent ↔ provider FK (agents domain), live model catalog probing, key rotation/re-encrypt command, audit logging, background verify scheduling, chat execution using the stored keys (chat domain's change).
*Note: rotation is not a spec-level exclusion — it's simply absent from v1; no requirement forbids it.*