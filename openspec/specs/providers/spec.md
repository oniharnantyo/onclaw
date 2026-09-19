# providers Specification

## Purpose
Tenant-scoped provider credential management: a code-defined catalog of six built-in provider types, per-workspace provider configs with AES-256-GCM-encrypted API keys, live connection verification, and the `providers.*` permissions — the credential foundation the chat/runs domains build on.

## Requirements

### Requirement: Provider type catalog
The system SHALL support exactly six built-in provider types — `openai`, `anthropic`, `gemini`, `openrouter`, `openai-compatible`, `anthropic-compatible` — defined in code and registered into a provider registry; built-ins are ordinary registrations. Configs referencing an unknown type SHALL be rejected with 400. A workspace MAY hold multiple configs of the same type.

#### Scenario: Unknown type rejected
- **WHEN** a config is created with type "azure-openai" (not in the catalog)
- **THEN** response is 400 invalid_request naming the valid types

#### Scenario: Duplicate types allowed
- **WHEN** a workspace creates two configs of type `openai` named "Acme prod" and "Acme sandbox"
- **THEN** both exist as independent rows

### Requirement: Provider config CRUD
Tenant-scoped endpoints under `/workspaces/:ws/providers` SHALL create, list, update, and delete provider configs. Fields: `type`, `name`, `base_url` (the full API base **including the family's version path**, e.g. `https://api.example.com/v1`; the server appends only resource paths), `enabled` (default true), and the credential key (write-only). `base_url` SHALL be required for `-compatible` types and optional for named types (empty = the provider's canonical origin including its version path; when set it overrides the origin). `base_url` SHALL accept only http/https. The key SHALL be optional at create and updatable separately; on update an omitted key means unchanged and an empty key SHALL be rejected (no unset in v1). Deletion SHALL be immediate, but SHALL be refused with 409 conflict when any agent in the workspace references the config, so a workspace never holds a dangling provider reference.

#### Scenario: Create OpenAI config with key
- **WHEN** an Owner POSTs {type: "openai", name: "Acme prod", key: "sk-..."}
- **THEN** response is 201 with the config; no key material in the response body

#### Scenario: Delete
- **WHEN** a config not referenced by any agent is DELETEd
- **THEN** 204; subsequent GETs of it return 404; a config belonging to another workspace is 404 (never 403 — indistinguishable from unknown)

#### Scenario: Delete blocked while in use
- **WHEN** a config referenced by one or more agents is DELETEd
- **THEN** response is 409 conflict stating that agents still reference the config; the config and its key remain intact

#### Scenario: Compatible type requires base_url
- **WHEN** a config of type `openai-compatible` is created without `base_url`
- **THEN** response is 400 invalid_request

#### Scenario: Named type base_url override
- **WHEN** a config of type `openai` is created with base_url "https://openai.acme-proxy.com/v1"
- **THEN** it is accepted and used as the versioned API base for that config's provider calls

#### Scenario: Invalid base_url scheme
- **WHEN** a config is created with base_url "ftp://x"
- **THEN** response is 400 invalid_request (only http/https accepted)

#### Scenario: Update without touching the key
- **WHEN** PATCH changes only name and enabled
- **THEN** the stored key is preserved and `key_set` stays true

#### Scenario: Empty key update rejected
- **WHEN** PATCH sends key: ""
- **THEN** response is 400 invalid_request

### Requirement: Key secrecy on the wire
API keys SHALL be write-only: responses SHALL expose `key_set` (bool) and `key_hint` (last 4 characters, display only) and SHALL NOT include the key or its ciphertext. The key hint SHALL be empty when no key is set.

#### Scenario: List response shape
- **WHEN** any member lists providers
- **THEN** each row shows type, name, base_url, enabled, key_set, key_hint, timestamps — never the key or ciphertext

### Requirement: Encryption at rest
Keys SHALL be encrypted at rest with AES-256-GCM using an instance master key, envelope format `v1:<nonce>:<ciphertext>`, with the workspace ID bound as AAD so a ciphertext cannot be replayed into another tenant's row. Decryption failure SHALL surface as a distinct domain error (undecryptable), and the config SHALL remain renamable, toggleable, and deletable.

#### Scenario: Cross-tenant replay blocked
- **WHEN** a ciphertext from workspace A's config is written into workspace B's config row
- **THEN** decryption in B fails with the undecryptable error (AAD mismatch)

#### Scenario: Key changed between restarts
- **WHEN** ONCLAW_ENCRYPTION_KEY changed since the key was stored
- **THEN** verify on that config reports undecryptable; rename/toggle/delete still succeed

### Requirement: Connection verification
POST `/workspaces/:ws/providers/:id/verify` SHALL decrypt the stored key and probe the provider per type: named types use their canonical (or overridden) origin; `openrouter` SHALL probe the key-info endpoint (`/api/v1/key`) because its models endpoint is public; `-compatible` types probe `{base_url}` with the type's canonical path. The result SHALL be returned synchronously as 200 {ok: true} or 200 {ok: false, error} and SHALL NOT be persisted on the config row. Verification SHALL require `providers.write`. Verifying a config without a key SHALL be 400. Provider-side auth failures (401/403) SHALL be reported as ok:false with the provider error, not as a server 5xx.

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
- **WHEN** verify is called on a keyless config
- **THEN** response is 400 invalid_request

#### Scenario: Verify requires write permission
- **WHEN** a Member-role holder calls verify
- **THEN** response is 403 (verify uses the credential; read-only members cannot)

### Requirement: Encryption key requirement
The `server` command SHALL refuse to start unless `ONCLAW_ENCRYPTION_KEY` decodes to exactly 32 bytes as hex or base64, exiting with an error naming the variable and the generation command (`openssl rand -hex 32`). Other commands (`migrate`, `user`, `superadmin`) SHALL NOT require it.

#### Scenario: Missing key
- **WHEN** `onclaw server` starts without ONCLAW_ENCRYPTION_KEY
- **THEN** the command exits with an error naming the variable and `openssl rand -hex 32`

#### Scenario: Malformed key
- **WHEN** ONCLAW_ENCRYPTION_KEY="short" is set
- **THEN** server exits with the same class of error (must decode to 32 bytes)

#### Scenario: Valid key starts
- **WHEN** a 32-byte hex key is set
- **THEN** server starts and provider endpoints function

### Requirement: Permission gating
Provider endpoints SHALL require `providers.read` for listing and `providers.write` for create/update/delete/verify. `providers.read` SHALL be granted to Owner, Admin, and Member built-in roles; `providers.write` SHALL be granted to Owner and Admin. The permissions SHALL belong to the closed catalog, so custom roles may express them via the standard role system.

#### Scenario: Member lists providers
- **WHEN** a Member-role holder lists providers
- **THEN** 200 with all workspace configs (read is a permission, not implied)

#### Scenario: Member cannot create
- **WHEN** a Member-role holder creates a config
- **THEN** response is 403

### Requirement: Catalog mapping hint for compatible gateways
An `openai-compatible` or `anthropic-compatible` provider configuration SHALL accept an optional catalog-mapping hint identifying which community-catalog provider the gateway corresponds to (e.g. a models.dev provider id). When present, the hint SHALL be used wherever the provider type alone is insufficient for catalog resolution (model lists, effort values, input-modality resolution). Known gateway hosts MAY pre-fill the hint, but the user's explicit selection SHALL always win. The hint SHALL be optional — an absent hint leaves resolution exactly as it behaves without mapping (unknown).

#### Scenario: Hint unlocks catalog resolution
- **WHEN** an openai-compatible provider config carries a catalog-mapping hint for a gateway known to the community catalog
- **THEN** catalog-backed resolution (models, effort values, input modalities) uses that gateway's catalog entries

#### Scenario: No hint keeps unknown semantics
- **WHEN** an openai-compatible provider config carries no catalog-mapping hint and the host is not in the built-in hint map
- **THEN** catalog resolution treats the provider as unmapped (unknown), and nothing else about the provider changes

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
