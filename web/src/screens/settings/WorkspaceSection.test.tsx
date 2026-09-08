import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { WorkspaceSection } from './WorkspaceSection';
import { api, ApiError } from '../../lib/api';
import { useAuthStore } from '../../store/auth';

const tenant = {
  id: 'acme',
  sub: 'acme',
  name: 'Acme Corp',
  tz: 'America/Los_Angeles',
  defaultModel: 'claude-sonnet-5',
  retention: '90 days',
};

const membership = (roleName: string, permissions: string[]) => ({
  workspace_id: 'acme',
  workspace_slug: 'acme',
  user_id: 'u1',
  email: 'u1@acme.dev',
  name: 'U One',
  role_id: `r_${roleName.toLowerCase()}`,
  role_name: roleName,
  role: {
    id: `r_${roleName.toLowerCase()}`,
    workspace_id: 'acme',
    name: roleName,
    is_owner: roleName === 'Owner',
    permissions,
    built_in: true,
    created_at: '',
  },
  joined_at: '',
});

function setup(role: 'Owner' | 'Admin' | 'Member') {
  const permissions =
    role === 'Member'
      ? ['workspace.read', 'agents.read']
      : role === 'Admin'
        ? ['workspace.write', 'agents.read']
        : ['*'];
  useAuthStore.setState({
    user: { id: 'u1', email: 'u1@acme.dev', name: 'U One', created_at: '', updated_at: '' },
    memberships: [membership(role, permissions)],
    status: 'authenticated',
  });
  return render(<WorkspaceSection tenant={tenant} onToast={vi.fn()} onUpdate={vi.fn()} />);
}

describe('screens/settings/WorkspaceSection shared memory', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('loads WORKSPACE.md for everyone and shows the counter from the server cap', async () => {
    vi.spyOn(api.memory, 'getWorkspace').mockResolvedValue({
      content: 'Existing notes.',
      max_chars: 8192,
      updated_at: null,
    });

    setup('Admin');

    const textarea = (await waitFor(() => screen.getByTestId('textarea-workspace-memory'))) as HTMLTextAreaElement;
    expect(textarea.value).toBe('Existing notes.');
    expect(screen.getByTestId('ws-memory-counter').textContent).toContain('/ 8192 chars');
  });

  it('saves shared memory via its own endpoint and PUT without touching workspace fields', async () => {
    vi.spyOn(api.memory, 'getWorkspace').mockResolvedValue({
      content: 'Existing notes.',
      max_chars: 8192,
      updated_at: null,
    });
    const updateMemory = vi
      .spyOn(api.memory, 'updateWorkspace')
      .mockResolvedValue({ content: 'Updated shared.', max_chars: 8192, updated_at: '2026-09-01T00:00:00Z' });
    const patch = vi.spyOn(api.workspaces, 'patch');

    setup('Admin');

    const textarea = (await waitFor(() => screen.getByTestId('textarea-workspace-memory'))) as HTMLTextAreaElement;
    fireEvent.change(textarea, { target: { value: 'Updated shared.' } });
    fireEvent.click(screen.getByTestId('btn-workspace-memory-save'));

    await waitFor(() => {
      expect(updateMemory).toHaveBeenCalledWith('acme', { content: 'Updated shared.' });
    });
    await waitFor(() => {
      expect(screen.getByTestId('ws-memory-saved')).not.toBeNull();
    });
    // Independent submit: the workspace-details PATCH is never fired
    expect(patch).not.toHaveBeenCalled();
  });

  it('saves a workspace rename without rewriting memory', async () => {
    vi.spyOn(api.memory, 'getWorkspace').mockResolvedValue({
      content: 'Do not touch.',
      max_chars: 8192,
      updated_at: null,
    });
    const updateMemory = vi.spyOn(api.memory, 'updateWorkspace');
    const patch = vi.spyOn(api.workspaces, 'patch').mockResolvedValue({
      workspace: {
        id: 'acme',
        slug: 'acme',
        name: 'Renamed Corp',
        timezone: 'America/Los_Angeles',
        is_master: false,
        created_at: '',
        updated_at: '',
      },
    });

    setup('Admin');

    await waitFor(() => {
      expect(screen.getByTestId('textarea-workspace-memory')).not.toBeNull();
    });
    fireEvent.change(screen.getByLabelText(/workspace name/i), { target: { value: 'Renamed Corp' } });
    fireEvent.click(screen.getByTestId('btn-workspace-save'));

    await waitFor(() => {
      expect(patch).toHaveBeenCalledWith('acme', { name: 'Renamed Corp', timezone: 'America/Los_Angeles' });
    });
    expect(updateMemory).not.toHaveBeenCalled();
    expect((screen.getByTestId('textarea-workspace-memory') as HTMLTextAreaElement).value).toBe(
      'Do not touch.'
    );
  });

  it('shows the editor read-only for Members with no save control', async () => {
    vi.spyOn(api.memory, 'getWorkspace').mockResolvedValue({
      content: 'Shared context.',
      max_chars: 8192,
      updated_at: null,
    });

    setup('Member');

    const textarea = (await waitFor(() => screen.getByTestId('textarea-workspace-memory'))) as HTMLTextAreaElement;
    expect(textarea.disabled).toBe(true);
    expect(textarea.value).toBe('Shared context.');
    expect(screen.queryByTestId('btn-workspace-memory-save')).toBeNull();
  });

  it('surfaces an over-cap 422 inline with the content intact', async () => {
    vi.spyOn(api.memory, 'getWorkspace').mockResolvedValue({
      content: '',
      max_chars: 10,
      updated_at: null,
    });
    vi.spyOn(api.memory, 'updateWorkspace').mockRejectedValue(
      new ApiError(422, 'invalid_request', 'Memory exceeds the 10 character limit')
    );

    setup('Owner');

    const textarea = (await waitFor(() => screen.getByTestId('textarea-workspace-memory'))) as HTMLTextAreaElement;
    fireEvent.change(textarea, { target: { value: 'way more than ten characters' } });
    fireEvent.click(screen.getByTestId('btn-workspace-memory-save'));

    const err = await waitFor(() => screen.getByTestId('ws-memory-error'));
    expect(err.textContent).toContain('10 character limit');
    expect((screen.getByTestId('textarea-workspace-memory') as HTMLTextAreaElement).value).toBe(
      'way more than ten characters'
    );
    expect(screen.queryByTestId('ws-memory-saved')).toBeNull();
  });
});
