## REMOVED Requirements

### Requirement: Skills CRUD
**Reason**: Skills move from database rows to filesystem directories with three tiers (system, workspace, agent) — see the agent-runtime capability. Per-workspace CRUD endpoints over the `workspace_skills` table no longer exist.
**Migration**: System skills ship embedded in the server binary and are mirrored into `<ONCLAW_DIR>/skills` on every start; workspace skills are files under `<ONCLAW_DIR>/workspaces/<tenant_slug>/skills/`; agent skills are files under the agent's own directory. The `workspace_skills` table and its endpoints are dropped.

### Requirement: Progressive disclosure contract
**Reason**: Superseded by the agent-runtime capability's three-tier skills requirement, which defines discovery (tier directories) and on-demand body disclosure directly against the filesystem.
**Migration**: The runtime lists skill metadata and fetches SKILL.md bodies from the tier directories; no HTTP surface is required for the runtime's own consumption.

### Requirement: Agent subscription validated
**Reason**: Agents no longer store a skills allowlist. The denylist fields (`disabled_skills`) are not referentially validated; unknown names are inert.
**Migration**: Existing agent `skills` arrays are not migrated — the denylist model starts empty (everything enabled). Rows in `workspace_skills` are dropped with the table.
