-- Reverse 000056 (integrate-agent-zero-memory D12): drop the consolidator's
-- evidence links and the last morning reports. Evidence links are derived
-- pointers (the raw session events they cited remain untouched) and reports
-- are regenerable by the next pass, so the down path drops the rows with the
-- tables — the 000054 memory stores stay applied.

DROP TABLE IF EXISTS memory_reports;
DROP TABLE IF EXISTS memory_note_evidence;
