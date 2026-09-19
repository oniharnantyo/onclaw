-- Reverse 000054 (integrate-agent-zero-memory): drop the two memory stores.
-- Rows are inert once the ingestion worker stops reading them, so removal
-- restores pre-000054 behavior. The pg_trgm extension is intentionally left
-- installed: the down path drops only what this migration alone owns, and
-- dropping an extension under other gin_trgm_ops indexes would fail anyway.

DROP TABLE IF EXISTS memory_notes;
DROP TABLE IF EXISTS memory_events;
