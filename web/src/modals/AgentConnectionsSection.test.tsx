import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { AgentConnectionsSection } from './AgentConnectionsSection';
import { connectionsApi, type ApiConnection } from '../lib/connectionsApi';

const connectionRow = (overrides: Partial<ApiConnection> = {}): ApiConnection => ({
  id: 'conn-gh',
  workspace_id: 'acme',
  service: 'github',
  access_level: 'read_write',
  status: 'connected',
  status_error: null,
  token_hint: 'a1b2',
  server_id: 'srv-gh-mcp',
  server_enabled: true,
  tool_count: 24,
  attached_agents: ['Beacon'],
  created_at: '',
  updated_at: '',
  ...overrides,
});

describe('modals/AgentConnectionsSection', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(connectionsApi, 'recipes').mockResolvedValue({
      recipes: [
        {
          id: 'github',
          service: 'GitHub',
          icon: 'github',
          auth_kind: 'pat',
          availability: 'available',
          transport: 'streamable_http',
          access_levels: ['read_only', 'read_write'],
          steps: [],
          scopes: [],
          probe: { tool: 'list-repositories' },
        },
      ],
    });
  });

  it('lists managed connections as attachable entries with the access level shown', async () => {
    vi.spyOn(connectionsApi, 'list').mockResolvedValue({ connections: [connectionRow()] });
    const onToggle = vi.fn();

    render(<AgentConnectionsSection targetWsId="acme" enabledMcps={['srv-gh-mcp']} onToggle={onToggle} />);

    await waitFor(() => {
      expect(screen.getByTestId('agent-connection-conn-gh')).not.toBeNull();
    });
    expect(screen.getByText('GitHub')).not.toBeNull();
    expect(screen.getByText('Read & write')).not.toBeNull();
    expect(screen.getByTestId('agent-connection-status-conn-gh').textContent).toBe('Connected');
    // The draft's opt-in state drives the switch.
    expect(
      (screen.getByRole('switch', { name: 'Opt this agent into GitHub' }).getAttribute('aria-checked'))
    ).toBe('true');
  });

  it('toggles attachment through the existing enabled_mcps handler', async () => {
    vi.spyOn(connectionsApi, 'list').mockResolvedValue({ connections: [connectionRow()] });
    const onToggle = vi.fn();

    render(<AgentConnectionsSection targetWsId="acme" enabledMcps={[]} onToggle={onToggle} />);

    await waitFor(() => {
      expect(screen.getByRole('switch', { name: 'Opt this agent into GitHub' })).not.toBeNull();
    });
    fireEvent.click(screen.getByRole('switch', { name: 'Opt this agent into GitHub' }));

    // The materialized server id is the opt-in value — same UUID space as the
    // MCP server attach rows.
    await waitFor(() => {
      expect(onToggle).toHaveBeenCalledWith('srv-gh-mcp');
    });
  });

  it('renders nothing while there are no connections or the read fails', async () => {
    vi.spyOn(connectionsApi, 'list').mockResolvedValue({ connections: [] });
    const { container } = render(<AgentConnectionsSection targetWsId="acme" enabledMcps={[]} onToggle={vi.fn()} />);
    await waitFor(() => {
      expect(container.childElementCount).toBe(0);
    });

    vi.spyOn(connectionsApi, 'list').mockRejectedValue(new Error('down'));
    const { container: failing } = render(
      <AgentConnectionsSection targetWsId="acme" enabledMcps={[]} onToggle={vi.fn()} />
    );
    await waitFor(() => {
      expect(failing.childElementCount).toBe(0);
    });
  });

  it('warns when an attached connection is paused at the workspace level', async () => {
    vi.spyOn(connectionsApi, 'list').mockResolvedValue({
      connections: [connectionRow({ server_enabled: false })],
    });

    render(<AgentConnectionsSection targetWsId="acme" enabledMcps={['srv-gh-mcp']} onToggle={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByTestId('agent-connection-paused-warn-conn-gh')).not.toBeNull();
    });
    expect(screen.getByTestId('agent-connection-status-conn-gh').textContent).toBe('Paused');
  });

  it('shows the read/write tool-tier split beside the access level (add-integration-authority 3.1)', async () => {
    // The counts are the API's effective-tier projection — never computed here.
    vi.spyOn(connectionsApi, 'list').mockResolvedValue({
      connections: [connectionRow({ tier_counts: { read: 6, write: 2 } })],
    });

    render(<AgentConnectionsSection targetWsId="acme" enabledMcps={['srv-gh-mcp']} onToggle={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByTestId('agent-connection-tiers-conn-gh')).not.toBeNull();
    });
    const tiers = screen.getByTestId('agent-connection-tiers-conn-gh');
    expect(tiers.textContent).toContain('6 read');
    expect(tiers.textContent).toContain('2 write');
    // The split sits in the same chip row as the access level it qualifies.
    expect(screen.getByText('Read & write')).not.toBeNull();
  });

  it('hides the tier split gracefully when the API projected no tier_counts', async () => {
    // Legacy/unknown connections carry no tier_counts — no counts, no stubs.
    vi.spyOn(connectionsApi, 'list').mockResolvedValue({
      connections: [connectionRow()],
    });

    render(<AgentConnectionsSection targetWsId="acme" enabledMcps={['srv-gh-mcp']} onToggle={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByTestId('agent-connection-conn-gh')).not.toBeNull();
    });
    expect(screen.queryByTestId('agent-connection-tiers-conn-gh')).toBeNull();
  });

  it('attaches HTTP-kind connections by connection id and never calls them paused', async () => {
    // add-connection-http: no materialized server — server_enabled is
    // structurally false, the toggle stores the raw connection id.
    vi.spyOn(connectionsApi, 'recipes').mockResolvedValue({
      recipes: [
        {
          id: 'figma',
          service: 'Figma',
          icon: 'figma',
          auth_kind: 'pat',
          availability: 'available',
          access_levels: ['read_only'],
          steps: [],
          scopes: [],
          kind: 'http',
          base_url: 'https://api.figma.com',
          verbs: [{ name: 'figma.get_me', method: 'GET', path: '/v1/me' }],
          probe: { tool: 'figma.get_me' },
        },
      ],
    });
    vi.spyOn(connectionsApi, 'list').mockResolvedValue({
      connections: [
        connectionRow({
          id: 'conn-figma',
          service: 'figma',
          server_id: null,
          server_enabled: false,
          tool_count: 1,
        }),
      ],
    });
    const onToggle = vi.fn();

    render(<AgentConnectionsSection targetWsId="acme" enabledMcps={['conn-figma']} onToggle={onToggle} />);

    await waitFor(() => {
      expect(screen.getByTestId('agent-connection-conn-figma')).not.toBeNull();
    });
    expect(screen.getByText('Figma')).not.toBeNull();
    // Kind chip reads HTTP, and the structural server_enabled: false is NOT
    // reported as Paused.
    expect(screen.getByTestId('agent-connection-conn-figma').textContent).toContain('HTTP');
    expect(screen.getByTestId('agent-connection-status-conn-figma').textContent).toBe('Connected');
    expect(screen.queryByTestId('agent-connection-paused-warn-conn-figma')).toBeNull();

    // The draft's opt-in drives the switch from the connection id...
    expect(
      (screen.getByRole('switch', { name: 'Opt this agent into Figma' }).getAttribute('aria-checked'))
    ).toBe('true');
    // ...and the toggle stores the connection id, not a server id.
    fireEvent.click(screen.getByRole('switch', { name: 'Opt this agent into Figma' }));
    await waitFor(() => {
      expect(onToggle).toHaveBeenCalledWith('conn-figma');
    });
  });
});
