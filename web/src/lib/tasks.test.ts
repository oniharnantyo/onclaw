/**
 * @vitest-environment jsdom
 */
// Per-session task store + stream client (add-subagent-runs-surface, tasks
// 4.1): folding (REST list / progress frames / task_completed), the D5 join,
// subscription scoping (one underlying stream for N consumers, released on
// the last close), and the transcript-isolation guarantee. The transport is
// mocked: './api' narrowly (listTasks) and globalThis.fetch (the SSE stream).
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import {
  useTaskEntries,
  useTaskStream,
  joinTaskForCard,
  streamSessionTasks,
  taskFromWire,
  type TaskEntry,
} from './tasks';
import { useStore } from '../store';
import { seedDb } from '../data/seed';

const { listTasksMock } = vi.hoisted(() => ({ listTasksMock: vi.fn() }));

vi.mock('./api', () => ({
  api: {
    onUnauthorized: () => () => {},
    agents: {
      listTasks: listTasksMock,
    },
  },
  API_ORIGIN: '',
  getToken: () => 'jwt-test-token',
}));

// This environment's jsdom exposes no localStorage; the store module touches
// it at boot (thread persistence) — install a minimal stub.
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

// ---------------------------------------------------------------------------
// Harness: a chat ('a-atlas') whose active session is server-bound, plus SSE
// response builders copied from the livechat suite's idiom.
// ---------------------------------------------------------------------------

const encoder = new TextEncoder();

/** Minimal SSE Response; `open` keeps the stream unclosed (only an abort or
 * cancel unblocks the reader). `onCancel` observes teardown. */
function sseResponse(
  chunks: string[],
  init?: { open?: boolean; onCancel?: () => void }
): Response {
  const headers = new Headers();
  headers.set('Content-Type', 'text/event-stream');
  return {
    ok: true,
    status: 200,
    headers,
    body: new ReadableStream<Uint8Array>({
      start(controller) {
        for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
        if (!init?.open) controller.close();
      },
      cancel() {
        init?.onCancel?.();
      },
    }),
  } as unknown as Response;
}

const frame = (ev: any) => `data: ${JSON.stringify(ev)}\n\n`;

/** Boots the store with the seeded acme tenant and a bound active session for
 * a chat; returns nothing — tests address the chat directly. */
function bootChat(chatId: string, sessionId: string, messages: any[] = []) {
  const db: any = seedDb();
  db.acme.threads[chatId] = {
    active: sessionId,
    list: [{ id: sessionId, sess: sessionId, title: 'S', updated: '', messages }],
  };
  useStore.setState({
    db,
    tasks: {},
    pos: { tenantId: 'acme', view: 'chats', chatId, showContext: false },
  });
}

const tasksOf = (chatId: string) => useStore.getState().tasks[chatId];

const entry = (over: Partial<TaskEntry>): TaskEntry => ({
  taskId: 't',
  kind: 'delegation',
  status: 'running',
  description: '',
  ...over,
});

beforeEach(() => {
  localStorage.clear();
  listTasksMock.mockReset();
  listTasksMock.mockResolvedValue({ tasks: [] });
  useStore.setState({ tasks: {} });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

// ---------------------------------------------------------------------------
// Wire mapping + folding
// ---------------------------------------------------------------------------

describe('taskFromWire (pinned wire contract → store entry)', () => {
  it('maps snake_case wire rows onto camelCase entries', () => {
    expect(taskFromWire({ task_id: 't1', kind: 'shell', status: 'running', description: 'build' })).toEqual({
      taskId: 't1',
      kind: 'shell',
      status: 'running',
      description: 'build',
    });
    expect(taskFromWire({
      task_id: 't2', kind: 'delegation', status: 'completed', description: 'd',
      output_path: '/a/.tasks/t2.output', summary: 'done thing',
    })).toEqual({
      taskId: 't2', kind: 'delegation', status: 'completed', description: 'd',
      outputPath: '/a/.tasks/t2.output', summary: 'done thing',
    });
  });

  it('degrades malformed rows instead of crashing', () => {
    expect(taskFromWire({} as any)).toEqual({ taskId: '', kind: 'delegation', status: 'created', description: '' });
  });
});

describe('applyTaskList (REST fold: merge, never clobber)', () => {
  it('folds the list ordered running-first, ended after', () => {
    bootChat('a-atlas', 'sess_l1');
    useStore.getState().applyTaskList('a-atlas', [
      entry({ taskId: 't-done', kind: 'shell', status: 'completed', description: 'ended first in the list' }),
      entry({ taskId: 't-run', description: 'still going' }),
      entry({ taskId: 't-fail', status: 'failed', description: 'ended second' }),
    ]);
    expect(tasksOf('a-atlas').entries.map((e) => e.taskId)).toEqual(['t-run', 't-done', 't-fail']);
  });

  it('merges by taskId and preserves the live record buffer (no clobber)', () => {
    bootChat('a-atlas', 'sess_l2');
    useStore.getState().applyTaskFrame('a-atlas', {
      task_id: 't1', kind: 'delegation', sequence: 1, record: { type: 'text', text: 'working' },
    });
    const before = tasksOf('a-atlas').records.t1;
    useStore.getState().applyTaskList('a-atlas', [entry({ taskId: 't1', description: 'the join key' })]);
    expect(tasksOf('a-atlas').entries[0]).toMatchObject({ taskId: 't1', description: 'the join key', status: 'running' });
    expect(tasksOf('a-atlas').records.t1).toBe(before);
  });

  it('is present-only: an empty list on an unknown chat writes nothing', () => {
    bootChat('a-atlas', 'sess_l3');
    useStore.getState().applyTaskList('a-atlas', []);
    expect(useStore.getState().tasks['a-atlas']).toBeUndefined();
    expect(tasksOf('no-such-chat')).toBeUndefined();
  });
});

describe('applyTaskFrame (progress buffers; transcript untouched)', () => {
  it('appends records in sequence order and mints unknown tasks as running', () => {
    bootChat('a-atlas', 'sess_f1');
    useStore.getState().applyTaskFrame('a-atlas', {
      task_id: 't9', kind: 'shell', sequence: 0, record: { type: 'text', text: 'line one' },
    });
    useStore.getState().applyTaskFrame('a-atlas', {
      task_id: 't9', kind: 'shell', sequence: 1,
      record: { type: 'tool_call', name: 'execute', args: '{"command":"ls"}' },
    });
    const records = tasksOf('a-atlas').records.t9;
    expect(records).toHaveLength(2);
    expect(records[0]).toEqual({ sequence: 0, type: 'text', text: 'line one' });
    expect(records[1]).toEqual({ sequence: 1, type: 'tool_call', name: 'execute', args: '{"command":"ls"}' });
    expect(tasksOf('a-atlas').entries[0]).toMatchObject({ taskId: 't9', kind: 'shell', status: 'running' });
  });

  it('drops replayed frames at or below the last applied sequence', () => {
    bootChat('a-atlas', 'sess_f2');
    useStore.getState().applyTaskFrame('a-atlas', {
      task_id: 't1', kind: 'delegation', sequence: 3, record: { type: 'text', text: 'newest' },
    });
    useStore.getState().applyTaskFrame('a-atlas', {
      task_id: 't1', kind: 'delegation', sequence: 3, record: { type: 'text', text: 'newest again' },
    });
    useStore.getState().applyTaskFrame('a-atlas', {
      task_id: 't1', kind: 'delegation', sequence: 1, record: { type: 'text', text: 'stale replay' },
    });
    expect(tasksOf('a-atlas').records.t1).toHaveLength(1);
    expect(tasksOf('a-atlas').records.t1[0].text).toBe('newest');
  });

  it('never folds progress into the transcript message list', () => {
    bootChat('a-atlas', 'sess_f3', [{ id: 'm1', author: 'you', ts: 't', text: 'hello' }]);
    const db = useStore.getState().db;
    useStore.getState().applyTaskFrame('a-atlas', {
      task_id: 't1', kind: 'delegation', sequence: 0, record: { type: 'text', text: 'child text' },
    });
    // Same db reference, same message list — only the task buffers grew.
    expect(useStore.getState().db).toBe(db);
    expect(useStore.getState().db.acme.threads['a-atlas'].list[0].messages).toHaveLength(1);
    expect(tasksOf('a-atlas').records.t1).toHaveLength(1);
  });

  it('ignores frames for an ended task and malformed frames entirely', () => {
    bootChat('a-atlas', 'sess_f4');
    useStore.getState().applyTaskCompleted('a-atlas', { taskId: 't1', kind: 'shell', outcome: 'failed' });
    const snapshot = tasksOf('a-atlas');
    useStore.getState().applyTaskFrame('a-atlas', {
      task_id: 't1', kind: 'shell', sequence: 9, record: { type: 'text', text: 'late' },
    });
    useStore.getState().applyTaskFrame('a-atlas', { task_id: 't2' } as any);
    useStore.getState().applyTaskFrame('', { task_id: 't2', sequence: 0, record: { type: 'text' } } as any);
    expect(tasksOf('a-atlas')).toEqual(snapshot);
  });
});

describe('applyTaskCompleted (idempotent terminal flip)', () => {
  it.each([
    ['completed', 'completed'],
    ['failed', 'failed'],
    ['canceled', 'canceled'],
  ])('flips a running task to %s with the wire extras', (outcome, status) => {
    bootChat('a-atlas', 'sess_c1');
    useStore.getState().applyTaskList('a-atlas', [entry({ taskId: 't1', description: 'research' })]);
    useStore.getState().applyTaskCompleted('a-atlas', {
      taskId: 't1', kind: 'delegation', outcome,
      ...(outcome === 'completed' ? { outputPath: '/x/.tasks/t1.output', summary: 'researched it' } : {}),
    });
    const e = tasksOf('a-atlas').entries[0];
    expect(e.status).toBe(status);
    if (outcome === 'completed') {
      expect(e.outputPath).toBe('/x/.tasks/t1.output');
      expect(e.summary).toBe('researched it');
    }
  });

  it('is idempotent: a repeat completion does not reorder or duplicate', () => {
    bootChat('a-atlas', 'sess_c2');
    useStore.getState().applyTaskCompleted('a-atlas', { taskId: 't1', kind: 'shell', outcome: 'completed', summary: 'once' });
    const snapshot = tasksOf('a-atlas');
    useStore.getState().applyTaskCompleted('a-atlas', { taskId: 't1', kind: 'shell', outcome: 'completed', summary: 'once' });
    expect(tasksOf('a-atlas')).toEqual(snapshot);
  });

  it('mints unknown tasks (completion beat the list) and keeps the buffer', () => {
    bootChat('a-atlas', 'sess_c3');
    useStore.getState().applyTaskFrame('a-atlas', {
      task_id: 't1', kind: 'delegation', sequence: 0, record: { type: 'text', text: 'tail' },
    });
    useStore.getState().applyTaskCompleted('a-atlas', { taskId: 't1', kind: 'delegation', outcome: 'canceled' });
    expect(tasksOf('a-atlas').entries[0]).toMatchObject({ taskId: 't1', status: 'canceled' });
    expect(tasksOf('a-atlas').records.t1).toHaveLength(1);
  });

  it('ignores unknown outcomes and malformed payloads', () => {
    bootChat('a-atlas', 'sess_c4');
    useStore.getState().applyTaskCompleted('a-atlas', { taskId: 't1', kind: 'shell', outcome: 'exploded' });
    useStore.getState().applyTaskCompleted('a-atlas', { outcome: 'completed' } as any);
    expect(useStore.getState().tasks['a-atlas']).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// D5 join (display-only)
// ---------------------------------------------------------------------------

describe('joinTaskForCard (description match, order tiebreak)', () => {
  const entries = [
    entry({ taskId: 'a', description: 'same brief' }),
    entry({ taskId: 'b', status: 'completed', description: 'other' }),
    entry({ taskId: 'c', status: 'failed', description: 'same brief' }),
  ];

  it('joins the single match regardless of index noise', () => {
    expect(joinTaskForCard(entries, 'other', 0)?.taskId).toBe('b');
  });

  it('breaks same-description ties by order index', () => {
    expect(joinTaskForCard(entries, 'same brief', 0)?.taskId).toBe('a');
    expect(joinTaskForCard(entries, 'same brief', 1)?.taskId).toBe('c');
  });

  it('returns null when unjoinable: no match, out-of-range index, empty description', () => {
    expect(joinTaskForCard(entries, 'nope', 0)).toBeNull();
    expect(joinTaskForCard(entries, 'same brief', 2)).toBeNull();
    expect(joinTaskForCard(entries, 'same brief', -1)).toBeNull();
    expect(joinTaskForCard(entries, '', 0)).toBeNull();
    expect(joinTaskForCard(undefined as any, 'same brief', 0)).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Selectors (present-only)
// ---------------------------------------------------------------------------

describe('useTaskEntries (present-only selector)', () => {
  it('starts empty and folds on store writes, keeping the empty shape for unknown chats', () => {
    bootChat('a-atlas', 'sess_s1');
    const { result } = renderHook(() => useTaskEntries('a-atlas'));
    expect(result.current).toEqual([]);

    act(() => {
      useStore.getState().applyTaskList('a-atlas', [entry({ taskId: 't1' })]);
    });
    expect(result.current).toHaveLength(1);
    expect(result.current[0].taskId).toBe('t1');
  });
});

// ---------------------------------------------------------------------------
// Ref-counted stream (chat-runtime spec: subscription scoped to the consumer
// window; one underlying stream for any number of consumers)
// ---------------------------------------------------------------------------

describe('useTaskStream subscription scoping', () => {
  it('opens ONE underlying stream for two consumers and folds the REST list first', async () => {
    bootChat('a-atlas', 'sess_sub1');
    const fetchMock = vi.fn().mockResolvedValue(
      sseResponse(
        [
          frame({ task_id: 't1', kind: 'delegation', sequence: 0, record: { type: 'text', text: 'hello from the child' } }),
        ],
        { open: true }
      )
    );
    vi.stubGlobal('fetch', fetchMock);
    listTasksMock.mockResolvedValue({
      tasks: [{ task_id: 't1', kind: 'delegation', status: 'running', description: 'the join key' }],
    });

    // Two consumers in one tree share one refcount.
    const { unmount } = renderHook(() => {
      useTaskStream('a-atlas', true);
      useTaskStream('a-atlas', true);
    });

    // Two consumers, one SSE stream; the REST list preceded it.
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    expect(listTasksMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toBe('/api/v1/workspaces/acme/agents/a-atlas/sessions/sess_sub1/tasks/stream');
    expect(init.headers.Authorization).toBe('Bearer jwt-test-token');

    // The list folded and the streamed progress record grew ONLY the buffer —
    // the transcript's message list is untouched.
    await vi.waitFor(() => expect(useStore.getState().tasks['a-atlas']?.records?.t1).toHaveLength(1));
    expect(useStore.getState().tasks['a-atlas'].entries[0]).toMatchObject({
      taskId: 't1', status: 'running', description: 'the join key',
    });
    expect(useStore.getState().db.acme.threads['a-atlas'].list[0].messages).toHaveLength(0);

    unmount();
    await new Promise((r) => setTimeout(r, 0));
  });

  it('releases the stream only when the LAST consumer closes, re-arms on re-open', async () => {
    bootChat('a-atlas', 'sess_sub2');
    let cancelled = 0;
    // A fresh Response per call — a ReadableStream body is single-use, so the
    // re-arm pass needs its own.
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() =>
      Promise.resolve(sseResponse([], { open: true, onCancel: () => { cancelled += 1; } }))
    ));

    const twoConsumers = () => {
      useTaskStream('a-atlas', true);
      useTaskStream('a-atlas', true);
    };
    const first = renderHook(twoConsumers);
    const second = renderHook(twoConsumers);
    await vi.waitFor(() => expect(globalThis.fetch).toHaveBeenCalledTimes(1));

    first.unmount();
    await new Promise((r) => setTimeout(r, 0));
    // One consumer remains — the stream stays open.
    expect(cancelled).toBe(0);

    second.unmount();
    await vi.waitFor(() => expect(cancelled).toBe(1));

    // Re-arming after full teardown re-subscribes (list first, then stream).
    listTasksMock.mockClear();
    const third = renderHook(twoConsumers);
    await vi.waitFor(() => expect(listTasksMock).toHaveBeenCalledTimes(1));
    await vi.waitFor(() => expect(globalThis.fetch).toHaveBeenCalledTimes(2));
    third.unmount();
    await vi.waitFor(() => expect(cancelled).toBe(2));
  });

  it('does not subscribe when disabled or the session is unresolvable', async () => {
    bootChat('a-atlas', 'sess_sub3');
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    const { unmount } = renderHook(() => useTaskStream('a-atlas', false));
    await new Promise((r) => setTimeout(r, 0));
    expect(fetchMock).not.toHaveBeenCalled();
    expect(listTasksMock).not.toHaveBeenCalled();
    unmount();

    // A chat with no bound session has no wire address either.
    const db: any = seedDb();
    db.acme.threads['a-atlas'] = { active: 'local-1', list: [{ id: 'local-1', title: 'L', updated: '', messages: [] }] };
    useStore.setState({ db, pos: { tenantId: 'acme', view: 'chats', chatId: 'a-atlas', showContext: false } });
    const second = renderHook(() => useTaskStream('a-atlas', true));
    await new Promise((r) => setTimeout(r, 0));
    expect(fetchMock).not.toHaveBeenCalled();
    second.unmount();
  });
});

// ---------------------------------------------------------------------------
// streamSessionTasks (SSE consumer mechanics)
// ---------------------------------------------------------------------------

describe('streamSessionTasks', () => {
  it('parses task-keyed frames and resolves on stream end', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      sseResponse([
        frame({ task_id: 't1', kind: 'delegation', sequence: 0, record: { type: 'text', text: 'a' } }),
        frame({ task_id: 't1', kind: 'delegation', sequence: 1, terminal: { task_id: 't1', kind: 'delegation', outcome: 'completed' } }),
        'data: [DONE]\n\n',
      ])
    );
    vi.stubGlobal('fetch', fetchMock);

    const frames: any[] = [];
    const onDone = vi.fn();
    const onError = vi.fn();
    await streamSessionTasks({
      workspaceId: 'acme', agentSlug: 'atlas', sessionId: 'sess_sse',
      onFrame: (f) => frames.push(f), onDone, onError,
    });

    expect(frames).toHaveLength(2);
    expect(frames[0].record).toEqual({ type: 'text', text: 'a' });
    expect(frames[1].terminal.outcome).toBe('completed');
    expect(onDone).toHaveBeenCalledTimes(1);
    expect(onError).not.toHaveBeenCalled();
    expect(String(fetchMock.mock.calls[0][0])).toBe(
      '/api/v1/workspaces/acme/agents/atlas/sessions/sess_sse/tasks/stream'
    );
  });

  it('reports failures via onError and aborts silently', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('fetch failed')));
    const onError = vi.fn();
    const onDone = vi.fn();
    await streamSessionTasks({
      workspaceId: 'acme', agentSlug: 'atlas', sessionId: 'sess_sse-err',
      onFrame: () => {}, onDone, onError,
    });
    expect(onError).toHaveBeenCalledTimes(1);
    expect(onDone).not.toHaveBeenCalled();

    // Abort before connect: silent teardown.
    const controller = new AbortController();
    controller.abort();
    const onErr2 = vi.fn();
    await streamSessionTasks({
      workspaceId: 'acme', agentSlug: 'atlas', sessionId: 'sess_sse-abort',
      signal: controller.signal, onFrame: () => {}, onDone: vi.fn(), onError: onErr2,
    });
    expect(onErr2).not.toHaveBeenCalled();
  });
});
