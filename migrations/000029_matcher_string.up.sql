-- Claude Code parity (design.md D19): the matcher JSONB tagged union —
-- {"type":"tools","mode":"all|except|only","tools":[...]} or
-- {"type":"regex","pattern":"..."} (000026) — collapses to one plain string
-- on all three hook tables. Reading is tier-first: '' and '*' are match-all;
-- charset-valid entries joined by '|' (',' and whitespace also split at the
-- API) match exactly or by trailing-".*" family; anything else is an
-- unanchored RE2 regex. 'except' is dropped (RE2 has no negative lookahead),
-- so mode "except" degrades to match-all.
--
-- One-time dev-data conversion (nothing committed/prod exists):
--   mode "except" (any type)                         → ''  (match-all)
--   {"type":"regex","pattern":p}                     → p
--   {"type":"tools","mode":"only","tools":[a,b,…]}   → a|b
--   mode "all" / {"type":"all"} / null / other junk  → ''
--
-- The conversion runs as an UPDATE staging the result as a JSON scalar
-- because the USING clause of ALTER COLUMN TYPE cannot contain subqueries;
-- '#>> '{}'' unwraps it while altering the column to text. NOT NULL is
-- preserved — the CASE's ELSE keeps every converted value non-null.

UPDATE instance_hooks SET matcher = to_jsonb(CASE
    WHEN jsonb_typeof(matcher) = 'object' AND matcher->>'mode' = 'except'
        THEN ''
    WHEN jsonb_typeof(matcher) = 'object' AND matcher->>'type' = 'regex'
        THEN COALESCE(matcher->>'pattern', '')
    WHEN jsonb_typeof(matcher) = 'object' AND matcher->>'type' = 'tools'
         AND matcher->>'mode' = 'only' AND jsonb_typeof(matcher->'tools') = 'array'
        THEN (SELECT COALESCE(string_agg(entry, '|'), '')
              FROM jsonb_array_elements_text(matcher->'tools') AS t(entry))
    ELSE ''
END);
ALTER TABLE instance_hooks ALTER COLUMN matcher TYPE text USING (matcher #>> '{}');

UPDATE workspace_hooks SET matcher = to_jsonb(CASE
    WHEN jsonb_typeof(matcher) = 'object' AND matcher->>'mode' = 'except'
        THEN ''
    WHEN jsonb_typeof(matcher) = 'object' AND matcher->>'type' = 'regex'
        THEN COALESCE(matcher->>'pattern', '')
    WHEN jsonb_typeof(matcher) = 'object' AND matcher->>'type' = 'tools'
         AND matcher->>'mode' = 'only' AND jsonb_typeof(matcher->'tools') = 'array'
        THEN (SELECT COALESCE(string_agg(entry, '|'), '')
              FROM jsonb_array_elements_text(matcher->'tools') AS t(entry))
    ELSE ''
END);
ALTER TABLE workspace_hooks ALTER COLUMN matcher TYPE text USING (matcher #>> '{}');

UPDATE agent_hooks SET matcher = to_jsonb(CASE
    WHEN jsonb_typeof(matcher) = 'object' AND matcher->>'mode' = 'except'
        THEN ''
    WHEN jsonb_typeof(matcher) = 'object' AND matcher->>'type' = 'regex'
        THEN COALESCE(matcher->>'pattern', '')
    WHEN jsonb_typeof(matcher) = 'object' AND matcher->>'type' = 'tools'
         AND matcher->>'mode' = 'only' AND jsonb_typeof(matcher->'tools') = 'array'
        THEN (SELECT COALESCE(string_agg(entry, '|'), '')
              FROM jsonb_array_elements_text(matcher->'tools') AS t(entry))
    ELSE ''
END);
ALTER TABLE agent_hooks ALTER COLUMN matcher TYPE text USING (matcher #>> '{}');
