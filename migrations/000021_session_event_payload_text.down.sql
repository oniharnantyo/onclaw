ALTER TABLE session_events
    ALTER COLUMN payload TYPE bytea USING convert_to(payload::text, 'utf8');
