ALTER TABLE agents
    ADD COLUMN disabled_tools text[] NOT NULL DEFAULT '{}',
    DROP COLUMN tools;
