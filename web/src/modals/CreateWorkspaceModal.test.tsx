import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { CreateWorkspaceModal } from './CreateWorkspaceModal';
import { api, ApiError } from '../lib/api';

describe('modals/CreateWorkspaceModal', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(api.providers, 'modelsPreview').mockResolvedValue({
      source: 'live',
      models: [{ id: 'claude-3-7-sonnet', name: 'Claude 3.7 Sonnet' }],
    });
  });

  it('runs multi-step workspace creation with provider and starter agent atomic birth', async () => {
    const onCreateSuccess = vi.fn();
    const onToast = vi.fn();

    vi.spyOn(api.workspaces, 'create').mockResolvedValue({
      workspace: {
        id: 'ws-new',
        slug: 'beta-team',
        name: 'Beta Team',
        timezone: 'America/Los_Angeles',
        is_master: false,
        created_at: '',
        updated_at: '',
      },
      role: {
        id: 'r-owner',
        workspace_id: 'ws-new',
        name: 'Owner',
        is_owner: true,
        permissions: [],
        built_in: true,
        created_at: '',
      },
      member: {
        workspace_id: 'ws-new',
        user_id: 'u1',
        role_id: 'r-owner',
        created_at: '',
      },
      starter_agent: {
        id: 'agent-starter',
        workspace_id: 'ws-new',
        slug: 'radar',
        name: 'Radar',
        role: 'Research',
        description: '',
        brief: 'Research topics',
        identity: '',
        soul: '',
        provider_id: 'prov-1',
        model: 'claude-3-7-sonnet',
        temperature: 1.0,
        autonomy: 'approval',
        disabled_tools: [],
        skills: [],
        enabled_mcps: [],
        avatar: {},
        prompts_status: 'generating',
        created_at: '',
        updated_at: '',
      },
    });

    render(
      <CreateWorkspaceModal
        onClose={vi.fn()}
        onCreateSuccess={onCreateSuccess}
        onToast={onToast}
      />
    );

    // Step 1: Fill name and check slug auto-suggest
    const nameInput = screen.getByTestId('input-ws-name');
    fireEvent.change(nameInput, { target: { value: 'Beta Team' } });

    const slugInput = screen.getByTestId('input-ws-slug') as HTMLInputElement;
    expect(slugInput.value).toBe('beta-team');

    // Go to Step 2
    fireEvent.click(screen.getByTestId('btn-ws-next-step1'));

    await waitFor(() => {
      expect(screen.getByTestId('ws-step-2')).not.toBeNull();
    });

    // Step 2: Provider step
    const provNameInput = screen.getByTestId('input-provider-name');
    expect(provNameInput).not.toBeNull();

    const provKeyInput = screen.getByTestId('input-provider-key');
    fireEvent.change(provKeyInput, { target: { value: 'sk-ant-test' } });

    // Go to Step 3
    fireEvent.click(screen.getByTestId('btn-ws-next-step2'));

    await waitFor(() => {
      expect(screen.getByTestId('ws-step-3')).not.toBeNull();
    });

    // Step 3: Starter Agent
    const agentNameInput = screen.getByTestId('input-starter-agent-name');
    expect(agentNameInput).not.toBeNull();

    // Submit birth
    fireEvent.click(screen.getByTestId('btn-ws-submit-birth'));

    await waitFor(() => {
      expect(api.workspaces.create).toHaveBeenCalledWith({
        name: 'Beta Team',
        slug: 'beta-team',
        timezone: 'America/Los_Angeles',
        provider: {
          type: 'anthropic',
          name: 'Anthropic Primary',
          base_url: undefined,
          key: 'sk-ant-test',
          enabled: true,
        },
        starter_agent: expect.objectContaining({
          name: 'Radar',
          slug: 'radar',
          model: 'claude-3-7-sonnet',
          temperature: 1.0,
          autonomy: 'approval',
          // Design D1: the starter agent ships with an empty opt-in set under
          // the renamed field — the legacy `mcp` key is gone.
          enabled_mcps: [],
        }),
      });
    });
    const birthPayload = (api.workspaces.create as any).mock.calls[0][0];
    expect(birthPayload.starter_agent).not.toHaveProperty('mcp');

    expect(onCreateSuccess).toHaveBeenCalled();
  });

  it('keeps entered data intact when atomic birth API returns 400 error', async () => {
    const onToast = vi.fn();

    vi.spyOn(api.workspaces, 'create').mockRejectedValue(
      new ApiError(400, 'invalid_request', 'Invalid starter agent slug', [
        { field: 'starter_agent.slug', message: 'slug must contain only lowercase letters and numbers' },
      ])
    );

    render(
      <CreateWorkspaceModal
        onClose={vi.fn()}
        onToast={onToast}
      />
    );

    // Step 1
    fireEvent.change(screen.getByTestId('input-ws-name'), { target: { value: 'Beta Team' } });
    fireEvent.click(screen.getByTestId('btn-ws-next-step1'));

    // Step 2
    await waitFor(() => {
      expect(screen.getByTestId('ws-step-2')).not.toBeNull();
    });
    fireEvent.change(screen.getByTestId('input-provider-key'), { target: { value: 'sk-ant-key-123' } });
    fireEvent.click(screen.getByTestId('btn-ws-next-step2'));

    // Step 3
    await waitFor(() => {
      expect(screen.getByTestId('ws-step-3')).not.toBeNull();
    });

    // Enter rejected starter agent slug
    fireEvent.change(screen.getByTestId('input-starter-agent-slug'), { target: { value: 'bad-slug-rejected' } });

    // Submit birth
    fireEvent.click(screen.getByTestId('btn-ws-submit-birth'));

    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('Invalid starter agent slug', 'danger');
    });

    // User stays on Step 3 with entered data preserved
    expect(screen.getByTestId('ws-step-3')).not.toBeNull();
    const slugInput = screen.getByTestId('input-starter-agent-slug') as HTMLInputElement;
    expect(slugInput.value).toBe('bad-slug-rejected');

    // Field error is displayed
    expect(screen.getByText('slug must contain only lowercase letters and numbers')).not.toBeNull();
  });
});
