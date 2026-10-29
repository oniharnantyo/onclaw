# Tasks

## 1. Workspace layout and event payload

- [x] 1.1 Add the reserved `artifacts` directory name beside `.tasks` and an on-demand `ArtifactsDir(slug)` mirroring `TasksDir` in `internal/workspaces/workspaces.go`; extend `workspaces_test.go` so on-demand creation and invalid-slug refusal pass
- [x] 1.2 Add the artifact payload to `internal/sessions/types.go`: an artifact record (workspace-relative path, size, change kind `created`/`modified`/empty) plus `Artifacts` and `ArtifactsTruncated` fields on the event, omitted when empty and set on `turn_completed` only; extend `types_test.go` so a payload with and without the fields round-trips through JSON and a payload without the field still decodes

## 2. Turn-boundary detection

- [x] 2.1 Create `internal/agents/artifacts.go`: list `artifacts/` (recursive, regular files only, dot-prefixed entries skipped, symlinks not followed) into a path → {size, mtime} record; diff two records into path-sorted `created`/`modified` entries capped at 50 with the truncation flag; a cold-resume listing that reports files with mtime at or after a given time with no change kind; unit tests in `artifacts_test.go` cover each walk rule, created vs modified, the unchanged omission, the cap, and the mtime predicate
- [x] 2.2 Hold the start record on the running turn in `internal/agents/manager_impl.go` so a live approval resume keeps it, and on the cold-resume path derive the predicate time from the persisted user-message event's occurrence time; in `internal/agents/runner.go` take the record at turn start, create `artifacts/` on demand, and stamp the delta onto the `turn_completed` event at its emission site; the hook-blocked completion stays artifact-free; an `ArtifactsDir` failure logs and disables detection for that turn; tests in a new agents test file cover a turn that creates a file under `artifacts/`, a turn that writes only at the root, a blocked submission, and the disabled path

## 3. Generated documents and the workspace binary read

- [x] 3.1 Root `create_document` output at `ArtifactsDir(slug)` in `internal/agents/docgen_tool.go`, keeping the plain-name input, the file-name validation, and the plain-name result unchanged; update the model-facing description to say output lands under `artifacts/`; update `docgen_tool_test.go` so the file lands under `artifacts/`, escapes are still refused, and overwrite still works
- [x] 3.2 Make `GeneratedDocument` in `internal/services/chat_documents.go` resolve the plain name under `artifacts/` first and fall back to the workspace root when absent there, both through the jailed reader; extend `chat_documents_test.go` to cover the artifacts hit, the legacy-root fallback, both-present preferring `artifacts/`, the unchanged refusals, and not-found
- [x] 3.3 Add `WorkspaceFileBinary(ctx, agentID, relPath)` on the chat service in `internal/services/chat.go` returning the mime (by extension; `application/pdf` for `.pdf`, `application/octet-stream` otherwise) and base64 data under the same jail as `WorkspaceFile`; tests cover a PDF read, a traversal refusal, and not-found

## 4. Operating contract

- [x] 4.1 Add one rule to the "Working in the workspace" section of `internal/agents/contract.go` directly after the "don't proactively create report files" rule: deliverables the user should find — reports, generated documents, exported data — go under `artifacts/` at the workspace root, and files that appear or change there during the turn are shown to the user with the reply; `contract_test.go` keeps pinning the unchanged section set and order and gains an assertion that the rule text is present

## 5. Bindings

- [x] 5.1 With `gofmt -w .`, `go vet ./...`, and `go test ./...` green, run `task generate:bindings` and verify the generated sessions model carries the artifact fields and the chat service binding carries `WorkspaceFileBinary`

## 6. Frontend — artifacts chips row

- [x] 6.1 Stamp the `turn_completed` event's artifacts and truncation flag onto the turn's assistant entry in `frontend/src/features/chat/entries.ts` where the event finalizes the turn, typed in `types.ts`; extend `__test__/entries.test.ts` so a completed turn carries the list, an empty list stamps nothing, and a regenerated variant carries its own
- [x] 6.2 Mint a trailing `data-artifacts` part after every other part of the turn's assistant message in `frontend/src/features/chat/runtime/mapping.ts`; extend `runtime/__test__/mapping.test.ts` so the part is last, carries the entry's list, and is absent when there are no artifacts
- [x] 6.3 Create `frontend/src/features/chat/components/ArtifactsView.tsx` — chips with an icon by extension, the file name, the `formatBytes` size, the change kind as a tooltip, six inline then a "+N more" disclosure expanding the rest, and a note when the list was truncated — styled with the attachment chip tokens for both themes; render it from the `data` case in `thread/ChatAssistantMessage.tsx` so it sits after the reply and outside the tool-call grouping; activation opens `workspaceFileTab(activeAgentID, path)`; `__test__` coverage for rendering, the disclosure, the no-row case, and the open call; `vp check src` and `vitest run` pass

## 7. Frontend — workspace PDF preview

- [x] 7.1 Add the `WorkspaceFileBinary` call to `frontend/src/lib/transport.ts` and a `workspaceFileBinary` wrapper to `frontend/src/lib/client.ts` following the `workspaceFile` pattern
- [x] 7.2 In `frontend/src/features/chat/components/panelRenderers.tsx` make the workspace-file preview choose by extension: `.pdf` → binary read → the knowledge tab's document (`<object>`) renderer; everything else → the existing text path with its markdown toggle; extend `components/__test__/panelRenderers.test.tsx` to cover the PDF branch, its read failure, and the unchanged text branch; `vp check src` and `vitest run` pass

## 8. Integration checks

- [x] 8.1 `gofmt -w .`, `go vet ./...`, `go test ./...`, `vp check src`, and `vitest run` all pass, and `task build` exits 0 last so bindings are not stripped
- [x] 8.2 Live smoke via `task build` + `task run`: ask an agent to write a report under `artifacts/` and to generate a PDF with `create_document`; confirm both chips appear below the reply outside any fold, the markdown chip opens a text preview, the PDF chip opens a document preview, and reloading the session shows the same chips
