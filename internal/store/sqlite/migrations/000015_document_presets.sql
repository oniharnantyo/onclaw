-- Schema v15: document presets (add-create-document design D4/D5). One row
-- per saved presentation configuration for the create_document tool: name is
-- the user-visible label, format the document format the preset applies to
-- (only 'pdf' in v1 — the column exists so later format engines need no
-- reshaping), and config the preset's opaque JSON document (a buddies-defined
-- schema, design D5) persisted verbatim — the store never interprets it. The
-- composite UNIQUE over the NOCASE name enforces per-format name uniqueness,
-- mirroring schedules' name index. The default-preset selection does not
-- live in this table: it rides tool_settings under the create_document tool
-- key. No backfill (no presets are seeded); rollback is DROP TABLE.
CREATE TABLE document_presets (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL COLLATE NOCASE,
    format     TEXT NOT NULL DEFAULT 'pdf',
    config     TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_document_presets_format_name ON document_presets(format, name);
