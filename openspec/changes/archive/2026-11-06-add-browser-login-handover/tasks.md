# Tasks: add-browser-login-handover

## 1. Vault storage

- [x] 1.1 Migration 000021 `vault_items` + `internal/store/sqlite/vaultitems.go` (+ fake store)
- [x] 1.2 `internal/vault`: keyring key + keyfile fallback, AES-GCM seal, origin matching, upsert, redaction registry
- [x] 1.3 `internal/services/settings_vault.go`: `VaultLogins`/`SaveVaultLogin`/`RemoveVaultLogin`/`RevealVaultLogin` + tests
- [x] 1.4 Saved logins section in the Browser Use pane + tests

## 2. Secure submission (child)

- [x] 2.1 `submit_secret` tool schema (targets-only; value-field rejection test) + catalog entry
- [x] 2.2 `internal/browseruse/secret.go`: prepareSecret validations, form fingerprint, CDP `Input.insertText` fill, submission click/enter
- [x] 2.3 Scrub sequence: field clearing, document scan (incl. shadow roots), SPA-keep vs GET-reload, `urlCarriesSecret` guard, protection freeze of inspection tools, recording/diagnostics suppression — loopback-fixture tests
- [x] 2.4 Internal tools `__deliver_secret`/`__begin_takeover`/`__resolve_attention` + inertness-without-live-request guard
- [x] 2.5 `resolveMCPTools` reserved-prefix filter + test

## 3. Parking and resume (parent)

- [x] 3.1 `KindInputRequired`/`input_resolved` events + `ResolveInput` (log-derived pending validation, resolving guard, receipt-only event) + cold-restart expiry + tests
- [x] 3.2 Parent pending-value map (in-memory, keyed by interruptID) wired into the MCP tool executor
- [x] 3.3 `mapping.ts`: `approval: {id, prompt, display}` for attention parts; raw text omitted; receipt rendering

## 4. Frontend attention cards

- [x] 4.1 `SecretGate.tsx` (masked input / OTP boxes, save-to-vault checkbox, clear-on-submit, receipts) + tests
- [x] 4.2 Takeover card states (pending → active → resolved) + tests
- [x] 4.3 `client.ts`/`transport.ts` + `client.test.ts` mocks for the new calls

## 5. Takeover

- [x] 5.1 `request_takeover` tool + held-state (headed relaunch + focus, ref invalidation, inspection refusal) + resolve endings
- [x] 5.2 `BeginBrowserTakeover` service + wiring

## 6. Pane simplification (merged from simplify-browser-use-pane)

- [x] 6.1 GOOS user-data resolver + lookup seam
- [x] 6.2 Candidate extraction (`Local State` → `info_cache`, existing-dir filter, name fallback, `Default` first, sizes)
- [x] 6.3 `ChromeProfileCandidate` + `DetectChromeProfiles` + tests
- [x] 6.4 Pane rewrite: engine section + manual import removed; default badge + Make default pass-through; detected-Chrome import list with disclosures + tests
- [x] 6.5 Bindings regen (`task common:generate:bindings`)

## 7. Gates

- [x] 7.1 `gofmt -w .`, `go vet ./...`, `go test ./...`
- [x] 7.2 Frontend vitest suite + `npm run build`
- [x] 7.3 `openspec validate add-browser-login-handover --strict`
- [x] 7.4 Secret-hygiene integration proof: loopback sign-in form, byte-grep every artifact (events, sqlite, transcript, tool results, logs) for the typed value — zero hits

## 8. Identifier handover (2026-11-05 follow-up: username or email)

- [x] 8.1 Child: `identifierTarget` schema + prepare resolution/fingerprint + identifier-first fill in `__deliver_secret`
- [x] 8.2 Parent: `ResolveInput(+identifier)`, `input_required` Identifier flag, deliver args carry the identifier
- [x] 8.3 Services: typed identifier overrides the agent-supplied username for the vault save
- [x] 8.4 Frontend: SecretGate identifier box + client/transport/mocks + entries/mapping flag
- [x] 8.5 Tests: child fill order, parent deliver-args shape, vault-override, card render + submit; bindings regen

## 9. Screenshots inside the session workspace (2026-11-05 follow-up: read_file reachable)

- [x] 9.1 Child: `screenshot` writes the capture into the directory the call's `_meta` names (`buddies.dev/screenshot-dir`), falling back to `<browserDir>/screenshots` when the host supplies none; malformed values refused
- [x] 9.2 Parent: `MCPManager.Invoke` (+ supervisor `invokeWithMeta`, in-flight gauge honored) stamps the calling session's `<workspaces>/<slug>/sessions/<id>/browser` directory onto every model-invoked browser-use call; arguments untouched
- [x] 9.3 Serving: `BrowserScreenshot(sessionID, name)` resolves the same workspace directory; `WithChatBrowserDir` removed
- [x] 9.4 Frontend: BrowserTool thumb addresses the bound read with the viewing session; transport/client/mocks; bindings regen
- [x] 9.5 Tests: meta resolution table, live meta write, wire-shape invoke (meta on the wire, gauge honored, non-system refs refused), session-dir wrap + degradation, session-scoped serving; gates green
