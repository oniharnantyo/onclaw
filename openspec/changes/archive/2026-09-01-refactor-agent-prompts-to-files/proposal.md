## Why

The generated identity and soul documents live in `agents.identity` / `agents.soul` database columns, while the agent's on-disk workspace directory (`workspace_dir`, migration 000013) sits empty. The agent runtime — the next milestone — reads its persona from workspace files on disk, the way OpenClaw/GoClaw do (`AGENTS.md`, `IDENTITY.md`, `SOUL.md`, `BOOTSTRAP.md`). Storing prompt documents in the database forks the source of truth: content flows through the store layer for no benefit, and the runtime would need a DB round-trip to learn who it is.

## What Changes

- **BREAKING** — drop the `agents.identity` and `agents.soul` columns (migration 000014). `prompts_status` and `prompts_error` remain the only prompt metadata in the database.
- The prompt generator emits a third document, `BOOTSTRAP.md` — a personalized birth-sequence ritual adapted from OpenClaw's `BOOTSTRAP.md` template: the user's request always comes first, short beats (introduce yourself as the configured name, show your vibe, invite the first real task), and delete-on-completion semantics.
- Agent workspace directories become the prompt source of truth:
  - `AGENTS.md` (the embedded L1 base system prompt) is seeded at agent creation, in both the create endpoint and the workspace birth flow.
  - `IDENTITY.md`, `SOUL.md`, and `BOOTSTRAP.md` are written after a successful generation.
  - The directory is removed when the agent is deleted.
- The API keeps serving `identity`/`soul` and adds `bootstrap` — all composed from the files at read time. `PATCH` with `identity`/`soul` writes the files instead of storing text in the database.
- The store slims down: `SetPromptState` loses its content parameters; agent rows stop carrying prompt content.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agent-prompts` — generation writes the three documents as workspace files; the API composes `identity`/`soul`/`bootstrap` from those files; regeneration overwrites files only on success.
- `agents` — `Agent fields`: identity/soul become file-backed server-managed fields plus the new `bootstrap`; `Deletion semantics`: deleting an agent removes its workspace directory.

## Impact

- `internal/agents` — `prompts.go` (three-document schema, system prompt, parser), new `workspace.go` (seed / write / read file helpers), `service.go` (write files before the ready transition).
- `internal/store` — port, fake, and postgres adapters: `SetPromptState` signature, column removal from insert/scan/update, migration 000014.
- `internal/server/handlers` — `agents.go` (seed at create, file writes on PATCH, compose-on-read, directory removal on delete), `workspaces.go` (seed at birth).
- `web/src/data/types.ts` — additive `bootstrap` field.
- `scripts/smoke.sh` — assert the workspace files exist after agent creation.
