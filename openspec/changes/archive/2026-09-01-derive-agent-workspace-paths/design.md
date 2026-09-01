# Design — derive-agent-workspace-paths

## Context

The audit that motivated this change (`research` agent frozen with a relative `workspace_dir`) surfaced three failure modes of the stored-path design: a relative root froze cwd-dependent paths into rows, config-root changes never applied to existing agents, and the slug-rename scenario in the live spec mandated the stale-directory behavior. Two live-spec facts shaped the design: agent slugs are mutable in the handler today, and the web wizard echoes `slug` on every PATCH — so immutability must be *ignore*-semantics, not 400.

## Goals / Non-Goals

- **Goals:** the path is a pure function of (root, workspace slug, agent slug); no filesystem topology in the database; slug immutability; fail-fast on a mis-configured root.
- **Non-Goals:** renaming/moving directories on slug change (slugs become immutable instead); automating relocation of pre-existing directories (operational `mv`); changing the `<root>/<tenant>/agents/<slug>` layout.

## Decisions

### D1. Derive at the API boundary; pass the directory through the service
Handlers own the workspace context (`ws.Slug` in scope) and the current root (`h.workspaceDir`), so they derive `dir := domain.AgentWorkspaceDir(h.workspaceDir, ws.Slug, agent.Slug)` and pass it into the agents service as an explicit parameter. The service stays config-free — no injected root, no workspace-row fetch for the slug — and its tests keep passing isolated temp dirs trivially. `GenerateForCreate(ctx, dir, workspaceID, agent)` and `Generate(ctx, dir, workspaceID, agentID, instruction)` gain a `dir` parameter; `agent.WorkspaceDir` disappears from the service entirely.

### D2. ID-based re-verify replaces the directory comparison
`Generate` currently re-verifies the agent row before writing files and compares `fresh.WorkspaceDir != agent.WorkspaceDir` — a guard that could never fire, since both rows derive the same directory for the same slug. Replaced with an ID comparison (`fresh.ID != agent.ID → abort`): it detects row replacement regardless of directories and is simpler to reason about. The ErrNotFound no-op stays.

### D3. Drop the column; relocate operationally
Migration drops `agents.workspace_dir` (down migration restores the column, nullable). The postgres store stops reading/writing it; `domain.Agent` loses the field. Pre-existing directories are moved by the operator while the server is stopped (documented in the proposal); the layout and slugs are unchanged, so the `mv` is safe and the migration is lossless (paths were always derivable).

### D4. Fail fast on a relative root — in the config layer
`internal/config` rejects a non-absolute `--workspace-dir`/`ONCLAW_WORKSPACE_DIR` at assembly time with a clear error. This is config validation, not a handler guard: it runs once at startup, before any dependency is constructed, per the composition-root rule. The router's empty-root fallback (`router.go:66-68`) keeps using `DefaultWorkspaceDir()`, which is absolute whenever HOME resolves; the fallback path is additionally `.env`-independent and now provably absolute.

### D5. Slug immutability via managed-field semantics
PATCH ignores `slug` like `memory`/`prompts_status`/`prompts_error` — a slug field in an update payload is not an error, it is server-managed. This matches the existing convention, keeps the web's current echo (until the web stops sending it) saving cleanly, and avoids a breaking 400. `PatchAgentRequest` keeps the field for binding but the handler never applies it; the handler's empty-check (`req.Slug == nil` in the no-fields guard) is adjusted so an update with *only* a slug is rejected as "no fields to update"… unless identity/soul files are being written, which is unrelated.

## Risks / Trade-offs

- [D1] Service signatures change → moderate test churn in `internal/agents` and handlers tests; contained.
- [D3] Column drop is a **BREAKING** API change (agents JSON loses `workspace_dir`); no client reads it (web declares it optional).
- [D3] Operators who run with a relative root today must relocate directories once. Documented, lossless.
- [D5] A client that *renames* slugs via PATCH today (the live spec allowed it) loses that ability — deliberate.
