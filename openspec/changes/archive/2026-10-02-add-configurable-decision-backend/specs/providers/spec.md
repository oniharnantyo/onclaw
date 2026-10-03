# Spec Delta

## MODIFIED Requirements

### Requirement: Provider type catalog
The system SHALL support exactly seven built-in provider types — `openai`, `anthropic`, `gemini`, `openrouter`, `openai-compatible`, `anthropic-compatible`, `typesafe` — defined in code and registered into a provider registry; built-ins are ordinary registrations. `typesafe` SHALL be a decision-provider class: it backs typed-decision calls (the memory intent gate), never chat or agent model resolution, and model-catalog resolution SHALL NOT list any models for it. Configs referencing an unknown type SHALL be rejected with 400. A workspace MAY hold multiple configs of the same type.

#### Scenario: Unknown type rejected
- **WHEN** a config is created with type "azure-openai" (not in the catalog)
- **THEN** response is 400 invalid_request naming the valid types

#### Scenario: Duplicate types allowed
- **WHEN** a workspace creates two configs of type `openai` named "Acme prod" and "Acme sandbox"
- **THEN** both exist as independent rows

#### Scenario: Typesafe config created
- **WHEN** an Owner creates a config of type `typesafe` with name "TypeSafe production" and an API key
- **THEN** the config persists with no `base_url` (canonical default applies) and appears in provider listings

#### Scenario: Typesafe resolves no models
- **WHEN** a model list is resolved through a `typesafe` provider config
- **THEN** the resolution yields no models — decision providers never surface as model choices

### Requirement: Provider config CRUD
Tenant-scoped endpoints under `/workspaces/:ws/providers` SHALL create, list, update, and delete provider configs. Fields: `type`, `name`, `base_url` (the full API base **including the family's version path**, e.g. `https://api.example.com/v1`; the server appends only resource paths), `enabled` (default true), and the credential key (write-only). `base_url` SHALL be required for `-compatible` types and optional for named types and `typesafe` (empty = the provider's canonical origin including its version path — for `typesafe`, `https://api.typesafe.ai/v1/systemone`; when set it overrides the origin). `base_url` SHALL accept only http/https. The key SHALL be optional at create and updatable separately; on update an omitted key means unchanged and an empty key SHALL be rejected (no unset in v1). `typesafe` SHALL require an API key. Deletion SHALL be immediate, but SHALL be refused with 409 conflict when any agent in the workspace references the config, or when the workspace's memory decision configuration references it, so a workspace never holds a dangling provider reference in either seam.

#### Scenario: Create OpenAI config with key
- **WHEN** an Owner POSTs {type: "openai", name: "Acme prod", key: "sk-..."}
- **THEN** response is 201 with the config; no key material in the response body

#### Scenario: Delete
- **WHEN** a config not referenced by any agent or by the memory decision configuration is DELETEd
- **THEN** 204; subsequent GETs of it return 404; a config belonging to another workspace is 404 (never 403 — indistinguishable from unknown)

#### Scenario: Delete blocked while in use
- **WHEN** a config referenced by one or more agents is DELETEd
- **THEN** response is 409 conflict stating that agents still reference the config; the config and its key remain intact

#### Scenario: Delete blocked while backing the decision configuration
- **WHEN** the workspace's memory settings reference the config as the decision provider and it is DELETEd
- **THEN** response is 409 conflict stating that the memory decision configuration still references it; the config and its key remain intact

#### Scenario: Compatible type requires base_url
- **WHEN** a config of type `openai-compatible` is created without `base_url`
- **THEN** response is 400 invalid_request

#### Scenario: Named type base_url override
- **WHEN** a config of type `openai` is created with base_url "https://openai.acme-proxy.com/v1"
- **THEN** it is accepted and used as the versioned API base for that config's provider calls

#### Scenario: Typesafe base_url override
- **WHEN** a config of type `typesafe` is created with base_url "https://staging.typesafe.ai/v1/systemone"
- **THEN** it is accepted and used as the decision API base for that config's calls

#### Scenario: Typesafe without key rejected at verify-time contract
- **WHEN** a config of type `typesafe` is created with no key
- **THEN** creation succeeds (the key is optional at create like other key-requiring types) but the config verifies only after a key is stored; the decision backend SHALL refuse to run with a keyless config and fail open per its own contract

#### Scenario: Invalid base_url scheme
- **WHEN** a config is created with base_url "ftp://x"
- **THEN** response is 400 invalid_request (only http/https accepted)

#### Scenario: Update without touching the key
- **WHEN** PATCH changes only name and enabled
- **THEN** the stored key is preserved and `key_set` stays true

#### Scenario: Empty key update rejected
- **WHEN** PATCH sends key: ""
- **THEN** response is 400 invalid_request

### Requirement: Connection verification
POST `/workspaces/:ws/providers/:id/verify` SHALL decrypt the stored key and probe the provider per type: named types use their canonical (or overridden) origin; `openrouter` SHALL probe the key-info endpoint (`/api/v1/key`) because its models endpoint is public; `-compatible` types probe `{base_url}` with the type's canonical path; `typesafe` SHALL probe its canonical (or overridden) systemone origin with a minimal authenticated decision request (one noul question over trivial state) and read a 200 with an `answers` body as success. The result SHALL be returned synchronously as 200 {ok: true} or 200 {ok: false, error} and SHALL NOT be persisted on the config row. Verification SHALL require `providers.write`. Each provider type SHALL declare whether it requires an API key: the four named types (`openai`, `anthropic`, `gemini`, `openrouter`) and `typesafe` require one, and the `-compatible` types do not — their probes omit the auth header when no key is configured. Verifying a config without a key SHALL be 400 only when the config's type requires a key; a keyless config of a keyless-capable type SHALL verify with an empty key, delegating the auth question to the endpoint's own answer. Provider-side auth failures (401/403) SHALL be reported as ok:false with the provider error, not as a server 5xx.

#### Scenario: Successful probe
- **WHEN** verify is called on a config with a valid OpenAI key
- **THEN** 200 {ok: true}; the config row is unchanged afterward

#### Scenario: Invalid key
- **WHEN** the stored key is rejected by the provider with 401
- **THEN** 200 {ok: false, error contains the provider's message}; row unchanged

#### Scenario: Unreachable base_url
- **WHEN** base_url points at an address the server cannot reach
- **THEN** 200 {ok: false, error describes the transport failure}

#### Scenario: Typesafe probe
- **WHEN** verify is called on a `typesafe` config with a valid key
- **THEN** 200 {ok: true} when the endpoint answers with a decision body; a 401 reports 200 {ok: false, error} — never a server 5xx

#### Scenario: Verify without key
- **WHEN** verify is called on a keyless config whose type requires a key (e.g. `openai` or `typesafe`)
- **THEN** response is 400 invalid_request

#### Scenario: Keyless config verifies
- **WHEN** verify is called on a keyless config of a keyless-capable type (e.g. `openai-compatible`) whose endpoint needs no credential
- **THEN** 200 {ok: true} when the endpoint answers; a 401 from an endpoint that does require auth reports 200 {ok: false, error} — never a server 5xx

#### Scenario: Verify requires write permission
- **WHEN** a Member-role holder calls verify
- **THEN** response is 403 (verify uses the credential; read-only members cannot)
