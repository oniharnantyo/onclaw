# Proposal: add-chat-attachments

## Why

Chat is text-only today: the composer's clip button is a placeholder toast, `/v1` rejects any non-text input part, and agents cannot see a user's screenshot or document. The stack below is already multimodal-ready (eino `AgenticMessage` carries `UserInputImage`/`UserInputFile` blocks; all three agentic model backends convert them), so chat attachments are the highest-leverage gap: "look at this error" with a pasted screenshot is the canonical agent interaction, and it is currently impossible.

## What Changes

- **Attachment upload surface (new):** `POST /api/v1/workspaces/{ws}/attachments` (multipart, JWT, workspace-scoped) with magic-byte content sniffing, per-type size caps, and capability-key storage (avatar pattern). Returns `{id, name, mime, size, url}` where `url` is a capability URL that doubles as the wire token.
- **Pluggable blob storage (new):** attachment blobs go through the existing `storage.Storage` port — a new S3-compatible driver registers alongside local storage, and a per-workspace resolver picks the backend from workspace configuration (local is the zero-config default). Capability URLs stay onclaw-proxied regardless of driver, so transcripts and wire tokens behave identically on both backends.
- **`/v1` accepts multimodal input (convention-pure):** `input_image {image_url, detail?}` and `input_file {file_url, filename}` content parts, where `image_url`/`file_url` are either data URLs (inline; server demotes bytes to storage on arrival) or onclaw capability URLs (resolved locally, no HTTP self-fetch). `file_data` accepted as an inline alias; `file_id` rejected `invalid_param` (no OpenAI files backend). Remote third-party `file_url` fetch is **out of scope v1** (SSRF surface).
- **Multimodal turn execution:** `ExecRequest` grows an optional `Attachments` payload; the runner builds the user `AgenticMessage` with base64-carrying blocks for the current turn (replacing `FlattenInput`'s string-only contract at the v1 handler seam).
- **Bytes never persist:** the session adapter demotes base64 blocks to URL-bearing reference blocks when writing session events; the model-time middleware re-expands **current-turn** blocks only and replaces older URL-only blocks with pointer placeholders (current-turn-only context policy). Read-only fs mount of the attachments dir gives agents re-access via files tools.
- **Three-lane format matrix:** inline lane (images png/jpeg/webp/gif ≤ 5 MB, PDF ≤ 20 MB / ~100 pages, text-ish ≤ 200 KB fenced-inline), drop lane (text-like code/config files ≤ 50 MB stored + mounted, model gets a pointer note), reject lane (docx/xlsx/pptx/zip/exe/unknown → 400 with "export as PDF" hint).
- **Web chat composer + transcript:** attachment chip tray in the composer (uploading/ready/rejected/failed states, hard send gate, retry), clipboard paste and chat-surface drag-and-drop as additional entry paths, attachment chips on user messages (live optimistic + hydrated history alike).
- **assistant-ui stays a text passthrough** — chips are side-channel state in the composer/store; the chosen chip shape deliberately mirrors aui's `CompleteAttachment` for future compatibility.

- **Workspace storage settings (new):** a workspace **Storage** pane (last entry in the settings nav) configures the workspace's blob storage backend — chat attachments are its first consumer, and the config is deliberately workspace-generic for future storage surfaces: driver choice (Local default / S3-compatible) with endpoint, region, bucket, access key, secret (masked, keep-stored), and path-style toggle, plus a connectivity test that gates saving.

## Capabilities

### New Capabilities

- `workspace-attachments`: attachment upload, validation (sniff/caps/allowlist), storage through pluggable blob drivers (local default, S3-compatible), attachment records, capability-URL serving, per-workspace storage configuration with fallback, and the three-lane format classification.

### Modified Capabilities

- `openresponses`: input content parts — accept `input_image`/`input_file` (URL or inline forms) instead of erroring; attachments resolve against the API key's workspace; compact turns reject attachments.
- `agent-runtime`: multimodal user turn construction, base64-at-model-boundary delivery, persist-time demotion to references, current-turn-only re-expansion policy with placeholder pointers, run-scoped drop-lane materialization, history hydration emitting attachment metadata.
- `web-app/chat`: composer attachment tray (states, send gate, paste, drag-drop) and user-message attachment rendering for live and hydrated transcripts.
- `web-app/settings`: new Storage pane at the bottom of the settings navigation (driver selection, S3 fields with masked secret, connectivity test) configuring the workspace's blob storage backend.

## Impact

- **Backend:** new `attachments` store port + postgres adapter + migration (attachment records, incl. which backend holds each blob); workspace storage config migration (`workspace_storage`: driver + S3 fields + encrypted secret); new upload handler + route (workspace-scoped, JWT); workspace storage resolver (per-workspace driver instances, local fallback); S3-compatible storage driver registering on the existing storage port (aws-sdk-go-v2, new dependency); `internal/openresponses` (dto `FlattenInput` → parts-aware, handler resolution); `internal/agents` (`ExecRequest`, runner user-message construction, session adapter persist demotion, new model-time middleware, run-scoped drop-lane materialization, history hydration); history hydration; `scripts/smoke.sh` coverage.
- **Web:** `Composer.tsx` (tray, paste, drop, gate), new attachment chip + upload client, `UserMessage.tsx` (attachment rendering), `runtime.tsx`/store (send path carries chips, hydrated rendering), `api.ts` (upload + storage-config calls), settings Storage pane.
- **No wire breaking changes:** string-only `/v1` inputs remain valid; existing callers (cron, channels, compact) untouched; compact + attachments → 400. Instance-level storage (avatars, skills archives) is untouched by workspace storage configuration.
- **Explicit non-goals (v1):** remote `file_url` fetching; docx/xlsx extraction; channel/team composer attachments; cron attachments; model vision-capability gating (provider errors surface as run errors); attachment orphan GC sweep (orphans accepted, sweep deferred); presigned direct-to-S3 URLs (capability serving stays proxied); migrating already-stored blobs when the workspace backend changes (rows record their backend, so existing attachments stay readable).
