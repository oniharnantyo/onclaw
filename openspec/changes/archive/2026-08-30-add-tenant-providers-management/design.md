# Design — Tenant Providers Management

## Context

The backend currently covers identity, tenancy, and the instance-admin control plane (`internal/` has `auth`, `bootstrap`, `cli`, `config`, `domain`, `server`, `storage`, `store`); the store layer is a set of narrow ports (`UserStore`, `WorkspaceStore`, `RoleStore`, `MemberStore`) with a shared `WithTx`, and the closed permission catalog lives in `internal/domain/permissions.go`. No provider concept exists anywhere. On the web side, the agent config modal sources provider/model options from a hardcoded constant (`web/src/lib/constants.ts` `PROVIDERS`), while the members/workspace settings panes are already API-backed — providers joins that API-backed family.

## Goals / Non-Goals

**Goals:**
- Tenant-scoped provider configs with keys encrypted at rest (AES-256-GCM, tenant-bound AAD).
- The provider extension point (interface + registry of six built-ins) that chat/runs will consume.
- Live verify per type, ephemeral result.
- Providers pane in settings; agent config modal provider options API-driven.

**Non-Goals:**
- Agent ↔ provider FK (agents domain, later).
- Live model probing / dynamic model catalogs.
- Key rotation / re-encrypt command (rotation = re-enter tenant keys).
- Key unsetting (replacement only), audit logging, background verify scheduling.
- Chat execution using stored keys (the chat domain's change).

## Decisions

### D1: Catalog (code) / configs (data) split
Provider *types* are per-binary capabilities, so they are code: a registry of built-in implementations (same pattern as `auth.NewPasswordProvider` registration in `internal/server/router.go`). Tenant data references types by string, validated app-side against the registry. Alternative considered: a `provider_types` DB table — rejected; the DB catalog would go stale against the binary and contradict "built-ins are ordinary registrations".

### D2: AES-256-GCM envelope encryption, tenant-bound AAD
Format `v1:<b64 nonce>:<b64 ciphertext>`, 12-byte random nonce per encryption, AAD = workspace ID. AAD binding means a ciphertext lifted from tenant A and written into tenant B's row fails decryption — cross-tenant replay is blocked at the crypto layer, not by query discipline alone. Alternatives: plaintext (fails the requirement), pgcrypto (key material in DSN/logs; extension dependency), KMS-only (self-hosted friction for a v1).

### D3: Strict env requirement, no KDF
`ONCLAW_ENCRYPTION_KEY` (32-byte hex/base64) is required by `server` at startup, refusing with the openssl hint. Rationale: an ephemeral fallback is silent data loss (unlike JWT, where the worst case is logout). No passphrase KDF: a KDF invites `MyPassword123` as a master key — a setup error is safer than false security. Alternatives: lazy 503 (dual-mode paths, and the config surface stays ambiguous), auto-generated key file (ephemeral containers + persistent DB = undecryptable keys), passphrase KDF (weak master keys).

### D4: base_url = origin + optional prefix; server appends the canonical path
The SDK ecosystems disagree (OpenAI convention includes `/v1`, Anthropic excludes it), so pasted URLs would be wrong half the time. Rule: `base_url` is scheme + host + optional prefix, http/https only; the server appends each type's canonical version path (`/v1/models`, `/v1beta/models`, …). Named types may omit `base_url` (canonical origin); `-compatible` types require it. Alternative: SDK-convention passthrough — rejected; vendor docs are the wrong documentation to make users read.

### D5: Per-type verifier registry (capability interface)
The extension point is a small capability interface — `Verify(ctx, Credential)` — with one implementation per type, registered like the auth provider registry. A shared "call list-models" helper was rejected because the probes genuinely differ: OpenRouter's `/models` is public (probing it proves nothing), so it probes `/api/v1/key`; Anthropic/Gemini need their own auth headers. Per-type implementations are where that knowledge lives. When the chat domain arrives it adds a separate `ChatProvider` capability that implementations compose with — small interfaces, no god interface to break plugins later.

### D6: Ephemeral verify result
Nothing about verification is persisted; the response carries `{ok, error}` and the UI holds it in component state. Alternative: `last_verified_at`/`last_verify_ok` columns — rejected; status goes stale, and transient inline feedback matches the existing guard-rejection toast pattern.

### D7: providers.read for all built-ins; providers.write for Owner/Admin
Adding `providers.read` to Member keeps the catalog symmetric ("reads only" stays true: workspace.read, members.read, roles.read, providers.read) and unblocks member-facing agent configuration. Alternative: Owner/Admin-only read — rejected; the agent config modal would 403 for members. `providers.write` stays Owner/Admin. (Note: `SuperadminPermissions` inherits all workspace permissions, so it picks both up automatically.)

### D8: Key PATCH semantics — omitted = unchanged; empty = 400
Replacement-only, no unset in v1 (delete the config instead). `key` omitted from PATCH means the stored ciphertext is untouched — non-key edits work even when the master key changed and the stored key is undecryptable.

### D9: Models — static per-type catalog in the web app; free-text custom model
The agent modal's model select maps type → known models statically (extending today's `PROVIDERS`-style constant to the six types), with a free-text "custom model ID" option since `-compatible` types have no enumerable catalog. A backend catalog endpoint is deferred to the chat domain's change. Alternative: backend-served model catalog — deferred, not rejected; it becomes worthwhile when verify-adjacent listing needs the real key.

### D10: Package layout and store port
`internal/secrets` (envelope crypto + sentinels: `ErrKeyMissing`-class validation happens at config assembly; `ErrUndecryptable`), `internal/providers` (capability interface + registry + six verifiers), `domain.ProviderConfig`, `ProviderStore` port (every method takes workspaceID — store invariant #3), fake + postgres implementations, migration `000007_workspace_providers`. The ciphertext column lives on the entity but never crosses the HTTP boundary (handlers map to `key_set`/`key_hint`).

## Risks / Trade-offs

- [Operator changes `ONCLAW_ENCRYPTION_KEY` → every stored key undecryptable] → per-row `ErrUndecryptable` sentinel surfaced as a clear row error state ("instance encryption key changed?"); rename/toggle/delete keep working; rotation documented as "re-enter keys".
- [OpenRouter `/models` is public → naive probe reports garbage keys as valid] → dedicated `/api/v1/key` probe for that type.
- [Verify endpoint makes the server send tenant-controlled URLs (SSRF-ish reachability from the server host)] → action is `providers.write`-gated; scheme restricted to http/https; internal-network reachability is inherent to self-hosted deployments pointing at their own infrastructure — accepted.
- [Key hint (last 4) is a weak info leak] → accepted; standard practice (Stripe/GitHub).
- [CI/smoke now need a key] → smoke script exports a generated key; integration tests use a fixed fixture key.

## Migration Plan

Additive migration `000007_workspace_providers` (no backfill). Deploy: run `migrate up`, then deploy the server binary with `ONCLAW_ENCRYPTION_KEY` set — the new server refuses to start without it, so set the key before restarting. Rollback: `migrate down` drops the table; the previous binary ignores the env var.

## Open Questions

None — all decisions resolved during exploration (env policy, key format, base_url rule, verify persistence).