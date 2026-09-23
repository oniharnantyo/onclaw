import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { AdminView } from './AdminView';
import { TenantsPane } from './TenantsPane';
import { UsersPane } from './UsersPane';
import { CreateTenantModal } from './CreateTenantModal';
import { CreateUserModal } from './CreateUserModal';
import { api, ApiError } from '../../lib/api';
import { useAuthStore } from '../../store/auth';
import { useStore } from '../../store';

describe('Admin Area', () => {
  const mockMasterWorkspace = {
    id: 'master',
    sub: 'master',
    slug: 'master',
    name: 'Master Control',
    is_master: true,
    tz: 'UTC',
    agents: [],
  };

  const mockNonMasterWorkspace = {
    id: 'acme',
    sub: 'acme',
    slug: 'acme',
    name: 'Acme Corp',
    is_master: false,
    tz: 'America/Los_Angeles',
    agents: [],
  };

  const mockAdminUser = {
    id: 'u_super',
    email: 'super@onclaw.local',
    name: 'Super Admin',
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
  };

  const mockSuperadminMembership = {
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
    joined_at: '2026-08-01T00:00:00Z',
  };

  const mockWorkspacesList = [
    {
      id: 'master',
      slug: 'master',
      name: 'Master Control',
      timezone: 'UTC',
      is_master: true,
      created_at: '2026-08-01T00:00:00Z',
      updated_at: '2026-08-01T00:00:00Z',
      member_count: 2,
    },
    {
      id: 'ws_acme',
      slug: 'acme',
      name: 'Acme Corp',
      timezone: 'America/Los_Angeles',
      is_master: false,
      created_at: '2026-08-02T00:00:00Z',
      updated_at: '2026-08-02T00:00:00Z',
      member_count: 5,
    },
    {
      id: 'ws_beta',
      slug: 'beta',
      name: 'Beta Co',
      timezone: 'Europe/London',
      is_master: false,
      disabled_at: '2026-08-20T00:00:00Z',
      created_at: '2026-08-03T00:00:00Z',
      updated_at: '2026-08-20T00:00:00Z',
      member_count: 1,
    },
  ];

  const mockUsersList = [
    {
      id: 'u_super',
      email: 'super@onclaw.local',
      name: 'Super Admin',
      is_superadmin: true,
      created_at: '2026-08-01T00:00:00Z',
      updated_at: '2026-08-01T00:00:00Z',
      membership_count: 1,
    },
    {
      id: 'u_alice',
      email: 'alice@acme.dev',
      name: 'Alice',
      is_superadmin: false,
      created_at: '2026-08-02T00:00:00Z',
      updated_at: '2026-08-02T00:00:00Z',
      membership_count: 3,
    },
    {
      id: 'u_bob',
      email: 'bob@beta.dev',
      name: 'Bob',
      is_superadmin: false,
      disabled_at: '2026-08-25T00:00:00Z',
      created_at: '2026-08-03T00:00:00Z',
      updated_at: '2026-08-25T00:00:00Z',
      membership_count: 0,
    },
  ];

  const mockMasterMembers = [
    {
      user_id: 'u_super',
      email: 'super@onclaw.local',
      name: 'Super Admin',
      role_id: 'r_superadmin',
      role_name: 'Superadmin',
      joined_at: '2026-08-01T00:00:00Z',
    },
    {
      user_id: 'u_alice',
      email: 'alice@acme.dev',
      name: 'Alice',
      role_id: 'r_member',
      role_name: 'Member',
      joined_at: '2026-08-05T00:00:00Z',
    },
  ];

  beforeEach(() => {
    vi.restoreAllMocks();
    useStore.setState({
      pos: { tenantId: 'master', view: 'chats', chatId: 'a1', showContext: false },
      db: {
        master: mockMasterWorkspace as any,
        acme: mockNonMasterWorkspace as any,
      },
    });
    useAuthStore.setState({
      user: mockAdminUser,
      memberships: [mockSuperadminMembership],
      status: 'authenticated',
    });
  });

  describe('5.1 Admin Gating & Route Split', () => {
    it('renders not-authorized state when active workspace is not master', () => {
      useStore.setState({
        pos: { tenantId: 'acme', view: 'chats', chatId: 'a1', showContext: false },
      });

      render(
        <MemoryRouter initialEntries={['/admin/workspaces']}>
          <Routes>
            <Route path="/admin/workspaces" element={<AdminView tenant={mockNonMasterWorkspace} />} />
          </Routes>
        </MemoryRouter>
      );

      expect(screen.getByTestId('admin-unauthorized')).not.toBeNull();
      expect(screen.getByText(/not authorized/i)).not.toBeNull();
      expect(screen.getByText(/you must be an administrator in the master workspace/i)).not.toBeNull();
    });

    it('renders workspaces pane at /admin/workspaces for superadmin', async () => {
      vi.spyOn(api.admin.workspaces, 'list').mockResolvedValue({ workspaces: mockWorkspacesList });

      render(
        <MemoryRouter initialEntries={['/admin/workspaces']}>
          <Routes>
            <Route path="/admin/workspaces" element={<AdminView screen="workspaces" tenant={mockMasterWorkspace} />} />
          </Routes>
        </MemoryRouter>
      );

      expect(screen.getByTestId('admin-view')).not.toBeNull();
      expect(screen.getByTestId('pane-admin-tenants')).not.toBeNull();
    });

    it('renders accounts pane at /admin/accounts for superadmin', async () => {
      vi.spyOn(api.admin.users, 'list').mockResolvedValue({ users: mockUsersList });

      render(
        <MemoryRouter initialEntries={['/admin/accounts']}>
          <Routes>
            <Route path="/admin/accounts" element={<AdminView screen="accounts" tenant={mockMasterWorkspace} />} />
          </Routes>
        </MemoryRouter>
      );

      expect(screen.getByTestId('admin-view')).not.toBeNull();
      expect(screen.getByTestId('pane-admin-users')).not.toBeNull();
    });

  });

  describe('5.2 Tenants Screen & EditTenantModal', () => {
    beforeEach(() => {
      vi.spyOn(api.admin.workspaces, 'list').mockResolvedValue({ workspaces: mockWorkspacesList });
      vi.spyOn(api.admin.users, 'list').mockResolvedValue({ users: mockUsersList });
      vi.spyOn(api.admin.workspaces, 'listMembers').mockResolvedValue({
        members: [
          {
            user_id: 'u_alice',
            email: 'alice@acme.dev',
            name: 'Alice',
            role_id: 'r_owner',
            role_name: 'Owner',
            role: { id: 'r_owner', workspace_id: 'ws_acme', name: 'Owner', is_owner: true, permissions: ['*'], built_in: true, created_at: '' },
            joined_at: '2026-08-02T00:00:00Z',
          },
          {
            user_id: 'u_bob',
            email: 'bob@beta.dev',
            name: 'Bob',
            role_id: 'r_member',
            role_name: 'Member',
            role: { id: 'r_member', workspace_id: 'ws_acme', name: 'Member', is_owner: false, permissions: [], built_in: true, created_at: '' },
            joined_at: '2026-08-03T00:00:00Z',
          },
        ],
      });
    });

    it('lists all workspaces with slug, member count, status badges, and Edit buttons', async () => {
      const onToast = vi.fn();
      render(<TenantsPane onToast={onToast} />);

      await waitFor(() => {
        expect(screen.getByTestId('tenant-row-master')).toBeDefined();
        expect(screen.getByTestId('tenant-row-acme')).toBeDefined();
        expect(screen.getByTestId('tenant-row-beta')).toBeDefined();
        expect(screen.getByTestId('btn-edit-acme')).toBeDefined();
        expect(screen.getByTestId('btn-edit-beta')).toBeDefined();
        expect(screen.getByTestId('btn-edit-master')).toBeDefined();
        expect(screen.getByTestId('btn-suspend-acme')).toBeDefined();
        expect(screen.getByTestId('btn-restore-beta')).toBeDefined();
      });
    });

    it('suspends an active workspace and flips status to Suspended with toast', async () => {
      const disableSpy = vi.spyOn(api.admin.workspaces, 'disable').mockResolvedValue({
        workspace: { ...mockWorkspacesList[1], disabled_at: '2026-08-28T00:00:00Z' },
      });
      const onToast = vi.fn();

      render(<TenantsPane onToast={onToast} />);

      await waitFor(() => {
        expect(screen.getByTestId('btn-suspend-acme')).not.toBeNull();
      });

      fireEvent.click(screen.getByTestId('btn-suspend-acme'));

      await waitFor(() => {
        expect(disableSpy).toHaveBeenCalledWith('acme');
        expect(onToast).toHaveBeenCalledWith('Workspace "Acme Corp" suspended');
        expect(screen.getByTestId('btn-restore-acme')).not.toBeNull();
      });
    });

    it('restores a suspended workspace and flips status to Active with toast', async () => {
      const enableSpy = vi.spyOn(api.admin.workspaces, 'enable').mockResolvedValue({
        workspace: { ...mockWorkspacesList[2], disabled_at: null },
      });
      const onToast = vi.fn();

      render(<TenantsPane onToast={onToast} />);

      await waitFor(() => {
        expect(screen.getByTestId('btn-restore-beta')).not.toBeNull();
      });

      fireEvent.click(screen.getByTestId('btn-restore-beta'));

      await waitFor(() => {
        expect(enableSpy).toHaveBeenCalledWith('beta');
        expect(onToast).toHaveBeenCalledWith('Workspace "Beta Co" restored');
        expect(screen.getByTestId('btn-suspend-beta')).not.toBeNull();
      });
    });

    it('creates a new tenant via CreateTenantModal with UserPicker owner', async () => {
      const createSpy = vi.spyOn(api.admin.workspaces, 'create').mockResolvedValue({
        workspace: {
          id: 'ws_gamma',
          slug: 'gamma',
          name: 'Gamma Corp',
          timezone: 'America/New_York',
          is_master: false,
          created_at: '2026-08-28T00:00:00Z',
          updated_at: '2026-08-28T00:00:00Z',
          member_count: 1,
        },
        role: { id: 'r1', workspace_id: 'ws_gamma', name: 'Owner', is_owner: true, permissions: ['*'], built_in: true, created_at: '' },
        member: { workspace_id: 'ws_gamma', user_id: 'u_alice', role_id: 'r1', created_at: '' },
        user: { id: 'u_alice', email: 'alice@acme.dev', name: 'Alice', created_at: '', updated_at: '' },
      });

      const onToast = vi.fn();
      const onCreateSuccess = vi.fn();
      const onClose = vi.fn();

      render(
        <CreateTenantModal
          onClose={onClose}
          onCreateSuccess={onCreateSuccess}
          onToast={onToast}
        />
      );

      await waitFor(() => {
        expect(screen.getByTestId('input-admin-ws-name')).not.toBeNull();
      });

      fireEvent.change(screen.getByTestId('input-admin-ws-name'), { target: { value: 'Gamma Corp' } });
      expect((screen.getByTestId('input-admin-ws-slug') as HTMLInputElement).value).toBe('gamma-corp');
      fireEvent.change(screen.getByTestId('input-admin-ws-slug'), { target: { value: 'gamma' } });
      fireEvent.change(screen.getByTestId('select-admin-ws-tz'), { target: { value: 'America/New_York' } });
      fireEvent.change(screen.getByTestId('input-admin-ws-owner-email'), { target: { value: 'alice@acme.dev' } });

      fireEvent.click(screen.getByTestId('btn-admin-submit-tenant'));

      await waitFor(() => {
        expect(createSpy).toHaveBeenCalledWith({
          name: 'Gamma Corp',
          slug: 'gamma',
          timezone: 'America/New_York',
          owner_email: 'alice@acme.dev',
        });
        expect(onToast).toHaveBeenCalledWith('Created tenant "Gamma Corp"');
        expect(onCreateSuccess).toHaveBeenCalled();
        expect(onClose).toHaveBeenCalled();
      });
    });

    it('opens EditTenantModal and saves name & timezone changes via patch API', async () => {
      const patchSpy = vi.spyOn(api.admin.workspaces, 'patch').mockResolvedValue({
        workspace: {
          ...mockWorkspacesList[1],
          name: 'Acme International',
          timezone: 'Europe/London',
        },
      });

      const onToast = vi.fn();
      render(<TenantsPane onToast={onToast} />);

      await waitFor(() => {
        expect(screen.getByTestId('btn-edit-acme')).not.toBeNull();
      });

      fireEvent.click(screen.getByTestId('btn-edit-acme'));

      await waitFor(() => {
        expect(screen.getByTestId('modal-edit-tenant')).not.toBeNull();
        expect(screen.getByTestId('input-edit-ws-name')).not.toBeNull();
      });

      const nameInput = screen.getByTestId('input-edit-ws-name') as HTMLInputElement;
      expect(nameInput.value).toBe('Acme Corp');

      fireEvent.change(nameInput, { target: { value: 'Acme International' } });
      fireEvent.change(screen.getByTestId('select-edit-ws-tz'), { target: { value: 'Europe/London' } });

      fireEvent.click(screen.getByTestId('btn-save-tenant'));

      await waitFor(() => {
        expect(patchSpy).toHaveBeenCalledWith('acme', {
          name: 'Acme International',
          timezone: 'Europe/London',
        });
        expect(onToast).toHaveBeenCalledWith('Updated tenant "Acme International"');
      });
    });

    it('opens EditTenantModal for the master workspace and saves name & timezone changes', async () => {
      const patchSpy = vi.spyOn(api.admin.workspaces, 'patch').mockResolvedValue({
        workspace: {
          ...mockWorkspacesList[0],
          name: 'Master HQ',
          timezone: 'Asia/Jakarta',
        },
      });

      const onToast = vi.fn();
      render(<TenantsPane onToast={onToast} />);

      await waitFor(() => {
        expect(screen.getByTestId('btn-edit-master')).not.toBeNull();
      });

      fireEvent.click(screen.getByTestId('btn-edit-master'));

      await waitFor(() => {
        expect(screen.getByTestId('modal-edit-tenant')).not.toBeNull();
        expect((screen.getByTestId('input-edit-ws-name') as HTMLInputElement).disabled).toBe(false);
        expect((screen.getByTestId('select-edit-ws-tz') as HTMLInputElement).disabled).toBe(false);
        expect(screen.getByTestId('btn-save-tenant')).not.toBeNull();
      });

      fireEvent.change(screen.getByTestId('input-edit-ws-name'), { target: { value: 'Master HQ' } });
      fireEvent.change(screen.getByTestId('select-edit-ws-tz'), { target: { value: 'Asia/Jakarta' } });
      fireEvent.click(screen.getByTestId('btn-save-tenant'));

      await waitFor(() => {
        expect(patchSpy).toHaveBeenCalledWith('master', {
          name: 'Master HQ',
          timezone: 'Asia/Jakarta',
        });
        expect(onToast).toHaveBeenCalledWith('Updated tenant "Master HQ"');
      });
    });

    it('transfers workspace ownership with confirmation dialog and toast', async () => {
      const transferSpy = vi.spyOn(api.admin.workspaces, 'transferOwner').mockResolvedValue({
        workspace: mockWorkspacesList[1],
        owner: { id: 'u_bob', email: 'bob@beta.dev', name: 'Bob', created_at: '', updated_at: '' },
        role: { id: 'r_owner', workspace_id: 'ws_acme', name: 'Owner', is_owner: true, permissions: ['*'], built_in: true, created_at: '' },
      });

      const onToast = vi.fn();
      render(<TenantsPane onToast={onToast} />);

      await waitFor(() => {
        expect(screen.getByTestId('btn-edit-acme')).not.toBeNull();
      });

      fireEvent.click(screen.getByTestId('btn-edit-acme'));

      await waitFor(() => {
        expect(screen.getByTestId('current-owners-list')).not.toBeNull();
        expect(screen.getAllByText('Alice').length).toBeGreaterThan(0);
      });

      // Select new owner
      fireEvent.change(screen.getByTestId('select-transfer-owner'), { target: { value: 'u_bob' } });

      // Click transfer ownership button -> reveals confirm button
      fireEvent.click(screen.getByTestId('btn-transfer-owner'));

      expect(screen.getByTestId('btn-confirm-transfer-owner')).not.toBeNull();
      fireEvent.click(screen.getByTestId('btn-confirm-transfer-owner'));

      await waitFor(() => {
        expect(transferSpy).toHaveBeenCalledWith('acme', { user_id: 'u_bob' });
        expect(onToast).toHaveBeenCalledWith(
          expect.stringContaining('Transferred ownership of "Acme Corp" to Bob')
        );
      });
    });

    it('adds a member to the workspace with role selection via addMember API', async () => {
      const addMemberSpy = vi.spyOn(api.admin.workspaces, 'addMember').mockResolvedValue({
        member: { workspace_id: 'ws_acme', user_id: 'u_super', role_id: 'r_admin', created_at: '' },
        user: { id: 'u_super', email: 'super@onclaw.local', name: 'Super Admin', created_at: '', updated_at: '' },
        role: { id: 'r_admin', workspace_id: 'ws_acme', name: 'Admin', is_owner: false, permissions: [], built_in: true, created_at: '' },
      });

      const onToast = vi.fn();
      render(<TenantsPane onToast={onToast} />);

      await waitFor(() => {
        expect(screen.getByTestId('btn-edit-acme')).not.toBeNull();
      });

      fireEvent.click(screen.getByTestId('btn-edit-acme'));

      await waitFor(() => {
        expect(screen.getByTestId('tenant-members-list')).not.toBeNull();
      });

      // Pick user to add
      fireEvent.change(screen.getByTestId('select-add-member-user'), { target: { value: 'u_super' } });
      fireEvent.change(screen.getByTestId('select-add-member-role'), { target: { value: 'Admin' } });

      fireEvent.click(screen.getByTestId('btn-add-member'));

      await waitFor(() => {
        expect(addMemberSpy).toHaveBeenCalledWith('acme', {
          user_id: 'u_super',
          role_name: 'Admin',
        });
        expect(onToast).toHaveBeenCalledWith('Added "Super Admin" as Admin');
      });
    });
  });

  describe('5.3 Users Screen', () => {
    beforeEach(() => {
      vi.spyOn(api.admin.users, 'list').mockResolvedValue({ users: mockUsersList });
    });

    it('lists all registered users with avatar, status, and created date', async () => {
      const onToast = vi.fn();
      render(<UsersPane onToast={onToast} />);

      await waitFor(() => {
        expect(screen.getByTestId('user-row-u_super')).toBeDefined();
        expect(screen.getByTestId('user-row-u_alice')).toBeDefined();
        expect(screen.getByTestId('user-row-u_bob')).toBeDefined();
        expect(screen.getByTestId('badge-superadmin-u_super')).toBeDefined();
        expect(screen.getByTestId('btn-demote-user-u_super')).toBeDefined();
        expect(screen.getByTestId('btn-promote-user-u_alice')).toBeDefined();
        expect(screen.getByTestId('btn-promote-user-u_bob')).toBeDefined();
        expect(screen.getByText('1 workspace')).not.toBeNull();
        expect(screen.getByText('3 workspaces')).not.toBeNull();
        expect(screen.getByText('0 workspaces')).not.toBeNull();
        expect(screen.getByTestId('btn-disable-user-u_super')).toBeDefined();
        expect(screen.getByTestId('btn-disable-user-u_alice')).toBeDefined();
        expect(screen.getByTestId('btn-enable-user-u_bob')).toBeDefined();
      });
    });

    it('promotes a user to superadmin via grant API with toast and immediate UI update', async () => {
      const grantSpy = vi.spyOn(api.admin.superadmins, 'grant').mockResolvedValue({
        member: { workspace_id: 'master', user_id: 'u_alice', role_id: 'r_superadmin', created_at: '' },
        user: { id: 'u_alice', email: 'alice@acme.dev', name: 'Alice', created_at: '', updated_at: '' },
        role: { id: 'r_superadmin', workspace_id: 'master', name: 'Superadmin', is_owner: true, permissions: ['*'], built_in: true, created_at: '' },
      });

      const onToast = vi.fn();
      render(<UsersPane onToast={onToast} />);

      await waitFor(() => {
        expect(screen.getByTestId('btn-promote-user-u_alice')).not.toBeNull();
      });

      fireEvent.click(screen.getByTestId('btn-promote-user-u_alice'));

      await waitFor(() => {
        expect(grantSpy).toHaveBeenCalledWith({ user_id: 'u_alice' });
        expect(onToast).toHaveBeenCalledWith('Granted superadmin to "Alice"');
        expect(screen.getByTestId('badge-superadmin-u_alice')).not.toBeNull();
        expect(screen.getByTestId('btn-demote-user-u_alice')).not.toBeNull();
      });
    });

    it('demotes a superadmin via revoke API with toast and immediate UI update', async () => {
      const revokeSpy = vi.spyOn(api.admin.superadmins, 'revoke').mockResolvedValue();

      const onToast = vi.fn();
      render(<UsersPane onToast={onToast} />);

      await waitFor(() => {
        expect(screen.getByTestId('btn-demote-user-u_super')).not.toBeNull();
      });

      fireEvent.click(screen.getByTestId('btn-demote-user-u_super'));

      await waitFor(() => {
        expect(revokeSpy).toHaveBeenCalledWith('u_super');
        expect(onToast).toHaveBeenCalledWith('Revoked superadmin from "Super Admin"');
        expect(screen.queryByTestId('badge-superadmin-u_super')).toBeNull();
        expect(screen.getByTestId('btn-promote-user-u_super')).not.toBeNull();
      });
    });

    it('surfaces last_owner_protected error toast when attempting to demote last superadmin', async () => {
      const revokeSpy = vi.spyOn(api.admin.superadmins, 'revoke').mockRejectedValue(
        new ApiError(409, 'last_owner_protected', 'The last owner cannot be removed or demoted.')
      );

      const onToast = vi.fn();
      render(<UsersPane onToast={onToast} />);

      await waitFor(() => {
        expect(screen.getByTestId('btn-demote-user-u_super')).not.toBeNull();
      });

      fireEvent.click(screen.getByTestId('btn-demote-user-u_super'));

      await waitFor(() => {
        expect(revokeSpy).toHaveBeenCalledWith('u_super');
        expect(onToast).toHaveBeenCalledWith('The last owner cannot be removed or demoted.', 'danger');
        expect(screen.getByTestId('badge-superadmin-u_super')).not.toBeNull();
        expect(screen.getByTestId('btn-demote-user-u_super')).not.toBeNull();
      });
    });

    it('requires two clicks within confirmation window to disable an active user', async () => {
      const disableSpy = vi.spyOn(api.admin.users, 'disable').mockResolvedValue({
        user: { ...mockUsersList[1], disabled_at: '2026-08-28T00:00:00Z' },
      });
      const onToast = vi.fn();

      render(<UsersPane onToast={onToast} />);

      await waitFor(() => {
        expect(screen.getByTestId('btn-disable-user-u_alice')).not.toBeNull();
      });

      const disableBtn = screen.getByTestId('btn-disable-user-u_alice');
      expect(disableBtn.textContent).toBe('Disable');

      // Click 1: enters confirmation
      fireEvent.click(disableBtn);
      expect(disableBtn.textContent).toBe('Confirm disable');
      expect(disableSpy).not.toHaveBeenCalled();

      // Click 2: executes disable
      fireEvent.click(disableBtn);

      await waitFor(() => {
        expect(disableSpy).toHaveBeenCalledWith('u_alice');
        expect(onToast).toHaveBeenCalledWith('User "alice@acme.dev" disabled');
      });
    });

    it('enables a disabled user directly with toast', async () => {
      const enableSpy = vi.spyOn(api.admin.users, 'enable').mockResolvedValue({
        user: { ...mockUsersList[2], disabled_at: null },
      });
      const onToast = vi.fn();

      render(<UsersPane onToast={onToast} />);

      await waitFor(() => {
        expect(screen.getByTestId('btn-enable-user-u_bob')).not.toBeNull();
      });

      fireEvent.click(screen.getByTestId('btn-enable-user-u_bob'));

      await waitFor(() => {
        expect(enableSpy).toHaveBeenCalledWith('u_bob');
        expect(onToast).toHaveBeenCalledWith('User "bob@beta.dev" enabled');
      });
    });

    it('disables the disable action for the current active user to prevent self-disable', async () => {
      const disableSpy = vi.spyOn(api.admin.users, 'disable');
      const onToast = vi.fn();

      render(<UsersPane onToast={onToast} />);

      await waitFor(() => {
        expect(screen.getByTestId('btn-disable-user-u_super')).not.toBeNull();
      });

      const selfDisableBtn = screen.getByTestId('btn-disable-user-u_super') as HTMLButtonElement;
      expect(selfDisableBtn.disabled).toBe(true);
      expect(selfDisableBtn.getAttribute('title')).toBe('You cannot disable your own account');

      fireEvent.click(selfDisableBtn);
      expect(disableSpy).not.toHaveBeenCalled();
    });

    it('creates a new user via CreateUserModal with email, name, and optional password', async () => {
      const createSpy = vi.spyOn(api.admin.users, 'create').mockResolvedValue({
        user: {
          id: 'u_dave',
          email: 'dave@example.com',
          name: 'Dave Smith',
          created_at: '2026-08-28T00:00:00Z',
          updated_at: '2026-08-28T00:00:00Z',
        },
      });

      const onToast = vi.fn();
      const onCreateSuccess = vi.fn();
      const onClose = vi.fn();

      render(
        <CreateUserModal
          onClose={onClose}
          onCreateSuccess={onCreateSuccess}
          onToast={onToast}
        />
      );

      fireEvent.change(screen.getByTestId('input-admin-user-email'), { target: { value: 'dave@example.com' } });
      fireEvent.change(screen.getByTestId('input-admin-user-name'), { target: { value: 'Dave Smith' } });
      fireEvent.change(screen.getByTestId('input-admin-user-password'), { target: { value: 'secretpass123' } });

      fireEvent.click(screen.getByTestId('btn-admin-submit-user'));

      await waitFor(() => {
        expect(createSpy).toHaveBeenCalledWith({
          email: 'dave@example.com',
          name: 'Dave Smith',
          password: 'secretpass123',
        });
        expect(onToast).toHaveBeenCalledWith('Created user "dave@example.com"');
        expect(onCreateSuccess).toHaveBeenCalled();
        expect(onClose).toHaveBeenCalled();
      });
    });
  });
});
