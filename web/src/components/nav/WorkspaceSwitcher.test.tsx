import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { WorkspaceSwitcher } from './WorkspaceSwitcher';
import type { ApiMemberView } from '../../lib/api';

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
