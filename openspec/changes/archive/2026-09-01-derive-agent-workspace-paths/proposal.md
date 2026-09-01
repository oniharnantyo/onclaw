# Derive agent workspace paths

## Why

The agent's on-disk workspace directory is snapshotted into the `workspace_dir` column at create time. A snapshotted path can only go stale: a server run with a relative `ONCLAW_WORKSPACE_DIR` froze a relative path into existing rows (observed: an agent whose `workspace_dir` is `.onclaw/workspaces/master/agents/research`, resolved against whatever cwd the server happens to have at each use), config-root changes never apply to existing agents, and a slug rename leaves files in the old slug-derived directory. Deriving the path on demand from the current root and the slugs eliminates the entire failure class — the path is a pure function of (root, workspace slug, agent slug), and agent slugs become immutable so the derivation is stable for the agent's lifetime.

## What Changes

- **BREAKING** — the agent's `workspace_dir` column and API field are removed; the path is derived at the point of use from the current `ONCLAW_WORKSPACE_DIR` root and the slugs (`<root>/<workspace_slug>/agents/<agent_slug>`).
- Agent slugs become immutable: update payloads that include a slug SHALL be ignored (managed-field semantics, like `prompts_status`), and the wizard shows the slug read-only in edit mode.
- The agents generation service stops reading `agent.WorkspaceDir` and receives the directory as a parameter; handlers derive the directory from the current root.
- The composition root validates the workspace root is absolute at startup and refuses to start otherwise (fail fast instead of cwd-dependent paths).
- A migration drops the `workspace_dir` column. Existing agent directories are relocated operationally: move `<old-path>` to `<new-root>/<workspace_slug>/agents/<agent_slug>` while the server is stopped (documented; no data rewrite needed since slugs and layout are unchanged).

## Capabilities

### New Capabilities
- none

### Modified Capabilities
- `agents` — workspace-directory requirement rewritten (derived paths, no column, startup absoluteness validation, slug immutability); slug-conflict scenario scoped to create; agent-fields requirement gains slug-immutability; slug-rename scenario removed.
- `web-app/agents` — new requirement scoping slug editing to create mode; update payloads omit slug.
