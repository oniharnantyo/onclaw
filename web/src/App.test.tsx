import { describe, it, expect, beforeEach, beforeAll, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import App from './App';
import { useAuthStore } from './store/auth';
import { useStore } from './store';
import { seedDb } from './data/seed';

vi.mock('@assistant-ui/react', () => ({
  useExternalStoreRuntime: vi.fn((opts) => opts),
  AssistantRuntimeProvider: ({ children }: any) => <div>{children}</div>,
}));

beforeAll(() => {
  // Node's disabled `localStorage` global shadows jsdom's on this Node
  // version; install a memory-backed stub so boot/store behavior is testable
  // (same workaround as ChatRoute.test).
  if (typeof localStorage === 'undefined' || !localStorage) {
    const mem = new Map<string, string>();
    const stub = {
      getItem: (k: string) => mem.get(k) ?? null,
      setItem: (k: string, v: string) => void mem.set(k, String(v)),
      removeItem: (k: string) => void mem.delete(k),
      clear: () => mem.clear(),
      key: (i: number) => Array.from(mem.keys())[i] ?? null,
      get length() {
        return mem.size;
      },
    };
    Object.defineProperty(globalThis, 'localStorage', { value: stub, configurable: true, writable: true });
  }
});

describe('App & Route Guard', () => {
  beforeEach(() => {
    localStorage.clear();
    useAuthStore.setState({
      user: null,
      memberships: [],
      status: 'loading',
      boot: vi.fn(),
    });
    useStore.setState({
      db: seedDb(),
      agentsLoaded: {},
      pos: { tenantId: 'acme', view: 'chats', chatId: 'a-atlas', showContext: false, railExpanded: false },
    });
    vi.restoreAllMocks();
  });

  it('renders boot loader while session is loading', () => {
    useAuthStore.setState({ status: 'loading', boot: vi.fn() });

    render(<App />);

    expect(screen.getByText(/loading onclaw…/i)).not.toBeNull();
  });

  it('renders BootError page when auth status is error', async () => {
    useAuthStore.setState({
      status: 'error',
      bootError: 'Network connection failed',
      boot: vi.fn(),
    });

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText("Couldn't reach OnClaw")).not.toBeNull();
      expect(screen.getByText("Network connection failed")).not.toBeNull();
      expect(screen.getByRole('button', { name: 'Retry' })).not.toBeNull();
      expect(screen.getByRole('button', { name: 'Log in instead' })).not.toBeNull();
    });
  });

  it('redirects unauthenticated users to /login', async () => {
    useAuthStore.setState({
      status: 'unauthenticated',
      boot: vi.fn(),
    });

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText(/welcome to onclaw/i)).not.toBeNull();
      expect(screen.getByRole('button', { name: /sign in/i })).not.toBeNull();
    });
  });

  it('renders authenticated layout and navigation rail when authenticated', async () => {
    useAuthStore.setState({
      status: 'authenticated',
      user: { id: 'u1', email: 'alice@example.com', name: 'Alice', created_at: '', updated_at: '' },
      memberships: [],
      boot: vi.fn(),
    });

    render(<App />);

    await waitFor(() => {
      expect(screen.getByLabelText(/primary/i)).not.toBeNull();
      expect(screen.getByRole('button', { name: /user menu for alice/i })).not.toBeNull();
    });
  });

  it('renders suspended workspace screen when active workspace is suspended', async () => {
    useAuthStore.setState({
      status: 'authenticated',
      user: { id: 'u1', email: 'alice@example.com', name: 'Alice', created_at: '', updated_at: '' },
      memberships: [
        {
          workspace_id: 'acme',
          workspace_slug: 'acme',
          workspace_name: 'Acme Corp',
          user_id: 'u1',
          email: 'alice@example.com',
          name: 'Alice',
          role_id: 'r1',
          role_name: 'Owner',
          workspace: {
            id: 'acme',
            slug: 'acme',
            name: 'Acme Corp',
            timezone: 'UTC',
            is_master: false,
            disabled_at: '2026-08-28T12:00:00Z',
            created_at: '',
            updated_at: '',
          },
          joined_at: '',
        },
      ],
      boot: vi.fn(),
    });

    render(<App />);

    await waitFor(() => {
      expect(screen.getByText(/workspace suspended/i)).not.toBeNull();
      expect(screen.getByText(/this workspace has been suspended by an administrator/i)).not.toBeNull();
      expect(screen.getByRole('button', { name: 'Switch workspace' })).not.toBeNull();
    });
  });

  it('allows access to /agents, /cron, and /runs in a zero-agent workspace, while chat routes redirect to /welcome', async () => {
    const zeroAgentWorkspace = {
      id: 'empty_ws',
      sub: 'empty_ws',
      name: 'Empty WS',
      plan: 'Free',
      tz: 'UTC',
      agents: [],
      channels: [],
      people: [],
      cron: [],
      runs: [],
      threads: {},
    };

    useAuthStore.setState({
      status: 'authenticated',
      user: { id: 'u1', email: 'alice@example.com', name: 'Alice', created_at: '', updated_at: '' },
      memberships: [
        {
          workspace_id: 'empty_ws',
          workspace_slug: 'empty_ws',
          workspace_name: 'Empty WS',
          user_id: 'u1',
          email: 'alice@example.com',
          name: 'Alice',
          role_id: 'r1',
          role_name: 'Owner',
          workspace: {
            id: 'empty_ws',
            slug: 'empty_ws',
            name: 'Empty WS',
            timezone: 'UTC',
            is_master: false,
            created_at: '',
            updated_at: '',
          },
          joined_at: '',
        },
      ],
      boot: vi.fn(),
    });

    const { useStore } = await import('./store');
    useStore.setState({
      pos: { tenantId: 'empty_ws', view: 'agents', chatId: '', showContext: false, railExpanded: false },
      db: { empty_ws: zeroAgentWorkspace as any },
    });

    window.history.pushState({}, '', '/agents');
    render(<App />);

    await waitFor(() => {
      expect(screen.getByTestId('agents-view')).not.toBeNull();
      expect(screen.getByText(/no agents in empty ws yet/i)).not.toBeNull();
    });
  });

  it('lands / and /c on the chat page with no conversation opened when agents exist', async () => {
    useAuthStore.setState({
      status: 'authenticated',
      user: { id: 'u1', email: 'alice@example.com', name: 'Alice', created_at: '', updated_at: '' },
      memberships: [],
      boot: vi.fn(),
    });

    window.history.pushState({}, '', '/c');
    const { unmount } = render(<App />);
    await waitFor(() => {
      expect(window.location.pathname).toBe('/c');
      expect(screen.getByTestId('chat-empty')).not.toBeNull();
      expect(screen.getByText('Select a conversation')).not.toBeNull();
    });
    // Agents exist in the workspace — none may be auto-opened.
    expect(screen.queryByLabelText(/conversation with atlas/i)).toBeNull();
    unmount();

    window.history.pushState({}, '', '/');
    render(<App />);
    await waitFor(() => {
      expect(window.location.pathname).toBe('/c');
      expect(screen.getByTestId('chat-empty')).not.toBeNull();
    });
    expect(screen.queryByText(/welcome to onclaw/i)).toBeNull();
  });  it('does not bounce chat routes to /welcome while the agent list is still loading', async () => {
    useAuthStore.setState({
      status: 'authenticated',
      user: { id: 'u1', email: 'alice@example.com', name: 'Alice', created_at: '', updated_at: '' },
      memberships: [],
      boot: vi.fn(),
    });

    // Seeded acme has agents, but simulate a workspace whose agent list has
    // not arrived yet: a materialized blank workspace + no agentsLoaded mark.
    useStore.setState({
      db: {
        acme: {
          id: 'acme',
          sub: 'acme',
          name: 'Acme Corp',
          plan: 'Free',
          tz: 'UTC',
          agents: [],
          channels: [],
          people: [],
          cron: [],
          runs: [],
          threads: {},
        } as any,
      },
    });

    window.history.pushState({}, '', '/c');
    render(<App />);

    await waitFor(() => {
      expect(screen.getByTestId('chat-empty')).not.toBeNull();
    });
    expect(screen.queryByTestId('onboarding-pane')).toBeNull();
  });

  it('redirects chat routes to /welcome once a genuinely zero-agent workspace is loaded', async () => {
    useAuthStore.setState({
      status: 'authenticated',
      user: { id: 'u1', email: 'alice@example.com', name: 'Alice', created_at: '', updated_at: '' },
      memberships: [],
      boot: vi.fn(),
    });

    useStore.setState({
      db: {
        acme: {
          id: 'acme',
          sub: 'acme',
          name: 'Acme Corp',
          plan: 'Free',
          tz: 'UTC',
          agents: [],
          channels: [],
          people: [],
          cron: [],
          runs: [],
          threads: {},
        } as any,
      },
      agentsLoaded: { acme: true },
    });

    window.history.pushState({}, '', '/c');
    render(<App />);

    await waitFor(() => {
      expect(screen.getByTestId('onboarding-pane')).not.toBeNull();
    });
  });

  it('renders /welcome with nothing highlighted in the rail', async () => {
    const zeroAgentWorkspace = {
      id: 'empty_ws',
      sub: 'empty_ws',
      name: 'Empty WS',
      plan: 'Free',
      tz: 'UTC',
      agents: [],
      channels: [],
      people: [],
      cron: [],
      runs: [],
      threads: {},
    };

    useAuthStore.setState({
      status: 'authenticated',
      user: { id: 'u1', email: 'alice@example.com', name: 'Alice', created_at: '', updated_at: '' },
      memberships: [],
      boot: vi.fn(),
    });

    const { useStore } = await import('./store');
    useStore.setState({
      pos: { tenantId: 'empty_ws', view: '', chatId: '', showContext: false, railExpanded: false },
      db: { empty_ws: zeroAgentWorkspace as any },
    });

    window.history.pushState({}, '', '/welcome');
    render(<App />);

    await waitFor(() => {
      expect(screen.getByTestId('onboarding-pane')).not.toBeNull();
      // Verify no rail item is active
      const navButtons = screen.getByRole('navigation', { name: /primary/i }).querySelectorAll('button[data-od-id^="rail-"]');
      navButtons.forEach((btn) => {
        expect(btn.className).not.toContain('text-accent font-semibold');
      });
    });
  });

  it('renders Workspaces and Accounts admin routes for qualified superadmin in master tenant', async () => {
    const masterWorkspace = {
      id: 'master',
      sub: 'master',
      name: 'Master Control',
      plan: 'Enterprise',
      tz: 'UTC',
      is_master: true,
      agents: [{ id: 'a1', name: 'Atlas' }],
      channels: [],
      people: [],
      cron: [],
      runs: [],
      threads: {},
    };

    const { api } = await import('./lib/api');
    vi.spyOn(api.admin.workspaces, 'list').mockResolvedValue({ workspaces: [] });
    vi.spyOn(api.admin.users, 'list').mockResolvedValue({ users: [] });

    useAuthStore.setState({
      status: 'authenticated',
      user: { id: 'u_super', email: 'super@onclaw.local', name: 'Super Admin', created_at: '', updated_at: '' },
      memberships: [
        {
          workspace_id: 'master',
          workspace_slug: 'master',
          workspace_name: 'Master Control',
          user_id: 'u_super',
          email: 'super@onclaw.local',
          name: 'Super Admin',
          role_id: 'r_superadmin',
          role_name: 'Superadmin',
          role: {
            id: 'r_superadmin',
            workspace_id: 'master',
            name: 'Superadmin',
            is_owner: true,
            permissions: ['*'],
            built_in: true,
            created_at: '',
          },
          workspace: {
            id: 'master',
            slug: 'master',
            name: 'Master Control',
            timezone: 'UTC',
            is_master: true,
            created_at: '',
            updated_at: '',
          },
          joined_at: '',
        },
      ],
      boot: vi.fn(),
    });

    const { useStore } = await import('./store');
    useStore.setState({
      pos: { tenantId: 'master', view: 'admin-workspaces', chatId: 'a1', showContext: false, railExpanded: false },
      db: { master: masterWorkspace as any },
    });

    window.history.pushState({}, '', '/admin/workspaces');
    render(<App />);

    await waitFor(() => {
      expect(screen.getByTestId('rail-admin-workspaces')).not.toBeNull();
      expect(screen.getByTestId('rail-admin-accounts')).not.toBeNull();
      expect(screen.getByTestId('admin-view')).not.toBeNull();
    });
  });

  it('navigates to /settings/workspace when /settings is requested, hiding the sidebar', async () => {
    useAuthStore.setState({
      status: 'authenticated',
      user: { id: 'u1', email: 'alice@example.com', name: 'Alice', created_at: '', updated_at: '' },
      memberships: [],
      boot: vi.fn(),
    });

    window.history.pushState({}, '', '/settings');
    render(<App />);

    await waitFor(() => {
      expect(screen.getByTestId('settings-page')).not.toBeNull();
      expect(screen.getByTestId('pane-workspace')).not.toBeNull();
      expect(screen.queryByTestId('sidebar')).toBeNull();
      expect(window.location.pathname).toBe('/settings/workspace');
    });
  });

  it('deep links directly to /settings/keys and redirects unknown section to /settings/workspace', async () => {
    useAuthStore.setState({
      status: 'authenticated',
      user: { id: 'u1', email: 'alice@example.com', name: 'Alice', created_at: '', updated_at: '' },
      memberships: [],
      boot: vi.fn(),
    });

    window.history.pushState({}, '', '/settings/keys');
    const { unmount } = render(<App />);

    await waitFor(() => {
      expect(screen.getByTestId('pane-keys')).not.toBeNull();
    });

    unmount();

    window.history.pushState({}, '', '/settings/nonexistent');
    render(<App />);

    await waitFor(() => {
      expect(screen.getByTestId('pane-workspace')).not.toBeNull();
      expect(window.location.pathname).toBe('/settings/workspace');
    });
  });

  it('navigates to /settings when Rail settings button is clicked', async () => {
    useAuthStore.setState({
      status: 'authenticated',
      user: { id: 'u1', email: 'alice@example.com', name: 'Alice', created_at: '', updated_at: '' },
      memberships: [],
      boot: vi.fn(),
    });

    window.history.pushState({}, '', '/agents');
    render(<App />);

    await waitFor(() => {
      expect(screen.getByTestId('rail-settings')).not.toBeNull();
    });

    fireEvent.click(screen.getByTestId('rail-settings'));

    await waitFor(() => {
      expect(screen.getByTestId('settings-page')).not.toBeNull();
      expect(window.location.pathname).toBe('/settings/workspace');
    });
  });

  it('navigates to /settings when onboarding Workspace settings button is clicked', async () => {
    const zeroAgentWorkspace = {
      id: 'empty_ws',
      sub: 'empty_ws',
      name: 'Empty WS',
      plan: 'Free',
      tz: 'UTC',
      agents: [],
      channels: [],
      people: [],
      cron: [],
      runs: [],
      threads: {},
    };

    useAuthStore.setState({
      status: 'authenticated',
      user: { id: 'u1', email: 'alice@example.com', name: 'Alice', created_at: '', updated_at: '' },
      memberships: [],
      boot: vi.fn(),
    });

    const { useStore } = await import('./store');
    useStore.setState({
      pos: { tenantId: 'empty_ws', view: '', chatId: '', showContext: false, railExpanded: false },
      db: { empty_ws: zeroAgentWorkspace as any },
    });

    window.history.pushState({}, '', '/welcome');
    render(<App />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-onboarding-settings')).not.toBeNull();
    });

    fireEvent.click(screen.getByTestId('btn-onboarding-settings'));

    await waitFor(() => {
      expect(screen.getByTestId('settings-page')).not.toBeNull();
      expect(window.location.pathname).toBe('/settings/workspace');
    });
  });

  it('renders in-shell 404 ErrorState for non-existent routes', async () => {
    useAuthStore.setState({
      status: 'authenticated',
      user: { id: 'u1', email: 'alice@example.com', name: 'Alice', created_at: '', updated_at: '' },
      memberships: [],
      boot: vi.fn(),
    });

    window.history.pushState({}, '', '/nonexistent-route-path');
    render(<App />);

    await waitFor(() => {
      expect(screen.getByText('Page not found')).not.toBeNull();
      expect(screen.getByText(/the page you are looking for does not exist/i)).not.toBeNull();
      expect(screen.getByRole('button', { name: 'Back to chats' })).not.toBeNull();
      expect(screen.getByRole('button', { name: 'View agents' })).not.toBeNull();
    });
  });

  it('renders ConnectionBanner in Layout when connection is degraded', async () => {
    useAuthStore.setState({
      status: 'authenticated',
      user: { id: 'u1', email: 'alice@example.com', name: 'Alice', created_at: '', updated_at: '' },
      memberships: [],
      boot: vi.fn(),
    });

    const { useConnectionStore } = await import('./store/connection');
    useConnectionStore.getState().reportFailure();

    window.history.pushState({}, '', '/agents');
    render(<App />);

    await waitFor(() => {
      expect(screen.getByTestId('connection-banner')).not.toBeNull();
      expect(screen.getByText(/connection lost/i)).not.toBeNull();
    });
  });
});


