/**
 * Toast store (task 2.1): onclaw-v2 keeps toasts on its app-wide zustand
 * store (`ui.toasts` plus a `toast(text, kind)` action with a 3.2s
 * auto-dismiss timer); buddies has no such store, so the primitive lives
 * here as a tiny module store that <Toasts> subscribes to via
 * useSyncExternalStore. Public API matches onclaw's call sites:
 * toast(text, kind). The network-degradation suppression stays app-side —
 * it reads onclaw's connection store, which buddies does not share.
 */

export type ToastKind = "info" | "danger";

export type Toast = {
  id: string;
  text: string;
  kind: ToastKind;
};

/** onclaw-v2 parity: toasts live 3.2s, then dismiss themselves. */
const TOAST_DURATION_MS = 3200;

let toasts: Toast[] = [];

const listeners = new Set<() => void>();

let nextId = 0;

function emit(): void {
  listeners.forEach((listener: () => void) => listener());
}

/** Snapshot getter for useSyncExternalStore (array replaced per mutation). */
export function getToasts(): Toast[] {
  return toasts;
}

export function subscribeToasts(listener: () => void): () => void {
  listeners.add(listener);

  return (): void => {
    listeners.delete(listener);
  };
}

export function dismissToast(id: string): void {
  const next = toasts.filter((t: Toast) => t.id !== id);

  if (next.length === toasts.length) return;

  toasts = next;
  emit();
}

export function toast(text: string, kind: ToastKind = "info"): string {
  nextId += 1;

  const id = `t_${nextId}`;

  toasts = [...toasts, { id, text, kind }];
  emit();
  setTimeout((): void => {
    dismissToast(id);
  }, TOAST_DURATION_MS);

  return id;
}
