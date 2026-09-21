# agent-prompts Specification

## Purpose
Synchronous generation of an agent's identity and soul prompts from the stored brief, using the agent's own provider config and model — the platform's first real LLM invocation — with a visible status machine and an interactive loading experience while the request is in flight.

## Requirements

### Requirement: Generation uses the agent's own provider and model
Generation SHALL decrypt the agent's provider config key (AAD = workspace id), call the agent's configured model, and use hardcoded generation parameters (generation temperature and a generation-time max_tokens large enough to satisfy the anthropic API contract). The user brief SHALL be persisted at create so regeneration works without client state.

#### Scenario: Anthropic generation satisfies the API contract
- **WHEN** generation runs for an agent on an anthropic-type provider
- **THEN** the model call includes max_tokens and succeeds for a valid key

### Requirement: base_url carries the API version path
A provider config's `base_url` SHALL be the full API base including the family's version path (e.g. `https://api.example.com/v1`, `https://openrouter.ai/api/v1`, or the Gemini `…/v1beta`). Requests SHALL append only resource paths (`/models`, `/chat/completions`) so the version path appears exactly once. SDKs that append their own version path (Anthropic, Gemini) SHALL receive the base_url with the version path stripped. The provider form SHALL instruct users to include the version path.

#### Scenario: Compatible provider with versioned base
- **WHEN** an openai-compatible config stores base_url `https://opencode.ai/zen/v1`
- **THEN** chat-completion and models requests hit `https://opencode.ai/zen/v1/<resource>` — the version path appears exactly once

### Requirement: Status machine and startup sweep
`prompts_status` SHALL move `generating → ready | failed` only. A server restart SHALL sweep stale `generating` rows (e.g. a crash mid-request) to `failed` with `prompts_error` "prompt generation interrupted — retry"; agents in `failed` remain fully editable, and retry is available from the UI and via the regenerate endpoint.

#### Scenario: Interrupted by restart
- **WHEN** the server dies mid-generation and restarts
- **THEN** the affected agents read `failed` with the interrupted error; no row is stuck in `generating`

#### Scenario: Failed agent stays usable
- **WHEN** prompts generation fails
- **THEN** the agent stays editable and listed; only prompts are affected

### Requirement: Regeneration
`POST /workspaces/:ws/agents/:agent/regenerate` (`agents.write`) SHALL re-run generation synchronously from the currently stored brief and MAY carry a JSON body `{instruction}` with the client's requested changes. When the agent's workspace holds generated documents, generation SHALL run in enhance mode: the current IDENTITY.md, SOUL.md, and BOOTSTRAP.md contents are always passed to the model with instructions to strengthen them — preserve the established voice and structure, incorporate the current brief — rather than write from scratch, with or without an instruction; with no documents on disk, generation runs fresh and the instruction rides along as a Requested Changes section. Regeneration SHALL NOT remove or overwrite any existing document until the model output is in hand and backups exist: on success each overwritten document's previous content is preserved as `<name>.bak` beside it before the enhanced content is committed, and a backup failure SHALL abort the commit and fail the generation. On failure the existing documents stay untouched and retryable.

#### Scenario: Regenerate from stored brief
- **WHEN** regeneration is requested for a ready or failed agent
- **THEN** the response returns after generation completes with the refreshed agent (`ready`, or `failed` with the reason)

#### Scenario: Regenerate while in flight
- **WHEN** regenerate is called while status is `generating`
- **THEN** response is 409 conflict

#### Scenario: Regenerate enhances existing documents
- **WHEN** regeneration runs for an agent whose workspace holds generated documents
- **THEN** the model call includes the current documents with enhancement instructions, and on success each previous document is preserved as `<name>.bak` beside the enhanced file

#### Scenario: Change instruction rides along with the old prompts
- **WHEN** regeneration is requested with a non-empty `instruction`
- **THEN** the model call includes the current documents plus the instruction as a Requested Changes section — the old prompts are never dropped in favor of the instruction

#### Scenario: Regenerate without documents runs fresh
- **WHEN** regeneration runs for an agent whose workspace holds no generated documents
- **THEN** the model call carries no existing documents and builds them from the brief alone

#### Scenario: Failed regeneration preserves files
- **WHEN** the model call fails during regeneration
- **THEN** `prompts_status` becomes `failed` with the reason, the previous documents remain on disk unchanged, and no backups are created

#### Scenario: Backup failure aborts the commit
- **WHEN** preserving a previous document as `<name>.bak` fails after a successful model call
- **THEN** the commit is aborted, the original documents remain unchanged on disk, and the generation is marked failed

#### Scenario: Restore from backup
- **WHEN** a user wants the pre-regeneration documents back
- **THEN** the previous version of each file is available as `<name>.bak` beside the document

### Requirement: Loading experience during generation
The deploy wizard and workspace-birth flow SHALL show an interactive loading state while the create request generates prompts: a spinner plus rotating corporate-onboarding status lines (e.g. "Finding the employee handbook…", "Waiting for IT… 👀"), rotating every 5 seconds, with an elapsed timer appearing after 30 seconds so long generations never look hung. The roster Retry action SHALL show a busy state while the synchronous regenerate runs.

#### Scenario: Wizard shows onboarding loader
- **WHEN** the deploy step confirms and create is in flight
- **THEN** the modal body shows a spinner and rotating onboarding status lines until the response arrives

#### Scenario: Elapsed timer after 30 seconds
- **WHEN** the create request is still in flight after 30 seconds
- **THEN** the loader shows the elapsed time ("1m 32s") beneath the status lines

#### Scenario: Retry shows busy state
- **WHEN** a failed agent's Retry is pressed on the roster
- **THEN** while the regenerate request runs, the button is disabled and labelled "Retrying…"

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

### Requirement: BOOTSTRAP.md birth sequence
The generator SHALL emit a third document, `BOOTSTRAP.md`, personalized to the agent. It SHALL open with the birth-sequence title and a "you just woke up" opener, SHALL state that the user's request always comes first (the ritual is not a gate), and SHALL carry short beats adapted to OnClaw: introduce yourself as the configured name (never re-ask or invent a name), show your vibe in one line consistent with SOUL.md, and invite the user's first real task. It SHALL instruct the agent to end the ritual by removing the file, completing the birth sequence.

#### Scenario: Personalized ritual
- **WHEN** generation runs for an agent named Atlas with a given brief
- **THEN** the generated `BOOTSTRAP.md` instructs introduction as Atlas and stays consistent with the co-generated identity and soul

#### Scenario: User request first
- **WHEN** the first chat message asks for real work
- **THEN** the ritual instructions defer to the request instead of gating the conversation on the beats

### Requirement: Generate-before-persist on create
Creating an agent SHALL generate identity/soul/bootstrap prompts synchronously **before** the agent row is persisted: the generation pipeline resolves the payload's provider config, invokes the configured model, parses the three documents, and writes them into the agent's workspace directory; only then is the row inserted — directly in `prompts_status: ready`. The create request path MAY block on the LLM call within the bounded generation timeout. A generation failure SHALL abort the create: response is 400 `invalid_request` with the sanitized provider error, no agent row is created, and the seeded workspace directory is removed. `generating` no longer occurs on the create path; it describes in-flight regeneration only.

#### Scenario: Create blocks while the model answers
- **WHEN** an agent is created with a valid provider config
- **THEN** the 201 response arrives after generation completes, carrying `prompts_status: ready` (documents populated)

#### Scenario: Generation failure aborts create
- **WHEN** the model call fails during agent creation
- **THEN** the response is 400 `invalid_request` with the sanitized generation error, no agent row exists, and the agent's workspace directory is removed

#### Scenario: No failed agents from create
- **WHEN** agents are created through the endpoint
- **THEN** no agent enters the workspace in `prompts_status: failed` from the create path; `failed` arises only from regeneration or the interrupted-generation sweep
