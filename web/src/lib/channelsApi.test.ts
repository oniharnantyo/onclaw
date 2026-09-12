/**
 * @vitest-environment node
 */
// Wire mapping for the channels client (change integrate-agent-channels):
// paths, methods, query cursors, and snake_case payloads must match the
// backend routes (internal/server/router.go) and JSON tags
// (internal/domain/channels.go, handlers.channelMemberView).
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { api, ApiError, setToken, type ApiChannel, type ApiChannelMessage, type ApiChannelMember } from './api';

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
  setToken('jwt-channels');
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

const wireChannel: ApiChannel = {
  id: 'ch-1', workspace_id: 'ws-1', name: 'Production Ops', slug: 'ops',
  purpose: 'Production ops & alerting', conventions: '', created_by: 'u-1',
  created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
};

// Roster read view — handlers.channelMemberView: no workspace_id, resolved
// display_name/handle.
const wireMember: ApiChannelMember = {
  id: 'cm-1', channel_id: 'ch-1', member_type: 'agent', agent_id: 'a-1',
  display_name: 'Atlas', handle: 'atlas', specialization: 'Alert triage',
  added_at: '2026-09-01T00:00:00Z',
};

const wireMessage: ApiChannelMessage = {
  id: 'm-7', workspace_id: 'ws-1', channel_id: 'ch-1', seq: 7,
  author_type: 'user', author_user_id: 'u-1', body: 'hello @atlas',
  mentions: [{ type: 'agent', id: 'a-1', handle: 'atlas' }],
  chain_depth: 0, created_at: '2026-09-01T00:00:00Z',
};

describe('channels client — CRUD wire mapping', () => {
  it('lists channels on the workspace-scoped path with the bearer token', async () => {
    reply({ channels: [wireChannel] });
    const res = await api.channels.list('acme');
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toBe('/api/v1/workspaces/acme/channels');
    expect(init.method).toBe('GET');
    expect((init.headers as Headers).get('Authorization')).toBe('Bearer jwt-channels');
    expect(res.channels[0]).toEqual(wireChannel);
  });

  it('creates with name + slug (D15 split) and returns the {channel} envelope', async () => {
    reply({ channel: wireChannel });
    const res = await api.channels.create('acme', { name: 'Production Ops', slug: 'ops', purpose: 'ops' });
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toBe('/api/v1/workspaces/acme/channels');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body)).toEqual({ name: 'Production Ops', slug: 'ops', purpose: 'ops' });
    expect(res.channel.slug).toBe('ops');
  });

  it('addresses one channel by id for get/update/delete', async () => {
    reply({ channel: wireChannel });
    await api.channels.get('acme', 'ch 1');
    expect(String(fetchMock.mock.calls[0][0])).toBe('/api/v1/workspaces/acme/channels/ch%201');

    reply({ channel: wireChannel });
    await api.channels.update('acme', 'ch-1', { purpose: 'alerting' });
    const [patchUrl, patchInit] = fetchMock.mock.calls[1];
    expect(String(patchUrl)).toBe('/api/v1/workspaces/acme/channels/ch-1');
    expect(patchInit.method).toBe('PATCH');
    expect(JSON.parse(patchInit.body)).toEqual({ purpose: 'alerting' });

    reply(undefined, { status: 204 });
    await expect(api.channels.delete('acme', 'ch-1')).resolves.toBeUndefined();
    expect(fetchMock.mock.calls[2][1].method).toBe('DELETE');
  });
});

describe('channels client — membership wire mapping', () => {
  it('lists the heterogeneous roster ({members} of read views)', async () => {
    reply({ members: [wireMember] });
    const res = await api.channels.members.list('acme', 'ch-1');
    expect(String(fetchMock.mock.calls[0][0])).toBe('/api/v1/workspaces/acme/channels/ch-1/members');
    expect(res.members[0]).toEqual(wireMember);
    expect(res.members[0]).not.toHaveProperty('workspace_id');
  });

  it('adds with member_type + exactly one reference id + specialization', async () => {
    reply({ member: wireMember });
    const res = await api.channels.members.add('acme', 'ch-1', {
      member_type: 'agent', agent_id: 'a-1', specialization: 'Alert triage',
    });
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toBe('/api/v1/workspaces/acme/channels/ch-1/members');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body)).toEqual({
      member_type: 'agent', agent_id: 'a-1', specialization: 'Alert triage',
    });
    expect(res.member.handle).toBe('atlas');
  });

  it('patches specialization on the member row and removes by member id', async () => {
    reply({ member: wireMember });
    await api.channels.members.patch('acme', 'ch-1', 'cm-1', { specialization: 'Incident comms' });
    const [patchUrl, patchInit] = fetchMock.mock.calls[0];
    expect(String(patchUrl)).toBe('/api/v1/workspaces/acme/channels/ch-1/members/cm-1');
    expect(patchInit.method).toBe('PATCH');
    expect(JSON.parse(patchInit.body)).toEqual({ specialization: 'Incident comms' });

    reply(undefined, { status: 204 });
    await api.channels.members.remove('acme', 'ch-1', 'cm-1');
    expect(fetchMock.mock.calls[1][1].method).toBe('DELETE');
    expect(String(fetchMock.mock.calls[1][0])).toBe('/api/v1/workspaces/acme/channels/ch-1/members/cm-1');
  });
});

describe('channels client — feed wire mapping', () => {
  it('lists messages ascending with the seq cursor as ?after=&limit=', async () => {
    reply({ messages: [wireMessage] });
    const res = await api.channels.messages.list('acme', 'ch-1', { after: 6, limit: 50 });
    expect(String(fetchMock.mock.calls[0][0])).toBe(
      '/api/v1/workspaces/acme/channels/ch-1/messages?after=6&limit=50'
    );
    expect(res.messages[0].seq).toBe(7);
  });

  it('omits the query string when no cursor options are given', async () => {
    reply({ messages: [] });
    await api.channels.messages.list('acme', 'ch-1');
    expect(String(fetchMock.mock.calls[0][0])).toBe('/api/v1/workspaces/acme/channels/ch-1/messages');
  });

  it('posts {body} only — the wire request carries no other fields', async () => {
    reply({ message: wireMessage });
    const res = await api.channels.messages.post('acme', 'ch-1', { body: 'hello @atlas' });
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toBe('/api/v1/workspaces/acme/channels/ch-1/messages');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body)).toEqual({ body: 'hello @atlas' });
    expect(res.message).toEqual(wireMessage);
  });

  it('parses the error envelope into ApiError like every other client call', async () => {
    fetchMock.mockResolvedValueOnce({
      ok: false,
      status: 409,
      headers: new Headers({ 'Content-Type': 'application/json' }),
      json: async () =>
        ({ error: { code: 'conflict', message: 'channel slug already taken' } }),
    });
    const err = await api.channels
      .create('acme', { name: 'Ops', slug: 'ops' })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(409);
    expect((err as ApiError).code).toBe('conflict');
    expect((err as ApiError).message).toBe('channel slug already taken');
  });
});
