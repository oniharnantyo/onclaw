# Spec Delta — agent-prompts

## REMOVED Requirements

### Requirement: BOOTSTRAP.md birth sequence

**Why:** the birth ritual is a fixed-template special case across every prompt-document layer (seed, compose, project, display) for a one-time cold-start aid the base prompt and IDENTITY/SOUL personas already cover; the maintainer removed the feature 2026-09-26. The requirement also described an LLM-generated personalized document while the shipped code seeded a fixed template — removed rather than reconciled.

**Migration:** the startup sweep removes stray `BOOTSTRAP.md`/`.bak` files from existing agent workspaces (see the modified "Prompt documents are workspace files" requirement).

## MODIFIED Requirements

### Requirement: Prompt documents are workspace files

An agent's prompt documents SHALL live as markdown files in the agent's workspace directory — `IDENTITY.md`, `SOUL.md` (generated) — with one exception: the L1 base prompt (`AGENTS.md`) SHALL NOT be materialized as a workspace file; it is platform-embedded content injected into the instruction at every composition as specified in the `agent-runtime` capability. The generated files SHALL be the single source of truth for their content; the database SHALL carry only `prompts_status` and `prompts_error`. Writes SHALL be atomic (write-to-temp-then-rename) with `0o755` directories and `0o644` files. A successful generation SHALL write the files before the ready transition, so a ready agent always has files on disk. On regenerate, stale files SHALL be left untouched until the new generation succeeds. Read-side APIs SHALL compose `identity` and `soul` onto the agent from the files on every fetch; a missing or unreadable file SHALL compose as empty rather than failing the request. A startup sweep SHALL remove `AGENTS.md` files seeded into existing agent workspaces by earlier versions, and SHALL also remove `BOOTSTRAP.md` and `BOOTSTRAP.md.bak` files left by the removed birth-sequence feature, so the workspace never shows stale copies of injected or removed content.

#### Scenario: Ready implies files on disk

- **WHEN** an agent reaches `prompts_status: ready`
- **THEN** its workspace directory contains `IDENTITY.md` and `SOUL.md`

#### Scenario: Create seeds AGENTS.md before generation

- **WHEN** a new agent's workspace directory is prepared
- **THEN** its workspace directory contains no `AGENTS.md` — the base prompt is platform-embedded content injected at composition (the `agent-runtime` capability), and generation proceeds over `IDENTITY.md`/`SOUL.md` alone

#### Scenario: Startup sweep removes seeded base prompts

- **WHEN** the server starts and an agent workspace still contains an `AGENTS.md` seeded by an earlier version
- **THEN** the sweep deletes it, leaving generated documents untouched

#### Scenario: Startup sweep removes stray bootstrap documents

- **WHEN** the server starts and an agent workspace contains a `BOOTSTRAP.md` or `BOOTSTRAP.md.bak` left by the removed birth-sequence feature
- **THEN** the sweep deletes both, logs the removal, and leaves generated documents untouched

#### Scenario: Failed regeneration keeps old files

- **WHEN** regeneration fails for a previously ready agent
- **THEN** the old `IDENTITY.md`/`SOUL.md` remain on disk and `prompts_status` reads `failed`

#### Scenario: Compose-on-read projection

- **WHEN** a client fetches a ready agent
- **THEN** the response carries `identity` and `soul` read from the workspace files; the database does not store the content

### Requirement: Generate-before-persist on create

Creating an agent SHALL generate identity/soul prompts synchronously **before** the agent row is persisted: the generation pipeline resolves the payload's provider config, invokes the configured model, parses the two documents, and writes them into the agent's workspace directory; only then is the row inserted — directly in `prompts_status: ready`. The create request path MAY block on the LLM call within the bounded generation timeout. A generation failure SHALL abort the create: response is 400 `invalid_request` with the sanitized provider error, no agent row is created, and the seeded workspace directory is removed. `generating` no longer occurs on the create path; it describes in-flight regeneration only.

#### Scenario: Create blocks while the model answers

- **WHEN** an agent is created with a valid provider config
- **THEN** the 201 response arrives after generation completes, carrying `prompts_status: ready` (documents populated)

#### Scenario: Generation failure aborts create

- **WHEN** the model call fails during agent creation
- **THEN** the response is 400 `invalid_request` with the sanitized generation error, no agent row exists, and the agent's workspace directory is removed

#### Scenario: No failed agents from create

- **WHEN** agents are created through the endpoint
- **THEN** no agent enters the workspace in `prompts_status: failed` from the create path; `failed` arises only from regeneration or the interrupted-generation sweep
