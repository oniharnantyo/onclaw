# tenancy Specification

## Purpose

Workspaces as tenants: immutable slugs as external IDs, stateless tenant scoping (token identifies the user, URL identifies the workspace), creation via bootstrap or API, and the switcher payload that makes tenant switching a client-side concern.

## Requirements

### Requirement: Workspace creation
Creating a workspace SHALL atomically create the workspace, its three built-in roles, and the creator's Owner membership in one transaction. The create payload MAY include a `description` (free text, display description of the workspace; optional). Creation SHALL validate the slug (DNS-label format, not reserved — `master` is instance-reserved) and reject duplicates with 409. The create payload MAY additionally include a `provider` object and a `starter_agent` object; when present, the provider config and starter agent SHALL be created in the same transaction (atomic birth): the starter agent is born configured (its provider_id points at the just-created config) with `prompts_status` `generating`, and its identity/soul prompts generate in the background per the agent-prompts capability. Provider and starter agent are all-or-nothing with the workspace: any validation failure aborts the entire birth.

#### Scenario: API creation
- **WHEN** an authenticated user POSTs {name, slug, timezone, description?} to /workspaces
- **THEN** response is 201 with the workspace (carrying the description when provided) and the creator's Owner membership

#### Scenario: Birth with provider and starter agent
- **WHEN** the payload includes provider {type, name, base_url?, key} and starter_agent {name, slug, role, brief, model, ...}
- **THEN** 201 returns workspace + provider config + starter agent (prompts_status generating); all three exist or none do

#### Scenario: Birth validation failure aborts everything
- **WHEN** the starter agent payload fails validation (e.g. anthropic provider without max_tokens)
- **THEN** response is 400 and the workspace is NOT created

#### Scenario: Duplicate slug
- **WHEN** a slug already in use is submitted
- **THEN** response is 409 conflict

#### Scenario: Invalid slug
- **WHEN** the slug fails DNS-label format or is reserved
- **THEN** response is 400 invalid_request

### Requirement: Immutable slug
A workspace's slug SHALL be immutable once created; rename SHALL change the name only. Slug is the workspace's external ID in URLs.

#### Scenario: Rename keeps slug
- **WHEN** name is changed via PATCH /workspaces/:ws
- **THEN** slug stays the same and older scoped URLs remain valid

### Requirement: Stateless tenant scoping
Tenant-scoped endpoints SHALL be addressed as /workspaces/:ws/…, with the token identifying only the user. There SHALL be no server-side active-workspace state; the switcher is client-side.

#### Scenario: Member access
- **WHEN** a member requests a tenant-scoped resource in a workspace they belong to
- **THEN** the handler runs with workspace, membership, and role bound

#### Scenario: Non-member 404
- **WHEN** a non-member (or unknown slug) requests a tenant-scoped resource
- **THEN** response is 404 — indistinguishable from unknown workspace

### Requirement: Switcher payload
GET /auth/me (and GET /workspaces) SHALL return every workspace the user belongs to, with their role in each — so tenant switching needs no server-side state.

#### Scenario: Two workspaces
- **WHEN** a user belongs to two workspaces
- **THEN** /auth/me lists both, each with the user's role

### Requirement: Workspace settings
PATCH /workspaces/:ws SHALL require the `workspace.write` permission for name/timezone/description updates and for setting or clearing the workspace default model. The default model SHALL be stored as a `(default_provider_id, default_model)` pair governed by both-or-neither semantics: both set pins the default, both empty (or both absent from the payload) leaves it unset; a half-set pair SHALL be 400 invalid_request, and a set pair SHALL reference a provider config in the same workspace (unknown provider SHALL be 400 invalid_request). Clearing the default SHALL be refused with 422 while any agent in the workspace inherits it (empty provider/model pair), the refusal naming the agent count. Workspace read payloads (including the switcher/boot payload) SHALL carry the default model as `{provider_id, model}` so clients can offer it for agent inheritance.

#### Scenario: Authorized update
- **WHEN** a member with workspace.write updates name, timezone, or description
- **THEN** response is 200, workspace updated, slug untouched

#### Scenario: Set default model
- **WHEN** a member with workspace.write PATCHes `default_model: {provider_id, model}` naming a workspace provider
- **THEN** response is 200 and the pair persists on the workspace

#### Scenario: Half-set pair rejected
- **WHEN** a PATCH carries a model with no provider id (or the reverse)
- **THEN** response is 400 invalid_request

#### Scenario: Unknown provider rejected
- **WHEN** a PATCH sets `default_model` naming a provider id that does not exist in the workspace
- **THEN** response is 400 invalid_request

#### Scenario: Clearing blocked while inherited
- **WHEN** a PATCH clears `default_model` while agents with an empty provider/model pair exist in the workspace
- **THEN** response is 422 naming the inherit-agent count, and the stored default is unchanged

#### Scenario: Payload carries the default model
- **WHEN** a client reads the workspace through the switcher/boot payload
- **THEN** the payload includes `default_model` (`null` when unset)

#### Scenario: Unauthorized update
- **WHEN** a member without workspace.write PATCHes the workspace
- **THEN** response is 403

#### Scenario: First-run seeding (see instance-admin)
- **WHEN** a fresh instance seeds its first superadmin from the environment
- **THEN** the superadmin's home tenant is the default master tenant, not a customer workspace
