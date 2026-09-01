## Why

Today `CreateAgent` persists the agent row first and generates prompts second; a failed generation leaves a persisted agent in `prompts_status: failed` — an "error agent" the deployer never asked to keep. Deploying against a broken provider key pollutes the roster with a dead card whose only purpose is to host a Retry button. Generating before persisting makes a failed deploy a failed *request* instead: the form stays open, the error is actionable, and nothing lands in the workspace.

## What Changes

- **BREAKING (API behavior):** `POST /workspaces/:ws/agents` now runs prompt generation **before** the row insert. On generation failure the request fails with 400 `invalid_request` carrying the sanitized provider error, no agent row is created, and the seeded workspace directory is removed. Previously the request returned 201 with `prompts_status: failed` and a persisted, retryable agent.
- On success the row is created directly in `prompts_status: ready` — the create response no longer carries `generating` or `failed`.
- `agents.Service` gains `GenerateForCreate` (generation for a not-yet-persisted agent: no store fetch, no status writes); the existing `Generate` keeps its regenerate contract (load row, status transitions, delete-during-flight no-ops) on a shared `generateDocuments` pipeline.
- Regeneration, the status machine (`generating → ready | failed`), the startup sweep, and the workspace birth flow (which never generated synchronously) are unchanged.
- The deploy wizard keeps its interactive loader; on failure the modal stays open with the error, so the user can fix the provider config in Settings and re-submit without retyping anything.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agent-prompts`: "Synchronous generation on create" inverts the failure semantics — generation runs pre-persist; failure aborts the create (no agent, no directory) instead of persisting a failed agent.

## Impact

- **Backend:** `internal/agents/service.go` (pipeline extraction + `GenerateForCreate`), `internal/server/handlers/agents.go` (`CreateAgent` reordering + cleanup + error mapping).
- **Tests:** service test for `GenerateForCreate` (writes files without a row; failure writes nothing); handler tests (success → 201 `ready`; generation failure → 400, no row, directory removed).
- **Frontend:** no change required — the modal's existing catch path (error state + toast, form stays populated) is exactly the new failure UX.
- **Coordination:** archive after both `refactor-agent-prompts-to-files` and `agent-roster-search-sort`. The roster change's unarchived delta snapshots pick up one-word consistency updates (create response carries `ready`, not `generating`) so sequential archives stay truthful.
- **Not changed:** regenerate-failure semantics (still `failed` + Retry on a live agent — the agent already exists there), birth flow, status sweep.
