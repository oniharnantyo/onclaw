import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { WorkspaceSwitcher } from './WorkspaceSwitcher';
import type { ApiMemberView } from '../../lib/api';
import { useAuthStore } from '../../store/auth';
import { useStore } from '../../store';

describe('components/nav/WorkspaceSwitcher', () => {
  const mockMemberships: ApiMemberView[] = [
    {
      workspace_id: 'ws_acme',
      workspace_slug: 'acme',
      workspace_name: 'Acme Corp',
      user_id: 'u1',
      email: 'alice@acme.dev',
      name: 'Alice',
      role_id: 'r_owner',
      role_name: 'Owner',
      workspace: {
        id: 'ws_acme',
        slug: 'acme',
        name: 'Acme Corp',
        timezone: 'America/Los_Angeles',
        is_master: false,
        created_at: '',
        updated_at: '',
      },
      joined_at: '',
    },
    {
      workspace_id: 'ws_globex',
      workspace_slug: 'globex',
      workspace_name: 'Globex Inc',
      user_id: 'u1',
      email: 'alice@globex.io',
      name: 'Alice',
      role_id: 'r_admin',
      role_name: 'Admin',
      workspace: {
        id: 'ws_globex',
        slug: 'globex',
        name: 'Globex Inc',
        timezone: 'Europe/Berlin',
        is_master: false,
        created_at: '',
        updated_at: '',
      },
      joined_at: '',
    },
    {
      workspace_id: 'ws_suspended',
      workspace_slug: 'suspended-corp',
      workspace_name: 'Suspended Corp',
      user_id: 'u1',
      email: 'alice@suspended.dev',
      name: 'Alice',
      role_id: 'r_member',
      role_name: 'Member',
      workspace: {
        id: 'ws_suspended',
        slug: 'suspended-corp',
        name: 'Suspended Corp',
        timezone: 'UTC',
        is_master: false,
        disabled_at: '2026-08-28T10:00:00Z',
        created_at: '',
        updated_at: '',
      },
      joined_at: '',
    },
  ];

  it('renders nothing when closed', () => {
    const { container } = render(
      <WorkspaceSwitcher
        open={false}
        memberships={mockMemberships}
        currentId="acme"
        onPick={vi.fn()}
        onClose={vi.fn()}
      />
    );
    expect(container.firstChild).toBeNull();
  });

  it('renders real memberships with names and role badges', () => {
    render(
      <WorkspaceSwitcher
        open={true}
        memberships={mockMemberships}
        currentId="acme"
        onPick={vi.fn()}
        onClose={vi.fn()}
      />
    );

    expect(screen.getByText('Workspaces')).not.toBeNull();
    expect(screen.getByText('Acme Corp')).not.toBeNull();
    expect(screen.getByText('Owner')).not.toBeNull();
    expect(screen.getByText('Globex Inc')).not.toBeNull();
    expect(screen.getByText('Admin')).not.toBeNull();
  });

  it('marks suspended workspaces and blocks selection', () => {
    const onPick = vi.fn();
    render(
      <WorkspaceSwitcher
        open={true}
        memberships={mockMemberships}
        currentId="acme"
        onPick={onPick}
        onClose={vi.fn()}
      />
    );

    expect(screen.getByText('Suspended Corp')).not.toBeNull();
    expect(screen.getByText('Suspended')).not.toBeNull();

    const suspendedBtn = screen.getByRole('button', { name: /suspended corp/i });
    expect(suspendedBtn).not.toBeNull();
    expect((suspendedBtn as HTMLButtonElement).disabled).toBe(true);

    fireEvent.click(suspendedBtn);
    expect(onPick).not.toHaveBeenCalled();
  });

  it('calls onPick when active workspace is clicked', () => {
    const onPick = vi.fn();
    render(
      <WorkspaceSwitcher
        open={true}
        memberships={mockMemberships}
        currentId="acme"
        onPick={onPick}
        onClose={vi.fn()}
      />
    );

    const globexBtn = screen.getByRole('button', { name: /globex inc/i });
    fireEvent.click(globexBtn);

    expect(onPick).toHaveBeenCalledWith('globex', expect.objectContaining({ workspace_slug: 'globex' }));
  });
});

// fix-role-permission-audit 5.3: the switcher's only creation affordance
// renders exclusively to instance administrators (master-workspace members
// holding admin.workspaces.write — the useIsAdmin gate, exactly the backend's
// POST /workspaces guard).
describe('components/nav/WorkspaceSwitcher — admin-only creation entry', () => {
  const adminUser = { id: 'u_super', email: 'admin@master.dev', name: 'Super Admin', created_at: '', updated_at: '' };
  const masterMembership = {
    workspace_id: 'master',
    workspace_slug: 'master',
    workspace_name: 'Master Control',
    user_id: 'u_super',
    email: 'admin@master.dev',
    name: 'Super Admin',
    role_id: 'r_superadmin',
    role_name: 'Superadmin',
    role: { id: 'r_superadmin', workspace_id: 'master', name: 'Superadmin', is_owner: true, permissions: ['*'], built_in: true, created_at: '' },
    workspace: { id: 'master', slug: 'master', name: 'Master Control', timezone: 'UTC', is_master: true, created_at: '', updated_at: '' },
    joined_at: '',
  };
  const memberMembership = {
    workspace_id: 'ws_acme',
    workspace_slug: 'acme',
    workspace_name: 'Acme Corp',
    user_id: 'u_user',
    email: 'user@acme.dev',
    name: 'User',
    role_id: 'r_member',
    role_name: 'Member',
    role: {
      id: 'r_member',
      workspace_id: 'acme',
      name: 'Member',
      is_owner: false,
      permissions: [
        'workspace.read',
        'members.read',
        'roles.read',
        'providers.read',
        'agents.read',
        'skills.read',
        'scheduler.read',
        'channels.read',
        'channels.write',
      ],
      built_in: true,
      created_at: '',
    },
    workspace: { id: 'ws_acme', slug: 'acme', name: 'Acme Corp', timezone: 'UTC', is_master: false, created_at: '', updated_at: '' },
    joined_at: '',
  };

  beforeEach(() => {
    useAuthStore.setState({ user: null, memberships: [], status: 'authenticated' });
    useStore.setState({ pos: { tenantId: 'acme', view: 'chats', chatId: '', showContext: false, railExpanded: false } });
  });

  it('hides both creation entries from a non-instance-admin', () => {
    useAuthStore.setState({ user: { id: 'u_user', email: 'user@acme.dev', name: 'User', created_at: '', updated_at: '' }, memberships: [memberMembership] });
    useStore.setState({ pos: { tenantId: 'acme', view: 'chats', chatId: '', showContext: false, railExpanded: false } });

    render(
      <WorkspaceSwitcher
        open={true}
        memberships={[memberMembership]}
        currentId="acme"
        onPick={vi.fn()}
        onClose={vi.fn()}
        onCreateWorkspace={vi.fn()}
      />
    );

    expect(screen.getByText('Acme Corp')).not.toBeNull();
    expect(screen.queryByTestId('btn-switcher-create-ws')).toBeNull();
    expect(screen.queryByTestId('btn-create-workspace-footer')).toBeNull();
  });

  it('renders the creation entries for an instance administrator', () => {
    useAuthStore.setState({ user: adminUser, memberships: [masterMembership] });
    useStore.setState({ pos: { tenantId: 'master', view: 'chats', chatId: '', showContext: false, railExpanded: false } });

    const onCreate = vi.fn();
    render(
      <WorkspaceSwitcher
        open={true}
        memberships={[masterMembership]}
        currentId="master"
        onPick={vi.fn()}
        onClose={vi.fn()}
        onCreateWorkspace={onCreate}
      />
    );

    expect(screen.getByTestId('btn-switcher-create-ws')).not.toBeNull();
    fireEvent.click(screen.getByTestId('btn-create-workspace-footer'));
    expect(onCreate).toHaveBeenCalledTimes(1);
  });
});
