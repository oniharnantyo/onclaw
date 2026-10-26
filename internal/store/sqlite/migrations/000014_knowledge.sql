-- Schema v14: knowledge documents (add-knowledge-corpus design D2/D6). One
-- row per cataloged corpus document: the raw bytes live on disk under
-- ~/.buddies/knowledge/ — global documents at knowledge/global/, agent-scoped
-- at knowledge/agents/<agent-slug>/ — and this table is the catalog. scope is
-- 'global' or 'agent'; global rows carry a NULL agent_id, agent rows carry
-- both agent_id (the FK; under the connection's foreign_keys(1) deleting the
-- agent cascades its documents, mirroring schedules) and agent_slug (the path
-- component). The composite UNIQUE enforces the per-scope filename
-- uniqueness the store prechecks. Conventions as in schema v1: store-assigned
-- random hex ids, RFC3339Nano TEXT timestamps. No backfill; rollback is
-- DROP TABLE.
CREATE TABLE documents (
    id          TEXT PRIMARY KEY,
    filename    TEXT NOT NULL,
    mime        TEXT NOT NULL,
    size        INTEGER NOT NULL CHECK (size >= 0),
    scope       TEXT NOT NULL CHECK (scope IN ('global','agent')),
    agent_id    TEXT REFERENCES agents(id) ON DELETE CASCADE,
    agent_slug  TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    source      TEXT NOT NULL CHECK (source IN ('user','agent')),
    created_at  TEXT NOT NULL,
    UNIQUE (scope, agent_slug, filename)
);
