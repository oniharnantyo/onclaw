# Design

## Context

The two verify endpoints (`VerifyProvider` for stored configs, `VerifyDraft` for unsaved form values) unconditionally require a key: the stored path 400s on `!HasKey()`, the draft path falls back to resolving the key from a `provider_id` and 400s when none can be resolved. The frontend mirrors this with `canVerify = key present || (isEdit && key_set)`. Meanwhile the `-compatible` verifiers already treat the key as optional — `OpenAICompatibleProvider.Verify` and `AnthropicCompatibleProvider.Verify` set the auth header only when a key is present — so the backend contract and the wire behavior already agree that these types are keyless-capable; only the endpoint layer and the UI gate disagree. (See proposal.md — Why.)

## Goals / Non-Goals

**Goals:**
- One authoritative per-type answer to "does this provider type need a key", shared by both verify endpoints and the dialog UI.
- Keyless verification end-to-end: dialog button enabled, endpoint probes keyless, provider error (401 from an endpoint that does want auth) surfaces as `ok: false`.

**Non-Goals:**
- No first-class `ollama` provider type, no base_url semantics change, no catalog/embedding-lane changes (a keyless `openai-compatible` config already covers local Ollama for models and embeddings).
- No new endpoint or API field to expose the key-requirement flag (the six types are a closed, code-defined catalog on both sides).
- No change to model listing, provider CRUD, or permissions.

## Decisions

- **D1 — `RequiresAPIKey() bool` on the `Provider` interface, not a registry-side map.** The interface is the extension contract (plugins target the same contracts as built-ins), so keylessness is a property each implementation declares. Values: `openai`, `anthropic`, `gemini`, `openrouter` → `true`; `openai-compatible`, `anthropic-compatible` → `false` (their verifiers already omit the auth header keyless — this formalizes existing behavior). Alternative rejected: a `map[string]bool` in the registry — it splits the truth away from the implementations a third-party plugin registers.

- **D2 — One conditional rule in both endpoints: resolve credentials only when the type requires a key.** `VerifyProvider`: the `!HasKey()` 400 applies only when `providerImpl.RequiresAPIKey()`; otherwise an absent ciphertext verifies with an empty key. `VerifyDraft`: when the type does not require a key, skip the `provider_id` credential-resolution block entirely and verify with the submitted key as-is (possibly empty) — resolving a stored key for a keyless type would contradict the type's own declaration. Key-requiring types keep today's behavior byte-for-byte.

- **D3 — The dialog mirrors the flag in `web/src/lib/constants.ts`** (a `requiresKey` boolean next to each `PROVIDER_TYPES` entry). No API-driven discovery: the type catalog is code-defined on both sides and the existing spec pins it that way; a mirror flag drifts only when the catalog itself changes — the same drift surface `PROVIDER_TYPES` already has.

- **D4 — Keylessness is declared via a checkbox, not an always-on button.** For a keyless-capable type the dialog renders a "This endpoint needs no API key" checkbox directly above the key field. Checking it disables the key field (and clears any typed value from the verify path) and enables Verify connection; unchecking re-enables the field, restoring today's behavior. The checkbox defaults to unchecked — keylessness is an explicit user declaration, because most `-compatible` endpoints in the wild (corporate gateways, proxied providers) do require a key; the disabled-verify tooltip ("No API key needed? Tick 'This endpoint needs no API key'") routes keyless users to it. The checkbox is never rendered for key-requiring types, and never rendered when a key is stored (unset is unsupported in v1, so a checked state would be a lie). It is pure frontend state — never persisted; on edit it re-derives from `key_set`. `canVerify = checkboxChecked || key present || (isEdit && key_set)`. Alternative rejected: persisting a `keyless` boolean on the config row — keylessness is already fully derivable from (type, key_set), so a stored flag adds a migration and a sync burden for zero behavioral gain.

- **D5 — 401 stays a soft `ok: false`, not an input error.** Verifying a keyless declaration against an endpoint that *does* require auth is a legitimate configuration mistake the user must see: the probe's 401 flows through the existing `parseProviderError` path as `{ok: false, error}`, identical to a wrong key on OpenAI. No special-casing.

## Risks / Trade-offs

- **Mirrored flag drift (D3):** if a new provider type ships backend-only, the dialog would default to `requiresKey=true` — the conservative direction (button disabled until a key is typed), so drift degrades to today's behavior, never to a broken request.
- **Keyless probes against auth-wanting endpoints** return 401-shaped provider errors that name the endpoint, not OnClaw — acceptable; the error text is the endpoint's own.
- **No behavior change for key-requiring types** — the existing handler tests (verify/verify-draft 400 paths) must continue passing unmodified as the regression guard.
