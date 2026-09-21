## MODIFIED Requirements

### Requirement: Prompt documents are workspace files
An agent's prompt documents SHALL live as markdown files in the agent's workspace directory — `IDENTITY.md`, `SOUL.md`, `BOOTSTRAP.md` (generated) — with one exception: the L1 base prompt (`AGENTS.md`) SHALL NOT be materialized as a workspace file; it is platform-embedded content injected into the instruction at every composition as specified in the `agent-runtime` capability. The generated files SHALL be the single source of truth for their content; the database SHALL carry only `prompts_status` and `prompts_error`. Writes SHALL be atomic (write-to-temp-then-rename) with `0o755` directories and `0o644` files. A successful generation SHALL write the files before the ready transition, so a ready agent always has files on disk. On regenerate, stale files SHALL be left untouched until the new generation succeeds. Read-side APIs SHALL compose `identity`, `soul`, and `bootstrap` onto the agent from the files on every fetch; a missing or unreadable file SHALL compose as empty rather than failing the request. A startup sweep SHALL remove `AGENTS.md` files seeded into existing agent workspaces by earlier versions, so the workspace never shows a stale copy of injected content.

#### Scenario: Ready implies files on disk
- **WHEN** generation succeeds
- **ONLY THEN** `IDENTITY.md`, `SOUL.md`, and `BOOTSTRAP.md` exist in the workspace directory and `prompts_status` reads `ready`

#### Scenario: Create seeds AGENTS.md before generation
- **WHEN** a new agent is created
- **THEN** its workspace directory contains no `AGENTS.md` — the base prompt is platform-embedded content injected at composition (the `agent-runtime` capability), and generation proceeds over `IDENTITY.md`/`SOUL.md`/`BOOTSTRAP.md` alone

#### Scenario: Startup sweep removes seeded base prompts
- **WHEN** the server starts and an agent workspace still contains an `AGENTS.md` seeded by an earlier version
- **THEN** the sweep deletes it, leaving generated documents untouched

#### Scenario: Failed regeneration keeps old files
- **WHEN** regeneration fails for a previously ready agent
- **THEN** the old `IDENTITY.md`/`SOUL.md`/`BOOTSTRAP.md` remain on disk and `prompts_status` reads `failed`

#### Scenario: Compose-on-read projection
- **WHEN** a client fetches a ready agent
- **THEN** the response carries `identity`, `soul`, and `bootstrap` read from the workspace files; the database does not store the content
