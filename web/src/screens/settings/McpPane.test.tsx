import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
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

const agentsRows = () =>
  [
    { id: 'a1', name: 'Atlas', slug: 'atlas', enabled_mcps: ['srv-gh'] },
    { id: 'a2', name: 'Beacon', slug: 'beacon', enabled_mcps: ['srv-gh'] },
    { id: 'a3', name: 'Cedar', slug: 'cedar' },
  ] as any[];

describe('screens/settings/McpPane', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(api.agents, 'list').mockResolvedValue({ agents: [] });
  });

  it('renders registry cards from the workspace MCP endpoints', async () => {
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [serverRow(), erroredRow()] });
    vi.spyOn(api.agents, 'list').mockResolvedValue({ agents: agentsRows() });

    render(<McpPane tenant={mockTenant} />);

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

    render(<McpPane tenant={mockTenant} />);

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

    render(<McpPane tenant={mockTenant} onToast={onToast} />);

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

    render(<McpPane tenant={mockTenant} onToast={onToast} />);

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

    render(<McpPane tenant={mockTenant} onToast={onToast} />);

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

    render(<McpPane tenant={mockTenant} />);

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

    render(<McpPane tenant={mockTenant} onToast={onToast} />);

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

    render(<McpPane tenant={mockTenant} />);

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

    render(<McpPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('mcp-empty')).not.toBeNull();
    });
    expect(screen.getByText('No MCP servers configured')).not.toBeNull();
    expect(screen.getByTestId('btn-mcp-empty-add')).not.toBeNull();
  });

  it('shows the load error state with a retry action', async () => {
    vi.spyOn(api.mcp, 'list').mockRejectedValue(new ApiError(500, 'error', 'database unreachable'));

    render(<McpPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByText("Couldn't load MCP servers")).not.toBeNull();
    });
    expect(screen.getByText('database unreachable')).not.toBeNull();
  });

  it('hides every write control from holders without tools.write but keeps reads', async () => {
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [serverRow(), erroredRow()] });

    render(<McpPane tenant={mockTenant} canWrite={false} />);

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
});
