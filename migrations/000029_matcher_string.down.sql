-- Reverse 000029 best-effort: matcher text back to the JSONB tagged union.
-- INHERENTLY LOSSY — the up conversion collapsed two different shapes onto
-- the same value, so '' restores as mode "all" (mode "except" cannot be
-- distinguished from mode "all"); a '*' matcher restores as a regex; and a
-- regex whose text happens to be |-separated charset-valid entries restores
-- as a tools list. Strings that split cleanly on '|' into charset-valid
-- entries ([A-Za-z0-9_.-]+ with optional trailing ".*") restore as
-- {"type":"tools","mode":"only","tools":[...]}; everything else becomes
-- {"type":"regex","pattern":...}.

ALTER TABLE instance_hooks ALTER COLUMN matcher TYPE jsonb USING (CASE
    WHEN matcher = ''
        THEN '{"type":"tools","mode":"all"}'::jsonb
    WHEN matcher ~ '^([A-Za-z0-9_.-]+(\.\*)?\|)*[A-Za-z0-9_.-]+(\.\*)?$'
        THEN jsonb_build_object('type', 'tools', 'mode', 'only',
                                'tools', to_jsonb(regexp_split_to_array(matcher, '\|')))
    ELSE jsonb_build_object('type', 'regex', 'pattern', matcher)
END);

ALTER TABLE workspace_hooks ALTER COLUMN matcher TYPE jsonb USING (CASE
    WHEN matcher = ''
        THEN '{"type":"tools","mode":"all"}'::jsonb
    WHEN matcher ~ '^([A-Za-z0-9_.-]+(\.\*)?\|)*[A-Za-z0-9_.-]+(\.\*)?$'
        THEN jsonb_build_object('type', 'tools', 'mode', 'only',
                                'tools', to_jsonb(regexp_split_to_array(matcher, '\|')))
    ELSE jsonb_build_object('type', 'regex', 'pattern', matcher)
END);

ALTER TABLE agent_hooks ALTER COLUMN matcher TYPE jsonb USING (CASE
    WHEN matcher = ''
        THEN '{"type":"tools","mode":"all"}'::jsonb
    WHEN matcher ~ '^([A-Za-z0-9_.-]+(\.\*)?\|)*[A-Za-z0-9_.-]+(\.\*)?$'
        THEN jsonb_build_object('type', 'tools', 'mode', 'only',
                                'tools', to_jsonb(regexp_split_to_array(matcher, '\|')))
    ELSE jsonb_build_object('type', 'regex', 'pattern', matcher)
END);
