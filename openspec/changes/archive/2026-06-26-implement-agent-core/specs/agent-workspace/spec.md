# agent-workspace

## Purpose

Define the agent workspace as a first-class concept: how it is resolved (including the
selected agent's default), how it scopes tools, and how it grounds the system prompt.

## ADDED Requirements

### Requirement: The workspace is resolved from flag, agent default, env, config, or cwd

The system SHALL resolve the agent workspace as the first available of: a `--workspace`
flag, the selected agent's `workspace` (whose default is `~/.onclaw/workspace/<agent>/`),
the `ONCLAW_WORKSPACE` environment variable, the `workspace` config key, then the current
working directory. The resolved workspace SHALL be an absolute path. The system SHALL NOT
`os.Chdir` into the workspace; it SHALL pass the path explicitly to tools.

#### Scenario: Flag overrides everything

- **WHEN** `onclaw run --workspace /tmp/proj` is run from elsewhere
- **THEN** the agent operates on `/tmp/proj`, overriding the agent default

#### Scenario: The agent default workspace is used

- **WHEN** the selected agent `coder` has no explicit `workspace` and no flag/env/config is set
- **THEN** the workspace resolves to `~/.onclaw/workspace/coder/`

#### Scenario: cwd is the final fallback

- **WHEN** no flag, agent default, env, or config is set and the user runs `onclaw run` from `/home/me/repo`
- **THEN** the workspace is `/home/me/repo`

### Requirement: The workspace scopes file and shell tools

The system SHALL confine `read_file`, `write_file`, and `list_dir` to the workspace and SHALL
run `shell` with the workspace as its working directory (`cmd.Dir`).

#### Scenario: The shell runs inside the workspace

- **WHEN** the agent runs an allowed `shell` command
- **THEN** the command executes with its working directory set to the workspace

### Requirement: The workspace grounds the system prompt

The system SHALL include the workspace's absolute path and a lightweight project-type hint in
the agent's system prompt so the model is grounded in its working directory.

#### Scenario: The prompt names the workspace

- **WHEN** the agent is assembled for a workspace at `/home/me/repo`
- **THEN** the system prompt references that path and a detected project-type hint (e.g. "Go module")

### Requirement: An agent has a default workspace created on add

The system SHALL treat `~/.onclaw/workspace/<agent>/` as the default workspace for an agent
whose `workspace` is empty, and `onclaw agent add` SHALL create that directory when no
`--workspace` is supplied. The directory is the agent's persistent working area; file and
shell tools SHALL scope to the resolved workspace.

#### Scenario: agent add creates the default workspace

- **WHEN** the user runs `onclaw agent add coder --provider glm` with no `--workspace`
- **THEN** the directory `~/.onclaw/workspace/coder/` is created and becomes the agent's default workspace
