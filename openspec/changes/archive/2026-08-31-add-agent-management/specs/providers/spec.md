## MODIFIED Requirements

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
