# Proposal

## Why

The Verify connection affordance is unusable for keyless providers. Both verify endpoints (stored-config verify and verify-draft) hard-require an API key — a stored config without a key gets 400 and the dialog's Verify button is disabled until a key is typed — even though the `-compatible` verifiers already tolerate an empty key by design (they skip the auth header). Self-hosted OpenAI-compatible/Anthropic-compatible services that need no credential (Ollama, LM Studio, llama.cpp, vLLM) are therefore added blind: the user cannot confirm reachability, and re-verifying an existing keyless config is impossible. The workaround — typing a dummy key — works only because the endpoint ignores it, and the UI gives no hint of that.

## What Changes

- Add `RequiresAPIKey() bool` to the `providers.Provider` interface: `true` for `openai`, `anthropic`, `gemini`, `openrouter`; `false` for `openai-compatible` and `anthropic-compatible` (whose verifiers already skip the auth header when no key is present). This is the interface contract, so third-party providers decide keylessness for themselves.
- Stored-config verify (`POST /providers/:id/verify`): the "no API key configured" 400 becomes type-conditional — it applies only to key-requiring types; a keyless config of a keyless-capable type verifies with an empty key.
- Draft verification (`POST /providers/verify-draft`): the credential-resolution fallback (no key → resolve from `provider_id`, requiring that config to hold a key) becomes type-conditional; for keyless-capable types a draft with no key verifies immediately with the empty key.
- Providers dialog UI: for a keyless-capable type the dialog renders a "This endpoint needs no API key" checkbox; checking it disables the API key field and enables the Verify connection button without a key, unchecking re-enables the field. The Verify button is enabled when the checkbox is checked, a key is typed, or a stored key exists; the stored-credential resolution for key-requiring types is unchanged.
- No schema, API shape, or permission changes. Verification remains non-persisted, synchronous, and `providers.write`-gated.

## Capabilities

### New Capabilities

- (none)

### Modified Capabilities

- `providers`: connection verification and draft verification requirements — the without-key rejection becomes per-type (key-requiring types reject; keyless-capable types verify with an empty key).
- `web-app/settings`: providers pane — the Verify connection action and its disabled state must account for keyless provider types (enabled with no key, tooltip explains why).

## Impact

- `internal/providers/provider.go` — `Provider` interface gains `RequiresAPIKey()`; all six implementations implement it; `KnownTypes` untouched.
- `internal/server/handlers/providers.go` — `VerifyProvider` and `VerifyDraft` conditional credential resolution.
- `web/src/modals/ProviderFormDialog.tsx` — `canVerify` condition and tooltip; `web/src/lib/constants.ts` — per-type key-requirement flag next to `PROVIDER_TYPES`.
- Tests: `internal/providers` unit tests, handler tests for both verify endpoints (keyless 200 vs key-requiring 400), `ProviderFormDialog.test.tsx`.
- `scripts/smoke.sh` — keyless verify-draft scenario against the existing mock server pattern.
- No database migration; no new dependencies.
