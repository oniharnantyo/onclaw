-- Session events: payload stored as UTF-8 JSON text instead of bytea
ALTER TABLE session_events
    ALTER COLUMN payload TYPE text USING convert_from(payload, 'utf8');
