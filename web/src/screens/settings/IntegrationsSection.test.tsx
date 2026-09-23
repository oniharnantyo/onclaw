import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom';
import { IntegrationsSection } from './IntegrationsSection';
import {
  adminOAuthAppsApi,
  connectionsApi,
  type ApiConnection,
  type ApiIntegrationRecipe,
} from '../../lib/connectionsApi';
import { api, ApiError } from '../../lib/api';
import { useStore } from '../../store';

const mockTenant = { id: 'acme', sub: 'acme', name: 'Acme Corp' };

// add-recipe-base-url re-scope: the GitLab recipe is HTTP-kind PAT over the
// REST API with a declared origin parameter defaulting to the SaaS origin —
// the streamable_http MCP shape it used to carry is gone.
const gitlabRecipe = (overrides: Partial<ApiIntegrationRecipe> = {}): ApiIntegrationRecipe => ({
  id: 'gitlab',
  service: 'GitLab',
  icon: 'gitlab',
  auth_kind: 'pat',
  availability: 'available',
  kind: 'http',
  base_url: 'https://gitlab.com',
  origin_param: {
    name: 'GitLab instance URL',
    default: 'https://gitlab.com',
    help: 'The GitLab origin — gitlab.com or a self-managed instance serving its REST API under /api/v4.',
  },
  access_levels: ['read_only', 'read_write'],
  steps: [{ title: 'Create a personal access token' }],
  scopes: [
    { access_level: 'read_only', scopes: ['read_api'] },
    { access_level: 'read_write', scopes: ['api'] },
  ],
  token_header: 'PRIVATE-TOKEN',
  verbs: [
    {
      name: 'gitlab.list_projects',
      method: 'GET',
      path: '/api/v4/projects',
      description: 'List projects',
    },
  ],
  probe: { tool: 'gitlab.current_user', method: 'GET', path: '/api/v4/user' },
  ...overrides,
});

const atlassianRecipe = (overrides: Partial<ApiIntegrationRecipe> = {}): ApiIntegrationRecipe => ({
  id: 'atlassian',
  service: 'Atlassian (Jira & Confluence)',
  icon: 'atlassian',
  auth_kind: 'oauth',
  availability: 'coming_soon',
  transport: 'sse',
  access_levels: ['read_only'],
  steps: [],
  scopes: [],
  probe: { tool: 'get-visible-issues' },
  notes: 'Coming soon — requires OAuth sign-in with Atlassian (Jira and Confluence).',
  ...overrides,
});

// add-connection-http: the Figma reference recipe — HTTP kind, pinned base
// URL, auth header, declared verb surface, no MCP transport.
const figmaRecipe = (overrides: Partial<ApiIntegrationRecipe> = {}): ApiIntegrationRecipe => ({
  id: 'figma',
  service: 'Figma',
  icon: 'figma',
  auth_kind: 'pat',
  availability: 'available',
  access_levels: ['read_only', 'read_write'],
  steps: [{ title: 'Create a Figma personal access token' }],
  scopes: [{ access_level: 'read_only', scopes: ['file_dev:read'] }],
  kind: 'http',
  base_url: 'https://api.figma.com',
  token_header: 'X-Figma-Token',
  verbs: [
    { name: 'figma.get_me', method: 'GET', path: '/v1/me', description: 'The authenticated user' },
    {
      name: 'figma.get_file',
      method: 'GET',
      path: '/v1/files/:key',
      description: 'One file by key',
      params: [{ name: 'key', type: 'string', required: true, in: 'path' }],
    },
  ],
  probe: { tool: 'figma.get_me' },
  ...overrides,
});

const recipesFixture = (): ApiIntegrationRecipe[] => [
  gitlabRecipe(),
  {
    id: 'github',
    service: 'GitHub',
    icon: 'github',
    auth_kind: 'pat',
    availability: 'available',
    transport: 'streamable_http',
    access_levels: ['read_only', 'read_write'],
    steps: [{ title: 'Open Developer settings' }],
    scopes: [{ access_level: 'read_only', scopes: ['repo:read'] }],
    probe: { tool: 'list-repositories' },
  },
  atlassianRecipe(),
  {
    id: 'slack',
    service: 'Slack',
    icon: 'slack',
    auth_kind: 'oauth',
    availability: 'coming_soon',
    transport: 'streamable_http',
    access_levels: ['read_only'],
    steps: [],
    scopes: [],
    probe: { tool: 'conversations-history' },
    notes: 'Coming soon — requires OAuth sign-in with Slack.',
  },
];

const connectionRow = (overrides: Partial<ApiConnection> = {}): ApiConnection => ({
  id: 'conn-gh',
  workspace_id: 'acme',
  service: 'github',
  access_level: 'read_only',
  status: 'connected',
  status_error: null,
  token_hint: 'a1b2',
  server_id: 'srv-gh-mcp',
  server_enabled: true,
  tool_count: 24,
  attached_agents: ['Atlas', 'Beacon'],
  created_at: '',
  updated_at: '',
  ...overrides,
});

function renderPane(canWrite?: boolean) {
  return render(
    <MemoryRouter initialEntries={['/settings/integrations']}>
      <Routes>
        <Route path="/settings/integrations" element={<IntegrationsSection tenant={mockTenant} canWrite={canWrite} />} />
        <Route path="/settings/mcp" element={<div data-testid="mcp-target" />} />
      </Routes>
    </MemoryRouter>
  );
}

/** Renders the pane at a deep-linked URL (e.g. the OAuth callback return) and
 * exposes the router location so URL-cleaning assertions can read it. */
function renderPaneAt(url: string, opts: { canWrite?: boolean; onToast?: (t: string, k?: string) => void } = {}) {
  function LocationProbe() {
    const loc = useLocation();
    return <div data-testid="location-probe" data-loc={loc.pathname + (loc.search || '')} />;
  }
  return render(
    <MemoryRouter initialEntries={[url]}>
      <Routes>
        <Route
          path="/settings/integrations"
          element={
            <>
              <IntegrationsSection tenant={mockTenant} canWrite={opts.canWrite} onToast={opts.onToast} />
              <LocationProbe />
            </>
          }
        />
        <Route path="/settings/mcp" element={<div data-testid="mcp-target" />} />
      </Routes>
    </MemoryRouter>
  );
}

/** jsdom navigations need stubbing; same pattern as ErrorBoundary.test. */
function stubLocationAssign() {
  const assignMock = vi.fn();
  Object.defineProperty(window, 'location', {
    value: { ...window.location, assign: assignMock },
    writable: true,
    configurable: true,
  });
  return assignMock;
}

describe('screens/settings/IntegrationsSection', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(connectionsApi, 'recipes').mockResolvedValue({ recipes: recipesFixture() });
    vi.spyOn(connectionsApi, 'list').mockResolvedValue({ connections: [] });
    vi.spyOn(api.agents, 'list').mockResolvedValue({ agents: [] });
    vi.spyOn(api.agents, 'patch').mockResolvedValue({ agent: {} as any });
  });

  it('renders the gallery from the recipes endpoint and the Connected section from connections', async () => {
    vi.spyOn(connectionsApi, 'list').mockResolvedValue({
      connections: [connectionRow()],
    });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('recipe-gitlab')).not.toBeNull();
    });
    // Available recipe shows its Integrate affordance and its declared verb
    // surface (HTTP kind) instead of an MCP transport line.
    expect(screen.getByTestId('btn-integrate-gitlab')).not.toBeNull();
    expect(screen.getByTestId('recipe-gitlab').textContent).toContain('1 tool — gitlab.list_projects');

    // Already-connected service appears in the Connected section with live
    // status, access level, attached agents, and the last-4 hint only.
    const conn = screen.getByTestId('connection-conn-gh');
    expect(conn).not.toBeNull();
    expect(screen.getByTestId('connection-status-conn-gh').textContent).toBe('Connected');
    expect(screen.getByText('Read-only')).not.toBeNull();
    expect(screen.getByTestId('connection-agents-conn-gh').textContent).toBe('Atlas, Beacon');
    expect(screen.getByTestId('connection-hint-conn-gh').textContent).toContain('····a1b2');
    expect(conn.textContent).not.toContain('ghp_');

    // The gallery marks the connected recipe without a second Integrate.
    expect(screen.getByTestId('recipe-github')).not.toBeNull();
    expect(screen.queryByTestId('btn-integrate-github')).toBeNull();

    // Unregistered oauth services sit in the same gallery with an explicit
    // one-time setup hint; Integrate opens the guided setup flow instead of
    // a dead end.
    expect(screen.getByTestId('recipe-atlassian')).not.toBeNull();
    expect(screen.getByTestId('recipe-slack')).not.toBeNull();
    expect(screen.getByTestId('recipe-setup-needed-atlassian').textContent).toContain(
      'One-time instance setup needed'
    );
    expect(screen.getByTestId('recipe-setup-needed-slack').textContent).toContain(
      'signs in with Slack'
    );
    expect(screen.getByTestId('btn-integrate-atlassian')).not.toBeNull();

    // The advanced path links out to MCP management.
    expect(screen.getByTestId('btn-custom-mcp')).not.toBeNull();
  });

  it('shows the empty Connected state before any service is integrated', async () => {
    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('connections-empty')).not.toBeNull();
    });
    expect(screen.getByText('No services connected yet')).not.toBeNull();
  });

  it('routes the Custom MCP card to the existing MCP management pane', async () => {
    render(
      <MemoryRouter initialEntries={['/settings/integrations']}>
        <Routes>
          <Route path="/settings/integrations" element={<IntegrationsSection tenant={mockTenant} />} />
          <Route path="/settings/mcp" element={<div data-testid="mcp-target" />} />
        </Routes>
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByTestId('btn-custom-mcp')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-custom-mcp'));
    expect(screen.getByTestId('mcp-target')).not.toBeNull();
  });

  it('connects through the guided dialog and hands off to agent attachment', async () => {
    const connect = vi.spyOn(connectionsApi, 'connect').mockResolvedValue({
      connection: connectionRow({
        id: 'conn-gl',
        service: 'gitlab',
        access_level: 'read_only',
        // GitLab is HTTP-kind now — no materialized server row.
        server_id: null,
        server_enabled: false,
        origin: 'https://gitlab.com',
      }),
    });
    vi.spyOn(api.agents, 'list').mockResolvedValue({
      agents: [
        { id: 'a1', slug: 'atlas', name: 'Atlas', enabled_mcps: [] },
        { id: 'a2', slug: 'beacon', name: 'Beacon', enabled_mcps: ['conn-gl'] },
      ] as any[],
    });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('btn-integrate-gitlab')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-integrate-gitlab'));

    await waitFor(() => {
      expect(screen.getByTestId('modal-connect-service')).not.toBeNull();
    });
    // Guided steps render from the recipe.
    expect(screen.getByText('Create a personal access token')).not.toBeNull();
    // Read-only is preselected with its recommended scopes displayed.
    expect(screen.getByTestId('connect-access-read_only').getAttribute('aria-pressed')).toBe('true');
    expect(screen.getByTestId('connect-scopes').textContent).toContain('read_api');
    // add-recipe-base-url: the origin field presets the declared SaaS default.
    const originInput = screen.getByTestId('input-connect-origin') as HTMLInputElement;
    expect(originInput.value).toBe('https://gitlab.com');
    expect(screen.getByTestId('modal-connect-service').textContent).toContain('GitLab instance URL');

    // Connect is gated on a token.
    expect((screen.getByTestId('btn-connect-confirm') as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByTestId('input-connect-token'), { target: { value: 'glpat-x' } });
    expect((screen.getByTestId('btn-connect-confirm') as HTMLButtonElement).disabled).toBe(false);

    // Switching the access level swaps the recommended scopes.
    fireEvent.click(screen.getByTestId('connect-access-read_write'));
    expect(screen.getByTestId('connect-scopes').textContent).not.toContain('read_api');
    fireEvent.click(screen.getByTestId('connect-access-read_only'));

    fireEvent.click(screen.getByTestId('btn-connect-confirm'));

    await waitFor(() => {
      expect(connect).toHaveBeenCalledWith('acme', {
        recipe_id: 'gitlab',
        access_level: 'read_only',
        token: 'glpat-x',
        origin: 'https://gitlab.com',
      });
    });

    // Success hand-off lists agents over the existing enabled_mcps attach.
    await waitFor(() => {
      expect(screen.getByTestId('connect-success')).not.toBeNull();
    });
    await waitFor(() => {
      expect(screen.getByRole('switch', { name: 'Attach Atlas' })).not.toBeNull();
    });
    fireEvent.click(screen.getByRole('switch', { name: 'Attach Atlas' }));
    // HTTP-kind connection: attachment rides the connection id, not a server.
    await waitFor(() => {
      expect(api.agents.patch).toHaveBeenCalledWith('acme', 'atlas', {
        enabled_mcps: ['conn-gl'],
      });
    });

    fireEvent.click(screen.getByTestId('btn-connect-done'));
    expect(screen.queryByTestId('modal-connect-service')).toBeNull();
  });

  it('surfaces a probe failure inline so the token can be corrected', async () => {
    vi.spyOn(connectionsApi, 'connect').mockRejectedValue(
      new ApiError(400, 'invalid_request', 'probe failed: Bad credentials')
    );

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('btn-integrate-gitlab')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-integrate-gitlab'));
    await waitFor(() => {
      expect(screen.getByTestId('input-connect-token')).not.toBeNull();
    });
    fireEvent.change(screen.getByTestId('input-connect-token'), { target: { value: 'bad-token' } });
    fireEvent.click(screen.getByTestId('btn-connect-confirm'));

    await waitFor(() => {
      expect(screen.getByTestId('connect-error').textContent).toContain('probe failed: Bad credentials');
    });
    // Nothing stored — the dialog stays open for a corrected token.
    expect(screen.queryByTestId('connect-success')).toBeNull();
    expect(screen.queryByTestId('connection-conn-gl')).toBeNull();
  });

  it('surfaces a duplicate-service conflict from the connect flow', async () => {
    vi.spyOn(connectionsApi, 'connect').mockRejectedValue(
      new ApiError(409, 'conflict', 'GitLab is already connected')
    );

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('btn-integrate-gitlab')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-integrate-gitlab'));
    await waitFor(() => {
      expect(screen.getByTestId('input-connect-token')).not.toBeNull();
    });
    fireEvent.change(screen.getByTestId('input-connect-token'), { target: { value: 'glpat-x' } });
    fireEvent.click(screen.getByTestId('btn-connect-confirm'));

    await waitFor(() => {
      expect(screen.getByTestId('connect-error').textContent).toContain('GitLab is already connected');
    });
  });

  it('disconnects only after a confirmation that states the cascade', async () => {
    vi.spyOn(connectionsApi, 'list').mockResolvedValue({
      connections: [connectionRow()],
    });
    const disconnect = vi.spyOn(connectionsApi, 'disconnect').mockResolvedValue(undefined);
    const onToast = vi.fn();

    render(
      <MemoryRouter initialEntries={['/settings/integrations']}>
        <IntegrationsSection tenant={mockTenant} onToast={onToast} />
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByTestId('btn-connection-disconnect-conn-gh')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-connection-disconnect-conn-gh'));

    // The confirmation names the consequences before the DELETE is sent.
    const modal = screen.getByTestId('modal-connection-disconnect');
    expect(modal.textContent).toContain('materialized MCP server');
    expect(modal.textContent).toContain('cannot be recovered');
    expect(modal.textContent).toContain('Atlas, Beacon');
    expect(modal.textContent).toContain('runs will not fail');
    expect(disconnect).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId('btn-disconnect-cancel'));
    expect(screen.queryByTestId('modal-connection-disconnect')).toBeNull();

    fireEvent.click(screen.getByTestId('btn-connection-disconnect-conn-gh'));
    fireEvent.click(screen.getByTestId('btn-disconnect-confirm'));

    await waitFor(() => {
      expect(disconnect).toHaveBeenCalledWith('acme', 'conn-gh');
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('GitHub disconnected');
      expect(screen.queryByTestId('connection-conn-gh')).toBeNull();
    });
  });

  it('refreshes a connection status through the probe endpoint', async () => {
    vi.spyOn(connectionsApi, 'list').mockResolvedValue({
      connections: [connectionRow({ status: 'error', status_error: 'Bad credentials' })],
    });
    const probe = vi.spyOn(connectionsApi, 'probe').mockResolvedValue({
      connection: connectionRow({ tool_count: 30 }),
    });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('btn-connection-probe-conn-gh')).not.toBeNull();
    });
    expect(screen.getByTestId('connection-status-conn-gh').textContent).toBe('Error');
    fireEvent.click(screen.getByTestId('btn-connection-probe-conn-gh'));

    await waitFor(() => {
      expect(probe).toHaveBeenCalledWith('acme', 'conn-gh');
    });
    await waitFor(() => {
      expect(screen.getByTestId('connection-status-conn-gh').textContent).toBe('Connected');
    });
  });

  it('keeps the gallery read-only for holders without integrations.write', async () => {
    vi.spyOn(connectionsApi, 'list').mockResolvedValue({
      connections: [connectionRow()],
    });

    renderPane(false);

    await waitFor(() => {
      expect(screen.getByTestId('recipe-gitlab')).not.toBeNull();
    });
    expect(screen.queryByTestId('btn-integrate-gitlab')).toBeNull();
    expect(screen.getAllByText('Admins manage connections').length).toBeGreaterThan(0);
    expect(screen.queryByTestId('btn-connection-disconnect-conn-gh')).toBeNull();
    // Reads stay available.
    expect(screen.getByTestId('connection-conn-gh')).not.toBeNull();
    expect(screen.getByTestId('btn-connection-probe-conn-gh')).not.toBeNull();
  });

  it('shows the error state with a retry when the recipes endpoint fails', async () => {
    vi.spyOn(connectionsApi, 'recipes').mockRejectedValue(
      new ApiError(500, 'error', 'registry unavailable')
    );

    renderPane();

    await waitFor(() => {
      expect(screen.getByText("Couldn't load integrations")).not.toBeNull();
    });
    expect(screen.getByText('registry unavailable')).not.toBeNull();

    vi.spyOn(connectionsApi, 'recipes').mockResolvedValue({ recipes: recipesFixture() });
    fireEvent.click(screen.getByText('Retry'));
    await waitFor(() => {
      expect(screen.getByTestId('recipe-gitlab')).not.toBeNull();
    });
  });

  it('guides an unregistered oauth recipe into the inline instance-app setup', async () => {
    vi.spyOn(connectionsApi, 'recipes').mockResolvedValue({
      recipes: [atlassianRecipe({ notes: undefined })],
    });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('recipe-setup-needed-atlassian')).not.toBeNull();
    });
    expect(screen.getByTestId('recipe-setup-needed-atlassian').textContent).toContain(
      'One-time instance setup needed'
    );
    fireEvent.click(screen.getByTestId('btn-integrate-atlassian'));
    await waitFor(() => {
      expect(screen.getByTestId('modal-connect-service')).not.toBeNull();
    });
    // Setup mode. This viewer is a workspace admin, not a master-tenant
    // admin, so the honest ask renders; the fixture carries no public base
    // URL, so the operator prerequisite surfaces too.
    expect(screen.getByTestId('connect-app-setup')).not.toBeNull();
    expect(screen.getByTestId('connect-redirect-missing').textContent).toContain(
      'ONCLAW_PUBLIC_BASE_URL'
    );
    expect(screen.getByTestId('connect-app-needs-admin')).not.toBeNull();
    expect(screen.queryByTestId('btn-save-app')).toBeNull();
  });

  it('switches an oauth dialog from setup to the consent hand-off after the app is saved', async () => {
    const save = vi.spyOn(adminOAuthAppsApi, 'save').mockResolvedValue({
      app: {
        provider: 'atlassian',
        client_id: 'app-9',
        client_secret_hint: 'zz99',
        redirect_uri: 'http://localhost:3000/api/v1/integrations/oauth/callback',
      },
    });
    vi.spyOn(connectionsApi, 'recipes').mockResolvedValue({
      recipes: [
        atlassianRecipe({
          notes: undefined,
          oauth_redirect_uri: 'http://localhost:3000/api/v1/integrations/oauth/callback',
          scopes: [{ access_level: 'read_only', scopes: ['read:jira-user', 'offline_access'] }],
        }),
      ],
    });
    // The setup form is master-tenant-admin gated — render as master admin.
    useStore.setState({
      pos: { tenantId: 'master', view: 'chats', chatId: 'a1', showContext: false },
      db: {
        master: { id: 'master', sub: 'master', name: 'Master', is_master: true, isAdmin: true },
      } as any,
    });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('btn-integrate-atlassian')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-integrate-atlassian'));
    await waitFor(() => {
      expect(screen.getByTestId('input-app-client-id')).not.toBeNull();
    });
    expect(screen.queryByTestId('connect-app-needs-admin')).toBeNull();
    expect((screen.getByTestId('btn-save-app') as HTMLButtonElement).disabled).toBe(true);

    fireEvent.change(screen.getByTestId('input-app-client-id'), { target: { value: 'app-9' } });
    fireEvent.change(screen.getByTestId('input-app-client-secret'), {
      target: { value: 'secret-zz99' },
    });
    expect((screen.getByTestId('btn-save-app') as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(screen.getByTestId('btn-save-app'));

    await waitFor(() => {
      expect(save).toHaveBeenCalledWith('atlassian', {
        client_id: 'app-9',
        client_secret: 'secret-zz99',
      });
    });
    // The dialog drops into the ordinary consent hand-off — no re-open.
    await waitFor(() => {
      expect(screen.getByTestId('connect-oauth-handoff')).not.toBeNull();
    });
    expect(screen.queryByTestId('connect-app-setup')).toBeNull();
  });

  it('connects an oauth recipe through the consent hand-off — no token field', async () => {
    const assignMock = stubLocationAssign();
    const authorizeUrl = 'https://auth.atlassian.com/authorize?client_id=app-1&state=s1';
    const connect = vi.spyOn(connectionsApi, 'connect').mockResolvedValue({ authorize_url: authorizeUrl });
    vi.spyOn(connectionsApi, 'recipes').mockResolvedValue({
      recipes: recipesFixture().map((r) =>
        r.id === 'atlassian'
          ? {
              ...r,
              // Server-side availability: the provider app is registered.
              availability: 'available' as const,
              scopes: [{ access_level: 'read_only', scopes: ['read:jira-user', 'offline_access'] }],
              app_registration_guidance: 'Create an app at developer.atlassian.com and add the redirect URI.',
            }
          : r
      ),
    });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('btn-integrate-atlassian')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-integrate-atlassian'));

    await waitFor(() => {
      expect(screen.getByTestId('modal-connect-service')).not.toBeNull();
    });
    // The consent hand-off replaces the PAT steps and the token input.
    expect(screen.getByTestId('connect-oauth-handoff').textContent).toContain('Sign in with Atlassian');
    expect(screen.queryByTestId('connect-steps')).toBeNull();
    expect(screen.queryByTestId('input-connect-token')).toBeNull();
    // Consent scopes come from the recipe, labeled as approval rather than a
    // recommendation; operator registration copy lives in setup mode only.
    expect(screen.getByTestId('connect-scopes').textContent).toContain('read:jira-user');
    expect(screen.getByTestId('connect-scopes').textContent).toMatch(/approve/i);
    expect(screen.queryByTestId('connect-oauth-guidance')).toBeNull();

    // Connect is NOT gated on a token — there isn't one.
    expect((screen.getByTestId('btn-connect-confirm') as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(screen.getByTestId('btn-connect-confirm'));

    await waitFor(() => {
      expect(connect).toHaveBeenCalledWith('acme', {
        recipe_id: 'atlassian',
        access_level: 'read_only',
      });
    });
    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith(authorizeUrl);
    });
    // No in-dialog success hand-off — the connection activates at the callback.
    expect(screen.queryByTestId('connect-success')).toBeNull();
  });

  it('surfaces a missing-app oauth connect failure inline', async () => {
    vi.spyOn(connectionsApi, 'connect').mockRejectedValue(
      new ApiError(400, 'invalid_request', 'No Atlassian app is registered on this instance')
    );
    vi.spyOn(connectionsApi, 'recipes').mockResolvedValue({
      recipes: recipesFixture().map((r) =>
        r.id === 'atlassian' ? { ...r, availability: 'available' as const } : r
      ),
    });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('btn-integrate-atlassian')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-integrate-atlassian'));
    await waitFor(() => {
      expect(screen.getByTestId('btn-connect-confirm')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-connect-confirm'));

    await waitFor(() => {
      expect(screen.getByTestId('connect-error').textContent).toContain(
        'No Atlassian app is registered on this instance'
      );
    });
    expect(screen.queryByTestId('connect-success')).toBeNull();
  });

  it('names the declared verb surface and kind on HTTP recipe cards', async () => {
    vi.spyOn(connectionsApi, 'recipes').mockResolvedValue({
      recipes: [...recipesFixture(), figmaRecipe()],
    });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('recipe-figma')).not.toBeNull();
    });
    const card = screen.getByTestId('recipe-figma');
    expect(card.textContent).toContain('HTTP');
    // Compact verb-surface copy replaces the (absent) MCP transport line.
    expect(card.textContent).toContain('2 tools — figma.get_me, figma.get_file');
    expect(card.textContent).not.toContain('streamable_http');
    expect(screen.getByTestId('btn-integrate-figma')).not.toBeNull();
    // GitLab rides the HTTP kind too (add-recipe-base-url re-scope): the
    // verb surface replaces the MCP transport line it used to declare.
    expect(screen.getByTestId('recipe-gitlab').textContent).toContain('HTTP');
    expect(screen.getByTestId('recipe-gitlab').textContent).toContain('gitlab.list_projects');
    expect(screen.getByTestId('recipe-gitlab').textContent).not.toContain('streamable_http');
    // MCP-kind cards keep their transport and gain the kind chip.
    expect(screen.getByTestId('recipe-github').textContent).toContain('MCP');
    expect(screen.getByTestId('recipe-github').textContent).toContain('streamable_http');
  });

  it('connects an HTTP-kind recipe with the verb surface shown and attachment by connection id', async () => {
    vi.spyOn(connectionsApi, 'recipes').mockResolvedValue({
      recipes: [...recipesFixture(), figmaRecipe()],
    });
    const connect = vi.spyOn(connectionsApi, 'connect').mockResolvedValue({
      connection: {
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
      },
    });
    vi.spyOn(api.agents, 'list').mockResolvedValue({
      agents: [{ id: 'a1', slug: 'atlas', name: 'Atlas', enabled_mcps: [] }] as any[],
    });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('btn-integrate-figma')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-integrate-figma'));

    await waitFor(() => {
      expect(screen.getByTestId('modal-connect-service')).not.toBeNull();
    });
    // The recipe's guided API-token steps render, and the declared verb
    // surface unfolds from its collapsible with the pinned base URL — no MCP
    // wiring copy.
    expect(screen.getByText('Create a Figma personal access token')).not.toBeNull();
    const verbsToggle = screen.getByTestId('connect-verbs-toggle');
    expect(verbsToggle.getAttribute('aria-expanded')).toBe('false');
    expect(verbsToggle.textContent).toContain('2');
    fireEvent.click(verbsToggle);
    expect(screen.getByTestId('connect-verbs-toggle').getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByTestId('connect-http-surface').textContent).toContain('figma.get_me');
    expect(screen.getByTestId('connect-verb-figma.get_file').textContent).toContain('GET /v1/files/:key');
    expect(screen.getByTestId('connect-http-surface').textContent).toContain('api.figma.com');
    expect(screen.queryByTestId('connect-oauth-handoff')).toBeNull();

    // Connect stays probe-gated on a token, exactly like the PAT flow.
    expect((screen.getByTestId('btn-connect-confirm') as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByTestId('input-connect-token'), { target: { value: 'figd_x' } });
    fireEvent.click(screen.getByTestId('btn-connect-confirm'));

    await waitFor(() => {
      expect(connect).toHaveBeenCalledWith('acme', {
        recipe_id: 'figma',
        access_level: 'read_only',
        token: 'figd_x',
      });
    });

    // HTTP-kind connections have no server_id — attachment stores the
    // connection id in enabled_mcps.
    await waitFor(() => {
      expect(screen.getByTestId('connect-success')).not.toBeNull();
    });
    await waitFor(() => {
      expect(screen.getByRole('switch', { name: 'Attach Atlas' })).not.toBeNull();
    });
    fireEvent.click(screen.getByRole('switch', { name: 'Attach Atlas' }));
    await waitFor(() => {
      expect(api.agents.patch).toHaveBeenCalledWith('acme', 'atlas', {
        enabled_mcps: ['conn-figma'],
      });
    });
  });

  it('shows an HTTP-kind connection with its kind chip and truthful cascade copy', async () => {
    vi.spyOn(connectionsApi, 'disconnect').mockResolvedValue(undefined);
    vi.spyOn(connectionsApi, 'recipes').mockResolvedValue({
      recipes: [...recipesFixture(), figmaRecipe()],
    });
    vi.spyOn(connectionsApi, 'list').mockResolvedValue({
      connections: [
        {
          id: 'conn-figma',
          workspace_id: 'acme',
          service: 'figma',
          access_level: 'read_only',
          status: 'connected',
          status_error: null,
          token_hint: 'f9e8',
          server_id: null,
          server_enabled: false, // structural for HTTP kind — NOT a pause
          tool_count: 2,
          attached_agents: ['Atlas'],
          created_at: '',
          updated_at: '',
        },
      ],
    });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('connection-conn-figma')).not.toBeNull();
    });
    const card = screen.getByTestId('connection-conn-figma');
    expect(card.textContent).toContain('HTTP');
    expect(card.textContent).toContain('····f9e8');
    // Structural server_enabled: false must not read as Paused.
    expect(screen.getByTestId('connection-status-conn-figma').textContent).toBe('Connected');
    expect(card.textContent).not.toContain('Paused');

    fireEvent.click(screen.getByTestId('btn-connection-disconnect-conn-figma'));
    const modal = screen.getByTestId('modal-connection-disconnect');
    // No materialized server exists to remove — the copy stays truthful.
    expect(modal.textContent).not.toContain('materialized MCP server');
    expect(modal.textContent).toContain('cannot be recovered');
    expect(modal.textContent).toContain('Atlas');
    fireEvent.click(screen.getByTestId('btn-disconnect-cancel'));
    expect(screen.queryByTestId('modal-connection-disconnect')).toBeNull();
  });

  it('shows the resolved origin as a display-only chip — no origin edit on existing connections', async () => {
    vi.spyOn(connectionsApi, 'list').mockResolvedValue({
      connections: [
        // A connection resolved to a self-managed origin.
        connectionRow({
          id: 'conn-gl',
          service: 'gitlab',
          server_id: null,
          server_enabled: false,
          origin: 'https://gitlab.example.com',
        }),
        // A connection on its recipe's fixed/default endpoint — no origin.
        connectionRow(),
      ],
    });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('connection-conn-gl')).not.toBeNull();
    });
    // add-recipe-base-url: the origin renders as a monospace chip.
    const chip = screen.getByTestId('connection-origin-conn-gl');
    expect(chip.textContent).toBe('https://gitlab.example.com');
    // Immutability: the origin is display-only — no input or edit affordance
    // anywhere on the card.
    expect(screen.getByTestId('connection-conn-gl').querySelector('input')).toBeNull();
    expect(screen.queryByTestId('input-connect-origin')).toBeNull();
    // Connections without a resolved origin render no chip at all.
    expect(screen.queryByTestId('connection-origin-conn-gh')).toBeNull();
  });

  it('resolves the oauth callback return: refreshes the list, toasts, and cleans the URL', async () => {
    const onToast = vi.fn();
    const listSpy = vi.spyOn(connectionsApi, 'list').mockResolvedValue({
      connections: [
        connectionRow({ id: 'conn-atl', service: 'atlassian', token_hint: 'zz99' }),
      ],
    });

    renderPaneAt('/settings/integrations?oauth=atlassian&status=connected', { onToast });

    // The activated connection appears from the refresh.
    await waitFor(() => {
      expect(screen.getByTestId('connection-conn-atl')).not.toBeNull();
    });
    await waitFor(() => {
      expect(listSpy.mock.calls.length).toBeGreaterThanOrEqual(2); // initial load + callback refresh
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('Atlassian (Jira & Confluence) connected');
    });
    // Query params cleaned with a replace — a reload cannot replay the message.
    await waitFor(() => {
      expect(screen.getByTestId('location-probe').getAttribute('data-loc')).toBe('/settings/integrations');
    });
    expect(screen.queryByTestId('oauth-callback-failure')).toBeNull();
  });

  it('surfaces the oauth callback failure inline with the provider detail and cleans the URL', async () => {
    const onToast = vi.fn();

    renderPaneAt('/settings/integrations?oauth=atlassian&status=failed&detail=Consent%20was%20denied', {
      onToast,
    });

    await waitFor(() => {
      expect(screen.getByTestId('oauth-callback-failure')).not.toBeNull();
    });
    expect(screen.getByTestId('oauth-callback-failure').textContent).toContain(
      "Atlassian (Jira & Confluence) couldn't be connected"
    );
    expect(screen.getByTestId('oauth-callback-failure-detail').textContent).toBe('Consent was denied');
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith("Atlassian (Jira & Confluence) couldn't be connected", 'danger');
    });
    await waitFor(() => {
      expect(screen.getByTestId('location-probe').getAttribute('data-loc')).toBe('/settings/integrations');
    });

    // The inline notice is dismissible.
    fireEvent.click(screen.getByTestId('btn-oauth-failure-dismiss'));
    expect(screen.queryByTestId('oauth-callback-failure')).toBeNull();
  });

  it('renders an expired OAuth connection with a Reauthorize hand-off', async () => {
    const assignMock = stubLocationAssign();
    const authorizeUrl = 'https://auth.atlassian.com/authorize?client_id=app-1&state=r1';
    vi.spyOn(connectionsApi, 'list').mockResolvedValue({
      connections: [
        connectionRow({
          id: 'conn-atl',
          service: 'atlassian',
          status: 'expired',
          status_error: 'token revoked by provider',
        }),
      ],
    });
    const reauthorize = vi.spyOn(connectionsApi, 'reauthorize').mockResolvedValue({ authorize_url: authorizeUrl });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('connection-conn-atl')).not.toBeNull();
    });
    // Distinct expired state, not a plain error.
    expect(screen.getByTestId('connection-status-conn-atl').textContent).toBe('Expired');
    expect(screen.getByTestId('connection-conn-atl').textContent).toContain('token revoked by provider');
    expect(screen.getByTestId('btn-connection-reauthorize-conn-atl')).not.toBeNull();

    fireEvent.click(screen.getByTestId('btn-connection-reauthorize-conn-atl'));

    await waitFor(() => {
      expect(reauthorize).toHaveBeenCalledWith('acme', 'conn-atl');
    });
    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith(authorizeUrl);
    });
  });

  it('keeps the Reauthorize action writer-gated like the other mutations', async () => {
    const reauthorize = vi.spyOn(connectionsApi, 'reauthorize');
    vi.spyOn(connectionsApi, 'list').mockResolvedValue({
      connections: [
        connectionRow({ id: 'conn-atl', service: 'atlassian', status: 'expired', status_error: 'expired' }),
      ],
    });

    renderPane(false);

    await waitFor(() => {
      expect(screen.getByTestId('connection-status-conn-atl').textContent).toBe('Expired');
    });
    expect(screen.queryByTestId('btn-connection-reauthorize-conn-atl')).toBeNull();
    expect(reauthorize).not.toHaveBeenCalled();
  });
});
