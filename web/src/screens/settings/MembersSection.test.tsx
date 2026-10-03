import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MembersSection } from './MembersSection';
import { api, type ApiMemberItem, type ApiRole } from '../../lib/api';
import { useAuthStore } from '../../store/auth';

// Built-in Member set per the decided catalog (fix-role-permission-audit):
// reads + channels.read/channels.write — roles.write is gone.
const MEMBER_PERMS = [
  'workspace.read',
  'members.read',
  'roles.read',
  'providers.read',
  'agents.read',
  'skills.read',
  'scheduler.read',
  'channels.read',
  'channels.write',
];

const tenant = { id: 'acme', sub: 'acme', name: 'Acme Corp' };

const roles: ApiRole[] = [
  { id: 'r_owner', workspace_id: 'acme', name: 'Owner', is_owner: true, permissions: ['*'], built_in: true, created_at: '' },
  { id: 'r_admin', workspace_id: 'acme', name: 'Admin', is_owner: false, permissions: ['workspace.*'], built_in: true, created_at: '' },
  { id: 'r_member', workspace_id: 'acme', name: 'Member', is_owner: false, permissions: MEMBER_PERMS, built_in: true, created_at: '' },
];

const member = (over: Partial<ApiMemberItem>): ApiMemberItem =>
  ({
    user_id: 'u_bob',
    email: 'bob@acme.dev',
    name: 'Bob',
    role_id: 'r_member',
    role_name: 'Member',
    joined_at: '2026-08-02T00:00:00Z',
    ...over,
  }) as ApiMemberItem;

const bob = member({});

function setup(currentUser: any, memberships: any[]) {
  useAuthStore.setState({ user: currentUser, memberships, status: 'authenticated' } as any);
  const listMembers = vi.spyOn(api.members, 'list').mockResolvedValue({ members: [bob] });
  const listRoles = vi.spyOn(api.roles, 'list').mockResolvedValue({ roles });
  render(<MembersSection tenant={tenant} onToast={vi.fn()} />);
  return { listMembers, listRoles };
}

describe('screens/settings/MembersSection — members.write / members.remove gating (fix-role-permission-audit 5.1)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('a Member sees the roster read-only — no invite control, role selector, or remove action', async () => {
    setup(
      { id: 'u_carol', email: 'carol@acme.dev', name: 'Carol', created_at: '', updated_at: '' },
      [
        {
          workspace_id: 'acme',
          workspace_slug: 'acme',
          role_name: 'Member',
          role: { name: 'Member', is_owner: false, permissions: MEMBER_PERMS },
        },
      ]
    );

    await waitFor(() => {
      expect(screen.getByText('Bob')).not.toBeNull();
    });

    // The roster renders fully…
    expect(screen.getByText('bob@acme.dev')).not.toBeNull();
    // …but no mutation affordance does. The role shows as a read-only chip.
    expect(screen.queryByTestId('btn-invite-open')).toBeNull();
    expect(screen.queryByLabelText('Role for Bob')).toBeNull();
    expect(screen.queryByLabelText('Remove Bob')).toBeNull();
    expect(screen.getByText('Member')).not.toBeNull();
  });

  it('an Admin gets the invite control, the inline role selector, and remove', async () => {
    setup(
      { id: 'u_alice', email: 'alice@acme.dev', name: 'Alice', created_at: '', updated_at: '' },
      [
        {
          workspace_id: 'acme',
          workspace_slug: 'acme',
          role_name: 'Admin',
          role: { name: 'Admin', is_owner: false, permissions: ['workspace.*'] },
        },
      ]
    );

    await waitFor(() => {
      expect(screen.getByTestId('btn-invite-open')).not.toBeNull();
    });
    expect(screen.getByLabelText('Role for Bob')).not.toBeNull();
    expect(screen.getByLabelText('Remove Bob')).not.toBeNull();
  });

  it('mock mode (no memberships) keeps every affordance visible', async () => {
    setup(
      { id: 'u_alice', email: 'alice@acme.dev', name: 'Alice', created_at: '', updated_at: '' },
      []
    );

    await waitFor(() => {
      expect(screen.getByTestId('btn-invite-open')).not.toBeNull();
    });
    expect(screen.getByLabelText('Role for Bob')).not.toBeNull();
    expect(screen.getByLabelText('Remove Bob')).not.toBeNull();
  });
});
