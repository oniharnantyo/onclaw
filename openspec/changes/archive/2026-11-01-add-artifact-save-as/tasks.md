# Tasks

## 1. Implementation

- [ ] 1.1 `internal/services/chat_documents.go`: extract the shared name-validation helper; add `SaveGeneratedDocumentAs` per design D1 (typed `ErrExportCancelled` / `ErrExportUnavailable`).
- [ ] 1.2 `internal/services/chat.go` (or the service's config): the injectable `SaveDialog` interface with the unavailable default; wire the wails-backed implementation in the desktop host and leave the default in server assembly.
- [ ] 1.3 `internal/services/appinfo.go`: `NativeDialogs bool` on Metadata; set true in the desktop host wiring, false in server assembly.
- [ ] 1.4 `frontend/src/features/chat/tools/aui/CreateDocumentTool.tsx`: Save As control per design D4 (hidden when `nativeDialogs` is false; silent cancel; toasts otherwise).

## 2. Tests

- [ ] 2.1 Service tests: jailed-name refusals (identical to GeneratedDocument's), artifacts-first/root-fallback sourcing, cancelled sentinel, unavailable sentinel, copy lands with the same bytes (fake save dialog).
- [ ] 2.2 AppInfo test: metadata carries the flag as wired.
- [ ] 2.3 Frontend tests (`createDocumentTool.test.tsx` + harness): control hidden when the flag is false; success/cancel/unavailable toast paths; single in-flight invocation.

## 3. Verification

- [ ] 3.1 `gofmt -w .` && `go vet ./...` && `go test -count=1 ./internal/...` (scoped to touched packages minimum) && frontend `vp check src`.
- [ ] 3.2 Live smoke (manual, desktop): Save As on a generated PDF and on the akta DOCX — dialog seeded with the file name, copy opens from the chosen destination; cancel leaves no toast; server mode hides the control.
