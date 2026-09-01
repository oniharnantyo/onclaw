## Why

Regeneration today overwrites IDENTITY.md / SOUL.md / BOOTSTRAP.md wholesale from the stored brief. A regen after small brief tweaks destroys good prompt content, and the workspace directory is the only copy — a bad regeneration is unrecoverable. The generated documents are also only visible buried at the bottom of the config modal's Identity tab; there is no surface that presents them as the files they are.

## What Changes

- Regeneration runs in **enhance mode**: the current documents are passed to the model with instructions to strengthen them (preserve the established voice and structure, incorporate the current brief) instead of writing from scratch. Agents with no documents on disk (failed create, cleared workspace) generate fresh.
- **Backup before commit**: each existing document is preserved as `IDENTITY.md.bak` / `SOUL.md.bak` / `BOOTSTRAP.md.bak` beside it before the enhanced content is committed. A backup failure aborts the commit and fails the generation; the originals stay intact. Documents are never removed or overwritten before generation succeeds and backups exist.
- **Prompts tab** in the config modal (edit mode): lists the generated files by name — IDENTITY.md and SOUL.md as editable monospace textareas persisted via the existing PATCH contract, BOOTSTRAP.md read-only — with the prompts status and the Regenerate action. After a regenerate the modal stays open and refetches, so the enhanced documents are visible immediately.
- The status row + Regenerate control move out of the Identity tab into the Prompts tab.

## Impact

- **Code:** `internal/agents/prompts.go` (enhance framing + messages), `internal/agents/workspace.go` (backup-on-write), `internal/agents/service.go` (regenerate reads current docs → enhance), `web/src/modals/AgentConfigModal.tsx` (Prompts tab, regenerate UX).
- **Tests:** `internal/agents/service_test.go`, `internal/agents/workspace_test.go`, `web/src/modals/AgentConfigModal.test.tsx`.
- **Specs:** agent-prompts (Regeneration) MODIFIED; web-app/agents (Structured agent configuration) MODIFIED.
- No store, handler, or API contract changes; PATCH identity/soul is unchanged.
