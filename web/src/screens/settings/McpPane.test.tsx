import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { McpPane } from './McpPane';
import { canWriteTools } from '../../lib/tools';
import { api, ApiError, type ApiMcpServer } from '../../lib/api';

const mockTenant = { id: 'acme', sub: 'acme', name: 'Acme Corp' };

const serverRow = (overrides: Partial<ApiMcpServer> = {}): ApiMcpServer => ({
  id: 'srv-gh',
  workspace_id: 'acme',
  name: 'GitHub',
  transport: 'stdio',
  command: 'npx',
  args: ['-y', '@modelcontextprotocol/server-github'],
  env: [{ name: 'GITHUB_TOKEN', value_hint: 'ghp1' }],
  enabled: true,
  status: 'connected',
  status_error: null,
  tool_count: 24,
  created_at: '',
  updated_at: '',
  ...overrides,
});

const erroredRow = () =>
  serverRow({
    id: 'srv-broken',
    name: 'Broken DB',
    status: 'error',
    status_error: 'Connection refused',
    tool_count: 0,
  });

const OAUTH_REQUIRED_DETAIL =
  'no usable OAuth credential is stored for this server — start (or re-start) its sign-in from the MCP servers pane';

const oauthRow = (overrides: Partial<ApiMcpServer> = {}): ApiMcpServer =>
  serverRow({
    id: 'srv-notion',
    name: 'Notion',
    transport: 'streamable_http',
    url: 'https://mcp.notion.com/mcp',
    auth_mode: 'oauth',
    status: 'unknown',
    status_error: null,
    command: undefined,
    args: undefined,
    env: undefined,
    tool_count: 0,
    ...overrides,
  });

const agentsRows = () =>
  [
    { id: 'a1', name: 'Atlas', slug: 'atlas', enabled_mcps: ['srv-gh'] },
    { id: 'a2', name: 'Beacon', slug: 'beacon', enabled_mcps: ['srv-gh'] },
    { id: 'a3', name: 'Cedar', slug: 'cedar' },
  ] as any[];

/** The pane mounts inside the app's router (the OAuth callback return is
 * resolved through the query string); every render needs the context. */
function renderPane(opts: { canWrite?: boolean; onToast?: (t: string, k?: string) => void } = {}) {
  return renderPaneAt('/settings/mcp', opts);
}

/** Renders the pane at a deep-linked URL (e.g. the OAuth callback return) and
 * exposes the router location so URL-cleaning assertions can read it. */
function renderPaneAt(
  url: string,
  opts: { canWrite?: boolean; onToast?: (t: string, k?: string) => void } = {}
) {
  function LocationProbe() {
    const loc = useLocation();
    return <div data-testid="location-probe" data-loc={loc.pathname + (loc.search || '')} />;
  }
  return render(
    <MemoryRouter initialEntries={[url]}>
      <Routes>
        <Route
          path="/settings/mcp"
          element={
            <>
              <McpPane tenant={mockTenant} canWrite={opts.canWrite} onToast={opts.onToast} />
              <LocationProbe />
            </>
          }
        />
      </Routes>
    </MemoryRouter>
  );
}

/** jsdom navigations need stubbing; same pattern as IntegrationsSection.test. */
function stubLocationAssign() {
  const assignMock = vi.fn();
  Object.defineProperty(window, 'location', {
    value: { ...window.location, assign: assignMock },
    writable: true,
    configurable: true,
  });
  return assignMock;
}

describe('screens/settings/McpPane', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(api.agents, 'list').mockResolvedValue({ agents: [] });
  });

  it('renders registry cards from the workspace MCP endpoints', async () => {
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [serverRow(), erroredRow()] });
    vi.spyOn(api.agents, 'list').mockResolvedValue({ agents: agentsRows() });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('mcp-srv-gh')).not.toBeNull();
    });
    expect(screen.getByText('GitHub')).not.toBeNull();
    expect(screen.getAllByText('stdio').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByTestId('mcp-status-srv-gh').textContent).toBe('Connected');
    expect(screen.getByText('24 tools')).not.toBeNull();
    // Used-by counts both the enabled_mcps and legacy mcp reference shapes.
    expect(screen.getByText('2 agents')).not.toBeNull();

    expect(screen.getByText('Broken DB')).not.toBeNull();
    expect(screen.getByTestId('mcp-status-srv-broken').textContent).toBe('Error');
    expect(screen.getByText('Connection refused')).not.toBeNull();
    expect(screen.getByTestId('mcp-retry-srv-broken')).not.toBeNull();
  });

  it('renders a paused server dimmed and flags referencing agents as losing tools', async () => {
    vi.spyOn(api.mcp, 'list').mockResolvedValue({
      servers: [serverRow({ enabled: false })],
    });
    vi.spyOn(api.agents, 'list').mockResolvedValue({
      agents: [{ id: 'a1', name: 'Atlas', slug: 'atlas', enabled_mcps: ['srv-gh'] }] as any[],
    });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('mcp-srv-gh')).not.toBeNull();
    });
    expect(screen.getByTestId('mcp-status-srv-gh').textContent).toBe('Paused');
    expect(screen.getByTestId('mcp-srv-gh').className).toContain('opacity-70');
    const usage = screen.getByText('1 agent · loses tools');
    // The tooltip carries the referencing agent's name.
    expect(usage.parentElement?.getAttribute('title')).toBe('Atlas');
  });

  it('pause/resume toggles the master switch via PATCH', async () => {
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [serverRow()] });
    const update = vi.spyOn(api.mcp, 'update').mockResolvedValue({
      server: serverRow({ enabled: false }),
    });
    const onToast = vi.fn();

    renderPane({ onToast });

    await waitFor(() => {
      expect(screen.getByTestId('mcp-srv-gh')).not.toBeNull();
    });
    fireEvent.click(screen.getByRole('switch', { name: 'Enable GitHub' }));

    await waitFor(() => {
      expect(update).toHaveBeenCalledWith('acme', 'srv-gh', { enabled: false });
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('GitHub paused — agents lose access on the next run');
    });
    await waitFor(() => {
      expect(screen.getByTestId('mcp-status-srv-gh').textContent).toBe('Paused');
    });
  });

  it('retry re-probes an errored server and flips the card back to Connected', async () => {
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [erroredRow()] });
    const probe = vi.spyOn(api.mcp, 'probe').mockResolvedValue({
      server: serverRow({ id: 'srv-broken', name: 'Broken DB', tool_count: 8 }),
    });
    const onToast = vi.fn();

    renderPane({ onToast });

    await waitFor(() => {
      expect(screen.getByTestId('mcp-retry-srv-broken')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('mcp-retry-srv-broken'));

    await waitFor(() => {
      expect(probe).toHaveBeenCalledWith('acme', 'srv-broken');
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('Broken DB connected — 8 tools exposed');
    });
    await waitFor(() => {
      expect(screen.getByTestId('mcp-status-srv-broken').textContent).toBe('Connected');
      expect(screen.queryByTestId('mcp-retry-srv-broken')).toBeNull();
    });
  });

  it('surfaces a failed re-probe inline and as a danger toast', async () => {
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [erroredRow()] });
    vi.spyOn(api.mcp, 'probe').mockResolvedValue({
      server: serverRow({ id: 'srv-broken', name: 'Broken DB', status: 'error', status_error: 'still down', tool_count: 0 }),
    });
    const onToast = vi.fn();

    renderPane({ onToast });

    await waitFor(() => {
      expect(screen.getByTestId('mcp-retry-srv-broken')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('mcp-retry-srv-broken'));

    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('Broken DB: still down', 'danger');
    });
    expect(screen.getByTestId('mcp-status-srv-broken').textContent).toBe('Error');
    expect(screen.getByText('still down')).not.toBeNull();
  });

  it('expands a server to read-only tool chips, falling back to the count', async () => {
    vi.spyOn(api.mcp, 'list').mockResolvedValue({
      servers: [serverRow(), serverRow({ id: 'srv-count', name: 'NoNames', tool_count: 3 })],
    });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('mcp-srv-gh')).not.toBeNull();
    });

    // Rich row: chips when the response carries the probed names.
    fireEvent.click(screen.getByTestId('mcp-expand-srv-gh'));
    expect(screen.getByTestId('mcp-srv-gh').textContent).toContain('Tools exposed');

    // Count-only row: the contract carries tool_count, so the note explains.
    fireEvent.click(screen.getByTestId('mcp-expand-srv-count'));
    expect(screen.getByTestId('mcp-srv-count').textContent).toContain(
      '3 tools are exposed to opted-in agents'
    );
  });

  it('deletes a server through a confirmation that names referencing agents', async () => {
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [serverRow()] });
    vi.spyOn(api.agents, 'list').mockResolvedValue({ agents: agentsRows() });
    const del = vi.spyOn(api.mcp, 'delete').mockResolvedValue(undefined);
    const onToast = vi.fn();

    renderPane({ onToast });

    await waitFor(() => {
      expect(screen.getByTestId('mcp-delete-srv-gh')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('mcp-delete-srv-gh'));

    // Confirmation names the referencing agents before the delete is sent.
    expect(screen.getByTestId('modal-mcp-delete').textContent).toContain('Atlas, Beacon');
    expect(del).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId('btn-mcp-delete-cancel'));
    expect(screen.queryByTestId('modal-mcp-delete')).toBeNull();

    fireEvent.click(screen.getByTestId('mcp-delete-srv-gh'));
    fireEvent.click(screen.getByTestId('btn-mcp-delete-confirm'));

    await waitFor(() => {
      expect(del).toHaveBeenCalledWith('acme', 'srv-gh');
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('GitHub deleted');
    });
    await waitFor(() => {
      expect(screen.queryByTestId('mcp-srv-gh')).toBeNull();
    });
  });

  it('adds a server through the structured dialog', async () => {
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [serverRow()] });
    const create = vi.spyOn(api.mcp, 'create').mockResolvedValue({
      server: serverRow({
        id: 'srv-new',
        name: 'Linear',
        transport: 'streamable_http',
        url: 'https://mcp.linear.app/mcp',
        command: undefined,
        args: undefined,
        env: undefined,
      }),
    });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('btn-mcp-add')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-mcp-add'));
    await waitFor(() => {
      expect(screen.getByTestId('modal-mcp-server')).not.toBeNull();
    });

    fireEvent.change(screen.getByLabelText('Server name'), { target: { value: 'Linear' } });
    fireEvent.change(screen.getByLabelText('Transport'), { target: { value: 'streamable_http' } });
    fireEvent.change(screen.getByLabelText('URL'), { target: { value: 'https://mcp.linear.app/mcp' } });
    fireEvent.click(screen.getByTestId('btn-mcp-add-confirm'));

    await waitFor(() => {
      expect(create).toHaveBeenCalledWith('acme', {
        name: 'Linear',
        transport: 'streamable_http',
        url: 'https://mcp.linear.app/mcp',
      });
    });
    await waitFor(() => {
      expect(screen.queryByTestId('modal-mcp-server')).toBeNull();
      expect(screen.getByTestId('mcp-srv-new')).not.toBeNull();
    });
  });

  it('renders the empty state with an add affordance for writers', async () => {
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [] });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('mcp-empty')).not.toBeNull();
    });
    expect(screen.getByText('No MCP servers configured')).not.toBeNull();
    expect(screen.getByTestId('btn-mcp-empty-add')).not.toBeNull();
  });

  it('shows the load error state with a retry action', async () => {
    vi.spyOn(api.mcp, 'list').mockRejectedValue(new ApiError(500, 'error', 'database unreachable'));

    renderPane();

    await waitFor(() => {
      expect(screen.getByText("Couldn't load MCP servers")).not.toBeNull();
    });
    expect(screen.getByText('database unreachable')).not.toBeNull();
  });

  it('hides every write control from holders without tools.write but keeps reads', async () => {
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [serverRow(), erroredRow()] });

    renderPane({ canWrite: false });

    await waitFor(() => {
      expect(screen.getByTestId('mcp-srv-gh')).not.toBeNull();
    });
    expect(screen.queryByTestId('btn-mcp-add')).toBeNull();
    expect(screen.queryByRole('switch', { name: 'Enable GitHub' })).toBeNull();
    expect(screen.queryByTestId('btn-edit-srv-gh')).toBeNull();
    expect(screen.queryByTestId('mcp-delete-srv-gh')).toBeNull();
    expect(screen.queryByTestId('mcp-retry-srv-broken')).toBeNull();
    expect(screen.queryByTestId('btn-mcp-empty-add')).toBeNull();

    // Cards and the expandable tool list still render.
    fireEvent.click(screen.getByTestId('mcp-expand-srv-gh'));
    expect(screen.getByTestId('mcp-srv-gh').textContent).toContain('Tools exposed');
  });

  it('derives tools.write from workspace role permissions', () => {
    // Exported helper: mirrors lib/skills.ts semantics.
    const owner = [{ workspace_id: 'acme', role: { is_owner: true, permissions: [] } }];
    const admin = [{ workspace_id: 'acme', role_name: 'Admin', role: { permissions: ['workspace.*'] } }];
    const member = [
      { workspace_id: 'acme', role_name: 'Member', role: { name: 'Member', permissions: ['tools.read'] } },
    ];
    const customWriter = [
      { workspace_id: 'acme', role_name: 'Sre', role: { name: 'Sre', permissions: ['tools.write'] } },
    ];

    expect(canWriteTools([], mockTenant)).toBe(true); // offline/mock mode
    expect(canWriteTools(owner, mockTenant)).toBe(true);
    expect(canWriteTools(admin, mockTenant)).toBe(true);
    expect(canWriteTools(member, mockTenant)).toBe(false);
    expect(canWriteTools(customWriter, mockTenant)).toBe(true);
    expect(canWriteTools(member, { id: 'other' })).toBe(false);
  });

  // -------------------------------------------------------------------------
  // OAuth rows (add-mcp-oauth-client 7.1)
  // -------------------------------------------------------------------------

  it('offers the sign-in affordance on an unconnected oauth row and hands off to the provider', async () => {
    const assignMock = stubLocationAssign();
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [oauthRow()] });
    const authorize = vi
      .spyOn(api.mcp, 'authorize')
      .mockResolvedValue({ authorize_url: 'https://auth.notion.com/authorize?client_id=x' });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('mcp-srv-notion')).not.toBeNull();
    });
    // No Retry on an oauth row that has not signed in yet — the probe cannot
    // succeed without a token.
    expect(screen.queryByTestId('mcp-retry-srv-notion')).toBeNull();

    const btn = screen.getByTestId('mcp-oauth-srv-notion');
    expect(btn.textContent).toBe('Sign in');
    fireEvent.click(btn);

    await waitFor(() => {
      expect(authorize).toHaveBeenCalledWith('acme', 'srv-notion');
    });
    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith('https://auth.notion.com/authorize?client_id=x');
    });
  });

  it('marks an expired oauth row with the expired chip and a Re-authorize action', async () => {
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [oauthRow({ status: 'expired' })] });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('mcp-srv-notion')).not.toBeNull();
    });
    const status = screen.getByTestId('mcp-status-srv-notion');
    expect(status.textContent).toBe('Expired');
    // Warning tone, matching the connections pane's expired convention.
    expect(status.className).toContain('warn');

    expect(screen.getByTestId('mcp-oauth-srv-notion').textContent).toBe('Re-authorize');
    // Expired is recoverable through re-consent, not a re-probe.
    expect(screen.queryByTestId('mcp-retry-srv-notion')).toBeNull();
  });

  it('renders the needs-authorization detail with a sign-in action on a probe without a token', async () => {
    vi.spyOn(api.mcp, 'list').mockResolvedValue({
      servers: [oauthRow({ status: 'error', status_error: `mcp server requires OAuth authorization: ${OAUTH_REQUIRED_DETAIL}` })],
    });

    renderPane();

    await waitFor(() => {
      expect(screen.getByTestId('mcp-srv-notion')).not.toBeNull();
    });
    expect(screen.getByTestId('mcp-status-srv-notion').textContent).toBe('Error');
    // The trimmed actionable guidance replaces the typed prefix; the full
    // detail stays on the title tooltip.
    expect(screen.getByTestId('mcp-srv-notion').textContent).toContain(OAUTH_REQUIRED_DETAIL);
    expect(screen.getByTestId('mcp-oauth-srv-notion').textContent).toBe('Sign in');
    expect(screen.queryByTestId('mcp-retry-srv-notion')).toBeNull();
  });

  it('surfaces a failed sign-in begin as a danger toast', async () => {
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [oauthRow()] });
    vi.spyOn(api.mcp, 'authorize').mockRejectedValue(
      new ApiError(422, 'unprocessable', 'discovery failed — verify the server URL')
    );
    const onToast = vi.fn();

    renderPane({ onToast });

    await waitFor(() => {
      expect(screen.getByTestId('mcp-oauth-srv-notion')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('mcp-oauth-srv-notion'));

    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('discovery failed — verify the server URL', 'danger');
    });
  });

  it('resolves the OAuth callback return: refreshes, toasts, and cleans the URL', async () => {
    const onToast = vi.fn();
    const listSpy = vi
      .spyOn(api.mcp, 'list')
      .mockResolvedValue({ servers: [oauthRow({ status: 'connected', tool_count: 6 })] });

    renderPaneAt('/settings/mcp?mcp_oauth=srv-notion&status=connected', { onToast });

    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('Notion connected');
    });
    // Initial load + the callback refresh.
    await waitFor(() => {
      expect(listSpy.mock.calls.length).toBeGreaterThanOrEqual(2);
    });
    // Query params cleaned with a replace — a reload cannot replay the toast.
    await waitFor(() => {
      expect(screen.getByTestId('location-probe').getAttribute('data-loc')).toBe('/settings/mcp');
    });
    await waitFor(() => {
      expect(screen.getByTestId('mcp-status-srv-notion').textContent).toBe('Connected');
    });
  });

  it('surfaces the OAuth callback failure with the provider detail and cleans the URL', async () => {
    const onToast = vi.fn();
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [oauthRow()] });

    renderPaneAt('/settings/mcp?mcp_oauth=srv-notion&status=failed&detail=Consent%20was%20denied', {
      onToast,
    });

    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith("Notion couldn't be authorized — Consent was denied", 'danger');
    });
    await waitFor(() => {
      expect(screen.getByTestId('location-probe').getAttribute('data-loc')).toBe('/settings/mcp');
    });
  });

  it('hides the OAuth affordance once the row is connected and from non-writers', async () => {
    vi.spyOn(api.mcp, 'list').mockResolvedValue({
      servers: [oauthRow({ status: 'connected', tool_count: 6 })],
    });

    renderPane({ canWrite: false });

    await waitFor(() => {
      expect(screen.getByTestId('mcp-srv-notion')).not.toBeNull();
    });
    expect(screen.getByTestId('mcp-status-srv-notion').textContent).toBe('Connected');
    expect(screen.queryByTestId('mcp-oauth-srv-notion')).toBeNull();
  });
});
