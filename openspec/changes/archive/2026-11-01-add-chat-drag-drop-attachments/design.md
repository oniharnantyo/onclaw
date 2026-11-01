# Design

## Context

The attachment pipeline already exists end-to-end behind one entry point: `aui.composer.addAttachment(file)` → `attachmentAdapter.add` (`frontend/src/features/chat/runtime/useChatRuntime.ts`) → `useAttachmentStore.addFiles` → chip → upload → capability key. The paperclip and paste both use it; drag has no handler anywhere in `frontend/src`. The spec (`chat-attachments` "Attachment selection and limits") already requires drag-and-drop; the gap is wiring plus the affordance and navigation guard.

Relevant facts verified against the pinned stack:

- `@assistant-ui/react` 0.15.30 exports `ComposerPrimitive.AttachmentDropzone` (`src/primitives/composer/ComposerAttachmentDropzone.tsx`): capture-phase dragenter/over/leave/drop, gates on `thread.capabilities.attachments` (for the external-store runtime this is `!!adapters.attachments` — always true here, `external-store-thread-runtime-core.ts:575`), latches `data-dragging="true"` while a file drag is inside, preventDefaults file drags inside its own subtree, and on drop maps `Array.from(dataTransfer.files)` to parallel `aui.composer.addAttachment(file)` calls, catching per-file add errors.
- Wails v3 beta.29 has a parallel native drag-drop layer (`WebviewDrag` overlay NSView on macOS, IDropTarget on Windows) emitting `WindowFilesDropped` with filenames as strings. Buddies never enables it (`grep DragAndDrop` clean in `main.go`/`internal/`), so the webview itself is the dragging destination and standard HTML5 `dataTransfer.files` reaches the DOM. The shell env/layout invariants (`AGENTS.md`) are untouched — no Go code in this change.

## Goals / Non-Goals

**Goals:**
- Dragging files anywhere over the chat column attaches them through the existing lanes, with per-file independence.
- A drag-over affordance driven by the library's own latch, themed for light/dark, motion-safe.
- The app can never navigate to a dropped file, on any surface, in any desktop webview.

**Non-Goals:**
- No Wails native drag-drop adoption or Go-side file ingestion (string paths would bypass the capability-URL store).
- No folder-drop traversal (`dataTransfer.items` walk); a folder drop contributes an empty `files` list and is a no-op.
- No changes to lane caps, chip tray, upload gating, or degradation warnings — they apply unchanged.
- No changes to drops on the right bar (out of the column; the guard makes them inert).

## Decisions

**D1 — Carrier: the library dropzone, not bespoke handlers.** `ComposerPrimitive.AttachmentDropzone` wraps the chat column in `frontend/src/features/chat/index.tsx` (the `relative flex min-w-0 flex-1 flex-col bg-bg` div inside `ChatRuntimeProvider`, so `useAui` resolves; it spans MobileSessionBar/ChatHeader/ChatThread/ChatComposer and excludes `RightBar`). A bespoke handler set would reimplement the dragenter/over/leave containment latch, the capabilities gate, and the navigation-trap preventDefault the primitive already handles and tests. Alternatives: composer-bubble-only (precise but a small target — users aim mid-screen first) and app-root wrap (drops on Settings/Artifacts would attach — surprising). The column is the Claude/ChatGPT muscle-memory target.

**D2 — Drop entry: the existing adapter, untouched.** The primitive's `addAttachment` calls land in `attachmentAdapter.add` exactly like the picker. Per-file independence is free (each call adds one file; one reject mints a rejected chip without touching siblings), and send stays blocked while any dropped file uploads via the existing chip-state gate. No adapter, store, or backend edits.

**D3 — Affordance: the `data-dragging` latch, zero new state.** The dropzone element gets the Tailwind `group` class; the column (already `relative`) hosts a pointer-events-none overlay pill shown via `group-data-[dragging=true]:` utilities, and `ChatComposer`'s bubble picks up a ring the same way. Both themes come from tokens; the effect is opacity/ring only, so the reduced-motion toggle needs no special case. Alternative (React state mirroring drag events) would duplicate the latch the library owns.

**D4 — Navigation guard: window-level, Files-gated, at the shell.** A `useEffect` at the app shell adds `dragover` + `drop` listeners that `preventDefault` when `event.dataTransfer?.types.includes("Files")`. This is what makes drops outside the column inert instead of WebView2/Chromium navigating the window to the file; it also guarantees the spec's "app stays on the chat view" for drops on any surface. It composes with the dropzone (preventDefault suppresses the default action, not other listeners). Gate: `Files` only — the app has no internal HTML5 drag sources today (verified), and a future one would only collide if it carried files. Shell-level (not ChatView-level) so the guarantee holds even where the chat column is not mounted. `dragover` must be prevented at window level for the browser to deliver `drop` at all outside the dropzone's subtree.

**D4a — DropEffect repair: the guard is the last bubble-phase writer (live-smoke fix).** First live drag surfaced a second killer the static explore missed: the Wails JS runtime (`@wailsio/runtime` window.js) installs its own documentElement dragenter/over/leave/drop listeners, and with the shell's `enableFileDrop=false` flag — injected on every navigation by the Go side because `WebviewWindowOptions.EnableFileDrop` is a plain bool that buddies leaves at its `false` zero value (`webview_window_darwin.go:1460`) — its dragover handler forces `dataTransfer.dropEffect = "none"` on every file drag. A dragover that ends with dropEffect "none" never delivers a `drop` event, so the affordance latched but the drop landed nowhere. Flipping `EnableFileDrop: true` is rejected: on macOS it activates the native drag layer (the `WindowFilesDropped` filename-string path design D4 explicitly rejects). The repair rides the existing guard: bubble order is documentElement → document → window, so the guard's dragover handler re-claims the drag with `dropEffect = "copy"` after Wails has written "none" and the drop fires; off-column drops stay inert via the guard's `preventDefault`, and the copy cursor app-wide during a file drag is accepted (the column's pill/ring remains the primary signal). Wails' `data-file-drop-target` attribute is dead weight here — its drop-target logic is unreachable when the flag is `false`.

**D5 — Testing: synthetic drag events on the existing harness.** jsdom has no real DataTransfer, so tests dispatch drag events whose `dataTransfer` is a stub `{ types: ["Files"], files: [File] }` — the handlers only read those two fields. Cases: one file → one chip; five files → five chips; a cap-violating file in a multi-drop rejects alone; non-Files drag → no affordance, no chip; guard: a drop with the stub outside the column leaves the document navigable-nowhere (asserts no throw and no chip) — the window guard itself is also unit-covered by asserting `preventDefault` calls on the stub events.

## Risks / Trade-offs

- [Linux WebKitGTK file drags from file managers are historically flaky] → live smoke per platform (macOS first, the daily-driver; then Windows/Linux). Fallback contingency if a platform never delivers File objects: listen to Wails `WindowFilesDropped` (filenames) plus a new read-file binding — explicitly out of scope until a smoke proves it needed.
- [Window guard vs a future internal file-carrying HTML5 drag] → the guard prevents default on every Files dragover; an internal sortable that drags files is not a pattern this app has. Noted for whoever adds one.
- [macOS spurious dragleave] → WebKit fires dragleave with `relatedTarget=null` mid-hover for OS file drags (Wails' own listeners guard against the same); the primitive's latch clears and re-sets on the next dragover, so worst case the pill/ring flicker once — cosmetic, drop unaffected.
- [Dropping while a turn runs or no agent is selected] → same semantics as the picker (chips mint; send gated by `activeAgentID`/uploading); queue behavior unchanged. No special casing.
- [Latch stuck if the drag ends outside the window] → the primitive resets the latch when disabled and on drop; the dragleave containment check covers normal exits. Worst case a stale ring until the next drag — cosmetic, not behavioral.

## Migration Plan

None — no data, schema, API, or bindings changes. Rollback is reverting the frontend commit.

## Open Questions

None.
