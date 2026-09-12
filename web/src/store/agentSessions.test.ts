/**
 * @vitest-environment jsdom
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

// Narrow api mock — listAgentSessions / deleteAgentSession are the seams the
// session-data layer (agent-session-index, tasks 4.1–4.5) is tested against.
vi.mock('../lib/api', () => ({
  api: { onUnauthorized: () => () => {}, auth: {}, agents: {} },
  getToken: vi.fn(() => 'jwt-test'),
  setToken: () => {},
  clearToken: () => {},
  ApiError: class ApiError extends Error {
    status: number;
    constructor(status: number, _code?: string, message?: string) {
      super(message || 'api error');
      this.name = 'ApiError';
      this.status = status;
    }
  },
  formatApiError: (_e: unknown, fallback?: string) => fallback || 'api error',
  pollAgentPromptsStatus: async () => ({}),
  listAgentSessions: vi.fn(),
  deleteAgentSession: vi.fn(),
}));

// This environment's jsdom exposes no localStorage — install the stub before
// test bodies run (same mode as the other store suites).
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

import { useStore, deriveSessionTitle, installSessionRefetchListeners } from './index';
import { seedDb } from '../data/seed';
import { listAgentSessions, deleteAgentSession, getToken, ApiError } from '../lib/api';

const getTokenMock = getToken;
const listMock = vi.mocked(listAgentSessions);
const deleteMock = vi.mocked(deleteAgentSession);

const posFor = (tenantId: string, chatId: string) => ({ tenantId, view: 'chats', chatId, showContext: false });

const seedAgentThread = (chatId: string, thread: any) => {
  useStore.setState({
    db: { acme: { ...seedDb().acme, threads: { [chatId]: thread } } },
    pos: posFor('acme', chatId),
    ui: { ...useStore.getState().ui, toasts: [] },
  });
};

const threadOf = (chatId = 'a-atlas') => useStore.getState().db.acme.threads[chatId] as any;

const refetch = (tenantId = 'acme', chatId = 'a-atlas') => useStore.getState().refetchAgentSessions(tenantId, chatId);

// A server listing row (the API delivers rows last-activity-first).
const row = (session_id: string, extra: Record<string, unknown> = {}) => ({
  id: 'row-' + session_id,
  session_id,
  title: '',
  created_at: '2026-01-01T00:00:00Z',
  last_active_at: '2026-01-02T12:00:00Z',
  running: false,
  ...extra,
});

const flush = () => new Promise((r) => setTimeout(r, 0));

beforeEach(() => {
  localStorage.clear();
  vi.clearAllMocks();
  listMock.mockResolvedValue({ sessions: [] });
});

afterEach(() => {
  vi.useRealTimers();
});

describe('session index reconciliation (agent-session-index D4)', () => {
  it('puts server rows first in listing order, merges locals by binding, appends pre-index locals', async () => {
    vi.useFakeTimers();
    seedAgentThread('a-atlas', {
      active: 'sess_old',
      list: [
        { id: 'sess_old', sess: 'sess_old', title: 'Local draft', updated: '9:00 AM', messages: [{ id: 'm1', author: 'you', text: 'hello', ts: '' }] },
        { id: 's_local', title: 'Pre-index chat', updated: '', messages: [] },
      ],
    });
    listMock.mockResolvedValue({
      sessions: [
        row('sess_new', { title: 'Server born', running: true }),
        row('sess_old', { title: 'Server title', running: false }),
      ],
    });

    const done = refetch();
    await vi.advanceTimersByTimeAsync(500);
    await done;

    expect(listMock).toHaveBeenCalledWith('acme', 'a-atlas');
    const th = threadOf();
    expect(th.list.map((s: any) => s.id)).toEqual(['sess_new', 'sess_old', 's_local']);
    // Server-only row becomes an empty, server-born entry (hydration on select).
    expect(th.list[0]).toMatchObject({ id: 'sess_new', sess: 'sess_new', serverBorn: true, running: true, messages: [] });
    // Matched local entry keeps transcript + optimistic title; running mapped.
    expect(th.list[1].messages).toHaveLength(1);
    expect(th.list[1].title).toBe('Local draft');
    expect(th.list[1].running).toBe(false);
    expect(th.list[1].serverBorn).toBe(true);
    // The active pointer follows the session, not the slot.
    expect(th.active).toBe('sess_old');
  });

  it('server titles overwrite only fallbacks and server-born rows', async () => {
    vi.useFakeTimers();
    seedAgentThread('a-atlas', {
      active: 'sess_a',
      list: [
        { id: 'sess_a', sess: 'sess_a', title: 'New chat', messages: [] },
        { id: 'sess_b', sess: 'sess_b', title: 'Optimistic draft', messages: [] },
        { id: 'sess_c', sess: 'sess_c', title: '', messages: [] },
        { id: 'sess_d', sess: 'sess_d', title: 'Stale born', messages: [], serverBorn: true },
      ],
    });
    listMock.mockResolvedValue({
      sessions: [
        row('sess_a', { title: 'Server A' }),
        row('sess_b', { title: 'Server B' }),
        row('sess_c', { title: 'Server C' }),
        row('sess_d', { title: 'Fresh born' }),
      ],
    });

    const done = refetch();
    await vi.advanceTimersByTimeAsync(500);
    await done;

    const titles: Record<string, string> = {};
    threadOf().list.forEach((s: any) => { titles[s.id] = s.title; });
    expect(titles).toEqual({
      sess_a: 'Server A',      // "New chat" fallback → server wins
      sess_b: 'Optimistic draft', // fresh optimistic title → untouched
      sess_c: 'Server C',      // empty title → server wins
      sess_d: 'Fresh born',    // server-born row → server is authoritative
    });
  });

  it('drops server-confirmed rows the listing no longer returns, keeps pre-index locals', async () => {
    vi.useFakeTimers();
    seedAgentThread('a-atlas', {
      active: 'sess_dead',
      list: [
        { id: 'sess_dead', sess: 'sess_dead', title: 'Doomed', messages: [], serverBorn: true },
        { id: 's_keep', title: 'Local only', messages: [] },
      ],
    });
    listMock.mockResolvedValue({ sessions: [row('sess_alive', { title: 'Alive' })] });

    const done = refetch();
    await vi.advanceTimersByTimeAsync(500);
    await done;

    const th = threadOf();
    expect(th.list.map((s: any) => s.id)).toEqual(['sess_alive', 's_keep']);
    // The deleted-active case falls back to the first remaining session.
    expect(th.active).toBe('sess_alive');
  });

  it('a listing with no rows over server-born-only history spawns a fresh New chat', async () => {
    vi.useFakeTimers();
    seedAgentThread('a-atlas', {
      active: 'sess_x',
      list: [{ id: 'sess_x', sess: 'sess_x', title: 'Gone', messages: [], serverBorn: true }],
    });

    const done = refetch();
    await vi.advanceTimersByTimeAsync(500);
    await done;

    const th = threadOf();
    expect(th.list).toHaveLength(1);
    expect(th.list[0].title).toBe('New chat');
    expect(th.active).toBe(th.list[0].id);
  });

  it('a chat with zero sessions never exists even when nothing was server-listed', async () => {
    vi.useFakeTimers();
    seedAgentThread('a-atlas', { active: null, list: [] });

    const done = refetch();
    await vi.advanceTimersByTimeAsync(500);
    await done;

    const th = threadOf();
    expect(th.list).toHaveLength(1);
    expect(th.list[0].title).toBe('New chat');
  });
});

describe('refetchAgentSessions coalescing', () => {
  it('two triggers inside the window collapse into one fetch', async () => {
    vi.useFakeTimers();
    seedAgentThread('a-atlas', { active: null, list: [] });

    const p1 = refetch();
    const p2 = refetch();
    await vi.advanceTimersByTimeAsync(500);
    await Promise.all([p1, p2]);

    expect(listMock).toHaveBeenCalledTimes(1);
    // Both callers resolve once the coalesced fetch completes.
    expect(threadOf().list).toHaveLength(1); // empty listing → fresh New chat applied
  });

  it('a trigger landing mid-fetch schedules exactly one trailing fetch', async () => {
    vi.useFakeTimers();
    seedAgentThread('a-atlas', { active: null, list: [] });

    let release!: (v: unknown) => void;
    listMock.mockImplementationOnce(() => new Promise((r) => { release = r; }));
    listMock.mockResolvedValueOnce({ sessions: [row('sess_1', { title: 'One' })] });

    const p1 = refetch();
    await vi.advanceTimersByTimeAsync(500); // first fetch starts (pending)
    expect(listMock).toHaveBeenCalledTimes(1);

    const p2 = refetch(); // lands while the first fetch is in flight
    await vi.advanceTimersByTimeAsync(500); // its timer fires → waits on inflight
    expect(listMock).toHaveBeenCalledTimes(1);

    release(undefined);
    await p1;
    await p2;

    expect(listMock).toHaveBeenCalledTimes(2); // exactly one trailing fetch
    const ids = threadOf().list.map((s: any) => s.id);
    expect(ids[0]).toBe('sess_1'); // the trailing fetch's listing is applied
    // fetch1's empty listing spawned a purely-local "New chat" — the
    // local-append rule keeps it below the server row.
    expect(threadOf().list[1].title).toBe('New chat');
  });

  it('resolves without a fetch when no token is present', async () => {
    vi.useFakeTimers();
    seedAgentThread('a-atlas', { active: null, list: [] });
    vi.mocked(getTokenMock).mockReturnValueOnce(null);

    await refetch();
    await vi.advanceTimersByTimeAsync(500);

    expect(listMock).not.toHaveBeenCalled();
  });

  it('resolves without a fetch for non-agent chats', async () => {
    vi.useFakeTimers();
    seedAgentThread('a-atlas', { active: null, list: [] });

    await refetch('acme', 'not-an-agent');
    await vi.advanceTimersByTimeAsync(500);

    expect(listMock).not.toHaveBeenCalled();
  });
});

describe('refetch triggers (D4)', () => {
  it('window focus, visibilitychange, and the threads storage event schedule one coalesced refetch', async () => {
    vi.useFakeTimers();
    seedAgentThread('a-atlas', { active: null, list: [] });
    const before = listMock.mock.calls.length;

    window.dispatchEvent(new Event('focus'));
    document.dispatchEvent(new Event('visibilitychange'));
    window.dispatchEvent(new StorageEvent('storage', { key: 'onclaw.threads.v1', newValue: JSON.stringify({ acme: {} }) }));

    await vi.advanceTimersByTimeAsync(500);
    expect(listMock.mock.calls.length).toBe(before + 1);
    expect(listMock.mock.calls.at(-1)).toEqual(['acme', 'a-atlas']);
  });

  it('turn terminal (running true→false) schedules one coalesced refetch', async () => {
    vi.useFakeTimers();
    seedAgentThread('a-atlas', { active: null, list: [] });
    useStore.setState({ ui: { ...useStore.getState().ui, running: true } });
    const before = listMock.mock.calls.length;

    useStore.getState().patchUi({ running: false });
    await vi.advanceTimersByTimeAsync(500);

    expect(listMock.mock.calls.length).toBe(before + 1);
    expect(listMock.mock.calls.at(-1)).toEqual(['acme', 'a-atlas']);
  });

  it('ignores storage events for other keys', async () => {
    vi.useFakeTimers();
    seedAgentThread('a-atlas', { active: null, list: [] });
    const before = listMock.mock.calls.length;

    window.dispatchEvent(new StorageEvent('storage', { key: 'some.other.key', newValue: '{}' }));
    await vi.advanceTimersByTimeAsync(500);

    expect(listMock.mock.calls.length).toBe(before);
  });

  it('installs its listeners once per document', () => {
    installSessionRefetchListeners();
    installSessionRefetchListeners();
    expect((window as any).__onclawSessionRefetchListeners).toBe(true);
  });
});

describe('deleteSession (D6: server first, then local)', () => {
  it('calls the API before removing locally, then activates the adjacent session', async () => {
    seedAgentThread('a-atlas', {
      active: 'sess_a',
      list: [
        { id: 'sess_a', sess: 'sess_a', title: 'A', messages: [] },
        { id: 'sess_b', sess: 'sess_b', title: 'B', messages: [] },
      ],
    });
    let release!: () => void;
    deleteMock.mockImplementationOnce(() => new Promise<void>((r) => { release = r; }));

    useStore.getState().deleteSession('sess_a');
    await flush();
    expect(deleteMock).toHaveBeenCalledTimes(1);
    expect(deleteMock).toHaveBeenCalledWith('acme', 'a-atlas', 'sess_a');
    // Server call in flight — nothing removed yet.
    expect(threadOf().list.map((s: any) => s.id)).toEqual(['sess_a', 'sess_b']);

    release();
    await flush();
    const th = threadOf();
    expect(th.list.map((s: any) => s.id)).toEqual(['sess_b']);
    expect(th.active).toBe('sess_b');
    expect(useStore.getState().ui.toasts.some((t: any) => t.text === 'Session deleted')).toBe(true);
  });

  it('aborts on API failure — the row stays and a danger toast shows', async () => {
    seedAgentThread('a-atlas', {
      active: 'sess_a',
      list: [
        { id: 'sess_a', sess: 'sess_a', title: 'A', messages: [] },
        { id: 'sess_b', sess: 'sess_b', title: 'B', messages: [] },
      ],
    });
    deleteMock.mockRejectedValueOnce(new (ApiError as any)(0, 'network', 'down'));

    useStore.getState().deleteSession('sess_a');
    await flush();
    await flush();

    expect(threadOf().list.map((s: any) => s.id)).toEqual(['sess_a', 'sess_b']);
    expect(threadOf().active).toBe('sess_a');
    expect(useStore.getState().ui.toasts.some((t: any) => t.kind === 'danger')).toBe(true);
    expect(useStore.getState().ui.toasts.some((t: any) => t.text === 'Session deleted')).toBe(false);
  });

  it('treats 404 (pre-index history, no server row) as removal-complete', async () => {
    seedAgentThread('a-atlas', {
      active: 'sess_old',
      list: [{ id: 'sess_old', sess: 'sess_old', title: 'Pre-index', messages: [] }],
    });
    deleteMock.mockRejectedValueOnce(new (ApiError as any)(404, 'not_found', 'absent'));

    useStore.getState().deleteSession('sess_old');
    await flush();
    await flush();

    const th = threadOf();
    expect(th.list).toHaveLength(1);
    expect(th.list[0].title).toBe('New chat'); // last-session rule
  });

  it('deleting the last session spawns a fresh empty New chat', async () => {
    seedAgentThread('a-atlas', {
      active: 'sess_only',
      list: [{ id: 'sess_only', sess: 'sess_only', title: 'Only', messages: [] }],
    });
    deleteMock.mockResolvedValueOnce(undefined);

    useStore.getState().deleteSession('sess_only');
    await flush();
    await flush();

    const th = threadOf();
    expect(th.list).toHaveLength(1);
    expect(th.list[0]).toMatchObject({ title: 'New chat', messages: [] });
    expect(th.active).toBe(th.list[0].id);
  });

  it('purely-local sessions (never sent) skip the API call', async () => {
    seedAgentThread('a-atlas', {
      active: 's_loc',
      list: [{ id: 's_loc', title: 'Local', messages: [] }],
    });

    useStore.getState().deleteSession('s_loc');
    await flush();

    expect(deleteMock).not.toHaveBeenCalled();
    expect(threadOf().list).toHaveLength(1);
    expect(threadOf().list[0].title).toBe('New chat');
  });
});

describe('optimistic title parity (D2 rule, same on both sides)', () => {
  it('derives the title from the first line, trimmed', () => {
    expect(deriveSessionTitle('Fix the login bug\nstep one\nstep two')).toBe('Fix the login bug');
    expect(deriveSessionTitle('   padded line   ')).toBe('padded line');
    expect(deriveSessionTitle('')).toBe('');
    expect(deriveSessionTitle('   \n  ')).toBe('');
  });

  it('truncates long input at 42 chars with an ellipsis', () => {
    const long = 'a'.repeat(50);
    expect(deriveSessionTitle(long)).toBe('a'.repeat(42) + '…');
    expect(deriveSessionTitle(long).length).toBe(43);
    expect(deriveSessionTitle('short enough')).toBe('short enough');
  });

  it('pushMsg titles the session immediately; empty input keeps the New chat fallback', () => {
    seedAgentThread('a-atlas', { active: null, list: [] });
    useStore.getState().pushMsg('acme', 'a-atlas', { id: 'm1', author: 'you', text: 'Fix the login bug\nmore detail below', ts: '' });
    expect(threadOf().list[0].title).toBe('Fix the login bug');

    useStore.setState({ db: { ...useStore.getState().db, acme: { ...seedDb().acme, threads: { 'a-atlas': { active: null, list: [] } } } } as any });
    useStore.getState().pushMsg('acme', 'a-atlas', { id: 'm2', author: 'you', text: 'b'.repeat(60), ts: '' });
    expect(threadOf().list[0].title).toBe('b'.repeat(42) + '…');

    useStore.setState({ db: { ...useStore.getState().db, acme: { ...seedDb().acme, threads: { 'a-atlas': { active: null, list: [] } } } } as any });
    useStore.getState().pushMsg('acme', 'a-atlas', { id: 'm3', author: 'you', text: '', ts: '' });
    expect(threadOf().list[0].title).toBe('New chat');
  });

  it('server-confirmed titles agree with the optimistic ones (reconciliation is a no-op on titles)', async () => {
    vi.useFakeTimers();
    seedAgentThread('a-atlas', { active: null, list: [] });
    useStore.getState().pushMsg('acme', 'a-atlas', { id: 'm1', author: 'you', text: 'Fix the login bug\nsteps', ts: '' });
    useStore.getState().bindSession('a-atlas', 'sess_parity');
    const localId = threadOf().list[0].id;
    const localTitle = threadOf().list[0].title;

    // The server derives its birth title with the same rule (D2).
    listMock.mockResolvedValue({ sessions: [row('sess_parity', { title: 'Fix the login bug' })] });
    const done = refetch();
    await vi.advanceTimersByTimeAsync(500);
    await done;

    const th = threadOf();
    expect(th.list).toHaveLength(1);
    expect(th.list[0].id).toBe(localId);
    expect(th.list[0].title).toBe(localTitle);
  });
});
