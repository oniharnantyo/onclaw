-- Claude Code parity (design.md D19): per-hook `if` input-level condition —
-- `ToolName(pattern)` narrowing a tool-event hook to specific inputs. Named
-- if_rule to stay clear of the SQL keyword. '' = unset.
ALTER TABLE instance_hooks ADD COLUMN if_rule text NOT NULL DEFAULT '';
ALTER TABLE workspace_hooks ADD COLUMN if_rule text NOT NULL DEFAULT '';
ALTER TABLE agent_hooks ADD COLUMN if_rule text NOT NULL DEFAULT '';
