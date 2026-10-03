import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { KeysSection } from './KeysSection';
import { apiKeys, type ApiWorkspaceKey } from '../../lib/api';
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

const tenant = { id: 'acme', sub: 'acme', name: 'Acme Corp', keys: [] };

const keyRow = (id: string, createdBy: string): ApiWorkspaceKey => ({
  id,
  name: `Key ${id}`,
  key_prefix: 'oc_live',
  key_suffix: id.slice(-4),
  created_by: createdBy,
  created_at: '2026-09-01T00:00:00Z',
  revoked_at: null,
});

function setup(user: { id: string; email: string } | null, memberships: any[]) {
  useAuthStore.setState({ user, memberships, status: 'authenticated' } as any);
  return render(<KeysSection tenant={tenant} onToast={vi.fn()} onUpdate={vi.fn()} />);
}

describe('screens/settings/KeysSection — creator symmetry (fix-role-permission-audit 5.2)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('a Member sees only the keys they created, with reveal/copy/revoke', async () => {
    const listSpy = vi
      .spyOn(apiKeys, 'list')
      .mockResolvedValue({
        api_keys: [keyRow('k_mine', 'u1'), keyRow('k_other', 'u2')],
      });

    setup({ id: 'u1', email: 'u1@acme.dev' }, [
      {
        workspace_id: 'acme',
        workspace_slug: 'acme',
        role_name: 'Member',
        role: { name: 'Member', is_owner: false, permissions: MEMBER_PERMS },
      },
    ]);

    await waitFor(() => {
      expect(screen.getByText('Key k_mine')).not.toBeNull();
    });
    expect(screen.queryByText('Key k_other')).toBeNull();
    expect(listSpy).toHaveBeenCalledWith('acme');

    // The own row carries the revoke affordance (reveal/copy ride the
    // one-time creation plaintext, unchanged).
    expect(screen.getByRole('button', { name: 'Revoke' })).not.toBeNull();
  });

  it('a workspace.write holder sees every workspace key', async () => {
    vi.spyOn(apiKeys, 'list').mockResolvedValue({
      api_keys: [keyRow('k_mine', 'u1'), keyRow('k_other', 'u2')],
    });

    setup({ id: 'u1', email: 'u1@acme.dev' }, [
      {
        workspace_id: 'acme',
        workspace_slug: 'acme',
        role_name: 'Admin',
        role: { name: 'Admin', is_owner: false, permissions: ['workspace.write'] },
      },
    ]);

    await waitFor(() => {
      expect(screen.getByText('Key k_mine')).not.toBeNull();
    });
    expect(screen.getByText('Key k_other')).not.toBeNull();
  });

  it('a member whose id matches no created_by sees an empty list, not other keys', async () => {
    vi.spyOn(apiKeys, 'list').mockResolvedValue({
      api_keys: [keyRow('k_other', 'u2')],
    });

    setup({ id: 'u1', email: 'u1@acme.dev' }, [
      {
        workspace_id: 'acme',
        workspace_slug: 'acme',
        role_name: 'Member',
        role: { name: 'Member', is_owner: false, permissions: MEMBER_PERMS },
      },
    ]);

    await waitFor(() => {
      expect(screen.getByTestId('keys-empty')).not.toBeNull();
    });
    expect(screen.queryByText('Key k_other')).toBeNull();
    // Key minting stays member-level: the create affordance remains.
    expect(screen.getByTestId('btn-key-empty-add')).not.toBeNull();
  });

  it('mock mode (no memberships) keeps the full list visible', async () => {
    vi.spyOn(apiKeys, 'list').mockResolvedValue({
      api_keys: [keyRow('k_mine', 'u1'), keyRow('k_other', 'u2')],
    });

    setup(null, []);

    await waitFor(() => {
      expect(screen.getByText('Key k_mine')).not.toBeNull();
    });
    expect(screen.getByText('Key k_other')).not.toBeNull();
  });
});
