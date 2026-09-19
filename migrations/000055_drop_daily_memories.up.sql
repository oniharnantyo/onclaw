-- Legacy takeout (integrate-agent-zero-memory D14): drop the agent daily
-- memory table. The MEMORY-* tool paths and the daily store methods are
-- removed with it; durable facts are re-derived from session history by the
-- memory pipeline going forward. Data is intentionally NOT migrated (D14) —
-- operators must export any daily logs they want to keep before upgrading.
-- The table owns no standalone indexes: the composite PK is its only index,
-- and the named FK constraint dies with the table.

DROP TABLE IF EXISTS agent_daily_memories;
