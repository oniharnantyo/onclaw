# Tasks — regenerate-enhances-prompts

## 1. Enhance-mode generation

- [x] 1.1 `prompts.go`: split the monolithic system prompt into a shared document-spec block plus fresh vs enhance framing; `BuildGenerationMessages(agent, current *GeneratedPrompts)` — nil current = fresh, non-nil = enhance with the current documents inline
- [x] 1.2 `service.go`: `generateDocuments` accepts the current documents and threads them into the messages; `Generate` reads `ReadPromptDocuments(agent.WorkspaceDir)` first — any non-empty document switches to enhance, read errors degrade to fresh

## 2. Backup-on-write

- [x] 2.1 `workspace.go`: `WritePromptDocuments` backs up each existing document as `<name>.bak` (staged temp + rename) after staging and before any commit rename; backup failure aborts the commit, removes staged files, leaves originals untouched
- [x] 2.2 `workspace_test.go`: backup created with the previous content on overwrite; no `.bak` on first write; backup failure leaves originals intact

## 3. Prompts tab

- [x] 3.1 `AgentConfigModal.tsx`: add the Prompts tab between Capabilities and Memory; move status row + failed banner + Regenerate + identity/soul editors there; BOOTSTRAP.md rendered read-only; `.bak` hint line; Identity tab keeps only identity fields
- [x] 3.2 `handleRegenerate` stays on the Prompts tab, keeps the modal open, refetches agent detail on success
- [x] 3.3 `AgentConfigModal.test.tsx`: Prompts tab renders the file list with status + Regenerate; identity/soul editable under the tab; regenerate refetches instead of closing

## 4. Verification

- [x] 4.1 `go build ./... && go vet ./... && go test ./...`
- [x] 4.2 `go test -tags=integration ./internal/store/postgres/`
- [x] 4.3 `rtk proxy npx tsc --noEmit && rtk proxy npx vitest run` (web)
- [x] 4.4 `openspec validate regenerate-enhances-prompts`
## 5. Two-pane Prompts tab & change instructions (follow-up)

- [x] 5.1 `prompts.go`: `requestedChangesSection` + instruction param on `BuildEnhancePrompt`/`BuildGenerationMessages` (fresh mode appends it too); `service.go`/`Generate` and the regenerate handler carry the optional `{instruction}` body; empty body regenerates without one
- [x] 5.2 `AgentConfigModal.tsx`: two-pane Prompts tab (file list left, selected-file preview right); Regenerate opens the change-request form; submission passes the instruction; helper text states the old prompts are always sent along
- [x] 5.3 `api.ts`/`store`: `regenerate`/`regenerateAgent` accept an optional instruction and POST `{instruction}` when non-empty
- [x] 5.4 Tests: `TestService_Generate_InstructionSteersEnhancement`, `TestBuildGenerationMessages_InstructionInFreshMode`, modal form-flow + empty-instruction tests
