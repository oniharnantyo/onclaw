# Tasks — derive-agent-workspace-paths

## 1. Domain & Store

- [x] 1.1 Remove `WorkspaceDir` from `domain.Agent`; drop the column in postgres store agents SQL and the fake store (compile-error driven)
- [x] 1.2 Migration `000015_drop_agent_workspace_dir`: up drops `agents.workspace_dir`, down re-adds it nullable

## 2. Config

- [x] 2.1 `internal/config` rejects a non-absolute workspace root at assembly with a clear error; `config_test.go` covers relative roots (env + flag)

## 3. Handlers derive the directory

- [x] 3.1 CreateAgent: derive `dir` from current root + slugs; seed and generate against it; stop setting `agent.WorkspaceDir`
- [x] 3.2 RegenerateAgent: derive and pass `dir` into the generation service
- [x] 3.3 Birth flow (`workspaces.go`): derive the starter agent's directory; generate against it
- [x] `3.4` PATCH: derive for identity/soul writes and `composePromptDocuments`; ignore slug (remove it from the no-fields guard and from application)
- [x] 3.5 Agent detail/list composition derives the directory (detail responses carry no `workspace_dir`)

## 4. Generation service

- [x] 4.1 `GenerateForCreate(ctx, dir, workspaceID, agent)` and `Generate(ctx, dir, workspaceID, agentID, instruction)` take the directory as a parameter; remove `agent.WorkspaceDir` reads
- [x] 4.2 Replace the `WorkspaceDir`-comparison re-verify with an ID comparison; keep the ErrNotFound no-op
- [x] 4.3 Update `internal/agents` tests to pass explicit dirs (temp dirs still isolated)

## 5. Web

- [x] 5.1 Wizard edit mode renders slug read-only; PATCH payload omits slug
- [x] 5.2 Drop `workspace_dir` from web Agent types (types.d.ts, data/types.ts)

## 6. Verification

- [x] 6.1 `go vet ./...`, `go test ./...`, Postgres integration tests
- [x] 6.2 `web`: `tsc --noEmit` + `vitest run` green
- [x] 6.3 `scripts/smoke.sh` green end-to-end (paths under `WS_ROOT` unchanged in behavior)
- [x] 6.4 `openspec validate --all` and archive
