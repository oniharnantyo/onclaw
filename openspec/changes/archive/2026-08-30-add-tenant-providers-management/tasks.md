## 1. Domain & crypto foundations

- [x] 1.1 `domain.ProviderConfig` entity (ID, WorkspaceID, Type, Name, BaseURL, KeyCiphertext, KeyHint, Enabled, timestamps) with doc comment that KeyCiphertext/KeyHint never cross the HTTP boundary
- [x] 1.2 `internal/secrets`: AES-256-GCM envelope (`v1:nonce:ciphertext`), 32-byte key parse (hex/base64), `Encrypt(key, aad, plaintext)` / `Decrypt` with AAD, `ErrUndecryptable` sentinel, unit tests incl. cross-tenant AAD replay and wrong-key failure
- [x] 1.3 Provider capability interface + registry in `internal/providers` (`Verify(ctx, Credential)`), six built-in verifiers with per-type request shape, auth header, path-append rule (openrouter → `/api/v1/key`), httptest-based tests per type

## 2. Store layer

- [x] 2.1 `ProviderStore` port in `internal/store/store.go` (Create, ByID, ListForWorkspace, Update, Delete — every method takes workspaceID), add to `Store` root interface
- [x] 2.2 Fake store implementation + contract tests matching existing fake patterns
- [x] 2.3 Migration `000007_workspace_providers` (up/down) and postgres implementation with embedded SQL; integration tests (build tag `integration`, `TEST_DATABASE_URL`)

## 3. Permissions & config

- [x] 3.1 Add `providers.read`/`providers.write` to the closed catalog; grant providers.read to Owner/Admin/Member, providers.write to Owner/Admin (SuperadminPermissions inherits automatically); update permissions tests
- [x] 3.2 `ONCLAW_ENCRYPTION_KEY` config: parse/validate 32-byte hex/base64 in `internal/config`, `server` refuses to start without it (error names the var + `openssl rand -hex 32`); other commands unaffected; `.env.example` entry

## 4. HTTP API

- [x] 4.1 `handlers/providers.go`: list/create/patch/delete handlers mapping entity → {type, name, base_url, enabled, key_set, key_hint, timestamps}; never serialize ciphertext; validation errors → 400 invalid_request
- [x] 4.2 Verify handler: `providers.write` gate, decrypt via `internal/secrets` (undecryptable → distinct error envelope), dispatch to registry verifier, 200 {ok, error} result semantics, 400 when keyless
- [x] 4.3 Router registration under `/workspaces/:ws/providers` with `RequireWorkspace` + `RequirePermission` middleware; router tests: permission matrix (Member read-ok / write-403, non-member 404), key secrecy (no ciphertext/key in any response body), AAD tenant binding end-to-end

## 5. Smoke & docs

- [x] 5.1 `scripts/smoke.sh`: provider CRUD + verify-scenarios coverage, export generated `ONCLAW_ENCRYPTION_KEY`
- [x] 5.2 `.env.example` + README/CLAUDE.md notes: key generation, rotation=re-enter-keys note

## 6. Web: Providers pane

- [x] 6.1 API client functions (list/create/patch/delete/verify) in `web/src/lib/api.ts`; typed `ProviderConfig` in `web/src/data/types.ts`
- [x] 6.2 Providers pane component: list rows (type badge, name, base_url, key_set + key hint, enabled toggle), structured create/edit form (type select, conditional base URL with origin-rule help text, password-style write-only key input), delete confirm, transient verify banner, guard toasts, empty state; register as eighth section in `SettingsModal.tsx`
- [x] 6.3 Agent config modal: provider select sourced from API configs; unconfigured types as disabled entries pointing at Settings → Providers; extend the static type→models map to the six types + free-text custom model option; remove hardcoded `PROVIDERS` from `constants.ts`; seed data updated

## 7. Verification

- [x] 7.1 `go build ./... && go vet ./... && go test ./...` green; integration tests green with `TEST_DATABASE_URL`
- [x] 7.2 `pnpm test` + `pnpm build` green in web/
- [x] 7.3 `./scripts/smoke.sh` green end-to-end

