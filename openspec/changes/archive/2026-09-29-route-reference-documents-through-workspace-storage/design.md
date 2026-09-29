# Design

## Context

`references.Service` holds one `storage.Storage` dependency (`stor storage.Storage`, `service.go:59`), wired at two construction sites to the instance-level driver opened at boot (`internal/cli/server.go:92→112`, `internal/server/router.go:313→rt.opts.Storage`). That instance lane is local-only in practice: it passes only `DataDir` into `StorageConfig`, so even `ONCLAW_STORAGE_DRIVER=s3` yields an unconfigured S3 driver. The workspace resolver (`internal/storage/resolver.WorkspaceStorage`, constructed at `cli/server.go:105` and exposed as `rt.opts.WorkspaceStorage`) already implements the per-workspace dispatch the pane controls: `ForWorkspace` (write-path driver from workspace config, instance default when unconfigured), `ForBackend` (driver for a recorded backend name), `DriverName` (backend name resolution). The service already anticipates it: `backendName` type-asserts a `backendNamer` interface for `DriverName`. Capability-URL serving (`handlers/files.go` reference-document branch) already streams via `ForBackend(doc.WorkspaceID, doc.Backend)` — verified, needs no change. The `reference_documents.backend` column exists and is populated (`local` on current rows).

See proposal.md — Why — for the live discovery.

## Goals / Non-Goals

Goals:
- Reference document blobs (upload, replace) land in the workspace's configured driver.
- Every blob read inside the references service (indexing, re-index, mount materialization, delete) dispatches on the recorded per-blob backend.
- Existing `backend = local` rows keep working unchanged — zero migration.

Non-Goals:
- No migration tooling for existing blobs (mixed backends are a supported steady state, mirroring attachments).
- No changes to the capability-URL serving path, the documents API surface, or the frontend.
- No per-workspace credentials handling beyond what the resolver already does (it owns decryption via `encKey`).
- No instance-level S3 wiring for the boot-time driver — unconfigured workspaces keep the local instance default through the resolver.

## Decisions

- **D1 — Swap the dependency to the resolver, not a second Storage.** `references.NewService` takes the `*resolver.WorkspaceStorage` (or a narrow interface `workspaceStorage{ ForWorkspace; ForBackend; DriverName }` to keep the constructor testable without the concrete type). The service calls `ForWorkspace(ctx, wsID)` for writes and `ForBackend(ctx, wsID, doc.Backend)` for reads. *Alternative considered:* implement `storage.Storage` on `WorkspaceStorage` and keep the constructor unchanged — rejected: `Put`/`Open` would need a workspace argument that the 4-method interface doesn't carry, forcing request-scoped state or a misleading contract.

- **D2 — Writes record the resolved driver's name.** `Upload`/`Replace` obtain `st := ForWorkspace(ctx, wsID)`, `Put` through it, and record `backendName` (existing `backendNamer` assertion — now always satisfied by the resolver) on the row. *Alternative:* hardcode "s3"/"local" by config sniffing — rejected: duplicates the resolver's driver knowledge.

- **D3 — Reads dispatch per row, never on current config.** `readBlob` (used by indexing and `WriteMount` materialization) and `Delete` resolve via `ForBackend(ctx, wsID, doc.Backend)`. This is what keeps pre-switch local rows readable and deletable after the workspace moves to S3 — the same invariant the attachments lane and `ServeFile` already honor.

- **D4 — Wiring: pass the existing resolver instances.** `cli/server.go` constructs `wsResolver` (line ~105) three lines before `referencesSvc` (line ~112) — pass it. `router.go:313` uses `rt.opts.WorkspaceStorage` (already on opts, line ~616). The instance `stor` stays as the resolver's `instanceDefault` argument — unchanged role.

- **D5 — Interface first, concrete type in tests.** The narrow interface (D1) lets fake-based service tests inject a scripted resolver (local fake driver + a second fake "s3" driver) without S3 or network. Integration coverage with a real S3-compatible endpoint stays out of scope; the resolver has its own tests.

## Risks / Trade-offs

- [Double-dispatch overhead per read (resolver config lookup)] → the resolver caches driver instances per workspace fingerprint (`fingerprint(cfg)`); repeat reads reuse the cached driver. No added I/O beyond the first.
- [`WriteMount` materializes many documents per run] → dispatch overhead stays flat (`ForBackend` hits the resolver's per-workspace driver cache), but with an S3-configured workspace each visible blob is a **network fetch per run**: the mount is materialized fresh into the run-scoped session directory and torn down after, so nothing is reused across runs — real latency and egress where local disk was a `read` syscall. Accepted for v1 (libraries are small; materialization happens once at run start, off the user's critical path), with blob-fetch caching as the named future optimization: a server-side cache keyed by storage key + backend ETag that `WriteMount` copies from instead of re-fetching, so unchanged blobs cost one HEAD per run. Not in scope here — it would add cache-invalidation state this change deliberately avoids.
- [Two construction sites drift again] → the narrow interface makes the wrong wiring a compile-time mismatch (`*storage` local driver no longer satisfies `workspaceStorage`), not a silent runtime default.
- [Coordination with the unarchived `add-reference-documents` delta spec] → this change's MODIFIED delta applies after that change archives and syncs `workspace-reference-documents` to main; validation against main specs happens at that point. Sequence recorded in proposal and tasks.

## Migration Plan

1. Land the service rewiring with fake-based tests (no behavior change for unconfigured workspaces — resolver returns the same instance default local driver).
2. Deploy. Existing rows: unchanged behavior (`backend = local`, same blob path). No backfill, no migration.
3. Workspaces that want S3 switch via the Storage settings pane; subsequent uploads land in the bucket; earlier documents keep serving from local.
4. Rollback: revert the deployment; rows written while on S3 remain readable only if the workspace config still resolves S3 (same mixed-backend rule as attachments — the pane already warns for the attachment case).

## Open Questions

- None blocking. (If the resolver's driver cache ever needs eviction semantics for config churn, that is the resolver's concern and already fingerprint-keyed.)
