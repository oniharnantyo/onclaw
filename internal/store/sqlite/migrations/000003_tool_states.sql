-- Global tool toggles: one row per built-in catalog tool the user has
-- explicitly toggled. Absence of a row means the tool is enabled (the
-- default); only an explicit disable persists. Name matches the built-in
-- tool catalog key in internal/agents.

CREATE TABLE tool_states (
    name       TEXT PRIMARY KEY,
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
