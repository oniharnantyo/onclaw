## Purpose

Synchronous generation of an agent's identity and soul prompts from the stored brief, using the agent's own provider config and model — the platform's first real LLM invocation — with a visible status machine and an interactive loading experience while the request is in flight.

## ADDED Requirements

### Requirement: Synchronous generation on create
Creating an agent SHALL generate identity/soul prompts synchronously inside the create request, within a bounded timeout: the response carries the agent with its final prompt state (`ready`, or `failed` with a short `prompts_error`); `generating` describes an in-flight generation only. The create request path MAY therefore block on the LLM call; a generation failure SHALL NOT fail the create — the agent still exists and is retryable.

#### Scenario: Create blocks while the model answers
- **WHEN** an agent is created with a valid provider config
- **THEN** the 201 response arrives after generation completes, carrying `prompts_status: ready` (identity and soul populated)

#### Scenario: Generation failure does not fail create
- **WHEN** the model call fails during agent creation
- **THEN** the create response is still 201 with `prompts_status: failed` and a short `prompts_error`; the agent is retryable via regenerate

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
`POST /workspaces/:ws/agents/:agent/regenerate` (`agents.write`) SHALL re-run generation synchronously from the currently stored brief and return the agent with its final prompt state. Regenerate SHALL be refused with 409 while a generation is already in flight for that agent.

#### Scenario: Regenerate from stored brief
- **WHEN** regeneration is requested for a ready or failed agent
- **THEN** the response returns after generation completes with the refreshed agent (`ready`, or `failed` with the reason)

#### Scenario: Regenerate while in flight
- **WHEN** regenerate is called while status is `generating`
- **THEN** response is 409 conflict

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
