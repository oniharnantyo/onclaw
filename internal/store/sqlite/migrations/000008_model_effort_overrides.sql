-- Schema v8: per-session model/reasoning-effort overrides and the agent's
-- default reasoning effort (integrate-agent-chat design D1/D2). Empty means
-- inherit: a session falls back to its agent's configuration, and an agent
-- without a stored effort runs at the provider default. No data backfill —
-- every existing row keeps '' and behaves exactly as before the columns
-- existed.

ALTER TABLE sessions ADD COLUMN model  TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN effort TEXT NOT NULL DEFAULT '';
ALTER TABLE agents   ADD COLUMN effort TEXT NOT NULL DEFAULT '';
