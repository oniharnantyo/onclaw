# Proposal

## Why

The `chat-attachments` spec already requires drag-and-drop ("The composer SHALL accept multiple files through an attach button, drag-and-drop onto the chat view, and pasted images"), but no drop handler exists anywhere in the frontend — dragging a file onto the chat does nothing (or, outside a guarded surface, risks navigating the webview to the file). This closes that spec–implementation gap with the primitive assistant-ui already ships.

## What Changes

- Wrap the chat column (`ChatView`'s inner transcript + composer column, not the right bar) in `ComposerPrimitive.AttachmentDropzone` (assistant-ui 0.15.30, already installed). Dropped files call `aui.composer.addAttachment(file)` — the same entry the paperclip and paste use — so lane caps, chip tray, upload gating, capability and connector-gap warnings all apply unchanged.
- Multiple dropped files attach independently: each is judged by the lane caps, mints its own chip, and rejects individually.
- A drag-over affordance: the dropzone's `data-dragging` state drives a highlight on the composer bubble and a centered "Drop files to attach" pill over the column. Works in both themes; respects the reduced-motion toggle.
- A window-level guard: `dragover`/`drop` events carrying `Files` are `preventDefault`-ed at the window so a drop landing outside the column (e.g. on the right bar) can never navigate the webview to the file — a desktop-shell crash, not a benign reload. The guard composes with the dropzone (preventDefault does not suppress its handlers). Because the Wails JS runtime's own dragover listener forces `dropEffect = "none"` onto every file drag (the shell injects `enableFileDrop=false`, the `EnableFileDrop` option's Go zero value), the guard — the last bubble-phase writer — also re-claims the drag with `dropEffect = "copy"`; without that, no `drop` event ever fires and dropping does nothing. `EnableFileDrop` stays off: enabling it activates the native macOS drag layer (filename strings) this change rejects.
- No backend, adapter, or Wails changes: buddies deliberately does NOT adopt Wails' native drag-drop layer (it delivers filenames as strings and its overlay would swallow the HTML5 events the dropzone needs); the webview's own HTML5 file drag is the carrier.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `chat-attachments`: adds a requirement specifying the drag-and-drop surface (whole chat column, same lanes as the picker), the drag-over affordance, and the no-navigation guarantee; the existing acceptance requirement already covers the gesture itself.

## Impact

- Frontend only: `frontend/src/features/chat/index.tsx` (dropzone wrap + overlay), `ChatComposer.tsx` (bubble highlight styling), `frontend/src/fileDropGuard.ts` + `App.tsx` (shell-level guard wiring), and the test suites (synthetic drag events with stubbed `dataTransfer`).
- Depends on `@assistant-ui/react` 0.15.30 `ComposerPrimitive.AttachmentDropzone` — verified present and exported; no new dependency.
- Zero Go code, zero migrations, zero bindings regeneration.
- Live smoke owed per platform: macOS first (WKWebView HTML5 file drop), then Windows (WebView2) and Linux (WebKitGTK file drags are historically flaky).
