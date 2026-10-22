/**
 * Clipboard writer for the copy-message action (task 8.1, design D12).
 * `navigator.clipboard.writeText` is the primary path and is invoked
 * synchronously on the click gesture — user activation only covers the
 * handler's first ticks, so nothing may await before it. When the webview
 * refuses (permission denied, unfocused document, insecure context), D12
 * wants the Wails clipboard binding as the fallback: `bindingCopy` routes
 * the write through the ClipboardService backend, whose OS write does not
 * depend on the webview's permission surface. When the backend refuses too,
 * the caller surfaces the danger toast.
 */
import * as clipboardBindings from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/clipboardservice";

/**
 * How long the binding fallback may take before it counts as refused. A bound
 * call that never settles — a host whose clipboard has no run loop to dispatch
 * to — must still end in the refusal toast, never an inert button.
 */
export const BINDING_COPY_TIMEOUT_MS = 1500;

/** The fallback writer: the bound ClipboardService by default, injectable so
 * tests can drive refusals and stalls without mocking the binding module. */
export type ClipboardSetter = (text: string) => Promise<void>;

export interface CopyTextOptions {
  setText?: ClipboardSetter;
  timeoutMs?: number;
}

/**
 * The Wails clipboard fallback: writes through the bound ClipboardService
 * and reports whether the OS write succeeded within the timeout.
 */
async function bindingCopy(
  text: string,
  setText: ClipboardSetter,
  timeoutMs: number
): Promise<boolean> {
  let timer: ReturnType<typeof setTimeout> | undefined;

  const timeout = new Promise<false>(
    (resolve: (value: false) => void): void => {
      timer = setTimeout((): void => {
        resolve(false);
      }, timeoutMs);
    }
  );

  try {
    return await Promise.race([setText(text).then((): true => true), timeout]);
  } catch {
    return false;
  } finally {
    clearTimeout(timer);
  }
}

/**
 * Copies `text` to the clipboard. Resolves `true` on success, `false` when
 * the primary write is refused and the fallback fails too — the caller
 * announces the refusal (danger toast), never a silent no-op.
 */
export async function copyText(
  text: string,
  options: CopyTextOptions = {}
): Promise<boolean> {
  const setText: ClipboardSetter =
    options.setText ??
    ((t: string): Promise<void> => clipboardBindings.SetText(t));

  try {
    await navigator.clipboard.writeText(text);

    return true;
  } catch {
    return bindingCopy(
      text,
      setText,
      options.timeoutMs ?? BINDING_COPY_TIMEOUT_MS
    );
  }
}
