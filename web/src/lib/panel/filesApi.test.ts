/**
 * @vitest-environment jsdom
 */
// filesApi tests (add-right-panel slice 2 contract, frontend side): URL
// building, bearer-header attachment (the same way lib/api.ts authenticates),
// and the 404 → null mapping.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { agentFileUrl, fetchAgentFile, listAgentDir } from './filesApi';
import { setToken, clearToken } from '../api';

const jsonResponse = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  clearToken();
  fetchMock = vi.fn();
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('lib/panel/filesApi — agentFileUrl', () => {
  it('builds the workspace-files read URL with an encoded relative path', () => {
    expect(agentFileUrl('acme', 'atlas', 'docs/brief.md'))
      .toBe('/api/v1/workspaces/acme/agents/atlas/files?path=docs%2Fbrief.md');
    expect(agentFileUrl('acme', 'atlas', 'a b/notes.md'))
      .toContain('?path=a%20b%2Fnotes.md');
  });

  it('escapes workspace and agent slugs, never the query shape', () => {
    expect(agentFileUrl('ws x', 'a/gent', 'p'))
      .toBe('/api/v1/workspaces/ws%20x/agents/a%2Fgent/files?path=p');
  });
});

describe('lib/panel/filesApi — fetchAgentFile', () => {
  it('fetches bytes and reports the content type with the bearer header attached', async () => {
    setToken('jwt-1');
    fetchMock.mockResolvedValue(new Response('hello', { status: 200, headers: { 'Content-Type': 'text/markdown' } }));
    const out = await fetchAgentFile('acme', 'atlas', 'docs/brief.md');
    expect(out).toEqual({ bytes: new TextEncoder().encode('hello').buffer, contentType: 'text/markdown' });
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toBe('/api/v1/workspaces/acme/agents/atlas/files?path=docs%2Fbrief.md');
    expect((init.headers as Headers).get('Authorization')).toBe('Bearer jwt-1');
  });

  it('maps 404 to null (missing or escaped path — the API refuses to distinguish)', async () => {
    fetchMock.mockResolvedValue(new Response('not found', { status: 404 }));
    expect(await fetchAgentFile('acme', 'atlas', '../escape')).toBeNull();
  });

  it('throws on other failures so the caller can render its error state', async () => {
    fetchMock.mockResolvedValue(new Response('boom', { status: 500 }));
    await expect(fetchAgentFile('acme', 'atlas', 'x')).rejects.toThrow('files api: 500');
  });
});

describe('lib/panel/filesApi — listAgentDir', () => {
  it('lists one directory level with mode=list', async () => {
    fetchMock.mockResolvedValue(jsonResponse([{ name: 'notes.md', kind: 'file', size: 12, modified: '2026-09-22T00:00:00Z' }]));
    const rows = await listAgentDir('acme', 'atlas', 'docs');
    expect(rows).toEqual([{ name: 'notes.md', kind: 'file', size: 12, modified: '2026-09-22T00:00:00Z' }]);
    const [url] = fetchMock.mock.calls[0];
    expect(String(url)).toContain('path=docs&mode=list');
  });

  it('maps 404 to null for a missing directory or a file-list request', async () => {
    fetchMock.mockResolvedValue(new Response('not found', { status: 404 }));
    expect(await listAgentDir('acme', 'atlas', 'missing')).toBeNull();
  });
});
