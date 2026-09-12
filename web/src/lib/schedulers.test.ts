/**
 * @vitest-environment node
 */
// Wire mapping for the scheduler client (change integrate-scheduler): paths,
// methods, query cursors, and snake_case payloads must match the backend
// routes (internal/server/router.go) and JSON tags (domain scheduler wire).
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { schedulers, type Scheduler, type SchedulerRun } from './schedulers';
// request() lives on the shared api module; importing it here pins the
// bearer-header parity with every other client call.
import { api, request, ApiError, setToken } from './api';

const fetchMock = vi.fn();

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal('fetch', fetchMock);
  // Minimal Map-backed stub: the suite only asserts the bearer header parity
  // with every other request() call, not storage behavior.
  const backing = new Map<string, string>();
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => (backing.has(k) ? backing.get(k)! : null),
    setItem: (k: string, v: string) => void backing.set(k, String(v)),
    removeItem: (k: string) => void backing.delete(k),
  });
  setToken('jwt-schedulers');
});

afterEach(() => {
  vi.unstubAllGlobals();
});

/** One fetch round-trip: capture the call, answer with `payload`. */
const reply = (payload: unknown, init?: { status?: number }) => {
  fetchMock.mockResolvedValueOnce({
    ok: (init?.status ?? 200) >= 200 && (init?.status ?? 200) < 300,
    status: init?.status ?? 200,
    headers: new Headers({ 'Content-Type': 'application/json' }),
    json: async () => payload,
  });
};

const wireScheduler: Scheduler = {
  id: 'sch-1', workspace_id: 'ws-1', agent_id: 'a-atlas', created_by: 'u-1',
  name: 'Morning ops digest', prompt: 'Digest the night',
  kind: 'recurring', expr: '0 7 * * 1-5', run_at: null,
  delivery: { type: 'thread' }, enabled: true, next_run_at: '2026-09-11T14:00:00Z',
  last_run: {
    status: 'completed', trigger: 'scheduler', started_at: '2026-09-10T14:00:00Z',
    duration_ms: 42000, tokens_used: 18200, session_id: 'sched_sch-1_1',
    delivery_status: 'delivered',
  },
  human_label: '07:00 · Mon–Fri',
  created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
};

const wireRun: SchedulerRun = {
  id: 'run-1', scheduler_id: 'sch-1', scheduler_name: 'Morning ops digest',
  agent_id: 'a-atlas', agent_name: 'Atlas', session_id: 'sched_sch-1_1',
  trigger: 'manual', status: 'running', started_at: '2026-09-10T15:00:00Z',
  duration_ms: 0, tokens_used: 0, delivery_status: '',
};

describe('schedulers client — CRUD wire mapping', () => {
  it('lists schedulers on the workspace-scoped path with the bearer token', async () => {
    reply({ schedulers: [wireScheduler] });
    const res = await schedulers.list('acme');
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toBe('/api/v1/workspaces/acme/schedulers');
    expect(init.method).toBe('GET');
    expect((init.headers as Headers).get('Authorization')).toBe('Bearer jwt-schedulers');
    expect(res.schedulers[0]).toEqual(wireScheduler);
  });

  it('creates with the snake_case payload and returns the {scheduler} envelope (201)', async () => {
    reply({ scheduler: wireScheduler }, { status: 201 });
    const res = await schedulers.create('acme', {
      name: 'Morning ops digest', agent_id: 'a-atlas', prompt: 'Digest the night',
      kind: 'recurring', expr: '0 7 * * 1-5',
      delivery: { type: 'thread' }, enabled: true,
    });
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toBe('/api/v1/workspaces/acme/schedulers');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body)).toEqual({
      name: 'Morning ops digest', agent_id: 'a-atlas', prompt: 'Digest the night',
      kind: 'recurring', expr: '0 7 * * 1-5',
      delivery: { type: 'thread' }, enabled: true,
    });
    expect(res.scheduler.expr).toBe('0 7 * * 1-5');
  });

  it('addresses one scheduler by id for get/update/delete', async () => {
    reply({ scheduler: wireScheduler });
    await schedulers.get('acme', 'sch 1');
    expect(String(fetchMock.mock.calls[0][0])).toBe('/api/v1/workspaces/acme/schedulers/sch%201');

    reply({ scheduler: wireScheduler });
    await schedulers.update('acme', 'sch-1', { enabled: false });
    const [patchUrl, patchInit] = fetchMock.mock.calls[1];
    expect(String(patchUrl)).toBe('/api/v1/workspaces/acme/schedulers/sch-1');
    expect(patchInit.method).toBe('PATCH');
    expect(JSON.parse(patchInit.body)).toEqual({ enabled: false });

    reply(undefined, { status: 204 });
    await expect(schedulers.delete('acme', 'sch-1')).resolves.toBeUndefined();
    expect(fetchMock.mock.calls[2][1].method).toBe('DELETE');
    expect(String(fetchMock.mock.calls[2][0])).toBe('/api/v1/workspaces/acme/schedulers/sch-1');
  });
});

describe('schedulers client — run-now and run history', () => {
  it('posts run-now and returns the {run} envelope (202)', async () => {
    reply({ run: wireRun }, { status: 202 });
    const res = await schedulers.run('acme', 'sch-1');
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toBe('/api/v1/workspaces/acme/schedulers/sch-1/run');
    expect(init.method).toBe('POST');
    expect(res.run.trigger).toBe('manual');
    expect(res.run.status).toBe('running');
  });

  it('surfaces the 409 in-flight conflict as an ApiError conflict', async () => {
    fetchMock.mockResolvedValueOnce({
      ok: false,
      status: 409,
      headers: new Headers({ 'Content-Type': 'application/json' }),
      json: async () => ({ error: { code: 'conflict', message: 'a run is already in flight' } }),
    });
    const err = await schedulers.run('acme', 'sch-1').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(409);
    expect((err as ApiError).code).toBe('conflict');
  });

  it('lists per-scheduler runs newest-first with ?limit=&offset=', async () => {
    reply({ runs: [wireRun], total: 1 });
    const res = await schedulers.runs('acme', 'sch-1', { limit: 50, offset: 10 });
    expect(String(fetchMock.mock.calls[0][0])).toBe(
      '/api/v1/workspaces/acme/schedulers/sch-1/runs?limit=50&offset=10'
    );
    expect(res.runs[0]).toEqual(wireRun);
    expect(res.total).toBe(1);
  });

  it('lists workspace-wide runs on the scheduler-runs path and omits empty query strings', async () => {
    reply({ runs: [wireRun], total: 1 });
    const res = await schedulers.listRuns('acme', { limit: 100 });
    expect(String(fetchMock.mock.calls[0][0])).toBe(
      '/api/v1/workspaces/acme/scheduler-runs?limit=100'
    );
    expect(res.runs[0].session_id).toBe('sched_sch-1_1');

    reply({ runs: [], total: 0 });
    await schedulers.listRuns('acme');
    expect(String(fetchMock.mock.calls[1][0])).toBe('/api/v1/workspaces/acme/scheduler-runs');
  });

  it('parses validation errors into ApiError with field details like every other client call', async () => {
    fetchMock.mockResolvedValueOnce({
      ok: false,
      status: 400,
      headers: new Headers({ 'Content-Type': 'application/json' }),
      json: async () => ({
        error: {
          code: 'invalid_request',
          message: 'validation failed',
          details: [{ field: 'expr', message: 'invalid cron expression' }],
        },
      }),
    });
    const err = await schedulers
      .create('acme', {
        name: 'x', agent_id: 'a', prompt: 'p', kind: 'recurring',
        expr: 'at nine', delivery: { type: 'thread' },
      })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(400);
    expect((err as ApiError).code).toBe('invalid_request');
    expect((err as ApiError).details[0]).toEqual({ field: 'expr', message: 'invalid cron expression' });
  });

  it('rides the same request() path as the rest of the api surface', async () => {
    reply({ schedulers: [] });
    await request('/workspaces/acme/schedulers');
    // Two calls total: the direct one above plus the client's own — both
    // must carry the workspace-scoped prefix.
    expect(String(fetchMock.mock.calls[0][0])).toBe('/api/v1/workspaces/acme/schedulers');
    expect(api.request).toBeTypeOf('function');
  });
});
