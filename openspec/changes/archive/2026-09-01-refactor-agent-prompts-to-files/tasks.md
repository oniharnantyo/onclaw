# Tasks — refactor-agent-prompts-to-files

## 1. Store: drop content columns

- [x] 1.1 Add migration `000014_drop_agent_prompt_columns` (up: drop `identity`, `soul`; down: re-add both as `text NOT NULL DEFAULT ''`).
- [x] 1.2 Store port: `SetPromptState(ctx, workspaceID, id, status, promptsErr)` — remove `identity`/`soul` params; update fake and postgres adapters (drop CASE-write of content, keep status/error).
- [x] 1.3 Remove `identity`/`soul` from postgres insert/column lists, scans, and `Update`; keep them on `domain.Agent` as projection fields and add `Bootstrap string`.
- [x] 1.4 Update store fake/postgres tests for the new signature and columns.

## 2. agents package: file helpers

- [x] 2.1 New `internal/agents/workspace.go`: `SeedWorkspace(dir)` (mkdir + write `AGENTS.md` from the embedded base prompt, idempotent write-if-absent), `WritePromptDocument(dir, name, content)` (atomic temp+rename), `WritePromptDocuments(dir, identity, soul, bootstrap)`, `ReadPromptDocuments(dir)` (missing files → empty).
- [x] 2.2 Unit tests: seeding is idempotent, atomic write leaves no temp residue, read of empty dir returns empty strings.

## 3. Generator: third document

- [x] 3.1 `GeneratedPrompts` gains `Bootstrap`; `promptsJSONSchema` requires `bootstrap`; OpenAI `response_format` name/description updated.
- [x] 3.2 System prompt template gains the BOOTSTRAP.md spec (birth sequence, user-request-first, three OnClaw beats, delete-on-completion) and the output JSON gains the third key.
- [x] 3.3 `ParseGenerationOutput` returns three values; tests cover the new key and reject empty bootstrap.

## 4. Service: write files before ready

- [x] 4.1 `Service.Generate` step 8: write the three documents into `agent.WorkspaceDir` (fetched row), then `SetPromptState(ready)`; a file-write failure transitions to `failed` with a sanitized message.
- [x] 4.2 Update service tests: success path asserts files exist on disk with the generated content; parse-failure path asserts no files written; empty `workspace_dir` fails cleanly.

## 5. Handlers

- [x] 5.1 `CreateAgent` and the workspace birth flow call `SeedWorkspace` (replacing bare `MkdirAll`).
- [x] 5.2 Read-side composition: list/get/create-refresh/patch/regenerate responses carry `identity`/`soul`/`bootstrap` from `ReadPromptDocuments`. *(Note: list composition superseded by agent-roster-search-sort)*
- [x] 5.3 `PatchAgent`: payload `identity`/`soul` write the corresponding files before `Update`; payload `bootstrap` is ignored.
- [x] 5.4 `DeleteAgent` removes the workspace directory (`os.RemoveAll`) after the DB delete.
- [x] 5.5 Handler tests: seeded `AGENTS.md` exists after create; PATCH rewrites files; delete removes the dir; responses compose from files.

## 6. Web + smoke

- [x] 6.1 `web/src/data/types.ts`: add `bootstrap?: string` to the Agent type.
- [x] 6.2 `scripts/smoke.sh`: after agent create, assert `AGENTS.md` (and, with a live provider, `IDENTITY.md`) exist in the agent workspace dir.

## 7. Verification

- [x] 7.1 `go build ./... && go vet ./... && go test ./internal/... -count=1` green.
- [x] 7.2 `openspec validate refactor-agent-prompts-to-files` passes.
- [x] 7.3 Frontend typecheck green (`pnpm build` or `tsc --noEmit`).
