# Tasks

## 1. Provider interface flag (backend)

- [x] 1.1 Add `RequiresAPIKey() bool` to the `providers.Provider` interface in `internal/providers/provider.go`, with a doc comment stating the contract (keyless-capable types omit the auth header when no key is configured).
- [x] 1.2 Implement it on all six built-ins: `true` for openai, anthropic, gemini, openrouter; `false` for openai-compatible, anthropic-compatible (`internal/providers/{openai,anthropic,gemini,openrouter}.go`).
- [x] 1.3 Unit tests in `internal/providers/providers_test.go`: every registered type implements the flag; `-compatible` types return false, named types return true.

## 2. Verify endpoints (backend)

- [x] 2.1 `VerifyProvider` in `internal/server/handlers/providers.go`: gate the `!HasKey()` → 400 on `RequiresAPIKey()`; when keyless-capable and no ciphertext is stored, verify with an empty key (skip decryption).
- [x] 2.2 `VerifyDraft` in the same file: when the draft type does not require a key, skip the `provider_id` credential-resolution block and verify with the submitted key as-is; key-requiring types keep the existing resolution and 400 paths unchanged.
- [x] 2.3 Handler tests: stored keyless `openai-compatible` config verifies 200 {ok:true} against a stub endpoint (and 200 {ok:false, error} on a 401 from the stub); keyless verify-draft with no key returns 200; key-requiring no-credential paths still 400 (existing tests pass unmodified).

## 3. Providers dialog (frontend)

- [x] 3.1 Add the per-type `requiresKey` flag to `PROVIDER_TYPES` in `web/src/lib/constants.ts` (true ×4, false for the two `-compatible` types) and update the type where the flag is consumed.
- [x] 3.2 `web/src/modals/ProviderFormDialog.tsx`: render the "This endpoint needs no API key" checkbox for keyless-capable types only (hidden for key-requiring types and when a stored key exists); checking it disables the key input, unchecking re-enables it; the state is local component state, never submitted or persisted.
- [x] 3.3 `ProviderFormDialog.tsx`: `canVerify` gains the checkbox branch (`checkboxChecked || key present || (isEdit && key_set)`); the disabled tooltip becomes "No API key needed? Tick 'This endpoint needs no API key'" when the type is keyless-capable and nothing else enables verify (key-requiring tooltip unchanged).
- [x] 3.4 `ProviderFormDialog.test.tsx`: checkbox renders for `openai-compatible` and not for `openai`; checking disables the key field and enables Verify; unchecking reverses both; verify with the checkbox ticked submits no key; key-set edit shows no checkbox.

## 4. End-to-end verification

- [x] 4.1 `scripts/smoke.sh`: add a keyless verify-draft scenario (type `openai-compatible`, base URL pointing at the existing mock HTTP server, no key → `ok` verdict) inside the providers section.
- [ ] 4.2 Manual pass: create a keyless `openai-compatible` provider pointing at a local Ollama (`http://localhost:11434/v1`) — tick "This endpoint needs no API key" (key field disables), confirm Verify connection runs keyless and reports success; save, then re-verify from the edit dialog and via the row action.
- [x] 4.3 Full gates: `go build ./... && go vet ./... && go test ./...`, web typecheck/tests, and the smoke suite green.
