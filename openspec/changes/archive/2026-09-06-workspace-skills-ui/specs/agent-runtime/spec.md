# workspace-skills-ui Delta: agent-runtime

## MODIFIED Requirements

### Requirement: Three-tier skills
Skills SHALL be discovered from three sources: the system tier (`<ONCLAW_DIR>/skills`, embedded and mirrored at startup), the workspace tier (`<ONCLAW_DIR>/workspaces/<tenant_slug>/skills/<name>/` where each skill carries a registry row), and the agent tier (`<ONCLAW_DIR>/workspaces/<tenant_slug>/agents/<agent_slug>/skills`). Attachment SHALL be governed by tier rules with no per-agent skill denylist: system-tier skills SHALL attach to every agent always; a workspace-tier skill SHALL attach to every agent in the workspace when its registry row is `enabled` and to no agent when disabled; agent-tier skills SHALL attach to their owning agent only. There is no per-agent skill toggle at any tier. On name collision the most specific tier SHALL win: agent > workspace > system. Skills SHALL be consumed through progressive disclosure: metadata lists first, full SKILL.md bodies fetched on demand.

#### Scenario: System skills cannot be disabled
- **WHEN** any actor attempts to disable a system-tier skill
- **THEN** no operation or affordance exists; the skill remains attached to all agents

#### Scenario: Master switch governs workspace skills
- **WHEN** a workspace skill's registry row is disabled
- **THEN** no agent in that workspace attaches the skill until the row is re-enabled

#### Scenario: Agent authors its own skill
- **WHEN** an agent writes a new SKILL.md document into its own skills directory via a file tool
- **THEN** the skill is discoverable in a subsequent execution of that agent

#### Scenario: Collision precedence
- **WHEN** a skill name exists in both the workspace tier and the agent tier
- **THEN** the agent-tier document is the one served

#### Scenario: System skills synced at startup
- **WHEN** the server starts
- **THEN** the embedded system skill set is mirrored into `<ONCLAW_DIR>/skills` — changed files overwritten, files not in the embedded set removed

## ADDED Requirements

### Requirement: Explicit skill mentions
For every execution surface, user input SHALL be scanned for `$name` tokens matching an available skill (system, enabled workspace, or owning-agent tier). A match SHALL turn into a blocking instruction — injected ahead of the user's message — directing the agent to invoke the skill tool for that name before producing any other response; execution SHALL still flow through the skill middleware's tool. Non-matching `$` tokens SHALL pass through as ordinary text.

#### Scenario: Mention forces skill load
- **WHEN** an execution's input contains `$web-research` and the skill is available
- **THEN** the agent invokes the skill tool for `web-research` before generating any other response about the task

#### Scenario: Disabled skill mention is plain text
- **WHEN** an execution's input contains the name of a workspace skill whose registry row is disabled
- **THEN** no blocking instruction is injected and the token is ordinary text

### Requirement: Workspace skill reachability in the jail
The filesystem jail SHALL grant read-only access to the workspace skills directory tree (`<ONCLAW_DIR>/workspaces/<tenant_slug>/skills`) in addition to the agent's own directory: file tools SHALL resolve absolute paths under either root, reads and greps SHALL succeed, and write/edit SHALL remain restricted to the agent's own directory. Symlink escape checking SHALL apply per root. Shell executions SHALL resolve skill runtime environments through PATH precedence: when a workspace skill venv exists, its bin directory SHALL precede the default PATH for shell commands in that workspace.

#### Scenario: Bundled file readable by absolute path
- **WHEN** an agent reads `<ONCLAW_DIR>/workspaces/<tenant_slug>/skills/pdf-toolkit/references/usage.md` via a file tool
- **THEN** the read succeeds

#### Scenario: Write into workspace skills rejected
- **WHEN** an agent attempts to write or edit any file under the workspace skills directory
- **THEN** the tool returns an error and no file is modified

#### Scenario: Symlink escape still rejected
- **WHEN** a file tool resolves a symlink under the workspace skills root pointing outside both roots
- **THEN** the operation is rejected as a path escape

#### Scenario: Venv python resolves first
- **WHEN** a shell command runs `python3` in a workspace whose skills venv exists
- **THEN** the venv's python interpreter executes and its installed packages are importable
