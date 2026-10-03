import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react';
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
};const membership = (roleName: string, permissions: string[]) => ({
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
  // Member carries the decided built-in set (reads + channels.read/write);
  // roles.write is gone from the catalog entirely.
  const permissions =
    role === 'Member'
      ? ['workspace.read', 'members.read', 'roles.read', 'providers.read', 'agents.read', 'skills.read', 'scheduler.read', 'channels.read', 'channels.write']
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

describe('screens/settings/WorkspaceSection default model (refactor-workspace-settings)', () => {
  const providers = [
    {
      id: 'prov_anthropic',
      workspace_id: 'acme',
      type: 'anthropic',
      name: 'Anthropic Prod',
      base_url: '',
      key_set: true,
      key_hint: '7f3a',
      enabled: true,
      created_at: '',
      updated_at: '',
    },
    {
      id: 'prov_openai',
      workspace_id: 'acme',
      type: 'openai',
      name: 'OpenAI Prod',
      base_url: '',
      key_set: true,
      key_hint: '9k2b',
      enabled: true,
      created_at: '',
      updated_at: '',
    },
  ];

  const modelCatalog = (ws: string, id: string) => {
    if (id === 'prov_openai') {
      return Promise.resolve({
        source: 'live' as const,
        models: [
          { id: 'gpt-4o', name: 'GPT-4o' },
          { id: 'gpt-4o-mini', name: 'GPT-4o mini' },
        ],
      });
    }
    return Promise.resolve({
      source: 'live' as const,
      models: [{ id: 'claude-3-7-sonnet', name: 'Claude 3.7 Sonnet', efforts: ['low', 'high'] }],
    });
  };

  const workspacePayload = (defaultModel: unknown) => ({
    workspace: {
      id: 'acme',
      slug: 'acme',
      name: 'Acme Corp',
      timezone: 'America/Los_Angeles',
      is_master: false,
      default_model: defaultModel,
      created_at: '',
      updated_at: '',
    },
  });

  function setupApi(defaultModel: unknown) {
    vi.spyOn(api.memory, 'getWorkspace').mockResolvedValue({
      content: '',
      max_chars: 8192,
      updated_at: null,
    });
    vi.spyOn(api.workspaces, 'get').mockResolvedValue(workspacePayload(defaultModel) as any);
    vi.spyOn(api.providers, 'list').mockResolvedValue({ providers });
    vi.spyOn(api.providers, 'models').mockImplementation(modelCatalog as any);
  }

  function setup(
    role: 'Owner' | 'Admin' | 'Member',
    defaultModel: unknown,
    props: { onToast?: (text: string, kind?: string) => void; onUpdate?: (fn: any) => void } = {}
  ) {
    const permissions =
      role === 'Member'
        ? ['workspace.read', 'members.read', 'roles.read', 'providers.read', 'agents.read', 'skills.read', 'scheduler.read', 'channels.read', 'channels.write']
        : role === 'Admin'
          ? ['workspace.write', 'agents.read']
          : ['*'];
    useAuthStore.setState({
      user: { id: 'u1', email: 'u1@acme.dev', name: 'U One', created_at: '', updated_at: '' },
      memberships: [membership(role, permissions)],
      status: 'authenticated',
    });
    setupApi(defaultModel);
    const onToast = props.onToast || vi.fn();
    const onUpdate = props.onUpdate || vi.fn();
    render(<WorkspaceSection tenant={tenant} onToast={onToast} onUpdate={onUpdate} />);
    return { onToast, onUpdate };
  }

  const providerSelect = () => screen.getByTestId('select-ws-default-provider') as HTMLSelectElement;
  const modelSelect = () => screen.getByTestId('select-model') as HTMLSelectElement;

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('hydrates the saved pair from the workspace payload, persists a change, and a reload keeps it', async () => {
    setup('Admin', { provider_id: 'prov_openai', model: 'gpt-4o' });

    // Hydrated pair: provider select and model combobox mirror the payload.
    await waitFor(() => {
      expect(providerSelect().value).toBe('prov_openai');
    });
    await waitFor(() => {
      expect(modelSelect().value).toBe('gpt-4o');
    });

    // Pick another model from the same provider's catalog and save.
    fireEvent.change(modelSelect(), { target: { value: 'gpt-4o-mini' } });
    const patch = vi.spyOn(api.workspaces, 'patch').mockResolvedValue(
      workspacePayload({ provider_id: 'prov_openai', model: 'gpt-4o-mini' }) as any
    );
    fireEvent.click(screen.getByTestId('btn-workspace-save'));

    await waitFor(() => {
      expect(patch).toHaveBeenCalledWith(
        'acme',
        expect.objectContaining({
          name: 'Acme Corp',
          timezone: 'America/Los_Angeles',
          default_model: { provider_id: 'prov_openai', model: 'gpt-4o-mini' },
        })
      );
    });

    // Reload: a fresh pane reads the persisted pair back from the payload.
    cleanup();
    setupApi({ provider_id: 'prov_openai', model: 'gpt-4o-mini' });
    render(<WorkspaceSection tenant={tenant} onToast={vi.fn()} onUpdate={vi.fn()} />);
    await waitFor(() => {
      expect(modelSelect().value).toBe('gpt-4o-mini');
    });
    expect(providerSelect().value).toBe('prov_openai');
  });

  it('switching provider re-queries that catalog and clears the previously chosen model', async () => {
    setup('Admin', { provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet' });

    await waitFor(() => {
      expect(modelSelect().value).toBe('claude-3-7-sonnet');
    });

    fireEvent.change(providerSelect(), { target: { value: 'prov_openai' } });

    await waitFor(() => {
      expect(api.providers.models).toHaveBeenCalledWith('acme', 'prov_openai');
    });
    // The stored Anthropic model id is gone — the OpenAI catalog took over.
    await waitFor(() => {
      expect(modelSelect().value).toBe('gpt-4o');
    });
    expect(modelSelect().value).not.toBe('claude-3-7-sonnet');
  });

  it('the default-model picker never lists a decision (typesafe) config (5.3)', async () => {
    vi.spyOn(api.memory, 'getWorkspace').mockResolvedValue({
      content: '',
      max_chars: 8192,
      updated_at: null,
    });
    vi.spyOn(api.workspaces, 'get').mockResolvedValue(workspacePayload(null) as any);
    vi.spyOn(api.providers, 'list').mockResolvedValue({
      providers: [
        ...providers,
        {
          id: 'prov_typesafe',
          workspace_id: 'acme',
          type: 'typesafe',
          name: 'TypeSafe Routing',
          base_url: '',
          key_set: true,
          key_hint: 'ab12',
          enabled: true,
          created_at: '',
          updated_at: '',
        },
      ],
    });
    vi.spyOn(api.providers, 'models').mockImplementation(modelCatalog as any);
    render(<WorkspaceSection tenant={tenant} onToast={vi.fn()} onUpdate={vi.fn()} />);

    await waitFor(() => {
      expect(api.providers.list).toHaveBeenCalledWith('acme');
    });
    const options = Array.from(providerSelect().options).map((o) => o.textContent);
    expect(options).toContain('Anthropic Prod (Anthropic)');
    expect(options).not.toContain('TypeSafe Routing');
  });

  it('clearing both empties the default: PATCH carries null and the toast confirms', async () => {
    const { onToast } = setup('Admin', { provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet' });

    await waitFor(() => {
      expect(modelSelect().value).toBe('claude-3-7-sonnet');
    });

    const patch = vi
      .spyOn(api.workspaces, 'patch')
      .mockResolvedValue(workspacePayload(null) as any);

    fireEvent.change(providerSelect(), { target: { value: '' } });
    fireEvent.click(screen.getByTestId('btn-workspace-save'));

    await waitFor(() => {
      expect(patch).toHaveBeenCalledWith(
        'acme',
        expect.objectContaining({ default_model: null })
      );
      expect(onToast).toHaveBeenCalledWith('Workspace settings saved');
    });
  });

  it('surfaces a clear-blocked 422 as an error toast naming the inherit-agents count', async () => {
    const { onToast } = setup('Admin', { provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet' });

    await waitFor(() => {
      expect(modelSelect().value).toBe('claude-3-7-sonnet');
    });

    vi.spyOn(api.workspaces, 'patch').mockRejectedValue(
      new ApiError(422, 'invalid_request', 'Cannot clear the default model — 2 agents inherit it')
    );

    fireEvent.change(providerSelect(), { target: { value: '' } });
    fireEvent.click(screen.getByTestId('btn-workspace-save'));

    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith(
        expect.stringContaining('2 agents'),
        'danger'
      );
    });
  });

  it('never sends default_model when the picker was left untouched', async () => {
    setup('Admin', { provider_id: 'prov_openai', model: 'gpt-4o' });

    await waitFor(() => {
      expect(modelSelect().value).toBe('gpt-4o');
    });

    const patch = vi
      .spyOn(api.workspaces, 'patch')
      .mockResolvedValue(workspacePayload({ provider_id: 'prov_openai', model: 'gpt-4o' }) as any);

    fireEvent.change(screen.getByLabelText(/workspace name/i), { target: { value: 'Renamed Corp' } });
    fireEvent.click(screen.getByTestId('btn-workspace-save'));

    await waitFor(() => {
      expect(patch).toHaveBeenCalledWith('acme', { name: 'Renamed Corp', timezone: 'America/Los_Angeles' });
    });
  });

  it('leaves a read-only Member untouched: fields disabled, no save control, memory editor read-only', async () => {
    setup('Member', { provider_id: 'prov_openai', model: 'gpt-4o' });

    await waitFor(() => {
      expect(providerSelect().value).toBe('prov_openai');
    });
    await waitFor(() => {
      expect(modelSelect().value).toBe('gpt-4o');
    });

    // Fields render read-only and the save control is hidden (web-app/settings
    // Workspace pane requirement, fix-role-permission-audit 5.1).
    expect((providerSelect() as HTMLSelectElement).disabled).toBe(true);
    expect((screen.getByLabelText(/workspace name/i) as HTMLInputElement).disabled).toBe(true);
    expect(screen.queryByTestId('btn-workspace-save')).toBeNull();

    const textarea = (await waitFor(() => screen.getByTestId('textarea-workspace-memory'))) as HTMLTextAreaElement;
    expect(textarea.disabled).toBe(true);
    expect(screen.queryByTestId('btn-workspace-memory-save')).toBeNull();
  });
});
