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
});
