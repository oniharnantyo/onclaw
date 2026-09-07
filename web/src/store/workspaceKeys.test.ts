import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useAuthStore } from './auth';
import { api, setToken } from '../lib/api';
import { getWorkspaceKey, setWorkspaceKey, clearWorkspaceKey, purgeWorkspaceKeys } from './workspaceKeys';

// This environment's jsdom exposes no localStorage (same mode behind the
// ~66 pre-existing failures); install a minimal stub so these suites run.
if (typeof (globalThis as any).localStorage === 'undefined') {
  const backing = new Map<string, string>();
  (globalThis as any).localStorage = {
    getItem: (k: string) => (backing.has(k) ? backing.get(k)! : null),
    setItem: (k: string, v: string) => void backing.set(k, String(v)),
    removeItem: (k: string) => void backing.delete(k),
    clear: () => void backing.clear(),
    key: (i: number) => Array.from(backing.keys())[i] ?? null,
    get length() { return backing.size; },
  };
}

describe('store/workspaceKeys', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('set/get round-trip a per-workspace key in its own slot', () => {
    setWorkspaceKey('ws1', 'oc_key_ws1');
    setWorkspaceKey('ws2', 'oc_key_ws2');

    expect(getWorkspaceKey('ws1')).toBe('oc_key_ws1');
    expect(getWorkspaceKey('ws2')).toBe('oc_key_ws2');
    expect(localStorage.getItem('onclaw.api_key.ws1')).toBe('oc_key_ws1');
    expect(localStorage.getItem('onclaw.api_key.ws2')).toBe('oc_key_ws2');
  });

  it('getWorkspaceKey returns null for an unknown workspace', () => {
    expect(getWorkspaceKey('nope')).toBeNull();
  });

  it('clearWorkspaceKey removes only that workspace slot', () => {
    setWorkspaceKey('ws1', 'oc_key_ws1');
    setWorkspaceKey('ws2', 'oc_key_ws2');

    clearWorkspaceKey('ws1');

    expect(getWorkspaceKey('ws1')).toBeNull();
    expect(getWorkspaceKey('ws2')).toBe('oc_key_ws2');
  });

  it('purgeWorkspaceKeys drops all onclaw.api_key.* slots and the superseded global slot', () => {
    setWorkspaceKey('ws1', 'oc_key_ws1');
    setWorkspaceKey('ws2', 'oc_key_ws2');
    localStorage.setItem('onclaw.api_key', 'oc_key_legacy_global');
    localStorage.setItem('unrelated', 'keep-me');

    purgeWorkspaceKeys();

    expect(getWorkspaceKey('ws1')).toBeNull();
    expect(getWorkspaceKey('ws2')).toBeNull();
    expect(localStorage.getItem('onclaw.api_key')).toBeNull();
    expect(localStorage.getItem('unrelated')).toBe('keep-me');
  });

  it('logout purges every workspace key slot', async () => {
    setToken('my-token');
    setWorkspaceKey('ws1', 'oc_key_ws1');
    setWorkspaceKey('ws2', 'oc_key_ws2');
    useAuthStore.setState({
      user: { id: 'u1', email: 'u@example.com', name: 'U', created_at: '', updated_at: '' },
      memberships: [],
      status: 'authenticated',
    });

    vi.spyOn(api.auth, 'logout').mockResolvedValue(undefined);

    await useAuthStore.getState().logout();

    expect(useAuthStore.getState().status).toBe('unauthenticated');
    expect(getWorkspaceKey('ws1')).toBeNull();
    expect(getWorkspaceKey('ws2')).toBeNull();
  });
});
