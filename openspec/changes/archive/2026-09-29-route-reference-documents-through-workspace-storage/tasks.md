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
- [x] 4.3 Coordination note in this change's `.openspec.yaml` or proposal acknowledged by implementer: archive `add-reference-documents` (syncs `workspace-reference-documents` to main) before this change's spec sync/archive. Verify: archive order followed; `openspec validate` strict passes at sync time.
  - 2026-09-29: archive order followed — `add-reference-documents` archived first (2026-09-29-add-reference-documents), syncing `workspace-reference-documents` to main; `openspec validate --specs --strict` passed 55/55 at sync time before this change's sync.

## 5. Verification

- [x] 5.1 Full gate: `go build ./... && go vet ./... && go test ./...` and integration suite with a test database. Verify: all green.
- [x] 5.2 Live pass (user-gated): configure the dev workspace's storage for a local MinIO/S3-compatible endpoint via the settings pane, upload a document, confirm the blob lands in the bucket, the row records `s3`, the capability URL downloads byte-identical, `document.search`/mount work against it, and the pre-switch local document still reads. Verify: manual checklist recorded in the change.
  - 2026-09-29 live pass (dev workspace `master`, MinIO via Homebrew on :9000, bucket `onclaw-live` pre-created):
  - Storage switched to S3 via `PUT /workspaces/master/storage` — the exact contract the Storage pane submits; the pane re-hydrated showing driver S3-compatible with endpoint/region/bucket/keys, the secret masked to a hint (`••••dmin`), and path-style checked. (The pane's driver radio did not respond to automation clicks; the PUT is the same request the pane's Save issues.)
  - Post-switch upload: "Rotation Runbook" (1532-byte PDF fixture) → `indexStatus: ready` — the index was built from the S3-backed blob.
  - Blob in bucket: `ListObjectsV2` shows key `d1f84b5324c2d0773eb98105cc114944` (1532 bytes), matching the capability URL path.
  - Row records s3: `reference_documents.backend = 's3'` for the new row; pre-switch rows remain `'local'`.
  - Capability URL byte-identical: `GET /api/v1/files/d1f84b53…` downloaded 1532 bytes, `cmp` identical to the fixture.
  - `document.search` works against it: a chat run over the index returned hits with distinct document ids and correct `p. 1` locators for both same-fixture documents (agent-flagged near-duplicate). An earlier empty hit was FTS AND-semantics ("runbook" appears in no document text), not a storage failure.
  - Mount works: `references/Rotation Runbook` mounted in the jail (1532 bytes); the agent extracted real PDF content streams from it and `cmp`'d it byte-identical to the Service Manual through the mount.
  - Pre-switch local document still reads: `FDS_Sokratech_Spesifikasi_Input_Output.docx` (backend `local`) downloads via its capability URL byte-identical to the local blob after the switch; the panel source resolves it; it remains mounted in `references/`.
  - Env left sane: storage switched back to `driver: local` after the pass. Note: the Rotation Runbook's blob lives in MinIO (`/tmp/onclaw-minio-data`) — restart MinIO (`minio server /tmp/onclaw-minio-data --address :9000`) or delete that document if MinIO is gone.
