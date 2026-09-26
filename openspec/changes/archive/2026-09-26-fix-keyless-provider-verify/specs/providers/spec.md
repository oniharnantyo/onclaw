# Spec Delta

## MODIFIED Requirements

### Requirement: Connection verification
POST `/workspaces/:ws/providers/:id/verify` SHALL decrypt the stored key and probe the provider per type: named types use their canonical (or overridden) origin; `openrouter` SHALL probe the key-info endpoint (`/api/v1/key`) because its models endpoint is public; `-compatible` types probe `{base_url}` with the type's canonical path. The result SHALL be returned synchronously as 200 {ok: true} or 200 {ok: false, error} and SHALL NOT be persisted on the config row. Verification SHALL require `providers.write`. Each provider type SHALL declare whether it requires an API key: the four named types (`openai`, `anthropic`, `gemini`, `openrouter`) require one, and the `-compatible` types do not — their probes omit the auth header when no key is configured. Verifying a config without a key SHALL be 400 only when the config's type requires a key; a keyless config of a keyless-capable type SHALL verify with an empty key, delegating the auth question to the endpoint's own answer. Provider-side auth failures (401/403) SHALL be reported as ok:false with the provider error, not as a server 5xx.

#### Scenario: Successful probe
- **WHEN** verify is called on a config with a valid OpenAI key
- **THEN** 200 {ok: true}; the config row is unchanged afterward

#### Scenario: Invalid key
- **WHEN** the stored key is rejected by the provider with 401
- **THEN** 200 {ok: false, error contains the provider's message}; row unchanged

#### Scenario: Unreachable base_url
- **WHEN** base_url points at an address the server cannot reach
- **THEN** 200 {ok: false, error describes the transport failure}

#### Scenario: Verify without key
- **WHEN** verify is called on a keyless config whose type requires a key (e.g. `openai`)
- **THEN** response is 400 invalid_request

#### Scenario: Keyless config verifies
- **WHEN** verify is called on a keyless config of a keyless-capable type (e.g. `openai-compatible`) whose endpoint needs no credential
- **THEN** 200 {ok: true} when the endpoint answers; a 401 from an endpoint that does require auth reports 200 {ok: false, error} — never a server 5xx

#### Scenario: Verify requires write permission
- **WHEN** a Member-role holder calls verify
- **THEN** response is 403 (verify uses the credential; read-only members cannot)

### Requirement: Draft credential verification
`POST /workspaces/:ws/providers/verify-draft` SHALL test unsaved provider form values without persisting anything. The payload SHALL carry the form state — `type`, optional `base_url`, write-only optional `key`, optional `catalog_provider`, and optional `provider_id` naming an existing workspace config. When the draft's type requires an API key and `key` is absent, the endpoint SHALL decrypt the stored key of the config named by `provider_id` and verify it together with the submitted type and base URL; when the type requires a key and no credential can be resolved (no key and no config, or a keyless config) the endpoint SHALL be 400. When the draft's type does not require a key, the endpoint SHALL verify immediately with the submitted key — including an empty one — without consulting a stored config. Verification SHALL reuse the per-type provider probes of the stored-config verify endpoint, SHALL require `providers.write`, SHALL return synchronously as 200 `{ok: true}` or 200 `{ok: false, error}` (provider-side auth failures reported as `ok: false`, not a server 5xx), and SHALL NOT write any state.

#### Scenario: Verify typed values in the create dialog
- **WHEN** a client posts a draft with type, base URL, and a typed key
- **THEN** the endpoint probes the provider with those values and returns ok or the provider error, persisting nothing

#### Scenario: Edit dialog verifies against the stored key
- **WHEN** a client posts a draft with `provider_id` of a key-set config whose type requires a key, and no key
- **THEN** the stored key is verified against the submitted type and base URL

#### Scenario: Keyless draft verifies without a key
- **WHEN** a client posts a draft of a keyless-capable type (e.g. `openai-compatible`) with a base URL and no key
- **THEN** the endpoint probes the endpoint keyless and returns ok or the provider error, persisting nothing

#### Scenario: No credential to test
- **WHEN** a client posts a draft of a key-requiring type with no key and a `provider_id` of a keyless config (or none)
- **THEN** response is 400

#### Scenario: Nothing is persisted
- **WHEN** any draft verification completes, success or failure
- **THEN** no provider row, key, or verification state is stored

#### Scenario: Write permission required
- **WHEN** a Member-role holder calls verify-draft
- **THEN** response is 403
