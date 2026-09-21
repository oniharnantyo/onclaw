-- Reverse 000060 (wave3-memory-vectors-and-graph D7/D10): drop the
-- associative graph. Entities are pointer rows and edges are re-derivable
-- links, so the down path drops both with their indexes; the kept memory
-- stores (waves 0–2) are untouched, and the pg_trgm extension stays
-- installed exactly as 000054's down path left it.

DROP TABLE IF EXISTS memory_entity_edges;
DROP TABLE IF EXISTS memory_entities;
