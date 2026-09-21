-- Reverse 000059 (wave3-memory-vectors-and-graph D1): drop the vector
-- evidence index. Embeddings are derived data — re-embeddable from their
-- source rows and raw turn text — so the down path drops the rows with the
-- table and retrieval degrades to the lexical channel. The pgvector
-- extension is intentionally left installed: the down path drops only what
-- this migration alone owns, and dropping a shared extension type under
-- other users would fail anyway.

DROP TABLE IF EXISTS memory_embeddings;
