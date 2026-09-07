-- Recreate the removed agents.disabled_skills denylist column (rollback of
-- the D2 breaking cleanup). Stored values are lost on the down/up round-trip.
ALTER TABLE agents
    ADD COLUMN disabled_skills text[] NOT NULL DEFAULT '{}';

DROP TABLE IF EXISTS workspace_skills;
