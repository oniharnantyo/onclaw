# Design — refactor-agent-prompts-to-files

## Context

Migration 000013 gave every agent an on-disk workspace directory (`<ONCLAW_WORKSPACE_DIR>/<tenant_slug>/agents/<agent_slug>`, assigned before the DB write) — created and recorded, but empty. Meanwhile the generated prompt documents live in `agents.identity` / `agents.soul` columns scanned and written by the store. The agent runtime (next milestone) reads persona from workspace files, the way OpenClaw/GoClaw do, so the DB columns fork the source of truth.

## Goals / Non-Goals

**Goals:**
- Workspace files become the single source of truth for prompt documents; the DB keeps only `prompts_status` / `prompts_error`.
- The generator emits a third document, `BOOTSTRAP.md` — the personalized birth-sequence ritual.
- `AGENTS.md` (embedded L1 base prompt) is seeded at creation; the directory is removed on delete.

**Non-Goals:**
- The agent runtime itself (file reading at chat time) — later change.
- `USER.md` and the `memory/` directory layout.
- Migrating existing DB content to files (pre-production; data loss accepted).

## Decisions

### 1. Files are source of truth, DB keeps lifecycle only
`agents.identity` / `agents.soul` are dropped (migration 000014, down re-adds them empty). `SetPromptState` loses its `identity`/`soul` parameters; scan/insert/update stop touching content. `domain.Agent` keeps `Identity`/`Soul` and gains `Bootstrap` — now projection fields composed from disk.

### 2. Workspace dir is immutable after birth
Already canonical: `<root>/<tenant>/agents/<agent_slug>` is assigned before the DB write and stays stable across slug renames. This change builds on that rule — `service.Generate` reads the dir from the fetched agent row, never recomputes it.

### 3. Atomic writes, files-first ordering
All file writes go through a temp-file-then-rename helper (`0o644`, dirs `0o755`). `Generate` writes `IDENTITY.md`, `SOUL.md`, `BOOTSTRAP.md` and only then transitions to `ready` — a ready agent always has files. Regeneration failure leaves stale-but-valid files in place.

### 4. Compose-on-read projection
Read-side handlers fill `agent.Identity/Soul/Bootstrap` from the files after every fetch (get, list, create-refresh, patch, regenerate). Missing or unreadable files compose as empty — availability over strictness; the status column carries the truth about readiness. List reads N×3 small files per request, accepted at this scale.

### 5. PATCH identity/soul writes files
`PatchAgent` writes `IDENTITY.md` / `SOUL.md` for whichever of the two the payload carries, then updates the row. Status is untouched (parity with today). Manual edits before any generation are allowed — the file is simply overwritten by the next regeneration.

### 6. BOOTSTRAP.md is generated, personalized, OnClaw-adapted
The example template's interactive beats do not fit OnClaw (the name is chosen in the wizard at creation, and there is no plugin-recommendation CLI). The generated ritual keeps the template's spine — user's request first, short beats, delete-on-completion — while the beats become: introduce yourself as the configured name (never re-ask), show your vibe (one line, SOUL.md-consistent), invite the first real task. Deletion is the runtime's job at first chat; without a runtime the file persists, which is harmless.

### 7. Rejected alternatives
- Keep DB columns as a cache alongside files: two sources of truth, drift.
- Serve prompt files through the capability-URL storage port: those are blob semantics for avatars; workspace files are the runtime's working set.
- Move the directory on slug rename: contradicts the canonical sticky-dir rule.
- Static (non-generated) BOOTSTRAP.md template: loses personalization; the ritual must reference the actual agent.

## Risks / Trade-offs

- **Pre-prod data loss:** existing rows' identity/soul text is not migrated to files (SQL cannot write files). Accepted; regeneration restores content.
- **File/DB split-brain:** a ready row with a missing file composes as empty; recovery is the regenerate button. Rate-limited by the 409 in-flight guard.
- **Delete-during-file-write races:** the dir-removal-after-DB-delete ordering means a concurrent regeneration can recreate files into a dir that is then removed — same eventual state, no errors surfaced to clients.

## Migration Plan

Migration 000014 drops the two columns; `migrate down` re-adds them (empty). Deploy order: run migrations before starting the new binary (old binary writes columns the new code ignores — harmless in dev).

## Open Questions

- `workspace_dir` (server-absolute path) is serialized to workspace members in every agent response — pre-existing, spec'd column, unused by the web app. Flagged in review as an info-leak nit; decision deferred (TODO on `domain.Agent.WorkspaceDir`): omit via `json:"-"` or project behind an admin permission in a future change.
