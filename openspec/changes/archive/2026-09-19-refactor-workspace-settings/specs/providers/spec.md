## ADDED Requirements

### Requirement: Draft credential verification
`POST /workspaces/:ws/providers/verify-draft` SHALL test unsaved provider form values without persisting anything. The payload SHALL carry the form state — `type`, optional `base_url`, write-only optional `key`, optional `catalog_provider`, and optional `provider_id` naming an existing workspace config. When `key` is absent and `provider_id` names a config with a stored key, the endpoint SHALL decrypt the stored key and verify it together with the submitted type and base URL; when no credential can be resolved (no key and no config, or a keyless config) the endpoint SHALL be 400. Verification SHALL reuse the per-type provider probes of the stored-config verify endpoint, SHALL require `providers.write`, SHALL return synchronously as 200 `{ok: true}` or 200 `{ok: false, error}` (provider-side auth failures reported as `ok: false`, not a server 5xx), and SHALL NOT write any state.

#### Scenario: Verify typed values in the create dialog
- **WHEN** a client posts a draft with type, base URL, and a typed key
- **THEN** the endpoint probes the provider with those values and returns ok or the provider error, persisting nothing

#### Scenario: Edit dialog verifies against the stored key
- **WHEN** a client posts a draft with `provider_id` of a key-set config and no key
- **THEN** the stored key is verified against the submitted type and base URL

#### Scenario: No credential to test
- **WHEN** a client posts a draft with no key and a `provider_id` of a keyless config (or none)
- **THEN** response is 400

#### Scenario: Nothing is persisted
- **WHEN** any draft verification completes, success or failure
- **THEN** no provider row, key, or verification state is stored

#### Scenario: Write permission required
- **WHEN** a Member-role holder calls verify-draft
- **THEN** response is 403
