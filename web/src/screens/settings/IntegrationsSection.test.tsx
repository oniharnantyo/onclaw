import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom';
import { IntegrationsSection } from './IntegrationsSection';
import {
  connectionsApi,
  type ApiConnection,
  type ApiIntegrationRecipe,
} from '../../lib/connectionsApi';
import { api, ApiError } from '../../lib/api';

const mockTenant = { id: 'acme', sub: 'acme', name: 'Acme Corp' };

const gitlabRecipe = (overrides: Partial<ApiIntegrationRecipe> = {}): ApiIntegrationRecipe => ({
  id: 'gitlab',
  service: 'GitLab',
  icon: 'gitlab',
  auth_kind: 'pat',
  availability: 'available',
  transport: 'streamable_http',
  access_levels: ['read_only', 'read_write'],
  steps: [{ title: 'Create a personal access token' }],
  scopes: [
    { access_level: 'read_only', scopes: ['read_api'] },
    { access_level: 'read_write', scopes: ['api'] },
  ],
  probe: { tool: 'list-projects' },
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
    // Available recipe shows its Integrate affordance and its transport.
    expect(screen.getByTestId('btn-integrate-gitlab')).not.toBeNull();
    expect(screen.getByTestId('recipe-gitlab').textContent).toContain('streamable_http');

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

    // Coming-soon services render disabled and truthful — no connect flow.
    expect(screen.getByTestId('recipe-coming-soon-atlassian')).not.toBeNull();
    expect(screen.getByTestId('recipe-coming-soon-slack')).not.toBeNull();
    expect(screen.getAllByText(/OAuth sign-in/).length).toBe(2);
    expect(screen.queryByTestId('btn-integrate-atlassian')).toBeNull();

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
        server_id: 'srv-gl-mcp',
      }),
    });
    vi.spyOn(api.agents, 'list').mockResolvedValue({
      agents: [
        { id: 'a1', slug: 'atlas', name: 'Atlas', enabled_mcps: [] },
        { id: 'a2', slug: 'beacon', name: 'Beacon', enabled_mcps: ['srv-gl-mcp'] },
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
    await waitFor(() => {
      expect(api.agents.patch).toHaveBeenCalledWith('acme', 'atlas', {
        enabled_mcps: ['srv-gl-mcp'],
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
    expect(screen.getByText('Admins manage connections')).not.toBeNull();
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

  it('keeps the oauth coming-soon card truthful about the missing instance app', async () => {
    vi.spyOn(connectionsApi, 'recipes').mockResolvedValue({
      recipes: [atlassianRecipe({ notes: undefined })],
    });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('recipe-coming-soon-atlassian')).not.toBeNull();
    });
    expect(screen.getByTestId('recipe-coming-soon-atlassian').textContent).toContain(
      'register the app under Admin → OAuth apps'
    );
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
    // recommendation, and operator guidance renders as a note.
    expect(screen.getByTestId('connect-scopes').textContent).toContain('read:jira-user');
    expect(screen.getByTestId('connect-scopes').textContent).toMatch(/approve/i);
    expect(screen.getByTestId('connect-oauth-guidance').textContent).toContain('developer.atlassian.com');

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
