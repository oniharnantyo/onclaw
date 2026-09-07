-- Only the table is dropped; the fs tool names seeded into agent allowlists
-- are deliberately left in place — after a rollback they resolve as unknown
-- (inert) keys and stripping them could remove tools users added afterwards
-- (design.md D3 risk note).
DROP TABLE IF EXISTS workspace_tool_settings;
