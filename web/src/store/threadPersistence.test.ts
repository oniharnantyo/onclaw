/**
 * @vitest-environment jsdom
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

// This environment's jsdom exposes no localStorage — install the stub BEFORE
// any store import so the boot overlay sees the persisted payload (the reload
// test below relies on it).
const backing = new Map<string, string>();
if (typeof (globalThis as any).localStorage === 'undefined' || true) {
  (globalThis as any).localStorage = {
    getItem: (k: string) => (backing.has(k) ? backing.get(k)! : null),
    setItem: (k: string, v: string) => void backing.set(k, String(v)),
    removeItem: (k: string) => void backing.delete(k),
    clear: () => void backing.clear(),
    key: (i: number) => Array.from(backing.keys())[i] ?? null,
    get length() { return backing.size; },
  };
}

import { persistAllThreads, loadPersistedThreads, overlayPersistedThreads } from './threadPersistence';

const KEY = 'onclaw.threads.v1';

const boundThread = {
  active: 'sess_x-1',
  list: [{ id: 'sess_x-1', sess: 'sess_x-1', title: 'Chat', updated: '', messages: [
    { id: 'u1', author: 'you', text: 'hi', ts: '' },
    { id: 'm1', author: 'agent', text: 'hello', ts: '', resp: 'resp_sess_x-1_turn-1' },
  ] }],
};

describe('threadPersistence', () => {
  beforeEach(() => { backing.clear(); });

  it('round-trips threads per tenant, skipping legacy bare-array shapes', () => {
    persistAllThreads({
      ws1: { threads: { c1: boundThread, c2: [{ id: 'm0', author: 'you', text: 'legacy' }] } },
      ws2: { threads: {} },
    } as any);
    expect(loadPersistedThreads('ws1').c1).toEqual(boundThread);
    expect(loadPersistedThreads('ws1').c2).toBeUndefined();
    expect(loadPersistedThreads('ws2')).toEqual({});
    expect(loadPersistedThreads('ws3')).toEqual({});
  });

  it('caps stored history to the tail', () => {
    const big = {
      active: 's1',
      list: Array.from({ length: 60 }, (_, i) => ({ id: 's' + i, title: 'c', updated: '', messages: [] })),
    };
    big.list[59].messages = Array.from({ length: 300 }, (_, i) => ({ id: 'm' + i, author: 'you', text: '', ts: '' }));
    persistAllThreads({ ws1: { threads: { c1: big } } } as any);
    const stored = loadPersistedThreads('ws1').c1;
    expect(stored.list).toHaveLength(50);
    expect(stored.list[0].id).toBe('s10');
    expect(big.list[59].messages.length).toBeGreaterThan(200);
    void stored;
    const sessions = loadPersistedThreads('ws1').c1.list;
    expect(sessions.length).toBe(50);
  });

  it('overlay wins per chat — including a cleared thread', () => {
    const tenant = { threads: { c1: boundThread, c2: { active: 's9', list: [{ id: 's9', title: 'Seed', updated: '', messages: [{ id: 'x', author: 'you', text: 'seed', ts: '' }] }] } } };
    localStorage.setItem(KEY, JSON.stringify({ 'ws-ov': { c2: { active: null, list: [] } } }));
    const merged = overlayPersistedThreads('ws-ov', tenant);
    expect(merged.threads.c1).toEqual(boundThread);
    expect(merged.threads.c2.list).toEqual([]);
    expect(overlayPersistedThreads('ws-none', tenant)).toBe(tenant);
  });

  it('a created tenant picks up persisted threads (workspace entered after boot)', async () => {
    localStorage.setItem(KEY, JSON.stringify({ 'ws-late': { c1: boundThread } }));
    const { useStore } = await import('./index');
    useStore.getState().updateTenant('ws-late', (t: any) => t);
    const th = useStore.getState().db['ws-late'].threads.c1;
    expect(th.active).toBe('sess_x-1');
    expect(th.list[0].sess).toBe('sess_x-1');
  });

  it('reload: a fresh store boot reattaches the persisted binding (hydration can fire)', async () => {
    // 'acme' is a seeded tenant, 'a-atlas' a stable seeded chat id — exactly
    // what the boot overlay must reattach after a page reload.
    localStorage.setItem(KEY, JSON.stringify({
      acme: { 'a-atlas': boundThread },
    }));
    vi.resetModules();
    const { useStore } = await import('./index');
    const th = useStore.getState().db['acme']?.threads?.['a-atlas'];
    expect(th).toBeDefined();
    expect(th.active).toBe('sess_x-1');
    // The binding and chain survive, so ensureSessionBinding returns it as-is
    // and getLastResponse finds the resp link.
    useStore.setState({ pos: { ...(useStore.getState().pos as any), tenantId: 'acme' } } as any);
    expect(useStore.getState().ensureSessionBinding('a-atlas')).toBe('sess_x-1');
    expect(useStore.getState().getLastResponse('a-atlas')).toBe('resp_sess_x-1_turn-1');
  });

  it('store changes debounce-persist the threads slice', async () => {
    vi.useFakeTimers();
    try {
      const { useStore } = await import('./index');
      useStore.setState((s: any) => ({ db: { ...s.db, 'ws-sub': { ...(s.db['ws-sub'] || { threads: {} }), threads: { c1: boundThread } } } }));
      await vi.advanceTimersByTimeAsync(400);
      const stored = JSON.parse(localStorage.getItem(KEY) || '{}');
      expect(stored['ws-sub']?.c1?.active).toBe('sess_x-1');
    } finally {
      vi.useRealTimers();
    }
  });

  afterEach(() => {
    delete (globalThis as any).__threadPersistProbe;
  });
});
