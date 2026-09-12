# Design: add-chat-attachments

## Context

Everything between the browser and the model speaks plain text today: the web composer sends a string, `FlattenInput` (internal/openresponses/dto.go) errors on any non-`input_text` part, and `ExecRequest.Input` is a string consumed by `runner.Query(ctx, req.Input, ...)`. Underneath, the stack is multimodal-ready: eino `AgenticMessage.ContentBlocks` carries `UserInputImage`/`UserInputFile` (URL or Base64Data), `TypedRunner.Run` accepts full message arrays, and all three agentic model backends (agenticopenai/agenticclaude/agenticgemini) convert those blocks to native provider parts. Storage has an upload precedent (avatar: magic-byte sniff, capability keys, public serving) and the fs jail supports read-only extra roots (the mechanism skills ship through). The ADK exposes `BeforeModelRewriteState` — the sanctioned seam for rewriting messages before every model call. See proposal.md for motivation.

## Goals / Non-Goals

**Goals:**
- Users attach images and documents to agent chats; the model actually sees them on the carrying turn.
- Attachment bytes never persist in session events, never ride the turn request, and never re-send on later turns.
- The `/v1` wire stays OpenResponses-convention-pure (`input_image`/`input_file` URL forms; inline data URLs tolerated).
- Composer UX: chips with real upload states, paste, drag-drop, hard send gate; transcript chips identical live and hydrated.

**Non-Goals:**
- Remote third-party `file_url` fetching (SSRF surface; cut for v1).
- docx/xlsx extraction (reject with "export as PDF"; a future extraction change flips the lane).
- Channel/team composer attachments; cron attachments.
- Model vision-capability gating (provider errors surface as run errors).
- Orphan GC sweep (orphans accepted); attachment deletion/revocation UI; EXIF scrubbing; dedupe.
- Keeping old images in model context (any keep-N policy — the dial is one function, revisit on demand).

## Decisions

### D1 — Bytes cross the wire once, on their own endpoint

`POST /api/v1/workspaces/{ws}/attachments` (multipart `file` field, JWT workspace-member middleware). Handler sniffs magic bytes (`http.DetectContentType`), enforces caps, `storage.Put` under a fresh capability key, inserts an `attachments` row, returns `201 {id, name, mime, size, url}` where `url` is the capability URL. **The capability URL is the wire token**: turn inputs reference attachments by it, the browser renders from it, and the server resolves its own URL pattern straight from storage (no HTTP self-fetch).

*Alternatives:* inline-only (no durable copy, megabyte turn bodies, no upload progress) and an id-only extension field (`onclaw:file_id` — rejected; the OpenResponses convention has no `file_id` in input parts and a custom part breaks convention purity for zero gain).

### D2 — /v1 input parts, convention shapes with a tolerance alias

`FlattenInput` becomes parts-aware: `input_image {image_url, detail?}` and `input_file {file_url, filename}` where the URL is either a `data:` URL or an onclaw capability URL. Accepted alias: `file_data` (OpenAI-live inline form) → treated as the inline form. Rejected: `file_id` (`invalid_param` — no OpenAI files backend to resolve against) and any remote third-party URL (`invalid_param`, v1). Inline forms are **demoted on arrival**: bytes → `storage.Put` + attachment row → downstream sees the same reference shape as uploads. `detail` passes through to the OpenAI backend and is ignored elsewhere. String-only inputs behave byte-for-byte as before.

### D3 — Data model and migration

One table (migration, next free number):

```sql
CREATE TABLE attachments (
  id          UUID PRIMARY KEY,
  workspace_id UUID NOT NULL REFERENCES workspaces(id),
  storage_key TEXT NOT NULL,          -- capability key; random, not enumerable
  name        TEXT NOT NULL,          -- original or minted filename
  mime        TEXT NOT NULL,          -- server-sniffed
  size        BIGINT NOT NULL,
  lane        TEXT NOT NULL,          -- 'inline-image' | 'inline-pdf' | 'inline-text' | 'drop'
  created_by  UUID NOT NULL REFERENCES users(id),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now());
CREATE INDEX idx_attachments_workspace ON attachments(workspace_id);
```

No FK from messages to attachments (session events carry references in payloads; nothing dangles when rows are swept later). Store port: `store.AttachmentStore` with `Create`, `ByID(workspaceID, id)`; injected as its own positional dependency per repo convention.

### D4 — Three lanes and the caps matrix (locked)

| Lane | Types | Cap | Mechanic |
|---|---|---|---|
| inline-image | png jpeg webp gif | 5 MB, ≤4/message | `UserInputImage` block, Base64Data at model time |
| inline-pdf | pdf | 20 MB, ~100 pages | `UserInputFile` block (oversize → reject; read_file can't parse PDF either, so no fallback) |
| inline-text | txt md csv json + code exts | 200 KB | server reads bytes → fenced text part (larger → drop lane automatically) |
| drop | text-like everything else (py sql yaml sh log …) | 50 MB | stored + read-only mount + pointer note |
| reject | docx xlsx pptx zip exe unknown | — | 400 at upload: "Office formats aren't supported yet — export as PDF" |

Classification lives server-side (sniff + extension table) and is stored on the row (`lane`). Text detection: extension family + sniff confirms text-ish; null bytes reject.

### D5 — Context lifetime: current-turn-only, keyed on block shape

The runner builds the current turn's user message with real bytes (`Base64Data` set). Persist demotes blocks to URL-only references. At model time, one rule: **blocks carrying bytes pass; URL-only blocks become placeholder text** ("[image: shot.png — saved to workspace attachments; use read_file to re-open]"). No turn-boundary bookkeeping — the block's own shape is the age marker. Within the carrying turn (tool-loop iterations, retries, regeneration) bytes are present on every call, because that message is the one being constructed.

*Why not keep-all:* ~0.8–1.4k tokens/image re-billed every turn (up to ~14k for ten screenshots) with no Anthropic caching subsidy on this stack; PDFs make it unbounded. *Why not keep-N:* needs turn-boundary tracking in the middleware for a policy nobody has asked for yet; the dial is one function if that changes.

### D6 — Persist demotion lives in the session adapter

`ADKSessionAdapter.AppendEvents` sanitizes `SessionEventMessage` events: attachment blocks in user messages swap `Base64Data` → reference form (capability URL + name + mime + size preserved on the block). One chokepoint, matching the single-chokepoint instinct from channels; the ADK and runner stay uninvolved.

### D7 — Model-time expansion is a middleware

New `TypedChatModelAgentMiddleware` whose `BeforeModelRewriteState` walks inbound user messages and replaces URL-only attachment blocks with their placeholder text blocks (D5). Registered in `buildMiddlewares` after the fs middleware. The placeholder names the file and its read-only workspace path so the pointer is actionable.

### D8 — Drop lane: read-only mount + marked pointer note

The attachments storage root mounts into the agent jail as a read-only extra root (existing skills mechanism) at a stable path (`attachments/<attachment-id>/<name>` per file layout to be settled at implementation). The pointer note is emitted as its own marked text block so both `extractAgenticText` (live bubble text) and the history projection (hydrated text) skip it — the note reaches the model but never the visible transcript (the leak hazard from exploration).

### D9 — ExecRequest grows an optional Attachments payload

```go
type AttachmentRef struct {
    ID, Name, MimeType, Lane string
    Size int64
}
// ExecRequest
Attachments []AttachmentRef   // nil for cron/channels/compact — untouched callers
```

The v1 handler resolves input parts → refs (tenancy-checked, `invalid_param` on failure) and passes them with the flattened text. The runner resolves refs → bytes/storage paths when constructing the user message.

### D10 — Transcript fidelity: attachment metadata in CompletedMessage

`CompletedMessage` gains `Attachments []AttachmentMeta{Name, MimeType, Size, URL}`. The live runner emits it when the turn carried attachments; the history projection reads the demoted reference blocks. Both paths produce identical metadata; `extractAgenticText` unchanged except skipping pointer-note blocks (D8).

### D11 — Web: chips are composer-local; aui stays a text passthrough

Chip state lives in the Composer's local `useState` — **never in the global store** (progress ticks are high-frequency; per-event store writes already tripped React 19's nested-update limit once in this codebase). `onSend(text, readyChips)`; the store entry gains `attachments: [{id, name, mime, size, url}]`; `runtime.tsx`'s `onNew` passes chips through to `runTurn`, which builds item-array input (`input_text` + `input_image`/`input_file` with capability URLs). assistant-ui receives text-only `AppendMessage`s exactly as today; the chip shape deliberately mirrors `CompleteAttachment` so a future move onto aui primitives converts 1:1.

### D12 — Send gate: hard, no queueing

Send enabled ⟺ no chip is uploading AND (text non-empty OR ≥1 chip ready). Attachment-only sends are valid. No send-when-ready queueing: snapshot-vs-live ambiguity after Enter (edited text, changed mind) is a state-machine trap; the disabled button is the Slack/ChatGPT convention and costs nothing on the fast path. Regenerate sends chip references (ids persist server-side), never re-uploads.

### D13 — Client upload client

Shared `uploadAttachment(ws, file, {signal, onProgress})` (XHR for progress events; fetch lacks upload progress). Client pre-checks extension/size for instant feedback (paste/drop bypass the picker's `accept` filter) — the server's magic-byte sniff remains authoritative. Retry retains the `File` object on the chip until landing. Caps toasts: "Up to 4 attachments per message".

### D14 — Paste and drag-drop mechanics

Paste: textarea `onPaste`, `clipboardData.items` file-kind items → chips (timestamped default names: `screenshot 2026-09-09 14.32.png`); text paste untouched. Drop: handlers on the chat surface (message list + composer), `preventDefault` on dragover/drop (without it the browser navigates to the file — catastrophic), dragenter/dragleave counter for the overlay, `types.includes('Files')` gate so text drags don't trigger it, folder drops rejected with a toast. Mobile: paste works; drag-drop doesn't exist — fine, the picker remains canonical and accessible.

### D15 — Blob storage is the existing storage port, second driver

The abstraction already exists (`storage.Storage` Put/Open/Delete/URL + a `Register(name, DriverOpenFunc)` registry): attachment blobs are written through it and an **S3-compatible driver** (`internal/storage/s3`, aws-sdk-go-v2 — new dependency) registers alongside local as an ordinary driver, per the repo's extension-point rule. `StorageConfig` grows optional S3 fields (Endpoint, Region, Bucket, AccessKey, SecretKey, UsePathStyle) so one registry path serves both drivers. A small resolver — `WorkspaceStorage.ForWorkspace(ctx, wsID)` — maps the workspace's stored configuration to a driver instance (cached per workspace+config; instance default = local when unconfigured) and is injected positionally into the upload handler and the runner's byte resolution. **Capability serving stays proxied** (onclaw streams from the backend): `URL(key)` remains the onclaw cap path for every driver, so transcripts, wire tokens, and revocation semantics are driver-invariant. *Alternative considered:* presigned direct-to-S3 URLs (offloads bandwidth, providers could fetch them) — rejected for v1: expiring links in transcripts, a second URL semantics to keep honest, and no current need.

### D16 — Workspace storage configuration (new migration + encrypted secret)

`workspace_storage` (one row per workspace, next-free migration): `workspace_id` PK, `driver`, `endpoint`, `region`, `bucket`, `access_key_id`, `secret_access_key` (encrypted, same mechanism as existing workspace secrets), `use_path_style`, `updated_at`. API: GET (masked secret — keep-stored sentinel, same idiom as provider keys/MCP) and PUT for Owner/Admin; PUT with driver `s3` runs a connectivity probe (HeadBucket-equivalent) and rejects `422` with the probe reason on failure, leaving the previous config active. The `attachments` row records its backend (denormalized `driver` + workspace config snapshot reference — implementation settles exact shape), so flipping Local→S3→Local never orphans old blobs. Read paths (capability serving, runner byte resolution) consult the attachment's recorded backend, not the workspace's current one. The config is deliberately workspace-generic — chat attachments are its first consumer and later storage surfaces adopt the same row and resolver without a rename.

### D17 — Drop lane materializes run-scoped (uniform across drivers)

D8's read-only mount source changes shape: instead of mounting the storage root (impossible for S3), the runner **materializes drop-lane attachments into a per-run temp directory** at run start (downloads via the resolver), mounts that read-only, and removes it at teardown (`teardownBrowserSession` cleanup precedent). One code path for every driver; the agent-visible `/workspace` path is identical whether blobs live on disk or in a bucket. Local-driver workspaces accept the redundant copy in exchange for uniformity.

## UI Gallery (full-surface, implementation-gating)

Composer bottom section, every tray variant. Style: existing tokens (border-line chips, surface bg, muted meta text, accent send).

```
 A. EMPTY TRAY (today's composer, unchanged silhouette)
 ┌──────────────────────────────────────────────────────┐
 │  Message Atlas…                                      │
 │  📎                                              ▲   │
 └──────────────────────────────────────────────────────┘

 B. UPLOADING (progress on chip; send DIMMED — hard gate)
 ┌──────────────────────────────────────────────────────┐
 │  ┌───────────────────┐                               │
 │  │ ◌ report.pdf      │ ✕    ← ✕ cancels the upload   │
 │  │ ▓▓▓▓▓▓░░░░ 4.8 MB │                               │
 │  └───────────────────┘                               │
 │  Message Atlas…                                      │
 │  📎                          (send dimmed)      ▲    │
 └──────────────────────────────────────────────────────┘

 C. READY (thumbnail chip for images; icon chip for docs;
    send lit — text or ≥1 ready chip)
 ┌──────────────────────────────────────────────────────┐
 │  ┌───────────────┐  ┌───────────────────┐            │
 │  │ [thumbnail]   │  │ 📄 report.pdf     │ ✕          │
 │  │ shot.png ✕    │  │ PDF · 4.8 MB      │            │
 │  └───────────────┘  └───────────────────┘            │
 │  What's wrong here?                                  │
 │  📎                                              ▲   │
 └──────────────────────────────────────────────────────┘

 D. REJECTED (reason inline; treated as absent by the gate)
 ┌──────────────────────────────────────────────────────┐
 │  ┌───────────────────────────────────┐               │
 │  │ 📄 report.docx                    │ ✕            │
 │  │ ▓ Not supported — export as PDF   │               │
 │  └───────────────────────────────────┘               │
 │  Message Atlas…                                      │
 │  📎                                              ▲   │
 └──────────────────────────────────────────────────────┘

 E. FAILED (network/5xx after start — Retry reuses the held File)
 ┌──────────────────────────────────────────────────────┐
 │  ┌───────────────────┐                               │
 │  │ 📄 report.pdf     │ ✕                             │
 │  │ ▓ Upload failed   │                               │
 │  │ [Retry]           │                               │
 │  └───────────────────┘                               │
 │  Message Atlas…                                      │
 │  📎                                              ▲   │
 └──────────────────────────────────────────────────────┘

 F. DROP OVERLAY (dragenter on chat surface; Files-only;
    dashed accent border inset over the message list)
 ╔══════════════════════════════════════════════════════╗
 ║                                                      ║
 ║              ⤓  Drop to attach                       ║
 ║        (transcript dims slightly beneath)            ║
 ║                                                      ║
 ╚══════════════════════════════════════════════════════╝
        composer visible beneath, tray live

 G. ATTACHMENT-ONLY SEND (empty text, ready chip → send lit)
 ┌──────────────────────────────────────────────────────┐
 │  ┌───────────────┐                                   │
 │  │ [thumbnail]   │ ✕                                 │
 │  │ shot.png      │                                   │
 │  └───────────────┘                                   │
 │                                                      │
 │  📎                                              ▲   │
 └──────────────────────────────────────────────────────┘
```

Transcript user bubbles, live and hydrated (identical components):

```
 H. IMAGE MESSAGE          I. DOC MESSAGE            J. MIXED
┌─────────────────────┐  ┌─────────────────────┐  ┌─────────────────────┐
│   Here's the shot   │  │   Review this       │  │   From the audit    │
│  ┌───────────────┐  │  │  ┌───────────────┐  │  │  ┌───────────────┐  │
│  │ [thumbnail]   │  │  │  │ 📄 report.pdf │  │  │  │ [thumbnail]   │  │
│  │  (loads from  │  │  │  │ PDF · 4.8 MB⤓ │  │  │  │  shot.png     │  │
│  │   cap URL)    │  │  │  └───────────────┘  │  │  │ └───────────────┘  │
│  └───────────────┘  │  │                     │  │  │  ┌───────────────┐  │
│                     │  │                     │  │  │  │ 📄 dump.sql   │  │
│        (right-      │  │                     │  │  │  │ SQL · 4.1 MB⤓ │  │
│         aligned     │  │                     │  │  │  └───────────────┘  │
│          pill)      │  │                     │  │  │                     │
└─────────────────────┘  └─────────────────────┘  └─────────────────────┘
   thumbnails/cards clickable: image → open in new tab; doc → download
 K. SETTINGS SHELL + NAV SLOT ("Storage", bottom of the nav; md+ layout)
 ┌ Workspace Settings ────────────────────────────────────────────────┐
 │ ┌──────────────────┐  ┌──────────────────────────────────────────┐ │
 │ │ Workspace        │  │  Storage                                 │ │
 │ │ Providers        │  │  Where workspace uploads are stored.     │ │
 │ │ Members & roles  │  │ ┌──────────────────────────────────────┐ │ │
 │ │ Integrations     │  │ │ pane card (variants K1–K9 below)     │ │ │
 │ │ MCP servers      │  │ └──────────────────────────────────────┘ │ │
 │ │ Skills           │  └──────────────────────────────────────────┘ │
 │ │ Tools            │                                               │
 │ │ Hooks            │                                               │
 │ │ API keys         │                                               │
 │ │ Notifications    │                                               │
 │ │▸Storage          │   ← route /settings/storage (last nav entry)  │
 │ └──────────────────┘                                               │
 └─────────────────────────────────────────────────────────────────────┘
   (<768px: the same tabs render as a horizontal scroll row above the card)
   The pane configures the workspace's blob storage backend; chat
   attachments are its first consumer — the identity is deliberately
   workspace-generic so future storage surfaces adopt it, not a rename.

 K1. DEFAULT — NEVER CONFIGURED (Local active, nothing to edit yet)
 ┌──────────────────────────────────────────────────────────────────┐
 │  Storage                                               [Local ●] │
 │  Where workspace uploads are stored.                             │
 │                                                                  │
 │  Storage driver                                                  │
 │  ┌─────────────┬───────────────────┐                             │
 │  │ ● Local     │   S3-compatible   │  ← Segmented control        │
 │  └─────────────┴───────────────────┘                             │
 │                                                                  │
 │  ┌────────────────────────────────────────────────────────────┐ │
 │  │ ⓘ Uploads are stored in this server's data directory.      │ │
 │  │   No configuration needed.                                 │ │
 │  └────────────────────────────────────────────────────────────┘ │
 └──────────────────────────────────────────────────────────────────┘
   No Save button in this state — Local is the passive default;
   the Segmented control is the only affordance.

 K2. S3-COMPATIBLE SELECTED — FRESH FORM (nothing stored yet)
 ┌──────────────────────────────────────────────────────────────────┐
 │  Storage                                                         │
 │                                                                  │
 │  Storage driver                                                  │
 │  ┌─────────────┬───────────────────┐                             │
 │  │   Local     │ ● S3-compatible   │                             │
 │  └─────────────┴───────────────────┘                             │
 │                                                                  │
 │  Endpoint URL*                                                   │
 │  [ https://                                                  ]   │
 │  Region*                                                         │
 │  [                                                           ]   │
 │  Bucket*                                                         │
 │  [                                                           ]   │
 │  Access key id*                                                  │
 │  [                                                           ]   │
 │  Secret access key*                                              │
 │  [                                                           ]   │
 │  ☐ Path style — for MinIO, R2, or on-prem endpoints              │
 │                                                                  │
 │  [ Test connection ]                      [ Save configuration ] │
 │  ↑ Test probes with the form's current values, saves nothing     │
 │  ↑ Save runs the probe first: success persists (K7),             │
 │    failure shows K6 — Save disabled until * fields are non-empty │
 └──────────────────────────────────────────────────────────────────┘

 K3. STORED S3 — REOPENED (secret masked, keep-stored sentinel)
 ┌──────────────────────────────────────────────────────────────────┐
 │  Storage                                                 [S3 ●]  │
 │  …fields as in K2, filled…                                       │
 │  Endpoint URL*                                                   │
 │  [ https://s3.us-east-1.amazonaws.com                       ]   │
 │  Bucket*                                                         │
 │  [ acme-attachments                                         ]   │
 │  Secret access key*                                              │
 │  [ ••••••••••••••••  Stored — leave as-is to keep           ]   │
 │  ☑ Path style — for MinIO, R2, or on-prem endpoints              │
 │                                                                  │
 │  [ Test connection ]                      [ Save configuration ] │
 │  ↑ secret is write-only: saving without editing keeps the        │
 │    stored value; typing replaces it                              │
 └──────────────────────────────────────────────────────────────────┘

 K4. TEST CONNECTION — PROBING (busy; both buttons locked)
 ┌──────────────────────────────────────────────────────────────────┐
 │  …fields as in K3…                                               │
 │  [ ◌ Testing…                     ]       [ Save configuration ] │
 │    (disabled)                             (disabled)             │
 └──────────────────────────────────────────────────────────────────┘

 K5. TEST CONNECTION — SUCCESS (transient inline banner)
 ┌──────────────────────────────────────────────────────────────────┐
 │  …fields as in K3…                                              │
 │  ┌────────────────────────────────────────────────────────────┐ │
 │  │ ✓ Bucket reachable — credentials verified                  │ │
 │  └────────────────────────────────────────────────────────────┘ │
 │  [ Test connection ]                      [ Save configuration ] │
 │  ↑ banner clears on the next field edit (transient, matching     │
 │    the Providers pane's verify affordance)                       │
 └──────────────────────────────────────────────────────────────────┘

 K6. SAVE — PROBE FAILED (inline red reason; nothing changed)
 ┌──────────────────────────────────────────────────────────────────┐
 │  Storage                                               [Local ●] │
 │  ← active-backend badge still Local: a failed save changes       │
 │     nothing                                                      │
 │  …S3 fields as in K2/K3…                                        │
 │  ┌────────────────────────────────────────────────────────────┐ │
 │  │ ✕ Couldn't reach the bucket: The security token included   │ │
 │  │   in the request is invalid.   (probe reason, verbatim)    │ │
 │  └────────────────────────────────────────────────────────────┘ │
 │  [ Test connection ]                      [ Save configuration ] │
 │  ↑ Save stays enabled — fix the field and retry; the error       │
 │    clears on the next field edit                                 │
 └──────────────────────────────────────────────────────────────────┘

 K7. SAVE SUCCESS (toast; badge flips; new uploads land in S3)
 ┌──────────────────────────────────────────────────────────────────┐
 │  Storage                                                 [S3 ●]  │
 │  …S3 fields as in K3…                                          │
 │  [ Test connection ]                      [ Save configuration ] │
 └──────────────────────────────────────────────────────────────────┘
   ┌──────────────────────────────┐
   │ ✓ Storage configuration saved │   ← toast, auto-dismiss
   └──────────────────────────────┘
   Existing attachments stay readable — no migration, no visible
   change anywhere else.

 K8. SWITCH BACK TO LOCAL (stored S3 → Local selected)
 ┌──────────────────────────────────────────────────────────────────┐
 │  Storage                                                 [S3 ●]  │
 │  Storage driver                                                  │
 │  ┌─────────────┬───────────────────┐                             │
 │  │ ● Local     │   S3-compatible   │                             │
 │  └─────────────┴───────────────────┘                             │
 │                                                                  │
 │  ┌────────────────────────────────────────────────────────────┐ │
 │  │ ⓘ New attachments will be stored locally. The 214          │ │
 │  │   attachments already in bucket storage stay readable.      │ │
 │  └────────────────────────────────────────────────────────────┘ │
 │                                                                  │
 │                                           [ Save configuration ] │
 │  ↑ no probe needed for Local; Save persists the switch           │
 └──────────────────────────────────────────────────────────────────┘

 K9. MEMBER VIEW (read-only; actions disabled, badge visible)
 ┌──────────────────────────────────────────────────────────────────┐
 │  Storage                                                 [S3 ●]  │
 │  …all fields rendered disabled (K3 values, masked secret)…       │
 │  [ Test connection ]                      [ Save configuration ] │
 │    (disabled)                               (disabled)           │
 └──────────────────────────────────────────────────────────────────┘
```

## Risks / Trade-offs

- [Pointer note leaks into visible text] → the note is its own marked block (D8); projection and `extractAgenticText` skip it; regression tests on both paths.
- [Summarizer flattens image blocks at compaction] → accepted: compaction already reduces history to text; the placeholder pointers survive as text and the read-only mount keeps re-access.
- [Agent without files tools can't re-open dropped images] → documented caveat; placeholder still names the file so the agent can say so.
- [Provider rejects images on text-only models] → accepted gap (no capability flags in the model catalog); surfaces as a run error entry; future model-catalog change.
- [Capability URLs are bearer tokens without revocation] → same posture as avatars today; revocation is a future storage change, not an attachment change.
- [Progress re-renders flood React] → chips are composer-local state (D11); never touch the global store per tick.
- [Drop handler without preventDefault navigates away] → called out in tasks as its own test case.
- [~100-page PDF sniffing is approximate] → page count derived from PDF structure at upload best-effort; hard cap is bytes; provider limits are the backstop.
- [Proxied S3 serving costs onclaw bandwidth] → accepted for v1; presigned direct mode is the future lever, deliberately deferred.
- [S3 latency slows upload/hydration paths] → resolver caches driver clients; blob ops are already async request paths; no change to turn streaming.
- [Config flip orphans old blobs] → attachment rows record their backend (D16); reads consult the row, not the workspace's current setting.
- [Run-scoped materialization leaves temp files on crash] → teardown best-effort + OS temp reaping; drop-lane files are non-sensitive-by-upload but the sweep task covers restart cleanup.

## Migration Plan

1. Migrations up (attachments table; workspace_attachment_storage config) before deploying the binary; standard down migrations provided.
2. Rollout is additive: string-only `/v1` inputs unchanged, existing callers untouched; the composer's clip toast is replaced by the tray when the web build ships; workspaces without storage configuration behave exactly as plain local storage.
3. Rollback: revert binary; attachments tables and stored bytes are inert; no data migration back.

## Open Questions

None blocking. Sub-decisions delegated to implementation tasks: exact jail mount path layout (D8/D17), PDF page-count extraction method, whether `detail` maps to gemini/claude image options or is dropped (drop is acceptable), exact shape of the attachments-row backend snapshot (D16 — column vs. join).
