# Tasks

## 1. Dependency and converter adapter

- [x] 1.1 Add `github.com/alnah/picoloom/v2` (v2.1.3) to go.mod, resolving the go-rod version alongside the browser tool's; verify `go mod tidy` and `go build ./...` succeed
- [x] 1.2 Create the adapter package defining the buddies v1 preset schema (schema-version field; theme, page setup, cover, footer, watermark, TOC — no CSS, no signature) and mapping it onto picoloom's option structs; unit tests cover the mapping, an empty config falling back to library defaults, and an unknown schema version being flagged
- [x] 1.3 Implement the manager-held converter: lazy creation on first use, mutex-serialized, closed on shutdown; unit tests cover lazy init and serialization seams (a real conversion stays env-gated like the browser tool's live tests)

## 2. Preset storage

- [x] 2.1 Define the document-preset store interface and types (CRUD, uniqueness per (name, format), default-preset selection) and implement the fake store; tests cover duplicate rejection and the default-reset read path
- [x] 2.2 Implement the sqlite store plus the forward-only migration creating `document_presets`; tests cover persistence across reopen, atomic updates, duplicate rejection, and delete removing only the row (backfill_test pattern)

## 3. Services and bindings

- [x] 3.1 Implement the preset CRUD service (list/create/update/duplicate/delete, default selector, deleting the default clears it) over the store; unit tests cover the spec scenarios
- [x] 3.2 Implement the generated-document bound read (plain file name only, resolved inside the calling agent's workspace, refusal before touching the filesystem, absolute path never returned); unit tests cover valid, traversal, and missing-name cases
- [x] 3.3 Regenerate Wails bindings and verify the frontend bindings compile (`task build` last so bindings are not stripped)

## 4. Tool surface

- [x] 4.1 Add the `create_document` catalog entry (workspace-scoped) and its `buildDirectTool` case; extend the contract/gate tests for allowlist narrowing, global disable, and slugless withholding
- [x] 4.2 Implement the `create_document` tool: input `{markdown, filename, preset?}`, jail-validated plain file name at the workspace root, preset resolution (named → configured default → library defaults), conversion under the 60s timeout, result carrying name and size, readable errors for unknown preset / bad name / conversion failure, overwrite on regenerate; unit tests cover each spec scenario
- [x] 4.3 Add the conditional preset appendix to the operating-contract composition (preset names with the default marked, only when the tool is offered and presets exist; nothing when there are none); extend the compose tests

## 5. Frontend — preset management

- [x] 5.1 Add the third gear to the Tools pane for `create_document` opening the new configuration dialog; verify with `vp check src`
- [x] 5.2 Build the dialog: format tabs (PDF only), preset list with create/edit/duplicate/delete, default-preset selector, grouped editor (theme, page setup, cover, footer, watermark, TOC) saving atomically; add `__test__` coverage for CRUD round-trip and default reset; verify with `vp check src`

## 6. Frontend — generated document card

- [x] 6.1 Build the `create_document` card body presenting the generated PDF through the bound read and the existing PDF panel renderer, with the absolute path never displayed; add `__test__` coverage; verify with `vp check src`

## 7. Integration checks

- [x] 7.1 `gofmt -w .`, `go vet ./...`, and `go test ./...` all pass
- [x] 7.2 Live smoke via `task build` + `task run`: create a preset in the Tools pane, ask an agent for a document naming it, and verify the PDF lands in the agent's workspace and previews on the card; repeat once with no preset named to confirm the default path
