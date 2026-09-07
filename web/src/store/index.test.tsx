import { describe, it, expect, beforeEach, beforeAll, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { useStore, useWorkspace, useThread } from './index';
import { useAuthStore } from './auth';
import { seedDb } from '../data/seed';

beforeAll(() => {
  // Node's disabled `localStorage` global shadows jsdom's on this Node
  // version; install a memory-backed stub (same workaround as ChatRoute.test).
  if (typeof localStorage === 'undefined' || !localStorage) {
    const mem = new Map<string, string>();
    const stub = {
      getItem: (k: string) => mem.get(k) ?? null,
      setItem: (k: string, v: string) => void mem.set(k, String(v)),
      removeItem: (k: string) => void mem.delete(k),
      clear: () => mem.clear(),
      key: (i: number) => Array.from(mem.keys())[i] ?? null,
      get length() {
        return mem.size;
      },
    };
    Object.defineProperty(globalThis, 'localStorage', { value: stub, configurable: true, writable: true });
  }
});

const { agentsListMock } = vi.hoisted(() => ({ agentsListMock: vi.fn() }));

vi.mock('../lib/api', () => ({
  api: {
    onUnauthorized: () => () => {},
    auth: {},
    agents: { list: agentsListMock },
  },
  pollAgentPromptsStatus: async () => ({}),
  formatApiError: (_e: unknown, fallback: string) => fallback,
  getToken: () => null,
  setToken: () => {},
  clearToken: () => {},
  ApiError: class ApiError extends Error {},
}));

function WorkspaceProbe() {
  const ws = useWorkspace();
  return <div data-testid="ws">{ws.name}</div>;
}

function ThreadProbe({ chatId }: { chatId: string }) {
  const th = useThread(chatId);
  return <div data-testid="th">{th.list.length}</div>;
}

const posFor = (tenantId: string) => ({ tenantId, view: 'chats', chatId: '', showContext: false });

describe('derived store selectors', () => {
  beforeEach(() => {
    localStorage.clear();
    useStore.setState({ db: seedDb(), pos: posFor('acme') });
    useAuthStore.setState({ memberships: [] });
  });

  it('useWorkspace resolves a membership-only workspace without looping', () => {
    // Regression: the old selector built a fresh blankTenant per call for ids
    // missing from db, which re-rendered the component forever under
    // useSyncExternalStore ("Maximum update depth exceeded").
    useAuthStore.setState({
      memberships: [{ workspace_id: 'ghost-ws', workspace_name: 'Ghost WS' } as any],
    });
    useStore.setState({ pos: posFor('ghost-ws') });

    render(<WorkspaceProbe />);

    expect(screen.getByTestId('ws').textContent).toBe('Ghost WS');
  });

  it('useWorkspace falls back to a stable default when the id is unknown everywhere', () => {
    useStore.setState({ pos: posFor('nowhere') });

    render(<WorkspaceProbe />);

    expect(screen.getByTestId('ws').textContent).toBe('Acme Corp');
  });

  it('useThread wraps legacy array-shaped threads into a session', () => {
    useStore.setState({
      db: { acme: { ...seedDb().acme, threads: { 'legacy-agent': [{ id: 'm1', author: 'you', ts: '9:00 AM', text: 'hello' }] } } },
      pos: posFor('acme'),
    });

    render(<ThreadProbe chatId="legacy-agent" />);

    expect(screen.getByTestId('th').textContent).toBe('1');
  });

  it('useThread returns the shared empty state for missing threads', () => {
    render(<ThreadProbe chatId="does-not-exist" />);

    expect(screen.getByTestId('th').textContent).toBe('0');
  });
});

describe('loadAgents loaded marker', () => {
  beforeEach(() => {
    localStorage.clear();
    agentsListMock.mockReset();
    useStore.setState({ db: seedDb(), agentsLoaded: {}, pos: posFor('acme') });
    useAuthStore.setState({ memberships: [] });
  });

  it('marks the workspace loaded when the fetch succeeds — even with zero agents', async () => {
    agentsListMock.mockResolvedValue({ agents: [] });

    await useStore.getState().loadAgents('acme');

    expect(useStore.getState().agentsLoaded['acme']).toBe(true);
  });

  it('does not mark the workspace loaded when the fetch fails', async () => {
    agentsListMock.mockRejectedValue(new Error('offline'));

    await useStore.getState().loadAgents('acme');

    expect(useStore.getState().agentsLoaded['acme']).toBeUndefined();
  });
});
