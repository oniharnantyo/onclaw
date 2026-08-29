## Purpose

Workspaces as tenants: immutable slugs as external IDs, stateless tenant scoping (token identifies the user, URL identifies the workspace), creation via bootstrap or API, and the switcher payload that makes tenant switching a client-side concern.

## ADDED Requirements

### Requirement: Workspace creation
Creating a workspace SHALL atomically create the workspace, its three built-in roles, and the creator's Owner membership in one transaction. Creation SHALL validate the slug (DNS-label format, not reserved — `master` is instance-reserved) and reject duplicates with 409.

#### Scenario: API creation
- **WHEN** an authenticated user POSTs {name, slug, timezone} to /workspaces
- **THEN** response is 201 with the workspace and the creator's Owner membership

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
PATCH /workspaces/:ws SHALL require the `workspace.write` permission for name/timezone updates.

#### Scenario: Authorized update
- **WHEN** a member with workspace.write updates name or timezone
- **THEN** response is 200, workspace updated, slug untouched

#### Scenario: Unauthorized update
- **WHEN** a member without workspace.write PATCHes the workspace
- **THEN** response is 403

#### Scenario: First-run seeding (see instance-admin)
- **WHEN** a fresh instance seeds its first superadmin from the environment
- **THEN** the superadmin's home tenant is the default master tenant, not a customer workspace
