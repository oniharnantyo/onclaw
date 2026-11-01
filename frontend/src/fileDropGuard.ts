/**
 * Window-level navigation guard for file drags (design D4, task 2): an
 * unprevented file `drop` outside the chat column's dropzone makes the
 * WebView (Chromium/WebView2, WebKit) navigate the whole window to the
 * dropped file. The chat column guards its own surface, but drops on any
 * other surface (right bar, settings, agents — including where the chat view
 * is not mounted) must be inert too, so the guard lives at the app shell.
 *
 * The guard also repairs the drop effect on `dragover` (the fix for the
 * "affordance shows but the drop never lands" symptom): the Wails JS runtime
 * installs its own documentElement dragover listener that, with the shell's
 * `enableFileDrop=false` flag injected on every navigation
 * (webview_window_darwin.go:1460 — buddies never sets EnableFileDrop),
 * forces `dataTransfer.dropEffect = "none"` on every file drag — and a
 * dragover that ends with dropEffect "none" never delivers a `drop` event.
 * This window-level listener is the LAST bubble-phase writer (bubble order:
 * documentElement → document → window), so re-claiming the drag as "copy"
 * here lets the drop fire; drops outside the chat column stay inert via the
 * preventDefault, and the shell's EnableFileDrop stays false — the native
 * macOS drag layer (WindowFilesDropped filename strings) is never adopted.
 *
 * Gated on `Files` only: the app has no internal HTML5 drag sources carrying
 * files today (design D4, verified), while non-Files drags (e.g. selected
 * text) must stay untouched so text can still be dropped into the composer.
 */
/**
 * Installs the guard; the returned function removes both listeners (the
 * shell calls it on unmount).
 */
export function subscribeFileDropGuard(): () => void {
  // `dragover` must be prevented for the browser to deliver `drop` at all
  // outside a guarded surface, and must end with a non-"none" dropEffect for
  // the drop to actually fire (see the module comment): claim the drag.
  const claimDrag = (event: DragEvent): void => {
    if (event.dataTransfer?.types.includes("Files")) {
      event.preventDefault();
      event.dataTransfer.dropEffect = "copy";
    }
  };

  // A `drop` needs nothing but the preventDefault: whatever surface it lands
  // on outside the chat column is inert, and the navigation trap stays shut.
  const preventNavigation = (event: DragEvent): void => {
    if (event.dataTransfer?.types.includes("Files")) {
      event.preventDefault();
    }
  };

  window.addEventListener("dragover", claimDrag);
  window.addEventListener("drop", preventNavigation);

  return (): void => {
    window.removeEventListener("dragover", claimDrag);
    window.removeEventListener("drop", preventNavigation);
  };
}
