# workspace-skills Specification

## Purpose

Makes skills a first-class managed resource: a database-backed workspace skill library with install sources (author, upload, git/URL, fork), a tier-based activation model with no per-agent toggles, install-time dependency resolution and provisioning, explicit `$skill-name` invocation on every execution surface, and `skills.write`-gated management APIs.

## Requirements

### Requirement: Workspace skill registry
Each workspace-level skill SHALL have one row in the workspace skill registry storing: name (unique per workspace, DNS-label rules), description, version, source (`authored` | `upload` | `git` | `fork`), `enabled` (default true), the resolved dependency set (JSON), and timestamps. The multi-file skill body SHALL live on disk at `<ONCLAW_DIR>/workspaces/<tenant_slug>/skills/<name>/`. System-tier skills SHALL NOT be registered (they are embedded and read-only); agent-tier skills SHALL NOT be registered (they are per-agent directories only).

#### Scenario: Author install creates a registry row
- **WHEN** an admin authors a skill named `changelog-sweeper` through the install wizard
- **THEN** the registry contains a row with name `changelog-sweeper`, version `0.1.0`, source `authored`, `enabled` true, and the body files exist under the workspace skills directory

#### Scenario: Registry name is unique per workspace
- **WHEN** an install names a skill identical to an existing workspace skill
- **THEN** the wizard offers overwrite-with-confirm (files replaced, version bumped) or abort; two workspace skills with the same name cannot coexist

### Requirement: Skill installation sources
The install wizard SHALL support four sources: **Author** (name, description, SKILL.md body editor, version 0.1.0), **Upload** (a zip archive or client-walked folder; SKILL.md MUST exist at the archive root; bundled `scripts/` and `references/` preserved), **Git/URL** (server-side shallow clone or archive fetch with optional ref and token; the server SHALL scan the fetched tree for directories containing SKILL.md and let the user select which to import; fetch tokens SHALL NOT be persisted with the skill), and **Fork** (copy an embedded system skill into the workspace tier with source `fork` — the only customization path for system skills). The skill name SHALL derive from the directory name, slugified to the DNS-label rules and reserved list shared with agents and workspaces. Archive extraction SHALL reject path-escape entries (resolved paths escaping the skill directory, absolute paths, symlinks pointing outside) and enforce size and file-count caps; a rejected extraction SHALL write nothing.

#### Scenario: Upload without SKILL.md is rejected
- **WHEN** the user uploads a zip whose root contains no SKILL.md
- **THEN** the wizard step shows an error and no files are written to disk

#### Scenario: Zip-slip entry rejected
- **WHEN** an archive contains an entry whose resolved path escapes the skill directory
- **THEN** extraction aborts with an error and nothing is installed

#### Scenario: Multi-skill repository offers selection
- **WHEN** a git URL is fetched and the tree contains multiple directories with SKILL.md
- **THEN** the wizard lists each discovered skill with its description and the user selects which to import

#### Scenario: Fork a system skill
- **WHEN** the user forks the `web-research` system skill to the workspace
- **THEN** a workspace skill with source `fork` is created from the embedded content and the system skill itself remains locked and unchanged

### Requirement: Skill activation tiers
Skills SHALL be governed by tier rules with no per-agent skill toggle at any tier: **system** skills SHALL be injected into every agent's execution always and SHALL NOT be disableable by any actor (no API operation and no UI affordance exists); a **workspace** skill SHALL be attached to every agent in the workspace when its registry row is `enabled` and to no agent when disabled — the workspace master switch and uninstall are the only controls; an **agent-tier** skill SHALL attach to its owning agent only, and removal is deletion. On name collision the most specific tier SHALL win: agent > workspace > system.

#### Scenario: Enabled skill reaches a new agent automatically
- **WHEN** a workspace skill is enabled and a new agent is created in that workspace
- **THEN** the agent's executions list the skill in its available-skills metadata with no assignment step

#### Scenario: Master switch disables everywhere
- **WHEN** an admin disables a workspace skill
- **THEN** no agent in the workspace attaches it and it disappears from every `$` skill menu, until re-enabled

#### Scenario: No disable exists for system skills
- **WHEN** any actor attempts to disable a system-tier skill
- **THEN** no such API operation or UI affordance exists and the skill remains attached to all agents

#### Scenario: Agent-tier skill is private
- **WHEN** a skill is installed into one agent's skills directory
- **THEN** only that agent's executions list it

### Requirement: Skill dependency resolution
Skills SHALL declare dependencies in SKILL.md frontmatter (`dependencies.tools`, `dependencies.binaries`, `dependencies.python` — an OnClaw extension ignored by the middleware) and ecosystem manifests (`requirements.txt` beside scripts). At import, undeclared dependencies SHALL be inferred (tool-name patterns in the body, script imports/manifests) and the inferred set SHALL be stored on the registry row without modifying imported files. Resolution per kind: **tools** resolve against the workspace tool gate and agent tool allowlists, with a pre-checked "enable everywhere" install option that adds the tool to the gate and every agent's allowlist (agent-tier skills scope this to the owning agent); a non-empty `scripts/` directory implies the reserved `execute` shell tool as a dependency; **binaries** are probed on the server PATH and never auto-installed — the report names the package-manager command per platform with a re-check action; **python packages** provision into one shared per-workspace venv (pip union install across all enabled skills; a version conflict between skills SHALL name both sides and fail the dependency step) automatically when python is present on the server, otherwise reported as guidance. Unmet dependencies SHALL NOT gate execution: missing tools and missing binaries surface as natural tool errors in the transcript. Dependency status SHALL be re-checkable on demand.

#### Scenario: Requirements.txt feeds the declaration
- **WHEN** a skill is imported with `scripts/requirements.txt` containing `pypdf>=4.0` and no frontmatter declaration
- **THEN** the registry row stores `pypdf>=4.0` as a python dependency and the imported files are unchanged

#### Scenario: Enable-everywhere satisfies tool dependencies
- **WHEN** a skill declaring `dependencies.tools: [web.search]` is installed with the pre-checked enable option on
- **THEN** the workspace tool gate allows `web.search` and every agent's tool allowlist contains it

#### Scenario: Missing binary reports guidance
- **WHEN** a declared binary `pdftotext` is absent from the server PATH at install
- **THEN** the report shows the per-platform install command and a re-check action, and the skill still installs with a persistent warning chip

#### Scenario: Python packages provision into the workspace venv
- **WHEN** a skill with a python dependency is installed on a server with python3 present
- **THEN** the workspace shared venv installs the package (union with other enabled skills) and verification of the import succeeds

#### Scenario: Cross-skill version conflict named
- **WHEN** two enabled skills require incompatible versions of the same python package
- **THEN** the dependency step fails naming both skills' requirements and previously installed skills are untouched

### Requirement: Explicit skill invocation
The runtime SHALL recognize `$name` skill mentions in user input on every execution surface — direct chats, channels, and cron prompts — and convert them into a blocking instruction to invoke the skill tool for that name before responding; the skill middleware SHALL remain the single execution path, so explicit invocations render as tool-call cards in transcripts. A `$name` that matches no enabled skill (unknown, or workspace-disabled) SHALL be inert: transmitted as plain text.

#### Scenario: Dollar invocation in a direct chat
- **WHEN** a user sends `$web-research latest quantum computing news` to an agent
- **THEN** the execution records a skill tool invocation for `web-research` before the agent's response, rendered as a tool-call card

#### Scenario: Dollar invocation in a cron prompt
- **WHEN** a cron schedule's prompt contains `$web-research`
- **THEN** the scheduled execution invokes the skill the same way as a chat invocation

#### Scenario: Unknown name is inert
- **WHEN** a user sends `$no-such-skill hello`
- **THEN** the message is treated as plain text and no skill invocation occurs

### Requirement: Skills API and permission gating
The API SHALL expose workspace skills under `/workspaces/{slug}/skills`: list (registry rows plus the system tier, marked locked) requires `skills.read`; create (all sources), update (metadata and body), enable/disable, and delete require `skills.write` and return 403 otherwise. Agent-tier skill install and removal SHALL be managed under the agent's endpoints with the same `skills.write` gate. Every built-in role keeps its catalog grant: Owner/Admin/Superadmin manage, Member reads at every tier.

#### Scenario: Member cannot install
- **WHEN** a Member-role holder POSTs to the workspace skills endpoint
- **THEN** the response is 403 and no registry row or files are created

#### Scenario: Listing includes the system tier
- **WHEN** a Member lists workspace skills
- **THEN** the response includes system-tier skills marked locked alongside registry rows, read-only
