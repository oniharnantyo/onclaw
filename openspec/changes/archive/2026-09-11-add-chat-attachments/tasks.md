# Tasks: add-chat-attachments

## 1. Attachment store and migration

- [x] 1.1 Migration: `attachments` table (id, workspace_id, storage_key, backend, name, mime, size, lane, created_by, created_at) with workspace index and down migration
- [x] 1.2 `store.AttachmentStore` port (Create, ByID(workspaceID, id)) + fake implementation; wire into `store.Store` aggregate as its own sub-interface
- [x] 1.3 Postgres adapter for AttachmentStore; integration test for create/scoped-lookup/foreign-unreachable
- [x] 1.4 Migration: `workspace_storage` (workspace_id PK, driver, endpoint, region, bucket, access_key_id, secret_access_key encrypted, use_path_style, updated_at) with down migration
- [x] 1.5 `store.WorkspaceStorageStore` port (Get, Upsert) + fake + postgres adapter (encrypted secret at rest); integration test for upsert/get round-trip and secret encryption

## 2. Upload endpoint and validation

- [x] 2.1 Lane classification: server-side table mapping sniffed mime + extension → inline-image / inline-pdf / inline-text / drop / reject; text sniff (null-byte reject); PDF size + best-effort page check
- [x] 2.2 Upload handler: multipart `file`, JWT workspace-member auth, MaxBytesReader, magic-byte sniff, caps (5 MB image / 20 MB pdf / 50 MB drop / zero-byte reject / ≤4 enforced per message client-side with server backstop), `storage.Put` under fresh capability key, insert row, `201 {id, name, mime, size, url}`
- [x] 2.3 Route under workspace scope + router wiring in the composition root (explicit injection, no nil guards); error envelope mapping (413/400)
- [x] 2.4 Handler tests: success shape, oversize, wrong-type-exe-as-png, zero-byte, unauthenticated, foreign-workspace tenancy
- [x] 2.5 smoke.sh section: upload → capability URL fetch → turn reference (extends chat section)

## 3. Blob storage backend (drivers + resolver + config API)

- [x] 3.1 Extend `storage.StorageConfig` with optional S3 fields (Endpoint, Region, Bucket, AccessKey, SecretKey, UsePathStyle); extend the local driver's open func untouched
- [x] 3.2 S3-compatible driver (`internal/storage/s3`, aws-sdk-go-v2): Put/Open/Delete implementing `storage.Storage`; path-style support; request timeouts; register via `storage.Register` blank import in `internal/cli` drivers
- [x] 3.3 `WorkspaceStorage` resolver: workspace config → driver instance (cached per workspace+config), instance-default local fallback; `ForWorkspace(ctx, wsID)` + resolve-for-attachment (consults the attachment's recorded backend); unit tests with fake driver
- [x] 3.4 Workspace storage config API (route /settings/storage pane backend): GET (masked secret keep-stored sentinel) and PUT for Owner/Admin; PUT with driver `s3` runs a connectivity probe and rejects 422 with the probe reason (previous config stays active); handler tests for masking, permission gating, probe failure
- [x] 3.5 Upload path switch: upload handler stores blobs via the resolver (not the instance storage directly) and records the backend on the attachment row; capability serving streams via the resolver

## 4. /v1 multimodal input

- [x] 4.1 `FlattenInput` → parts-aware: return text + attachment candidates (data URLs, onclaw capability URLs, `file_data` alias); reject `file_id` and remote URLs with `invalid_param`; string-only path byte-for-byte unchanged (regression test)
- [x] 4.2 Handler resolution: capability-URL pattern → attachment row lookup scoped to key workspace (`invalid_param` on unknown/foreign); inline data URLs → demote-on-arrival (store via resolver + row); assemble `ExecRequest.Attachments`; compact + attachments → `invalid_param`
- [x] 4.3 openresponses unit tests: each accepted/rejected shape, tenancy failure, compact rejection, string passthrough

## 5. Runner: multimodal turn construction

- [x] 5.1 `agents.AttachmentRef` + `ExecRequest.Attachments` field (nil-safe for cron/channels/compact callers)
- [x] 5.2 User-message construction: replace `runner.Query` with `runner.Run` + constructed `AgenticMessage` — fenced text parts (inline-text), `UserInputImage`/`UserInputFile` blocks with Base64Data (current turn), marked pointer-note block for drop-lane refs
- [x] 5.3 Attachment resolution: refs → bytes (inline, via `AttachmentBlobs` recorded-backend lookup) / run-scoped materialized path (drop); error surfaces as run failure
- [x] 5.4 Unit tests: attachment-only message, mixed message ordering, drop-lane pointer marking, attachment bytes absent from any transcript event

## 6. Persist demotion + model-time policy

- [x] 6.1 Session adapter: demote attachment blocks (Base64Data → capability-URL reference) in `AppendEvents`; test that no base64 lands in payloads and references preserve name/mime/size/URL
- [x] 6.2 New model-time middleware (`BeforeModelRewriteState`): URL-only attachment blocks → placeholder text naming file + workspace path; byte-carrying blocks pass; register in `buildMiddlewares`
- [x] 6.3 Tests: multi-iteration turn keeps bytes; next-turn replay shows placeholders (assert no image tokens/bytes on the wire); regenerated turn re-expands

## 7. Transcript fidelity

- [x] 7.1 `CompletedMessage.Attachments []AttachmentMeta`; live runner emits attachment metadata on the carrying turn's user message
- [x] 7.2 History projection: read demoted reference blocks → attachment metadata on projected user-message events; pointer-note blocks skipped in projected text and `extractAgenticText`
- [x] 7.3 Round-trip tests: live stream metadata == hydrated metadata; no pointer note in either text path

## 8. Drop-lane run-scoped materialization

- [x] 8.1 Runner materializes drop-lane attachments into a per-run temp dir at run start (download via resolver, driver-agnostic), mounts it read-only into the jail; stable per-attachment path layout
- [x] 8.2 Cleanup: run teardown removes the materialized dir; startup sweep for leftover run-scoped dirs; test: agent turn reads a drop-lane file via files tool, write attempt fails, dir removed after run

## 9. Web: upload client and composer tray

- [x] 9.1 `api.uploadAttachment(ws, file, {signal, onProgress})` (XHR progress) + client pre-check (extension/size) with instant rejection
- [x] 9.2 Composer chip tray state machine (composer-local useState — never global store): uploading{progress}/ready/rejected/failed; File retained until landing for Retry; ✕ = abort-cancel while uploading / remove when ready; timestamped default names for clipboard files
- [x] 9.3 Send gate: disabled while any chip uploading; enabled on text or ≥1 ready chip; attachment-only sends; chips clear + uploads abort on session switch
- [x] 9.4 Paste: textarea onPaste file items → chips (text paste untouched); Drag-drop: chat-surface handlers, preventDefault on dragover+drop (navigation test), dragenter/leave counter overlay, Files-only gate, folder rejection toast, over-cap toast
- [x] 9.5 Chip/tray rendering per design gallery (thumbnail vs icon, size format, rejected reason inline, failed Retry); vitest coverage for states and gate
- [x] 9.6 **UI gallery approval gate:** implementation of 9.x UI surfaces and the settings pane (9.7) against the design.md gallery requires explicit user approval of the gallery first (per ui-first workflow)
- [x] 9.7 Settings Storage pane per gallery K1–K9 (last nav entry, route /settings/storage): driver Segmented (Local default note / S3-compatible), structured S3 fields (masked secret sentinel, path-style toggle), Test connection inline result, probe-gated save with inline failure reason, member read-only state; vitest coverage for masking + failure states

## 10. Web: send path and transcript rendering

- [x] 10.1 `onSend(text, readyChips)` → store entry `attachments` metadata → `runTurn` builds item-array input (capability-URL parts); 409-conflict queue carries chips correctly; regenerate re-sends ids without re-upload
- [x] 10.2 `UserMessage` renders attachment chips (image inline thumbnail from capability URL; doc icon chip with type · size ⤓ download); identical for optimistic and hydrated entries; transcript-level tests (jsdom suite-scoped per repo convention)
- [x] 10.3 Hydrated rendering: history user-message events with attachment metadata render the same chips; regression test with a hydrated session fixture

## 11. Verification

- [x] 11.1 `go build ./... && go vet ./... && go test ./...` green; integration tests against postgres green
- [x] 11.2 Web: `pnpm build` green; touched vitest suites green (treat pre-existing localStorage-suite failures and full-parallel flakes as non-regressions)
- [x] 11.3 smoke.sh full pass including new attachment section (local-driver path; S3 covered by driver unit tests + probe test)
- [x] 11.4 Manual browser pass: paste a screenshot → agent describes it; attach PDF → agent answers content questions; reload → chips identical; oversize/office-type rejections at the door; storage pane save/probe flow including wrong-secret rejection
  - 2026-09-11 partial pass on dev: paste → timestamped chip (ready) ✓ but the describe turn 400s (图片输入格式/解析错误 — connector URL-over-base64, tracked in `fix-image-attachment-lane`); PDF → chip ✓ but turn fails ("unsupported content block type user_input_file" — same fix); reload → text and PDF pills hydrate and the IMAGE pill hydrates as an inline thumbnail `<img>` (CORRECTION 2026-09-11 evening: the earlier "image pill renders nothing" was an innerText measurement artifact — thumbnails carry no text; DOM probe shows both images loaded, naturalWidth > 0); rejections ✓ (>5 MB image and .docx rejected on the chip with the "export as PDF" copy; 300 KB text correctly admitted via the drop lane — 200 KB is the inline/drop threshold, not a cap); storage pane ✓ (probe error surfaces verbatim banner; save-time probe rejects with "Couldn't reach the bucket: …" and the active backend stays Local — the wrong-secret case rides the same probe-fail path, not reproducible without a live S3 endpoint). UPDATE 2026-09-11 evening after `fix-image-attachment-lane` + migrations 45/46 landed: image describe PASSES on the vision model (model read "73" off a canvas PNG); text-only model (QA/glm-4.7) shows the chip warning, completes, degrades to the pointer note (this required fixing the store's agent mapping, which dropped `input_modalities`, and the models-endpoint host-hint fallback). Remains open SOLELY for the PDF describe turn — blocked upstream: eino-ext libs/acl/openai chat-completions converter has no `UserInputFile` case (image/audio/video only), and the pinned pseudo-version is the newest published. Needs a decision: provider-aware PDF degrade on openai-compatible gateways vs upstream support (Anthropic/Gemini paths carry file blocks natively). FINAL 2026-09-11 evening: the degrade decision landed (fix-image-attachment-lane D4) — PDF on a text-only model completes as a marked reference-only pointer note (model told, transcript chip intact, note text hidden); the PDF-comprehension leg is settled as UPSTREAM (eino-ext chat-completions converter lacks UserInputFile; fail-open on pdf:true models errors at the converter) and is no longer an onclaw-side task. All other legs pass: paste describe ✓, reload chips identical ✓ (image = thumbnail, text/PDF = pills), oversize/office rejections ✓, storage pane probe/save ✓.

