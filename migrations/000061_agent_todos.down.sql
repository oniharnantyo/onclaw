-- Reverse 000061 (adopt-assistant-ui-elements D5): drop the todo store. The
-- transcript keeps the revision history, so the down path loses only current
-- state — no other data shape depends on it.

DROP TABLE IF EXISTS agent_todos;
