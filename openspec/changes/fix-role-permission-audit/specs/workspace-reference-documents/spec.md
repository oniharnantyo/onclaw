## ADDED Requirements

### Requirement: Ownership-scoped document management
Reference-document uploads SHALL remain available to every workspace member, and each document SHALL record its uploader. A member MAY edit, replace the content of, or delete only documents they uploaded; mutating a document uploaded by another member SHALL require `workspace.write`. Attaching a document to agents SHALL require `agents.write`; attaching to channels SHALL require `channels.write`; promote and demote remain gated by `reference_documents.promote`. The member listing lens SHALL follow the tiered-visibility rules: members see documents they uploaded plus documents attached to agents or channels they can configure; holders of `reference_documents.promote` see all workspace documents. Ownership and permission checks SHALL be enforced server-side on every mutating endpoint, not only in the UI.

#### Scenario: Member manages their own upload
- **WHEN** a Member renames, replaces the content of, or deletes a document they uploaded
- **THEN** each succeeds

#### Scenario: Member cannot mutate another member's document
- **WHEN** a Member attempts to rename, replace, or delete a document uploaded by another member
- **THEN** response is 403 (the actor lacks workspace.write)

#### Scenario: Admin mutates any document
- **WHEN** an Owner or Admin (workspace.write) edits or deletes a document uploaded by a Member
- **THEN** the mutation succeeds

#### Scenario: Attachment requires the surface's write permission
- **WHEN** a Member without agents.write attempts to attach or detach a document on an agent, or a Member without channels.write attempts the same on a channel
- **THEN** response is 403; the same attempts by an agents.write (respectively channels.write) holder succeed

#### Scenario: Member listing lens
- **WHEN** a Member lists reference documents without agent or channel lenses
- **THEN** the result contains their own uploads plus documents attached to agents or channels they can configure — not the full workspace library
