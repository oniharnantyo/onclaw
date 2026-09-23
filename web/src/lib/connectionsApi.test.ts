import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  accessLevelLabel,
  adminOAuthAppsApi,
  canManageIntegrations,
  connectionAttachId,
  connectionKind,
  connectionKindLabel,
  connectionServiceName,
  connectionStatusView,
  connectionsApi,
  recipeKind,
  serviceIconKey,
  verbSurfaceCopy,
  type ApiConnection,
  type ApiIntegrationRecipe,
  type ApiRecipeVerb,
} from './connectionsApi';

const recipe: ApiIntegrationRecipe = {
  id: 'github',
  service: 'GitHub',
  icon: 'github',
  auth_kind: 'pat',
  availability: 'available',
  transport: 'streamable_http',
  endpoint: 'https://api.githubcopilot.com/mcp/',
  // add-recipe-base-url: GitHub declares the optional base-URL parameter.
  origin_param: {
    name: 'GitHub API base URL',
    default: 'https://api.githubcopilot.com',
    help: 'GitHub Enterprise Cloud data-residency hosts follow the copilot-api.<subdomain>.ghe.com pattern (the /mcp/ path is fixed).',
  },
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

// add-connection-http: an HTTP-kind recipe — pinned base URL, auth header,
// and the entire declared verb surface (D1 verbs-only, D6 kind declaration).
const figmaVerbs: ApiRecipeVerb[] = [
  {
    name: 'figma.get_me',
    method: 'GET',
    path: '/v1/me',
    description: 'The authenticated user',
  },
  {
    name: 'figma.get_file',
    method: 'GET',
    path: '/v1/files/:key',
    description: 'One file by key',
    params: [{ name: 'key', type: 'string', required: true, in: 'path' }],
  },
];

const figmaRecipe: ApiIntegrationRecipe = {
  id: 'figma',
  service: 'Figma',
  icon: 'figma',
  auth_kind: 'pat',
  availability: 'available',
  access_levels: ['read_only', 'read_write'],
  steps: [{ title: 'Create a personal access token', url: 'https://www.figma.com/settings' }],
  scopes: [{ access_level: 'read_only', scopes: ['file_dev:read'] }],
  kind: 'http',
  base_url: 'https://api.figma.com',
  token_header: 'X-Figma-Token',
  verbs: figmaVerbs,
  probe: { tool: 'figma.get_me' },
};

// The HTTP-kind joined view: no materialized server, server_enabled false
// structurally (there is no server row to pause).
const figmaConnection: ApiConnection = {
  id: 'conn-figma',
  workspace_id: 'acme',
  service: 'figma',
  access_level: 'read_only',
  status: 'connected',
  status_error: null,
  token_hint: 'f9e8',
  server_id: null,
  server_enabled: false,
  tool_count: 2,
  attached_agents: [],
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

  it('carries origin on the connect payload only for parametrized recipes (add-recipe-base-url)', async () => {
    // An edited origin crosses the wire verbatim.
    mockJson({ connection });
    await connectionsApi.connect('acme', {
      recipe_id: 'gitlab',
      access_level: 'read_only',
      token: 'glpat_x',
      origin: 'https://gitlab.example.com',
    });
    expect((globalThis.fetch as any).mock.calls[0][1].body).toBe(
      JSON.stringify({
        recipe_id: 'gitlab',
        access_level: 'read_only',
        token: 'glpat_x',
        origin: 'https://gitlab.example.com',
      })
    );

    // A cleared field submits empty — the backend resolves the declared default.
    mockJson({ connection });
    await connectionsApi.connect('acme', {
      recipe_id: 'gitlab',
      access_level: 'read_only',
      token: 'glpat_x',
      origin: '',
    });
    expect((globalThis.fetch as any).mock.calls[0][1].body).toBe(
      JSON.stringify({ recipe_id: 'gitlab', access_level: 'read_only', token: 'glpat_x', origin: '' })
    );

    // No origin_param declared — the field stays absent from the payload
    // (the backend would silently ignore it; the request carries only what
    // the recipe declares).
    mockJson({ connection });
    await connectionsApi.connect('acme', { recipe_id: 'github', access_level: 'read_only', token: 'ghp_x' });
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

  it('maps the expired OAuth status to a distinct recoverable state (D6)', () => {
    const view = connectionStatusView({ status: 'expired', status_error: 'token revoked', server_enabled: true });
    expect(view.label).toBe('Expired');
    expect(view.expired).toBe(true);
    // Expired is not a plain error — it is amber and recoverable.
    expect(view.errored).toBe(false);
    expect(view.dot).toBe('bg-warn');
    // Paused still wins over expired (master switch first).
    expect(
      connectionStatusView({ status: 'expired', status_error: null, server_enabled: false }).label
    ).toBe('Paused');
  });

  it('sends the oauth connect payload as exactly {recipe_id, access_level} and returns the authorize URL', async () => {
    mockJson({ authorize_url: 'https://auth.atlassian.com/authorize?client_id=x' });
    const res = await connectionsApi.connect('acme', { recipe_id: 'atlassian', access_level: 'read_only' });
    expect(res.authorize_url).toBe('https://auth.atlassian.com/authorize?client_id=x');
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/integrations/connections');
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');
    expect((globalThis.fetch as any).mock.calls[0][1].body).toBe(
      JSON.stringify({ recipe_id: 'atlassian', access_level: 'read_only' })
    );
  });

  it('posts reauthorize and returns the authorize URL hand-off', async () => {
    mockJson({ authorize_url: 'https://auth.atlassian.com/authorize?state=next' });
    const res = await connectionsApi.reauthorize('acme', 'conn-1');
    expect(res.authorize_url).toBe('https://auth.atlassian.com/authorize?state=next');
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/integrations/connections/conn-1/reauthorize');
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');
  });

  it('lists, gets, and saves instance OAuth apps under the admin scope', async () => {
    const app = {
      provider: 'atlassian',
      client_id: 'client-123',
      client_secret_hint: 'a1b2',
      redirect_uri: 'https://onclaw.example.com/api/v1/integrations/oauth/callback/atlassian',
    };
    mockJson({ apps: [app] });
    const list = await adminOAuthAppsApi.list();
    expect(list).toEqual({ apps: [app] });
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/admin/oauth-apps');
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('GET');

    mockJson({ app });
    await expect(adminOAuthAppsApi.get('atlassian')).resolves.toEqual({ app });
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/admin/oauth-apps/atlassian');
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('GET');

    const body = { client_id: 'client-123', client_secret: 'supersecret' };
    mockJson({ app: { ...app, client_id: body.client_id } });
    await adminOAuthAppsApi.save('atlassian', body);
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/admin/oauth-apps/atlassian');
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('PUT');
    expect((globalThis.fetch as any).mock.calls[0][1].body).toBe(JSON.stringify(body));
  });

  it('propagates the missing-app connect failure naming the registration', async () => {
    mockJson(
      { error: { code: 'invalid_request', message: 'no Atlassian app registered on this instance' } },
      400
    );
    await expect(
      connectionsApi.connect('acme', { recipe_id: 'atlassian', access_level: 'read_only' })
    ).rejects.toMatchObject({ status: 400, message: 'no Atlassian app registered on this instance' });
  });

  it('derives the recipe/connection kind with absent meaning mcp (add-connection-http D6)', () => {
    expect(recipeKind(figmaRecipe)).toBe('http');
    expect(recipeKind({ ...recipe, kind: 'mcp' })).toBe('mcp');
    // Absent kind — every pre-existing recipe — is the MCP kind.
    expect(recipeKind(recipe)).toBe('mcp');
    expect(recipeKind(undefined)).toBe('mcp');

    expect(connectionKind(figmaConnection, [recipe, figmaRecipe])).toBe('http');
    expect(connectionKind(connection, [recipe, figmaRecipe])).toBe('mcp');
    // Unknown recipe falls back to the row's shape: no materialized server = http.
    expect(connectionKind({ service: 'unknown', server_id: null }, [])).toBe('http');
    expect(connectionKind({ service: 'unknown', server_id: 'srv-x' }, [])).toBe('mcp');

    expect(connectionKindLabel('http')).toBe('HTTP');
    expect(connectionKindLabel('mcp')).toBe('MCP');
  });

  it('attaches by server id when one exists and by connection id when it does not', () => {
    expect(connectionAttachId(connection)).toBe('srv-mcp-1');
    // HTTP-kind connections contribute verb tools directly — the toggle
    // stores the raw connection id.
    expect(connectionAttachId(figmaConnection)).toBe('conn-figma');
    expect(connectionAttachId({ id: 'conn-x', server_id: undefined })).toBe('conn-x');
  });

  it('summarizes the declared verb surface compactly for cards', () => {
    expect(verbSurfaceCopy(figmaRecipe)).toBe('2 tools — figma.get_me, figma.get_file');
    // More than three verbs compact with a "+N more" tail; the full list
    // renders in the connect dialog.
    const many = { verbs: figmaVerbs.concat([
      { name: 'figma.list_files', method: 'GET', path: '/v1/projects/:id/files' },
      { name: 'figma.get_comments', method: 'GET', path: '/v1/files/:key/comments' },
    ]) };
    expect(verbSurfaceCopy(many)).toBe('4 tools — figma.get_me, figma.get_file, figma.list_files +1 more');
    expect(verbSurfaceCopy({ verbs: [] })).toBe('');
    expect(verbSurfaceCopy({})).toBe('');
  });

  it('never reports HTTP-kind connections as Paused despite server_enabled: false', () => {
    // server_enabled is structurally false for HTTP kind (no server row) —
    // the probe status alone decides.
    expect(connectionStatusView(figmaConnection, 'http').label).toBe('Connected');
    expect(connectionStatusView({ ...figmaConnection, status: 'error', status_error: 'x' }, 'http')).toMatchObject({
      label: 'Error',
      errored: true,
    });
    // The MCP kind keeps the paused master switch.
    expect(connectionStatusView({ ...connection, server_enabled: false }, 'mcp').label).toBe('Paused');
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
