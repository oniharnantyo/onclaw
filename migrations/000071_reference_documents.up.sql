-- Reference documents (add-reference-documents D1/D3/D7): the persistent
-- workspace library of as-is uploaded service documentation. The blob bytes
-- live in a storage-port driver under a random capability key (storage_key —
-- the wire token for serving, unique because a key names exactly one
-- document); the row records which backend holds the bytes (attachments
-- 000042 precedent). Derived state lives in document_sections below and is
-- rebuildable from the blob alone — the extracted text is never stored
-- anywhere else.
--
-- Tiered visibility (D7): scope ∈ {attached, workspace} picks the tier;
-- the two join tables carry the attached agents/channels. Join rows CASCADE
-- on both sides: deleting a document cleans its joins, and deleting an
-- attached agent or channel cleans the reverse direction (tenancy
-- requirement — no dangling references). Every table denormalizes
-- workspace_id (house tenant rule).

CREATE TABLE reference_documents (
    id           uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name         text NOT NULL,
    description  text NOT NULL DEFAULT '',
    mime         text NOT NULL,
    size_bytes   bigint NOT NULL,
    storage_key  text NOT NULL UNIQUE,
    backend      text NOT NULL,
    page_count   int NOT NULL DEFAULT 0,
    scope        text NOT NULL DEFAULT 'attached'
                 CHECK (scope IN ('attached', 'workspace')),
    index_status text NOT NULL DEFAULT 'processing'
                 CHECK (index_status IN ('ready', 'no_text_layer', 'processing')),
    uploaded_by  uuid NOT NULL REFERENCES users(id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_reference_documents_workspace ON reference_documents(workspace_id);

CREATE TABLE reference_document_agents (
    document_id  uuid NOT NULL REFERENCES reference_documents(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id     uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    PRIMARY KEY (document_id, agent_id)
);
CREATE INDEX idx_reference_document_agents_agent ON reference_document_agents(workspace_id, agent_id);

CREATE TABLE reference_document_channels (
    document_id  uuid NOT NULL REFERENCES reference_documents(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id   uuid NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    PRIMARY KEY (document_id, channel_id)
);
CREATE INDEX idx_reference_document_channels_channel ON reference_document_channels(workspace_id, channel_id);

-- The section index (D2/D3): one row per sectioner output slice, with the
-- rendered per-type locator alongside its kind, and the full-text body.
-- body_tsv is a STORED generated column (unlike the memory_notes expression
-- index) so search queries and ranking reference body_tsv directly.
CREATE TABLE document_sections (
    id           uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    document_id  uuid NOT NULL REFERENCES reference_documents(id) ON DELETE CASCADE,
    heading      text NOT NULL DEFAULT '',
    locator      text NOT NULL DEFAULT '',
    locator_kind text NOT NULL DEFAULT 'none'
                 CHECK (locator_kind IN ('page', 'slide', 'sheet', 'heading', 'none')),
    level        int NOT NULL DEFAULT 0 CHECK (level BETWEEN 0 AND 3),
    ordinal      int NOT NULL,
    body         text NOT NULL,
    body_tsv     tsvector GENERATED ALWAYS AS (to_tsvector('english', body)) STORED
);
CREATE INDEX idx_document_sections_tsv ON document_sections USING gin (body_tsv);
CREATE INDEX idx_document_sections_workspace ON document_sections(workspace_id);
CREATE INDEX idx_document_sections_document ON document_sections(document_id, ordinal);

-- reference_documents.promote joined the permission catalog after existing
-- built-in role rows were created; built-in role permissions are copied from
-- catalog constants at role creation with no runtime derivation, so Owner and
-- Admin rows that predate this migration keep the older snapshot and every
-- promote/demote call would 403 (the 000027/000031/000049/000066 backfill
-- precedent). Member gains nothing — promotion is admin-only surface.
UPDATE roles
SET permissions = array_append(permissions, 'reference_documents.promote')
WHERE built_in = true AND name IN ('Owner', 'Admin', 'Superadmin')
  AND NOT ('reference_documents.promote' = ANY(permissions));
