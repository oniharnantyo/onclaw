# onboarding (delta)

## Purpose

Delta for the existing `onboarding` capability: a fresh agent is driven by an onboarding
prompt that lives in a markdown file, encourages it to ask the user questions, and instructs
it to edit its own persona files with its normal file tools — no special tooling.

## ADDED Requirements

### Requirement: The onboarding prompt is a user-editable markdown file

The system SHALL source the onboarding prompt from a markdown file. The default content SHALL
ship as `internal/agent/templates/onboarding.md`, embedded into the binary, and SHALL be
written to the user config dir (`~/.onclaw/onboarding.md`) on first run so the user can edit
it. The system SHALL read the prompt from that file at runtime; the prompt's content SHALL be
defined by the file, not by a hardcoded constant.

#### Scenario: The user can edit the onboarding prompt

- **WHEN** the user edits `~/.onclaw/onboarding.md`
- **THEN** the next fresh run uses the edited prompt rather than the shipped default

#### Scenario: The default is seeded from the shipped file

- **WHEN** onclaw runs for the first time and `~/.onclaw/onboarding.md` does not exist
- **THEN** the file is created with the shipped default content

### Requirement: An agent receives the onboarding prompt until onboarding is completed

The system SHALL prepend the onboarding prompt to an agent's system prompt while onboarding
is incomplete (an `onboarding_completed` preference is unset/false), and SHALL mark onboarding
complete (set that preference) when the onboarding interaction concludes. The prompt SHALL
NOT be injected once onboarding is marked complete. (Persona files are seeded from templates,
so file emptiness no longer indicates a fresh state — cross-ref `agent-identity`.)

#### Scenario: An agent gets the onboarding prompt while onboarding is incomplete

- **WHEN** the master agent runs and `onboarding_completed` is unset
- **THEN** its system prompt includes the onboarding prompt, and it asks the user questions and edits its persona files

#### Scenario: The prompt stops once onboarding completes

- **WHEN** the agent runs after `onboarding_completed` has been set
- **THEN** the onboarding prompt is not injected

### Requirement: The default onboarding prompt guides a short interview and maps answers to files

The shipped default onboarding prompt content SHALL be:

> You just woke up for the first time. Your persona files are empty, so you don't yet know
> who the user is or how they like to work.
>
> Have a short, natural conversation to learn just enough to be useful, then start helping.
> Ask one question at a time — this isn't an interrogation, and a few answers are enough. Let
> the user skip anything.
>
> What to learn, and where to write it:
> - Who the user is — role, background, what they do → IDENTITY.md
> - How you should behave — tone, style, boundaries → SOUL.md
> - Domains and tasks that matter to them → CAPABILITIES.md
> - Standing preferences — tools, conventions, environment → USER.md
>
> When you have enough, record it with your file tools: write concise, factual notes into the
> matching files above (not essays). Things you pick up over time go in MEMORY.md later. Then
> greet the user and get to work on whatever they asked.

#### Scenario: The default prompt maps each answer to its file

- **WHEN** the shipped default prompt is inspected
- **THEN** it directs role → IDENTITY.md, behavior → SOUL.md, domains → CAPABILITIES.md, preferences → USER.md, ongoing facts → MEMORY.md

### Requirement: The agent edits its persona files with normal file tools

The agent SHALL populate its persona/memory files using its normal workspace file tools
(`write_file`/`edit_file`). The system SHALL NOT provide a special persona-writing tool.
Because file tools are workspace-scoped, the agent edits the per-agent files in its workspace;
the global `~/.onclaw/USER.md` is outside the workspace and is not agent-edited during
onboarding.

#### Scenario: The agent writes persona files with its file tools

- **WHEN** the agent has gathered the user's role and preferences during onboarding
- **THEN** it edits IDENTITY.md / USER.md / etc. in its workspace using the normal file tools