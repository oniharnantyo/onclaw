# Proposal

## Why

The Storage settings pane lets a workspace switch blob storage to S3, and chat attachments honor it — but reference documents are pinned to the instance-level storage driver opened at server boot (`internal/cli/server.go` passes the raw `storage.Storage` into `references.NewService`). Every upload therefore lands in `data/files/` regardless of workspace configuration, breaking the pane's promise and the `workspace-reference-documents` spec's own "local and S3-compatible drivers, per-blob backend recorded" requirement. Discovered live (2026-09-28): the workspace's only document row reads `backend = local` with its blob under `data/files/` while the pane advertised S3.

## What Changes

- `references.Service` swaps its `stor storage.Storage` dependency for the workspace storage resolver (`internal/storage/resolver`): `ForWorkspace(ctx, wsID)` for writes, `ForBackend(ctx, wsID, doc.Backend)` for per-blob reads.
- `Upload` and `Replace` write blobs through the workspace's configured driver and record that driver's name (`DriverName` — the existing `backendName` namer pattern) in the `backend` column.
- Internal blob reads — `readBlob` (indexing/re-index), `WriteMount` (runner references-mount materialization), and `Delete` (blob removal) — dispatch on the row's recorded backend.
- Construction sites (`internal/cli/server.go`, `internal/server/router.go`) inject the resolver instance that already exists there instead of the instance default.
- No change needed to capability-URL serving: `ServeFile`'s reference-document branch already streams via `ForBackend(doc.WorkspaceID, doc.Backend)` (verified).
- No **BREAKING** changes: existing rows keep `backend = local` and stay byte-identical readable; no schema or API change (the `backend` column already exists).

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `workspace-reference-documents`: the "As-is blob storage" requirement is tightened — blobs SHALL ride the workspace's configured storage driver (the resolver behind the Storage settings pane), not the instance default; reads SHALL dispatch on the recorded per-blob backend so mixed local/S3 coexist. Note: this capability currently exists only as the active `add-reference-documents` change's delta (not yet synced to main specs); that change archives first, then this delta applies on top of the synced spec.

## Impact

- `internal/references/service.go` — dependency type plus five call sites (`Upload`, `Replace`, `readBlob`, `WriteMount`, `Delete`); `backendName` already supports the resolver's `DriverName`.
- `internal/cli/server.go` (~line 112) and `internal/server/router.go` (~line 313) — inject the `resolver.WorkspaceStorage` instance each already constructs.
- `internal/store/fake` test doubles — a fake workspace-storage resolver for service tests.
- Runners: references-mount materialization becomes backend-aware (unchanged visible behavior).
- No migration, no API change, no frontend change.
- Coordination: `add-reference-documents` (active, 38/39) archives before this change's spec sync.
