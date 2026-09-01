-- Prompt documents move to the agent workspace directory (IDENTITY.md / SOUL.md);
-- the DB keeps only the generation lifecycle (prompts_status / prompts_error).
ALTER TABLE agents DROP COLUMN identity;
ALTER TABLE agents DROP COLUMN soul;
