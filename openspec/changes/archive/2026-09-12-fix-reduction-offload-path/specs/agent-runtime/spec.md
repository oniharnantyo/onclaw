## MODIFIED Requirements

### Requirement: Filesystem jail
File tools (list, read, write, edit, glob, grep, delete) and runtime capability middlewares (reduction truncation and context clearing) SHALL operate on virtual mount paths scoped to the agent's workspace directory (mounted at `/workspace`); any resolved path escaping it SHALL be rejected as a tool error, not a crash. The agent's generated prompt documents, its agent-tier skills directory, summarization offload files, and tool reduction offload artifacts (`/workspace/trunc/...` and `/workspace/clear/...`) all live inside this directory and are reachable through the file tools, including deletion of prompt documents such as `BOOTSTRAP.md`. (Shell execution is no longer banned outright — it is governed by the "Shell execution" and "Dangerous-command approval" requirements below.) Additionally, an agent that is a member of a channel with a project space SHALL have that channel's project directory mounted read-write at `/project` inside its jail (see the `channel-teams` capability); all jail rules apply to `/project` identically.

#### Scenario: Path escape rejected
- **WHEN** a file tool is invoked with a path resolving outside the agent's workspace directory (including via symlink or `..`)
- **THEN** the tool returns an error result and no file outside the directory is read or written

#### Scenario: Delete within the jail
- **WHEN** a file tool deletes a document inside the agent's workspace directory
- **THEN** the file is removed; deleting a missing file is an error result, not a crash

#### Scenario: Shared project root writable for members
- **WHEN** a channel member agent writes `/project/spec.md`
- **THEN** the write succeeds inside the channel's project directory and other member agents read it

#### Scenario: Project root cannot escape the jail
- **WHEN** a member agent follows a symlink in `/project` pointing outside the workspace data root
- **THEN** the resolved path is rejected as a tool error

#### Scenario: Tool reduction offloads under workspace mount
- **WHEN** a tool produces output exceeding the reduction truncation threshold or context clearing triggers
- **THEN** the middleware offloads the content to `/workspace/trunc/<call_id>` or `/workspace/clear/<call_id>` via the jail backend without triggering an absolute-path rejection error, and the agent can read the offloaded file using `read_file`
