# add-tools-management

A Tools management surface (REST API + Web UI) that lists builtin tools grouped by category, toggles each tool globally on/off via a new `tool_registry` table, and edits per-category configuration (e.g. the Browser engine) via a new `tool_group_config` table — with agent assembly intersecting global-enabled ∩ per-agent allowlist. No behavioral change when all tools remain enabled.
