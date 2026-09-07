import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { SettingsPage } from './SettingsPage';
import { api, ApiError, type ApiWorkspaceSkill } from '../../lib/api';
import { useAuthStore } from '../../store/auth';

describe('screens/settings/SettingsPage', () => {
  const mockTenant = {
    id: 'acme',
    sub: 'acme',
    name: 'Acme Corp',
    tz: 'America/Los_Angeles',
    defaultModel: 'claude-sonnet-5',
    retention: '90 days',
    members: [],
    integrations: [
      { id: 'slack', name: 'Slack', detail: 'alerts-prod', connected: true },
      { id: 'github', name: 'GitHub', detail: 'github.com/acme', connected: false },
    ],
    keys: [
      {
        id: 'k_live1',
        name: 'Prod key',
        masked: 'oc_live_••••••••live1',
        full: 'oc_live_secretkeyfullvaluelive1',
        created: 'Aug 2026',
      },
    ],
    mcpServers: [
      {
        id: 'mcp-sentry',
        name: 'Sentry',
        transport: 'stdio · sentry-mcp serve',
        auth: 'No credentials yet — configured on first launch',
        tools: 3,
        status: 'connected',
        sample: ['sentry_issues', 'sentry_events'],
      },
      {
        id: 'mcp-broken',
        name: 'Broken DB',
        transport: 'stdio · db-mcp',
        auth: 'No credentials yet',
        tools: 0,
        status: 'error',
        error: 'Connection refused',
        sample: [],
      },
    ],
    skillLib: [
      {
        id: 'sk-sweeper',
        name: 'Changelog sweeper',
        version: '0.1.0',
        desc: 'Sweeps commit logs for changelog entries.',
        uses: 12,
        enabled: true,
        source: 'workspace',
      },
    ],
    agents: [
      { id: 'a_triage', name: 'Triage Agent', mcp: ['mcp-sentry'], skills: ['sk-sweeper'] },
    ],
    notifications: { cronFail: true, agentErrors: true, digest: false, email: 'ops@acme.dev' },
  };

  const mockRoles = [
    { id: 'r_owner', workspace_id: 'acme', name: 'Owner', is_owner: true, permissions: ['*'], built_in: true, created_at: '' },
    { id: 'r_admin', workspace_id: 'acme', name: 'Admin', is_owner: false, permissions: ['workspace.*'], built_in: true, created_at: '' },
    { id: 'r_member', workspace_id: 'acme', name: 'Member', is_owner: false, permissions: ['chat.*'], built_in: true, created_at: '' },
  ];

  const mockMembers = [
    {
      user_id: 'u_alice',
      email: 'alice@acme.dev',
      name: 'Alice',
      role_id: 'r_owner',
      role_name: 'Owner',
      joined_at: '2026-08-01T00:00:00Z',
    },
    {
      user_id: 'u_bob',
      email: 'bob@acme.dev',
      name: 'Bob',
      role_id: 'r_admin',
      role_name: 'Admin',
      joined_at: '2026-08-02T00:00:00Z',
    },
    {
      user_id: 'u_carol',
      email: 'carol@acme.dev',
      name: 'carol',
      role_id: 'r_member',
      role_name: 'Member',
      invited: true,
      joined_at: '2026-08-03T00:00:00Z',
    },
  ];

  beforeEach(() => {
    vi.restoreAllMocks();
    useAuthStore.setState({
      user: { id: 'u_alice', email: 'alice@acme.dev', name: 'Alice', created_at: '', updated_at: '' },
      memberships: [],
      status: 'authenticated',
    });
    // The Tools pane fetches the catalog on mount; deep-link tests render it.
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools: [] });
  });

  function renderSettingsPage(initialPath = '/settings/workspace', overrides = {}) {
    const onToast = vi.fn();
    const onUpdate = vi.fn();
    const onLeaveWorkspace = vi.fn();

    const utils = render(
      <MemoryRouter initialEntries={[initialPath]}>
        <Routes>
          <Route
            path="/settings"
            element={
              <SettingsPage
                tenant={mockTenant}
                onToast={onToast}
                onUpdate={onUpdate}
                onLeaveWorkspace={onLeaveWorkspace}
                {...overrides}
              />
            }
          />
          <Route
            path="/settings/:section"
            element={
              <SettingsPage
                tenant={mockTenant}
                onToast={onToast}
                onUpdate={onUpdate}
                onLeaveWorkspace={onLeaveWorkspace}
                {...overrides}
              />
            }
          />
        </Routes>
      </MemoryRouter>
    );

    return {
      ...utils,
      onToast,
      onUpdate,
      onLeaveWorkspace,
    };
  }

  describe('Navigation & Routing', () => {
    it('renders the nine section tab labels with active highlighting and switches section on click', async () => {
      renderSettingsPage('/settings/workspace');

      const tablist = screen.getByRole('tablist');
      expect(tablist).not.toBeNull();

      const tabs = screen.getAllByRole('tab');
      expect(tabs.length).toBe(9);

      const expectedLabels = [
        'Workspace',
        'Providers',
        'Members & roles',
        'Integrations',
        'MCP servers',
        'Skills',
        'Tools',
        'API keys',
        'Notifications',
      ];
      tabs.forEach((tabEl, index) => {
        expect(tabEl.textContent).toBe(expectedLabels[index]);
      });

      // Workspace is active initially
      expect(screen.getByTestId('settings-tab-workspace').getAttribute('aria-selected')).toBe('true');
      expect(screen.getByTestId('settings-tab-providers').getAttribute('aria-selected')).toBe('false');
      expect(screen.getByTestId('pane-workspace')).not.toBeNull();

      // Click providers tab
      fireEvent.click(screen.getByTestId('settings-tab-providers'));

      // Active state updates to providers and renders providers pane
      await waitFor(() => {
        expect(screen.getByTestId('settings-tab-providers').getAttribute('aria-selected')).toBe('true');
        expect(screen.getByTestId('settings-tab-workspace').getAttribute('aria-selected')).toBe('false');
        expect(screen.getByTestId('pane-providers')).not.toBeNull();
      });
    });

    it('deep links to each settings section based on the URL path segment', () => {
      const sections = [
        { path: '/settings/workspace', paneTestId: 'pane-workspace' },
        { path: '/settings/providers', paneTestId: 'pane-providers' },
        { path: '/settings/members', paneTestId: 'pane-members' },
        { path: '/settings/integrations', paneTestId: 'pane-integrations' },
        { path: '/settings/mcp', paneTestId: 'pane-mcp' },
        { path: '/settings/skills', paneTestId: 'pane-skills' },
        { path: '/settings/tools', paneTestId: 'pane-tools' },
        { path: '/settings/keys', paneTestId: 'pane-keys' },
        { path: '/settings/notifications', paneTestId: 'pane-notifications' },
      ];

      for (const s of sections) {
        const { unmount } = renderSettingsPage(s.path);
        expect(screen.getByTestId(s.paneTestId)).not.toBeNull();
        unmount();
      }
    });

    it('redirects unknown section to /settings/workspace', async () => {
      renderSettingsPage('/settings/unknown-section');

      await waitFor(() => {
        expect(screen.getByTestId('pane-workspace')).not.toBeNull();
        expect(screen.getByTestId('settings-tab-workspace').getAttribute('aria-selected')).toBe('true');
      });
    });
  });

  describe('Workspace section & Danger zone', () => {
    it('saves workspace name and timezone via API', async () => {
      const patchSpy = vi.spyOn(api.workspaces, 'patch').mockResolvedValue({
        workspace: {
          id: 'acme',
          slug: 'acme',
          name: 'Acme Super Corp',
          timezone: 'Europe/London',
          is_master: false,
          created_at: '',
          updated_at: '',
        },
      });

      const { onToast } = renderSettingsPage('/settings/workspace');

      const nameInput = screen.getByLabelText(/workspace name/i);
      fireEvent.change(nameInput, { target: { value: 'Acme Super Corp' } });
      const tzSelect = screen.getByLabelText(/timezone/i);
      fireEvent.change(tzSelect, { target: { value: 'Europe/London' } });

      fireEvent.click(screen.getByRole('button', { name: /save workspace/i }));

      await waitFor(() => {
        expect(patchSpy).toHaveBeenCalledWith('acme', {
          name: 'Acme Super Corp',
          timezone: 'Europe/London',
        });
        expect(onToast).toHaveBeenCalledWith('Workspace settings saved');
      });
    });

    it('requires two clicks within 4s to leave workspace', async () => {
      const { onLeaveWorkspace } = renderSettingsPage('/settings/workspace');

      const leaveBtn = screen.getByTestId('btn-workspace-leave');
      expect(leaveBtn.textContent).toBe('Leave workspace');

      // Click once: enters confirmation mode
      fireEvent.click(leaveBtn);
      expect(leaveBtn.textContent).toBe('Click again to confirm');
      expect(onLeaveWorkspace).not.toHaveBeenCalled();

      // Click second time: invokes leave handler
      fireEvent.click(leaveBtn);
      expect(onLeaveWorkspace).toHaveBeenCalledTimes(1);
    });

    it('displays error toast if leaving as last owner fails with last_owner_protected', async () => {
      vi.spyOn(api.members, 'remove').mockRejectedValue(
        new ApiError(409, 'last_owner_protected', 'The last owner cannot be removed or demoted.')
      );

      const { onToast } = renderSettingsPage('/settings/workspace', { onLeaveWorkspace: undefined });

      const leaveBtn = screen.getByTestId('btn-workspace-leave');
      fireEvent.click(leaveBtn);
      fireEvent.click(leaveBtn);

      await waitFor(() => {
        expect(onToast).toHaveBeenCalledWith('The last owner cannot be removed or demoted.', 'danger');
      });
    });
  });

  describe('Members section', () => {
    beforeEach(() => {
      vi.spyOn(api.members, 'list').mockResolvedValue({ members: mockMembers });
      vi.spyOn(api.roles, 'list').mockResolvedValue({ roles: mockRoles });
    });

    it('loads and renders members, roles, and invited hints', async () => {
      renderSettingsPage('/settings/members');

      await waitFor(() => {
        expect(screen.getByText('Alice')).not.toBeNull();
        expect(screen.getByText('Bob')).not.toBeNull();
        expect(screen.getByText('carol')).not.toBeNull();
        expect(screen.getByText('You are the owner')).not.toBeNull();
        expect(screen.getByText('Invited')).not.toBeNull();
      });
    });

    it('validates email format in invite dialog', async () => {
      renderSettingsPage('/settings/members');

      await waitFor(() => {
        expect(screen.getByTestId('btn-invite-open')).not.toBeNull();
      });

      fireEvent.click(screen.getByTestId('btn-invite-open'));

      await waitFor(() => {
        expect(screen.getByTestId('modal-invite-member')).not.toBeNull();
      });

      const emailInput = screen.getByLabelText(/invite email/i);
      const submitBtn = screen.getByTestId('btn-invite') as HTMLButtonElement;

      // Invalid email
      fireEvent.change(emailInput, { target: { value: 'not-an-email' } });
      expect(submitBtn.disabled).toBe(true);

      // Valid email
      fireEvent.change(emailInput, { target: { value: 'valid@acme.dev' } });
      expect(submitBtn.disabled).toBe(false);
    });

    it('invites a new member via dialog with selected role', async () => {
      const addMemberSpy = vi.spyOn(api.members, 'add').mockResolvedValue({
        member: { workspace_id: 'acme', user_id: 'u_new', role_id: 'r_admin', created_at: '' },
        user: { id: 'u_new', email: 'dave@acme.dev', name: 'dave', created_at: '', updated_at: '' },
        role: mockRoles[1],
      });

      const { onToast } = renderSettingsPage('/settings/members');

      await waitFor(() => {
        expect(screen.getByTestId('btn-invite-open')).not.toBeNull();
      });

      fireEvent.click(screen.getByTestId('btn-invite-open'));

      await waitFor(() => {
        expect(screen.getByLabelText(/invite email/i)).not.toBeNull();
      });

      const emailInput = screen.getByLabelText(/invite email/i);
      const roleSelect = screen.getByLabelText(/role for invite/i);
      const inviteBtn = screen.getByTestId('btn-invite');

      fireEvent.change(emailInput, { target: { value: 'dave@acme.dev' } });
      fireEvent.change(roleSelect, { target: { value: 'r_admin' } });
      fireEvent.click(inviteBtn);

      await waitFor(() => {
        expect(addMemberSpy).toHaveBeenCalledWith('acme', {
          email: 'dave@acme.dev',
          role_id: 'r_admin',
        });
        expect(onToast).toHaveBeenCalledWith('Invite sent to dave@acme.dev');
        expect(screen.queryByTestId('modal-invite-member')).toBeNull();
      });
    });

    it('changes a member role inline via API and toasts success', async () => {
      const patchRoleSpy = vi.spyOn(api.members, 'patch').mockResolvedValue({
        member: { workspace_id: 'acme', user_id: 'u_bob', role_id: 'r_member', created_at: '' },
        role: mockRoles[2],
      });

      const { onToast } = renderSettingsPage('/settings/members');

      await waitFor(() => {
        expect(screen.getByLabelText('Role for Bob')).not.toBeNull();
      });

      const bobRoleSelect = screen.getByLabelText('Role for Bob');
      fireEvent.change(bobRoleSelect, { target: { value: 'r_member' } });

      await waitFor(() => {
        expect(patchRoleSpy).toHaveBeenCalledWith('acme', 'u_bob', {
          role_id: 'r_member',
        });
        expect(onToast).toHaveBeenCalledWith('Bob is now member');
      });
    });

    it('removes a member via API and toasts success', async () => {
      const removeSpy = vi.spyOn(api.members, 'remove').mockResolvedValue(undefined);
      const { onToast } = renderSettingsPage('/settings/members');

      await waitFor(() => {
        expect(screen.getByLabelText('Remove Bob')).not.toBeNull();
      });

      fireEvent.click(screen.getByLabelText('Remove Bob'));

      await waitFor(() => {
        expect(removeSpy).toHaveBeenCalledWith('acme', 'u_bob');
        expect(onToast).toHaveBeenCalledWith('Bob removed from Acme Corp');
      });
    });

    it('surfaces peer guard rejection as error toast', async () => {
      vi.spyOn(api.members, 'patch').mockRejectedValue(
        new ApiError(403, 'forbidden', 'Cannot modify role of peer admin')
      );

      const { onToast } = renderSettingsPage('/settings/members');

      await waitFor(() => {
        expect(screen.getByLabelText('Role for Bob')).not.toBeNull();
      });

      const bobRoleSelect = screen.getByLabelText('Role for Bob');
      fireEvent.change(bobRoleSelect, { target: { value: 'r_member' } });

      await waitFor(() => {
        expect(onToast).toHaveBeenCalledWith('Cannot modify role of peer admin', 'danger');
      });
    });
  });

  describe('Providers pane', () => {
    const mockProviders = [
      {
        id: 'prov_openai',
        workspace_id: 'acme',
        type: 'openai',
        name: 'OpenAI Primary',
        base_url: '',
        key_set: true,
        key_hint: '7f3a',
        enabled: true,
        created_at: '2026-08-01T00:00:00Z',
        updated_at: '2026-08-01T00:00:00Z',
      },
      {
        id: 'prov_custom',
        workspace_id: 'acme',
        type: 'openai-compatible',
        name: 'Together AI',
        base_url: 'https://api.together.xyz',
        key_set: false,
        key_hint: '',
        enabled: false,
        created_at: '2026-08-02T00:00:00Z',
        updated_at: '2026-08-02T00:00:00Z',
      },
    ];

    beforeEach(() => {
      vi.spyOn(api.providers, 'list').mockResolvedValue({ providers: mockProviders });
    });

    it('loads and renders providers list and badges', async () => {
      renderSettingsPage('/settings/providers');

      await waitFor(() => {
        expect(screen.getByText('OpenAI Primary')).not.toBeNull();
        expect(screen.getByText('Together AI')).not.toBeNull();
        expect(screen.getByText('Key: ••••7f3a')).not.toBeNull();
        expect(screen.getByText('No key set')).not.toBeNull();
        expect(screen.getByText('https://api.together.xyz')).not.toBeNull();
      });
    });

    it('creates an OpenAI provider with key via dialog', async () => {
      const createSpy = vi.spyOn(api.providers, 'create').mockResolvedValue({
        provider: {
          id: 'prov_new',
          workspace_id: 'acme',
          type: 'openai',
          name: 'Acme Prod',
          base_url: '',
          key_set: true,
          key_hint: '9999',
          enabled: true,
          created_at: '',
          updated_at: '',
        },
      });

      const { onToast } = renderSettingsPage('/settings/providers');

      await waitFor(() => {
        expect(screen.getByTestId('btn-provider-add')).not.toBeNull();
      });

      fireEvent.click(screen.getByTestId('btn-provider-add'));

      await waitFor(() => {
        expect(screen.getByTestId('modal-provider')).not.toBeNull();
      });

      const nameInput = screen.getByLabelText(/provider name/i);
      const keyInput = screen.getByLabelText(/api key/i);

      fireEvent.change(nameInput, { target: { value: 'Acme Prod' } });
      fireEvent.change(keyInput, { target: { value: 'sk-newkey123' } });

      fireEvent.click(screen.getByTestId('btn-provider-create-confirm'));

      await waitFor(() => {
        expect(createSpy).toHaveBeenCalledWith('acme', {
          type: 'openai',
          name: 'Acme Prod',
          key: 'sk-newkey123',
          enabled: true,
        });
        expect(onToast).toHaveBeenCalledWith('Provider Acme Prod created');
        expect(screen.queryByTestId('modal-provider')).toBeNull();
      });
    });

    it('requires base URL for compatible provider types in dialog', async () => {
      const createSpy = vi.spyOn(api.providers, 'create').mockResolvedValue({
        provider: {
          id: 'prov_compat',
          workspace_id: 'acme',
          type: 'openai-compatible',
          name: 'Custom Prox',
          base_url: 'https://custom.api.com',
          key_set: false,
          key_hint: '',
          enabled: true,
          created_at: '',
          updated_at: '',
        },
      });

      renderSettingsPage('/settings/providers');

      await waitFor(() => {
        expect(screen.getByTestId('btn-provider-add')).not.toBeNull();
      });

      fireEvent.click(screen.getByTestId('btn-provider-add'));

      await waitFor(() => {
        expect(screen.getByTestId('modal-provider')).not.toBeNull();
      });

      const typeSelect = screen.getByLabelText(/provider type/i);
      const nameInput = screen.getByLabelText(/provider name/i);
      const baseUrlInput = screen.getByLabelText(/base url/i);
      const confirmBtn = screen.getByTestId('btn-provider-create-confirm');

      fireEvent.change(typeSelect, { target: { value: 'openai-compatible' } });
      fireEvent.change(nameInput, { target: { value: 'Custom Prox' } });

      // Confirm button is disabled because base_url is required for compatible type
      expect((confirmBtn as HTMLButtonElement).disabled).toBe(true);

      fireEvent.change(baseUrlInput, { target: { value: 'https://custom.api.com' } });
      expect((confirmBtn as HTMLButtonElement).disabled).toBe(false);

      fireEvent.click(confirmBtn);

      await waitFor(() => {
        expect(createSpy).toHaveBeenCalledWith('acme', {
          type: 'openai-compatible',
          name: 'Custom Prox',
          base_url: 'https://custom.api.com',
          enabled: true,
        });
      });
    });

    it('edits a provider via dialog without echoing or requiring key', async () => {
      const patchSpy = vi.spyOn(api.providers, 'patch').mockResolvedValue({
        provider: {
          ...mockProviders[0],
          name: 'OpenAI Super',
        },
      });

      const { onToast } = renderSettingsPage('/settings/providers');

      await waitFor(() => {
        expect(screen.getByTestId('btn-edit-prov_openai')).not.toBeNull();
      });

      fireEvent.click(screen.getByTestId('btn-edit-prov_openai'));

      await waitFor(() => {
        expect(screen.getByTestId('modal-provider')).not.toBeNull();
      });

      const nameInput = screen.getByLabelText(/edit name/i);
      const keyInput = screen.getByLabelText(/edit api key/i);

      // Key input is write-only, empty value
      expect((keyInput as HTMLInputElement).value).toBe('');

      fireEvent.change(nameInput, { target: { value: 'OpenAI Super' } });
      fireEvent.click(screen.getByTestId('btn-save-edit-prov_openai'));

      await waitFor(() => {
        expect(patchSpy).toHaveBeenCalledWith('acme', 'prov_openai', {
          name: 'OpenAI Super',
          enabled: true,
        });
        expect(onToast).toHaveBeenCalledWith('OpenAI Super updated');
        expect(screen.queryByTestId('modal-provider')).toBeNull();
      });
    });

    it('toggles provider enabled state', async () => {
      const patchSpy = vi.spyOn(api.providers, 'patch').mockResolvedValue({
        provider: {
          ...mockProviders[0],
          enabled: false,
        },
      });

      const { onToast } = renderSettingsPage('/settings/providers');

      await waitFor(() => {
        expect(screen.getByLabelText('Enable OpenAI Primary')).not.toBeNull();
      });

      fireEvent.click(screen.getByLabelText('Enable OpenAI Primary'));

      await waitFor(() => {
        expect(patchSpy).toHaveBeenCalledWith('acme', 'prov_openai', {
          enabled: false,
        });
        expect(onToast).toHaveBeenCalledWith('Disabled OpenAI Primary');
      });
    });

    it('deletes a provider with two-click confirmation', async () => {
      const deleteSpy = vi.spyOn(api.providers, 'delete').mockResolvedValue(undefined);
      const { onToast } = renderSettingsPage('/settings/providers');

      await waitFor(() => {
        expect(screen.getByTestId('btn-delete-prov_openai')).not.toBeNull();
      });

      // First click: enters confirmation mode
      fireEvent.click(screen.getByTestId('btn-delete-prov_openai'));
      expect(screen.getByTestId('btn-delete-confirm-prov_openai')).not.toBeNull();
      expect(deleteSpy).not.toHaveBeenCalled();

      // Second click: deletes
      fireEvent.click(screen.getByTestId('btn-delete-confirm-prov_openai'));

      await waitFor(() => {
        expect(deleteSpy).toHaveBeenCalledWith('acme', 'prov_openai');
        expect(onToast).toHaveBeenCalledWith('OpenAI Primary deleted');
      });
    });

    it('verifies provider connection with transient result banner', async () => {
      const verifySpy = vi.spyOn(api.providers, 'verify').mockResolvedValue({ ok: true });

      renderSettingsPage('/settings/providers');

      await waitFor(() => {
        expect(screen.getByTestId('btn-verify-prov_openai')).not.toBeNull();
      });

      fireEvent.click(screen.getByTestId('btn-verify-prov_openai'));

      await waitFor(() => {
        expect(verifySpy).toHaveBeenCalledWith('acme', 'prov_openai');
        expect(screen.getByText('Connection verified successfully')).not.toBeNull();
      });

      // Error outcome
      verifySpy.mockResolvedValue({ ok: false, error: 'Invalid API key' });
      fireEvent.click(screen.getByTestId('btn-verify-prov_openai'));

      await waitFor(() => {
        expect(screen.getByText('Invalid API key')).not.toBeNull();
      });
    });

    it('surfaces permission error as danger toast on verify', async () => {
      vi.spyOn(api.providers, 'verify').mockRejectedValue(
        new ApiError(403, 'forbidden', "You don't have permission to perform this action.")
      );

      const { onToast } = renderSettingsPage('/settings/providers');

      await waitFor(() => {
        expect(screen.getByTestId('btn-verify-prov_openai')).not.toBeNull();
      });

      fireEvent.click(screen.getByTestId('btn-verify-prov_openai'));

      await waitFor(() => {
        expect(onToast).toHaveBeenCalledWith("You don't have permission to perform this action.", 'danger');
      });
    });

    it('displays empty state when no providers configured', async () => {
      vi.spyOn(api.providers, 'list').mockResolvedValue({ providers: [] });

      renderSettingsPage('/settings/providers');

      await waitFor(() => {
        expect(screen.getByText('No providers configured')).not.toBeNull();
        expect(screen.getByTestId('btn-provider-empty-add')).not.toBeNull();
      });
    });
  });

  describe('Integrations section', () => {
    it('loads integrations and handles connect / disconnect with toasts', async () => {
      const { onToast, onUpdate } = renderSettingsPage('/settings/integrations');

      expect(screen.getByText('Slack')).not.toBeNull();
      expect(screen.getByText('GitHub')).not.toBeNull();
      expect(screen.getByText('Connected')).not.toBeNull();

      // Disconnect Slack
      fireEvent.click(screen.getByRole('button', { name: 'Disconnect' }));
      expect(onToast).toHaveBeenCalledWith('Slack disconnected');
      expect(onUpdate).toHaveBeenCalled();

      // Connect GitHub
      fireEvent.click(screen.getByTestId('connect-github'));
      expect(onToast).toHaveBeenCalledWith('GitHub connected');
      expect(onUpdate).toHaveBeenCalled();
    });
  });

  describe('MCP servers pane', () => {
    it('loads and renders MCP servers and exposed tools', async () => {
      renderSettingsPage('/settings/mcp');

      expect(screen.getByText('Sentry')).not.toBeNull();
      expect(screen.getByText('stdio · sentry-mcp serve')).not.toBeNull();
      expect(screen.getByText('3 tools')).not.toBeNull();
      expect(screen.getByText('Broken DB')).not.toBeNull();
    });

    it('adds an MCP server via dialog', async () => {
      const { onUpdate, onToast } = renderSettingsPage('/settings/mcp');

      fireEvent.click(screen.getByTestId('btn-mcp-add'));

      await waitFor(() => {
        expect(screen.getByTestId('modal-mcp-server')).not.toBeNull();
      });

      const nameInput = screen.getByLabelText(/server name/i);
      const transportInput = screen.getByLabelText(/transport command/i);
      const addConfirmBtn = screen.getByTestId('btn-mcp-add-confirm') as HTMLButtonElement;

      expect(addConfirmBtn.disabled).toBe(true);

      fireEvent.change(nameInput, { target: { value: 'GitHub MCP' } });
      fireEvent.change(transportInput, { target: { value: 'stdio · gh-mcp' } });

      expect(addConfirmBtn.disabled).toBe(false);

      fireEvent.click(addConfirmBtn);

      expect(onUpdate).toHaveBeenCalled();
      expect(onToast).toHaveBeenCalledWith('GitHub MCP added — tools sync on the first handshake');
      expect(screen.queryByTestId('modal-mcp-server')).toBeNull();
    });

    it('edits an MCP server name and transport via dialog', async () => {
      const { onUpdate, onToast } = renderSettingsPage('/settings/mcp');

      fireEvent.click(screen.getByTestId('btn-edit-mcp-sentry'));

      await waitFor(() => {
        expect(screen.getByTestId('modal-mcp-server')).not.toBeNull();
      });

      const transportInput = screen.getByLabelText(/transport command/i);
      fireEvent.change(transportInput, { target: { value: 'stdio · sentry-v2-mcp' } });

      fireEvent.click(screen.getByTestId('btn-mcp-save'));

      expect(onUpdate).toHaveBeenCalled();
      expect(onToast).toHaveBeenCalledWith('Sentry updated');
      expect(screen.queryByTestId('modal-mcp-server')).toBeNull();
    });

    it('retries an errored MCP server and reconnects', async () => {
      const { onUpdate, onToast } = renderSettingsPage('/settings/mcp');

      fireEvent.click(screen.getByTestId('mcp-retry-mcp-broken'));

      expect(onUpdate).toHaveBeenCalled();
      expect(onToast).toHaveBeenCalledWith('Broken DB reconnected — 0 tools available');
    });
  });

  describe('Skills pane', () => {
    const skillRows = (): { skills: ApiWorkspaceSkill[] } => ({
      skills: [
        {
          id: 'sk-sweeper', workspace_id: 'acme', tier: 'workspace', name: 'changelog-sweeper',
          version: '0.1.0', source: 'authored', enabled: true,
          description: 'Sweeps commit logs for changelog entries.', created_at: '', updated_at: '',
          dependency_status: [{ kind: 'binaries', name: 'pdftotext', status: 'missing', install_hint: 'brew install poppler' }],
        },
        {
          id: 'sys-web-research', tier: 'system', name: 'web-research', version: '2.4.1', source: 'system',
          locked: true, enabled: true, description: 'Research briefs.', created_at: '', updated_at: '',
        },
      ],
    });

    it('renders library rows and the locked system section from the skills API', async () => {
      vi.spyOn(api.skills, 'list').mockResolvedValue(skillRows());

      renderSettingsPage('/settings/skills');

      await waitFor(() => {
        expect(screen.getByTestId('skill-changelog-sweeper')).not.toBeNull();
      });
      expect(screen.getByText('v0.1.0')).not.toBeNull();
      expect(screen.getByText('authored')).not.toBeNull();
      // Unmet dependency chip names the count
      expect(screen.getByTestId('skill-dep-warning-changelog-sweeper').textContent).toContain('1 unmet');

      // System skills render locked with a Fork action, no toggle/uninstall
      expect(screen.getByTestId('system-skills-section')).not.toBeNull();
      expect(screen.getByTestId('skill-web-research')).not.toBeNull();
      expect(screen.queryByRole('switch', { name: 'Enable web-research' })).toBeNull();
      expect(screen.queryByTestId('btn-uninstall-web-research')).toBeNull();
      expect(screen.getByTestId('btn-fork-web-research')).not.toBeNull();
    });

    it('master toggle drives PATCH and toasts removal from every agent', async () => {
      vi.spyOn(api.skills, 'list').mockResolvedValue(skillRows());
      const setEnabled = vi.spyOn(api.skills, 'setEnabled').mockResolvedValue({
        skill: { ...skillRows().skills[0], enabled: false },
      });
      const { onToast } = renderSettingsPage('/settings/skills');

      await waitFor(() => {
        expect(screen.getByTestId('skill-changelog-sweeper')).not.toBeNull();
      });
      fireEvent.click(screen.getByRole('switch', { name: 'Enable changelog-sweeper' }));

      await waitFor(() => {
        expect(setEnabled).toHaveBeenCalledWith('acme', 'changelog-sweeper', false);
      });
      await waitFor(() => {
        expect(onToast).toHaveBeenCalledWith('changelog-sweeper disabled — removed from every agent');
      });
    });

    it('renders read-only lists for Members with no action affordances', async () => {
      vi.spyOn(api.skills, 'list').mockResolvedValue(skillRows());

      renderSettingsPage('/settings/skills', { skillsCanWrite: false });

      await waitFor(() => {
        expect(screen.getByTestId('skill-changelog-sweeper')).not.toBeNull();
      });
      expect(screen.queryByTestId('btn-skill-add')).toBeNull();
      expect(screen.queryByRole('switch', { name: 'Enable changelog-sweeper' })).toBeNull();
      expect(screen.queryByTestId('btn-edit-changelog-sweeper')).toBeNull();
      expect(screen.queryByTestId('btn-uninstall-changelog-sweeper')).toBeNull();
      expect(screen.queryByTestId('btn-fork-web-research')).toBeNull();
      // Both lists still render
      expect(screen.getByTestId('system-skills-section')).not.toBeNull();
    });
  });

  describe('API keys pane', () => {
    it('requires name when creating an API key via dialog', async () => {
      renderSettingsPage('/settings/keys');

      fireEvent.click(screen.getByTestId('btn-new-key'));

      await waitFor(() => {
        expect(screen.getByTestId('modal-api-key')).not.toBeNull();
      });

      const nameInput = screen.getByLabelText(/key name/i);
      const createConfirmBtn = screen.getByTestId('btn-confirm-create-key') as HTMLButtonElement;

      // Empty name -> disabled
      expect(createConfirmBtn.disabled).toBe(true);

      fireEvent.change(nameInput, { target: { value: 'CI Automation' } });
      expect(createConfirmBtn.disabled).toBe(false);
    });

    it('creates a key through dialog and shows one-time full-key reveal with copy warning', async () => {
      // Mock navigator.clipboard
      const writeTextMock = vi.fn().mockResolvedValue(undefined);
      Object.assign(navigator, {
        clipboard: {
          writeText: writeTextMock,
        },
      });

      const { onToast, onUpdate } = renderSettingsPage('/settings/keys');

      fireEvent.click(screen.getByTestId('btn-new-key'));

      await waitFor(() => {
        expect(screen.getByTestId('modal-api-key')).not.toBeNull();
      });

      const nameInput = screen.getByLabelText(/key name/i);
      fireEvent.change(nameInput, { target: { value: 'Staging Server' } });

      fireEvent.click(screen.getByTestId('btn-confirm-create-key'));

      // Toast 1: Key created toast
      expect(onToast).toHaveBeenCalledWith("API key created — copy it now, it won't be shown again");
      expect(onUpdate).toHaveBeenCalled();

      // Modal transitioned to created reveal
      await waitFor(() => {
        expect(screen.getByTestId('modal-api-key-created')).not.toBeNull();
        expect(screen.getByText('Copy this key now')).not.toBeNull();
      });

      // Copy key
      fireEvent.click(screen.getByTestId('btn-copy-created-key'));

      await waitFor(() => {
        expect(writeTextMock).toHaveBeenCalled();
        expect(onToast).toHaveBeenCalledWith('Key copied to clipboard');
      });

      // Done closes modal
      fireEvent.click(screen.getByTestId('btn-key-done'));
      expect(screen.queryByTestId('modal-api-key-created')).toBeNull();
    });
  });

  describe('Notifications section', () => {
    it('toggles notification preferences and saves email routing via toast and update callback', async () => {
      const { onToast, onUpdate } = renderSettingsPage('/settings/notifications');

      expect(screen.getByText('Cron failures')).not.toBeNull();
      expect(screen.getByText('Agent errors')).not.toBeNull();
      expect(screen.getByText('Weekly digest')).not.toBeNull();

      const emailInput = screen.getByLabelText(/route email/i);
      expect((emailInput as HTMLInputElement).value).toBe('ops@acme.dev');

      fireEvent.change(emailInput, { target: { value: 'alerts@acme.dev' } });
      fireEvent.click(screen.getByTestId('btn-notif-save'));

      expect(onUpdate).toHaveBeenCalled();
      expect(onToast).toHaveBeenCalledWith('Notification settings saved');
    });
  });
});
