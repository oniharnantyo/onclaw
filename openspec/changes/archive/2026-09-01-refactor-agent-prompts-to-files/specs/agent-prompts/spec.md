## ADDED Requirements

### Requirement: Prompt documents are workspace files
An agent's prompt documents SHALL live as markdown files in the agent's workspace directory — `AGENTS.md` (static L1 base prompt, seeded at creation in both the create endpoint and the workspace birth flow), and `IDENTITY.md`, `SOUL.md`, `BOOTSTRAP.md` (generated). The files SHALL be the single source of truth; the database SHALL carry only `prompts_status` and `prompts_error`. Writes SHALL be atomic (write-to-temp-then-rename) with `0o755` directories and `0o644` files. A successful generation SHALL write the files before the ready transition, so a ready agent always has files on disk. On regenerate, stale files SHALL be left untouched until the new generation succeeds. Read-side APIs SHALL compose `identity`, `soul`, and `bootstrap` onto the agent from the files on every fetch; a missing or unreadable file SHALL compose as empty rather than failing the request.

#### Scenario: Create seeds AGENTS.md before generation
- **WHEN** an agent is created
- **THEN** its workspace directory contains `AGENTS.md` with the L1 base prompt before generation starts

#### Scenario: Ready implies files on disk
- **WHEN** generation succeeds
- **ONLY THEN** `IDENTITY.md`, `SOUL.md`, and `BOOTSTRAP.md` exist in the workspace directory and `prompts_status` reads `ready`

#### Scenario: Failed regeneration keeps old files
- **WHEN** regeneration fails for a previously ready agent
- **THEN** the old `IDENTITY.md`/`SOUL.md`/`BOOTSTRAP.md` remain on disk and `prompts_status` reads `failed`

#### Scenario: Compose-on-read projection
- **WHEN** a client fetches a ready agent
- **THEN** the response carries `identity`, `soul`, and `bootstrap` read from the workspace files; the database does not store the content

### Requirement: BOOTSTRAP.md birth sequence
The generator SHALL emit a third document, `BOOTSTRAP.md`, personalized to the agent. It SHALL open with the birth-sequence title and a "you just woke up" opener, SHALL state that the user's request always comes first (the ritual is not a gate), and SHALL carry short beats adapted to OnClaw: introduce yourself as the configured name (never re-ask or invent a name), show your vibe in one line consistent with SOUL.md, and invite the user's first real task. It SHALL instruct the agent to end the ritual by removing the file, completing the birth sequence.

#### Scenario: Personalized ritual
- **WHEN** generation runs for an agent named Atlas with a given brief
- **THEN** the generated `BOOTSTRAP.md` instructs introduction as Atlas and stays consistent with the co-generated identity and soul

#### Scenario: User request first
- **WHEN** the first chat message asks for real work
- **THEN** the ritual instructions defer to the request instead of gating the conversation on the beats

## MODIFIED Requirements

### Requirement: Synchronous generation on create
Creating an agent SHALL generate identity/soul prompts synchronously inside the create request, within a bounded timeout: the response carries the agent with its final prompt state (`ready`, or `failed` with a short `prompts_error`); `generating` describes an in-flight generation only. The create request path MAY therefore block on the LLM call; a generation failure SHALL NOT fail the create — the agent still exists and is retryable. Generated documents are written to the agent's workspace directory (see "Prompt documents are workspace files"), and every read-side agent response composes identity/soul/bootstrap from those files.

#### Scenario: Create blocks while the model answers
- **WHEN** an agent is created with a valid provider config
- **THEN** the 201 response arrives after generation completes, carrying `prompts_status: ready` with identity, soul, and bootstrap composed from the workspace files

#### Scenario: Generation failure does not fail create
- **WHEN** the model call fails during agent creation
- **THEN** the create response is still 201 with `prompts_status: failed` and a short `prompts_error`; the agent is retryable via regenerate

### Requirement: Regeneration
`POST /workspaces/:ws/agents/:agent/regenerate` (`agents.write`) SHALL re-run generation synchronously from the currently stored brief and return the agent with its final prompt state. Regenerate SHALL be refused with 409 while a generation is already in flight for that agent. A successful regeneration SHALL overwrite the workspace prompt files; a failed one SHALL leave the previous files untouched.

#### Scenario: Regenerate from stored brief
- **WHEN** regeneration is requested for a ready or failed agent
- **THEN** the response returns after generation completes with the refreshed agent (`ready`, or `failed` with the reason)

#### Scenario: Regenerate while in flight
- **WHEN** regenerate is called while status is `generating`
- **THEN** response is 409 conflict

#### Scenario: Failed regeneration preserves files
- **WHEN** regeneration of a ready agent fails
- **THEN** the workspace files still hold the previous ready documents
