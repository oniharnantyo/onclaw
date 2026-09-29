# Tasks

## 1. Service dependency swap

- [x] 1.1 Define the narrow `workspaceStorage` interface (`ForWorkspace`, `ForBackend`, `DriverName`) in `internal/references` and change `NewService` to take it instead of `storage.Storage`; the `stor` field becomes the interface. Verify: `go build ./...` fails at old call sites until 1.2 lands — build both together.
- [x] 1.2 Rewire construction sites: `internal/cli/server.go` (~112) passes the existing `wsResolver`; `internal/server/router.go` (~313) passes `rt.opts.WorkspaceStorage`. Verify: `go build ./...` and `go vet ./...` clean.

## 2. Write path

- [x] 2.1 `Upload`: resolve `ForWorkspace(ctx, wsID)` and `Put` through it; record the resolved driver name via the existing `backendName` path. Verify: fake-based service test — upload against a scripted two-driver resolver asserts `Put` hits the workspace-configured driver and the row records its backend name.
- [x] 2.2 `Replace`: same routing — new blob to the workspace's current driver, recorded backend updated, index rebuilt. Verify: fake-based service test — replace moves the recorded backend and leaves no stale sections.

## 3. Read path (per-blob dispatch)

- [x] 3.1 `readBlob` dispatches via `ForBackend(ctx, wsID, doc.Backend)`; indexing and re-index conversion inherit it. Verify: service test — a row recording the second driver reads its bytes from that driver, not the default.
- [x] 3.2 `WriteMount` materializes each document through its recorded backend. Verify: service test — mixed-backend library mounts both files with byte-identical content.
- [x] 3.3 `Delete` removes the blob from the recorded backend. Verify: service test — deleting an S3-recorded row removes the blob from the second driver and leaves the local driver untouched.

## 4. Regression & coordination guards

- [x] 4.1 Existing-rows regression: unconfigured workspace (resolver falls back to instance default) keeps `backend = local` behavior end to end — upload, download, search, mount, delete. Verify: fake-based service test plus `go test ./internal/references/... ./internal/server/...`.
- [x] 4.2 Capability-URL serving branch unchanged: confirm `handlers/files.go` reference branch still streams via `ForBackend` (no edit expected). Verify: existing handler tests pass; add one mixed-backend serving test if coverage is missing.
- [ ] 4.3 Coordination note in this change's `.openspec.yaml` or proposal acknowledged by implementer: archive `add-reference-documents` (syncs `workspace-reference-documents` to main) before this change's spec sync/archive. Verify: archive order followed; `openspec validate` strict passes at sync time.

## 5. Verification

- [x] 5.1 Full gate: `go build ./... && go vet ./... && go test ./...` and integration suite with a test database. Verify: all green.
- [ ] 5.2 Live pass (user-gated): configure the dev workspace's storage for a local MinIO/S3-compatible endpoint via the settings pane, upload a document, confirm the blob lands in the bucket, the row records `s3`, the capability URL downloads byte-identical, `document.search`/mount work against it, and the pre-switch local document still reads. Verify: manual checklist recorded in the change.
