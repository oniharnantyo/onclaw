import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  accessLevelLabel,
  canManageIntegrations,
  connectionServiceName,
  connectionStatusView,
  connectionsApi,
  serviceIconKey,
  type ApiConnection,
  type ApiIntegrationRecipe,
} from './connectionsApi';

const recipe: ApiIntegrationRecipe = {
  id: 'github',
  service: 'GitHub',
  icon: 'github',
  auth_kind: 'pat',
  availability: 'available',
  transport: 'streamable_http',
  endpoint: 'https://api.githubcopilot.com/mcp/',
  token_header: 'Authorization',
  token_scheme: 'Bearer',
  access_levels: ['read_only', 'read_write'],
  steps: [{ title: 'Open Developer settings', detail: 'Personal access tokens → Fine-grained', url: 'https://github.com/settings/personal-access-tokens' }],
  scopes: [
    { access_level: 'read_only', scopes: ['repo:read'] },
    { access_level: 'read_write', scopes: ['repo:read', 'repo:write'] },
  ],
  probe: { tool: 'list-repositories' },
};

const connection: ApiConnection = {
  id: 'conn-1',
  workspace_id: 'acme',
  service: 'github',
  access_level: 'read_only',
  status: 'connected',
  status_error: null,
  token_hint: 'a1b2',
  server_id: 'srv-mcp-1',
  server_enabled: true,
  tool_count: 24,
  attached_agents: ['Atlas', 'Beacon'],
  created_at: '',
  updated_at: '',
};

describe('lib/connectionsApi', () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  function mockJson(payload: unknown, status = 200) {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: status >= 200 && status < 300,
      status,
      headers: new Headers({ 'Content-Type': 'application/json' }),
      json: async () => payload,
    } as any);
  }

  function mockNoContent() {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 204,
      headers: new Headers(),
    } as any);
  }

  it('lists recipes under the workspace integrations scope', async () => {
    mockJson({ recipes: [recipe] });
    const res = await connectionsApi.recipes('acme');
    expect(res).toEqual({ recipes: [recipe] });
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/integrations/recipes');
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('GET');
  });

  it('lists, gets, and probes connections', async () => {
    mockJson({ connections: [connection] });
    await expect(connectionsApi.list('acme')).resolves.toEqual({ connections: [connection] });
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/integrations/connections');
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('GET');

    mockJson({ connection });
    await expect(connectionsApi.get('acme', 'conn-1')).resolves.toEqual({ connection });
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/integrations/connections/conn-1');

    const failed: ApiConnection = { ...connection, status: 'error', status_error: 'Bad credentials' };
    mockJson({ connection: failed });
    await expect(connectionsApi.probe('acme', 'conn-1')).resolves.toEqual({ connection: failed });
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/integrations/connections/conn-1/probe');
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');
  });

  it('sends the connect payload as exactly {recipe_id, access_level, token}', async () => {
    mockJson({ connection });
    await connectionsApi.connect('acme', { recipe_id: 'github', access_level: 'read_only', token: 'ghp_x' });
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/integrations/connections');
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');
    expect((globalThis.fetch as any).mock.calls[0][1].body).toBe(
      JSON.stringify({ recipe_id: 'github', access_level: 'read_only', token: 'ghp_x' })
    );
  });

  it('deletes a connection with DELETE and no body', async () => {
    mockNoContent();
    await connectionsApi.disconnect('acme', 'conn-1');
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/integrations/connections/conn-1');
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('DELETE');
  });

  it('propagates the probe-failure envelope with the upstream message', async () => {
    mockJson(
      { error: { code: 'invalid_request', message: 'probe failed: Bad credentials' } },
      400
    );
    await expect(
      connectionsApi.connect('acme', { recipe_id: 'github', access_level: 'read_only', token: 'bad' })
    ).rejects.toMatchObject({ status: 400, code: 'invalid_request', message: 'probe failed: Bad credentials' });
  });

  it('propagates duplicate-service conflicts and permission rejections', async () => {
    mockJson({ error: { code: 'conflict', message: 'GitHub is already connected' } }, 409);
    await expect(
      connectionsApi.connect('acme', { recipe_id: 'github', access_level: 'read_only', token: 'ghp_x' })
    ).rejects.toMatchObject({ status: 409, code: 'conflict' });

    mockJson({ error: { code: 'forbidden', message: 'missing integrations.write' } }, 403);
    await expect(connectionsApi.disconnect('acme', 'conn-1')).rejects.toMatchObject({
      status: 403,
      code: 'forbidden',
    });
  });

  it('labels access levels with the spec vocabulary', () => {
    expect(accessLevelLabel('read_only')).toBe('Read-only');
    expect(accessLevelLabel('read_write')).toBe('Read & write');
    expect(accessLevelLabel(undefined)).toBe('Read-only');
  });

  it('resolves display names and icon keys from the recipe registry', () => {
    expect(connectionServiceName(connection, [recipe])).toBe('GitHub');
    // Unknown recipe id falls back to the raw value.
    expect(connectionServiceName({ service: 'unknown' }, [recipe])).toBe('unknown');
    expect(serviceIconKey('github', 'github')).toBe('terminal');
    expect(serviceIconKey('unknown-id', undefined)).toBe('plug');
  });

  it('maps connection status the same way as the MCP panes', () => {
    expect(connectionStatusView({ status: 'connected', status_error: null, server_enabled: true }).label).toBe('Connected');
    expect(connectionStatusView({ status: 'ok', status_error: null, server_enabled: true }).label).toBe('Connected');
    expect(connectionStatusView({ status: 'error', status_error: 'x', server_enabled: true })).toMatchObject({ label: 'Error', errored: true });
    expect(connectionStatusView({ status: 'unknown', status_error: null, server_enabled: true }).label).toBe('Unknown');
    expect(connectionStatusView({ status: 'connected', status_error: null, server_enabled: false }).label).toBe('Paused');
  });

  it('derives integrations.write like the sibling write-permission helpers', () => {
    const tenant = { id: 'acme', sub: 'acme' };
    const owner = [{ workspace_id: 'acme', role: { is_owner: true, permissions: [] } }];
    const admin = [{ workspace_id: 'acme', role_name: 'Admin', role: { permissions: ['workspace.*'] } }];
    const member = [
      { workspace_id: 'acme', role_name: 'Member', role: { name: 'Member', permissions: ['tools.read'] } },
    ];
    const granted = [
      { workspace_id: 'acme', role_name: 'Sre', role: { name: 'Sre', permissions: ['integrations.write'] } },
    ];
    const toolsOnly = [
      { workspace_id: 'acme', role_name: 'Tooling', role: { name: 'Tooling', permissions: ['tools.write'] } },
    ];

    expect(canManageIntegrations([], tenant)).toBe(true); // offline / mock mode
    expect(canManageIntegrations(owner, tenant)).toBe(true);
    expect(canManageIntegrations(admin, tenant)).toBe(true);
    expect(canManageIntegrations(member, tenant)).toBe(false);
    expect(canManageIntegrations(granted, tenant)).toBe(true);
    // tools.write does NOT imply integrations.write (members-roles spec).
    expect(canManageIntegrations(toolsOnly, tenant)).toBe(false);
    expect(canManageIntegrations(member, { id: 'other' })).toBe(false);
  });
});
