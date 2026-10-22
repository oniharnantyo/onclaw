# Proposal

## Why

buddies' add-agent is a deliberately minimal name-only modal (shipped in tasks 10.1–10.5), while the predecessor project onclaw-v2 evolved a full agent-creation experience: a three-step wizard (Identity → Model → Capabilities) whose brief drives LLM generation of the agent's persona documents. Porting that experience gives buddies real personas instead of hand-edited system prompts, and it is the main piece of onclaw-v2's product feel still missing.

## What Changes

- Replace the name-only create modal with the three-step wizard: Identity (AvatarPicker, name, slug, description, brief), Model (provider/model; advanced: temperature, max tokens, context window with catalog auto-fill), Capabilities (skills/tools/MCP allowlists).
- Port onclaw-v2's file-backed prompt documents: each agent gets `~/.buddies/workspaces/<slug>/` holding `IDENTITY.md`, `SOUL.md`, `BOOTSTRAP.md`. **BREAKING**: the `system_prompt` column and `Agent.SystemPrompt` field are removed; the runtime composes turns from the three documents.
- Add brief-driven prompt generation on create, run synchronously on the agent's own provider/model through the eino runtime, with a `prompts_status` machine (`generating → ready | failed`), a startup sweep for crash-orphaned `generating` rows, and the rotating onboarding loader in buddies' own informal voice.
- Port regeneration into the config modal: editable mono editors for the three documents behind a file rail, a "What should change?" instruction form ("Enhance prompts"), enhance mode that sends current documents to the model, and `<name>.bak` backup-before-overwrite.
- Agent store row gains `slug`, `description`, `brief`, `prompts_status`, `prompts_error`, and `avatar` (the wizard's stored procedural-avatar config — 2026-10-17 parity pass, design D14); existing agents' `system_prompt` content migrates into their `IDENTITY.md`. Role, memory side-call, workspace-default inheritance, and autonomy/effort gating are NOT ported.
- Description renders as a multi-line textarea in the wizard and as a clamped summary line on agent cards.

## Capabilities

### New Capabilities

- `agent-prompts`: File-backed prompt documents (IDENTITY/SOUL/BOOTSTRAP) per agent workspace, brief-driven generation on the agent's own provider/model, the prompts status machine with startup sweep, turn composition from the documents, and enhance-mode regeneration with instruction and `.bak` backups.

### Modified Capabilities

- `agent-management`: Create becomes the three-step wizard requiring name, slug (DNS-label, unique, immutable), and brief; description is optional multi-line. The config modal's system-prompt section is replaced by the prompt-document editors with a regenerate control. Cards gain an optional description line.
- `data-store`: The home-directory layout gains `~/.buddies/workspaces/<slug>/`; agent prompt documents live on disk beside the SQLite row (which keeps slug, brief, and prompts status), and the schema migration drops `system_prompt`.

## Impact

- `internal/store`: `Agent` type (+slug, description, brief, prompts status/error, −SystemPrompt), `internal/store/sqlite` migration `000004_agent_prompt_docs.sql` (new columns, drop `system_prompt`, backfill `IDENTITY.md` for existing agents), agents store CRUD for the new fields.
- New workspace-directories manager under `internal/` for `~/.buddies/workspaces/<slug>/` (atomic writes, `.bak` backups, slug-dir lifecycle on create/delete).
- New prompt-generation service (mirrors onclaw-v2 `internal/promptgen`) using the eino model factory and provider registry; generation-time parameters hardcoded; 3-minute timeout.
- `internal/services/agents.go`: create/update DTOs gain slug/description/brief; create blocks on generation; new regenerate call; startup sweep wiring.
- `frontend/`: `CreateAgentModal` → three-step wizard; `AgentConfigModal` gains the Identity tab (file rail + editors + regenerate); new `OnboardingLoader`; bindings regenerated (`task` build); both daisyUI themes.
- `internal/agents` runtime: turn context composes identity + soul + bootstrap instead of `SystemPrompt`.
