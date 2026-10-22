-- Schema v4: agent prompt documents become files in per-agent workspace
-- directories (~/.buddies/workspaces/<slug>/). The row keeps the persona
-- metadata (slug, description, brief, prompt status) but never document
-- content. slug is nullable: pre-existing rows arrive without one and are
-- filled by the Go backfill that runs after this migration and before
-- 000005, so the UNIQUE index only ever sees distinct slugs or NULLs
-- (SQLite treats NULLs as distinct in unique indexes). system_prompt is
-- KEPT here — the backfill still reads it; 000005 drops it.

ALTER TABLE agents ADD COLUMN slug TEXT;

CREATE UNIQUE INDEX uq_agents_slug ON agents (slug);

ALTER TABLE agents ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN brief TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN prompts_status TEXT NOT NULL DEFAULT 'ready';
ALTER TABLE agents ADD COLUMN prompts_error TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN context_window INTEGER NOT NULL DEFAULT 0;
