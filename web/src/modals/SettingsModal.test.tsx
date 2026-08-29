import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { SettingsModal } from './SettingsModal';
import { api, ApiError } from '../lib/api';
import { useAuthStore } from '../store/auth';

describe('modals/SettingsModal', () => {
  const mockTenant = {
    id: 'acme',
    sub: 'acme',
    name: 'Acme Corp',
    tz: 'America/Los_Angeles',
    defaultModel: 'claude-sonnet-5',
    retention: '90 days',
    members: [],
    integrations: [],
    keys: [],
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
  });

  describe('Workspace pane & Danger zone', () => {
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

      const onToast = vi.fn();
      const onUpdate = vi.fn();

      render(
        <SettingsModal
          tenant={mockTenant}
          tab="workspace"
          onTab={vi.fn()}
          onClose={vi.fn()}
          onUpdate={onUpdate}
          onToast={onToast}
        />
      );

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
      const onLeaveWorkspace = vi.fn();
      render(
        <SettingsModal
          tenant={mockTenant}
          tab="workspace"
          onTab={vi.fn()}
          onClose={vi.fn()}
          onUpdate={vi.fn()}
          onToast={vi.fn()}
          onLeaveWorkspace={onLeaveWorkspace}
        />
      );

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

      const onToast = vi.fn();

      render(
        <SettingsModal
          tenant={mockTenant}
          tab="workspace"
          onTab={vi.fn()}
          onClose={vi.fn()}
          onUpdate={vi.fn()}
          onToast={onToast}
        />
      );

      const leaveBtn = screen.getByTestId('btn-workspace-leave');
      fireEvent.click(leaveBtn);
      fireEvent.click(leaveBtn);

      await waitFor(() => {
        expect(onToast).toHaveBeenCalledWith('The last owner cannot be removed or demoted.', 'danger');
      });
    });
  });

  describe('Members pane', () => {
    beforeEach(() => {
      vi.spyOn(api.members, 'list').mockResolvedValue({ members: mockMembers });
      vi.spyOn(api.roles, 'list').mockResolvedValue({ roles: mockRoles });
    });

    it('loads and renders members, roles, and invited hints', async () => {
      render(
        <SettingsModal
          tenant={mockTenant}
          tab="members"
          onTab={vi.fn()}
          onClose={vi.fn()}
          onUpdate={vi.fn()}
          onToast={vi.fn()}
        />
      );

      await waitFor(() => {
        expect(screen.getByText('Alice')).not.toBeNull();
        expect(screen.getByText('Bob')).not.toBeNull();
        expect(screen.getByText('carol')).not.toBeNull();
        expect(screen.getByText('You are the owner')).not.toBeNull();
        expect(screen.getByText('Invited')).not.toBeNull();
      });
    });

    it('invites a new member by email with selected role', async () => {
      const addMemberSpy = vi.spyOn(api.members, 'add').mockResolvedValue({
        member: { workspace_id: 'acme', user_id: 'u_new', role_id: 'r_admin', created_at: '' },
        user: { id: 'u_new', email: 'dave@acme.dev', name: 'dave', created_at: '', updated_at: '' },
        role: mockRoles[1],
      });

      const onToast = vi.fn();

      render(
        <SettingsModal
          tenant={mockTenant}
          tab="members"
          onTab={vi.fn()}
          onClose={vi.fn()}
          onUpdate={vi.fn()}
          onToast={onToast}
        />
      );

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
      });
    });

    it('changes a member role inline via API and toasts success', async () => {
      const patchRoleSpy = vi.spyOn(api.members, 'patch').mockResolvedValue({
        member: { workspace_id: 'acme', user_id: 'u_bob', role_id: 'r_member', created_at: '' },
        role: mockRoles[2],
      });

      const onToast = vi.fn();

      render(
        <SettingsModal
          tenant={mockTenant}
          tab="members"
          onTab={vi.fn()}
          onClose={vi.fn()}
          onUpdate={vi.fn()}
          onToast={onToast}
        />
      );

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
      const onToast = vi.fn();

      render(
        <SettingsModal
          tenant={mockTenant}
          tab="members"
          onTab={vi.fn()}
          onClose={vi.fn()}
          onUpdate={vi.fn()}
          onToast={onToast}
        />
      );

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

      const onToast = vi.fn();

      render(
        <SettingsModal
          tenant={mockTenant}
          tab="members"
          onTab={vi.fn()}
          onClose={vi.fn()}
          onUpdate={vi.fn()}
          onToast={onToast}
        />
      );

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
});
