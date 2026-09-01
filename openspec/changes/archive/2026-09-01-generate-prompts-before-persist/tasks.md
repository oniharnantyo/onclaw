# Tasks — generate-prompts-before-persist

## 1. Service pipeline extraction

- [x] 1.1 Extract `generateDocuments(ctx, workspaceID, agent) (identity, soul, bootstrap string, err error)` in `internal/agents/service.go`: provider fetch → key decrypt → model factory → `BuildGenerationMessages` → invoke → `ParseGenerationOutput`; all failures return sanitized-reason errors, **no status writes**
- [x] 1.2 Rewrite `Generate` to use the pipeline: load row (deleted-in-flight → no-op) → pipeline → on error `s.fail(...)` → re-verify directory ownership → write files → `SetPromptState(ready)`; behavior contract unchanged
- [x] 1.3 Add `GenerateForCreate(ctx, workspaceID, agent)`: pipeline + `WritePromptDocuments` into `agent.WorkspaceDir`; no store fetch, no status writes; error returns to caller
- [x] 1.4 Extend `internal/agents/service_test.go`: `GenerateForCreate` writes the three documents without any agent row existing; a failing factory/invocation returns the error and writes no documents

## 2. Handler reordering

- [x] 2.1 `CreateAgent`: move generation before `store.Agents().Create` via `GenerateForCreate`; on failure respond 400 (`domain.ErrInvalid` wrapped, sanitized reason) and remove the seeded directory; on success set `prompts_status: ready` before insert; also clean the directory when the insert itself fails
- [x] 2.2 Handler tests: create success → 201 with `prompts_status: ready` and populated documents; generation failure (failing model factory) → 400 `invalid_request`, agent absent from store, workspace directory absent on disk

## 3. Spec snapshot sync + verification

- [x] 3.1 Roster change's delta snapshots: `agents` CRUD "Create with wizard defaults" and `web-app/agents` "Deploy a new agent" scenarios reflect the create response carrying `ready` (wording only; behavior specified in agent-prompts)
- [x] 3.2 `rtk go build ./... && rtk go vet ./... && rtk go test ./...`
- [x] 3.3 `go test -tags=integration ./internal/store/postgres/` (no store changes expected — regression guard)
- [x] 3.4 `./scripts/smoke.sh` — agent create path end-to-end (smoke asserts `AGENTS.md` after create; still valid: success path unchanged in shape)
