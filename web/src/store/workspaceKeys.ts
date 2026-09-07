// Per-workspace chat API key slots (D4). Each workspace's auto-exchanged key
// lives in its own localStorage slot `onclaw.api_key.<workspaceId>`; logout
// purges every slot at once. The old global `onclaw.api_key` slot is
// superseded by this scheme and is also dropped on purge.

const KEY_PREFIX = 'onclaw.api_key.';
const LEGACY_GLOBAL_KEY = 'onclaw.api_key';

const slotFor = (workspaceId: string) => KEY_PREFIX + workspaceId;

export const getWorkspaceKey = (workspaceId: string): string | null => {
  try { return localStorage.getItem(slotFor(workspaceId)); }
  catch { return null; }
};

export const setWorkspaceKey = (workspaceId: string, key: string): void => {
  try { localStorage.setItem(slotFor(workspaceId), key); }
  catch { /* storage unavailable (private mode) — chat will re-exchange */ }
};

export const clearWorkspaceKey = (workspaceId: string): void => {
  try { localStorage.removeItem(slotFor(workspaceId)); }
  catch { /* nothing to clear */ }
};

// Drop every per-workspace key slot (and the superseded global slot).
export const purgeWorkspaceKeys = (): void => {
  try {
    const doomed: string[] = [];
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i);
      if (k === LEGACY_GLOBAL_KEY || (k && k.startsWith(KEY_PREFIX))) doomed.push(k);
    }
    doomed.forEach((k) => localStorage.removeItem(k));
  } catch { /* nothing to purge */ }
};
