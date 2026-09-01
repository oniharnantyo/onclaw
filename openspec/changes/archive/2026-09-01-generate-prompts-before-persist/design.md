# Design — generate-prompts-before-persist

## Context

`CreateAgent` persists the row (status `generating`), calls `Service.Generate` (which loads the row it just wrote, transitions status via `SetPromptState`), then refetches for the response — so a failed generation yields a 201 with a persisted `failed` agent. The generation pipeline's only store dependency for *producing documents* is fetching the agent it was handed an ID for; everything downstream (provider config resolution, key decryption, model invocation, output parsing) needs only the in-memory agent. See proposal.md for motivation.

## Goals / Non-Goals

**Goals:**
- Generation output and failure both resolve **before** any row exists; the create response is always `ready`.
- One shared generation pipeline for create and regenerate — no duplicated LLM-call logic.

**Non-Goals:**
- Changing regenerate semantics: a regenerate failure still transitions the (already-persisted) agent to `failed` + Retry.
- Changing the birth flow (it never generated synchronously) or the `generating → ready | failed` status machine.
- Frontend changes (the modal's existing catch path is the failure UX).

## Decisions

### D1. Extract the pipeline; add `GenerateForCreate`
`Service` splits into a shared `generateDocuments(ctx, workspaceID, agent) (identity, soul, bootstrap, err)` (provider fetch → key decrypt → model factory → messages → invoke → parse) plus two entry points:
- `GenerateForCreate(ctx, wsID, agent)` — pipeline + `WritePromptDocuments`; **no store agent fetch, no status writes**. Returns the error to the caller.
- `Generate(ctx, wsID, agentID)` — unchanged contract: loads the row (deleted-in-flight → no-op), runs the pipeline, `s.fail(...)` on error, re-verifies the directory still belongs to this agent, writes files, `SetPromptState(ready)`.

Alternative: a pre-persist temp row — rejected: throwaway rows need cleanup, and the whole point is that no row exists before readiness.

### D2. Failure maps to 400 invalid_request with the sanitized reason
`RespondError` maps unknown errors to 500 with the message swallowed ("internal server error") — hiding the actionable reason. Wrapping with `domain.ErrInvalid` yields 400 `invalid_request` with the sanitized text ("provider authentication failed — check provider API key") — client-visible and actionable. Alternative: 502 + a new error code — rejected: new envelope code off-contract for a provider-config problem the user can fix.

### D3. Cleanup is symmetric
Seed → generate → insert: a failure at generate or insert removes the seeded directory (`os.RemoveAll`, best-effort) so a failed deploy leaves nothing behind. Insert failure cleanup didn't exist before (the dir predated the row); added for symmetry.

### D4. The status field on the create path
`buildAgentFromCreateRequest` still builds the struct with `PromptsStatus: generating` (transient); the handler overwrites to `ready` after successful generation, immediately before insert. Create responses carry `ready` only.

### D5. Archive sequencing
Archive order stays: `refactor-agent-prompts-to-files` → `agent-roster-search-sort` → this change. The roster change's unarchived delta snapshots (`agents` CRUD, `web-app/agents`) get one-word consistency updates here (create carries `ready`, not `generating`) so each sequential archive lands truthful text. The behavioral requirement itself lives only in `agent-prompts`.

## Risks / Trade-offs

- [Slug race window widens] → validation still pre-checks the provider, and the insert still enforces the unique slug (409); a double-submit now also costs an LLM call. Accepted at workspace scale.
- [Create requests get slower on failure] → they were already synchronous on success; failures now cost the full generation attempt. Accepted — it buys no-error-agents.
- [The `prompts_status: generating` create-scenario wording in two unarchived deltas goes stale] → D5 folds the one-word sync into this session's roster-delta edits.
- [Regenerate in flight while create... ] → not possible: regenerate requires an existing row; the two paths don't intersect.
