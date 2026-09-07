-- Agent tools: allowlist replaces the denylist
ALTER TABLE agents
    ADD COLUMN tools text[] NOT NULL DEFAULT '{}',
    DROP COLUMN disabled_tools;
