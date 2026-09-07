import { api } from "./api";

// Per-workspace module-level cache over the tools catalog (design D7). The
// catalog is instance-static, so the only invalidation is a workspace switch.
// On failure the cache resets so a later `ensure` retries.
let cacheWorkspace: string | null = null;
let names: Record<string, string> = {};
let inflight: Promise<void> | null = null;

export const toolCatalog = {
  /** Fetch (once per workspace) the display-name map. Resolves when settled. */
  ensure(wsId: string | null | undefined): Promise<void> {
    if (!wsId || typeof wsId !== 'string') return Promise.resolve();
    if (cacheWorkspace === wsId) {
      if (inflight) return inflight;
      if (Object.keys(names).length > 0) return Promise.resolve();
    } else {
      cacheWorkspace = wsId;
      names = {};
    }
    inflight = api.tools
      .list(wsId)
      .then((res: any) => {
        const next: Record<string, string> = {};
        for (const t of res.tools || []) next[t.key] = t.display_name || t.key;
        names = next;
      })
      .catch(() => {
        // fall back to raw ids; retry on the next ensure call
        cacheWorkspace = null;
      })
      .finally(() => {
        inflight = null;
      });
    return inflight;
  },
  /** Display name for a tool id, or null when the catalog has no entry. */
  displayName(id: string): string | null {
    return names[id] ?? null;
  },
};
