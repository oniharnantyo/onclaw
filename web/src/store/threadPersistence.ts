// Thread persistence: the store's `db` is re-seeded on every page load, which
// used to orphan live chat sessions — the server-side `sess_<uuid>` address
// died with the page, so transcript hydration never fired and the next turn
// birthed a fresh session. This module persists the threads slice (session
// ids, `sess` bindings, `resp` chains, messages) per workspace so a reload
// reattaches to the same server session.
//
// Storage shape: `onclaw.threads.v1` → { [tenantId]: { [chatId]: ThreadState } }

const KEY = 'onclaw.threads.v1';

// Local storage is a cache, not an archive — cap what we write.
const MAX_SESSIONS_PER_THREAD = 50;
const MAX_MESSAGES_PER_SESSION = 200;

export type PersistedThreadState = { active: string | null; list: any[] };

export function loadPersistedThreads(tenantId: string): Record<string, PersistedThreadState> {
  try {
    const all = JSON.parse(localStorage.getItem(KEY) || '{}');
    const perTenant = all && typeof all === 'object' ? all[tenantId] : null;
    if (!perTenant || typeof perTenant !== 'object') return {};
    const out: Record<string, PersistedThreadState> = {};
    for (const [chatId, th] of Object.entries(perTenant)) {
      if (!th || typeof th !== 'object' || Array.isArray(th)) continue;
      const state = th as any;
      if (!Array.isArray(state.list)) continue;
      out[chatId] = { active: state.active ?? null, list: state.list };
    }
    return out;
  } catch {
    return {};
  }
}

export function persistAllThreads(db: Record<string, any> | undefined) {
  if (!db) return;
  const out: Record<string, Record<string, PersistedThreadState>> = {};
  for (const [tenantId, t] of Object.entries(db)) {
    const threads = (t as any)?.threads;
    if (!threads || typeof threads !== 'object') continue;
    const perChat: Record<string, PersistedThreadState> = {};
    for (const [chatId, th] of Object.entries(threads)) {
      // Legacy threads were bare message arrays — nothing to bind, skip.
      if (!th || typeof th !== 'object' || Array.isArray(th)) continue;
      const state = th as any;
      if (!Array.isArray(state.list)) continue;
      perChat[chatId] = {
        active: state.active ?? null,
        list: state.list.slice(-MAX_SESSIONS_PER_THREAD).map((s: any) => ({
          ...s,
          messages: Array.isArray(s.messages) ? s.messages.slice(-MAX_MESSAGES_PER_SESSION) : [],
        })),
      };
    }
    if (Object.keys(perChat).length) out[tenantId] = perChat;
  }
  try {
    localStorage.setItem(KEY, JSON.stringify(out));
  } catch {
    // quota exceeded / private mode — persistence is best-effort
  }
}

/** Merges persisted threads over a (seeded or blank) tenant's threads. Persisted wins per chat — including "user cleared it". */
export function overlayPersistedThreads(tenantId: string, tenant: any): any {
  if (!tenant || typeof tenant !== 'object') return tenant;
  const persisted = loadPersistedThreads(tenantId);
  if (!Object.keys(persisted).length) return tenant;
  return { ...tenant, threads: { ...(tenant.threads || {}), ...persisted } };
}
