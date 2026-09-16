import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  heartbeats,
  type ApiHeartbeat,
  type ApiHeartbeatRun,
  type HeartbeatPayload,
} from './heartbeats';
import { ApiError } from './api';

describe('lib/heartbeats client', () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  function mockJson(payload: unknown) {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Headers({ 'Content-Type': 'application/json' }),
      json: async () => payload,
    } as any);
  }

  const heartbeat: ApiHeartbeat = {
    id: 'hb-1',
    workspace_id: 'ws-1',
    agent_id: 'a-atlas',
    prompt: 'Check the ops dashboard',
    expr: '*/15 * * * *',
    human_label: 'every 15 minutes',
    active_start: '08:00',
    active_end: '22:00',
    delivery: { type: 'creator_dm' },
    enabled: true,
    next_tick_at: '2026-09-14T15:00:00Z',
    last_tick: {
      status: 'completed',
      trigger: 'scheduler',
      started_at: '2026-09-14T14:00:00Z',
      duration_ms: 42000,
      tokens_used: 18200,
      session_id: 'hb_a-atlas',
      delivery_status: 'delivered',
    },
    failure_streak: 0,
    created_at: '2026-09-01T00:00:00Z',
    updated_at: '2026-09-01T00:00:00Z',
  };

  const run: ApiHeartbeatRun = {
    id: 'hbrun-1',
    heartbeat_id: 'hb-1',
    session_id: 'hb_a-atlas',
    trigger: 'manual',
    status: 'running',
    started_at: '2026-09-14T15:00:00Z',
    duration_ms: 0,
    tokens_used: 0,
    delivery_status: '',
  };

  it('gets the heartbeat (or null) plus the default checklist template', async () => {
    mockJson({ heartbeat: null, default_prompt: 'Default checklist template' });
    const res = await heartbeats.get('acme', 'a-atlas');
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe(
      '/api/v1/workspaces/acme/agents/a-atlas/heartbeat'
    );
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('GET');
    expect(res.heartbeat).toBeNull();
    expect(res.default_prompt).toBe('Default checklist template');

    mockJson({ heartbeat, default_prompt: 'Default checklist template' });
    await expect(heartbeats.get('acme', 'a-atlas')).resolves.toEqual({
      heartbeat,
      default_prompt: 'Default checklist template',
    });
  });

  it('PUTs the exact heartbeat payload and returns the saved heartbeat', async () => {
    mockJson({ heartbeat });
    const payload: HeartbeatPayload = {
      enabled: true,
      expr: '0 9 * * 1-5',
      active_start: '08:00',
      active_end: null,
      delivery: { type: 'channel', channel_id: 'ch-ops' },
      prompt: 'Digest overnight activity',
    };
    const res = await heartbeats.update('acme', 'a-atlas', payload);
    const [url, init] = (globalThis.fetch as any).mock.calls[0];
    expect(String(url)).toBe('/api/v1/workspaces/acme/agents/a-atlas/heartbeat');
    expect(init.method).toBe('PUT');
    expect(JSON.parse(init.body)).toEqual(payload);
    expect(res.heartbeat).toEqual(heartbeat);
  });

  it('POSTs run-now with no body and surfaces the 409 in-flight conflict', async () => {
    mockJson({ run });
    const res = await heartbeats.runNow('acme', 'a-atlas');
    const [url, init] = (globalThis.fetch as any).mock.calls[0];
    expect(String(url)).toBe('/api/v1/workspaces/acme/agents/a-atlas/heartbeat/run-now');
    expect(init.method).toBe('POST');
    expect(init.body).toBeUndefined();
    expect(res.run).toEqual(run);

    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      headers: new Headers({ 'Content-Type': 'application/json' }),
      json: async () => ({ error: { code: 'conflict', message: 'a tick is already in flight' } }),
    } as any);
    const err = await heartbeats.runNow('acme', 'a-atlas').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(409);
    expect((err as ApiError).code).toBe('conflict');
  });

  it('POSTs resume and returns the resumed heartbeat', async () => {
    mockJson({ heartbeat: { ...heartbeat, enabled: true, next_tick_at: '2026-09-14T15:15:00Z' } });
    const res = await heartbeats.resume('acme', 'a-atlas');
    const [url, init] = (globalThis.fetch as any).mock.calls[0];
    expect(String(url)).toBe('/api/v1/workspaces/acme/agents/a-atlas/heartbeat/resume');
    expect(init.method).toBe('POST');
    expect(res.heartbeat.enabled).toBe(true);
    expect(res.heartbeat.next_tick_at).toBe('2026-09-14T15:15:00Z');
  });

  it('encodes path segments needing it (workspace slug and agent id)', async () => {
    mockJson({ heartbeat: null, default_prompt: '' });
    await heartbeats.get('acme corp', 'a atlas');
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe(
      '/api/v1/workspaces/acme%20corp/agents/a%20atlas/heartbeat'
    );
  });
});
