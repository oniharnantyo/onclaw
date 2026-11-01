# Proposal

## Why

Generated artifacts (the create_document products under the agent's workspace `artifacts/`) can only be viewed inside the app — there is no way to export a finished document to a location of the user's choosing (e.g. their client folder for a deed draft). The card already fetches the bytes through a jailed bound read; exporting needs a destination picker and a copy.

## What Changes

- A **Save As…** control on the `create_document` tool card for every generated format (PDF, DOCX, PPTX). It opens the host's native save dialog seeded with the artifact's file name, and on confirmation the backend copies the artifact from the same jailed resolution the view read uses (workspace `artifacts/` first, legacy workspace root fallback) to the chosen destination.
- The bound call returns the user-chosen destination path; cancellation is a silent no-op. In server mode (no native dialog) the call reports unavailable and the card hides the control via a new `nativeDialogs` capability flag on AppInfo metadata.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `chat-tool-cards`: adds an Artifact export requirement — the generated document's card SHALL offer Save As, sourced through the same jailed name rules as the view read, surfaced through the host's native save dialog, and degraded to unavailable where the host has no dialog service.

## Impact

- `internal/services/chat_documents.go` (new bound method mirroring `GeneratedDocument`'s validation), `internal/services/appinfo.go` (capability field), the desktop/server hosts' AppInfo wiring, and frontend `CreateDocumentTool.tsx` (the control + toast handling) with tests. Wails v3 beta.29 provides the save dialog with a built-in server-mode no-op.
- Non-goals: Save As on turn-artifact chips or knowledge/workspace files (follow-up if wanted); moving artifacts out of the workspace (export copies, never moves); any preset or generation-path change.
