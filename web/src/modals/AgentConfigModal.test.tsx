import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { AgentConfigModal } from './AgentConfigModal';
import { api } from '../lib/api';
import { useStore } from '../store';

/** Fill Step 1 (Identity) fields and advance to Step 2 (Model). */
async function goToStep2(fields?: { name?: string; role?: string; brief?: string }) {
  fireEvent.change(screen.getByTestId('input-agent-name'), {
    target: { value: fields?.name ?? 'Radar Agent' },
  });
  fireEvent.change(screen.getByTestId('input-agent-role'), {
    target: { value: fields?.role ?? 'code-reviewer' },
  });
  fireEvent.change(screen.getByTestId('input-agent-brief'), {
    target: { value: fields?.brief ?? 'Review all PRs' },
  });
  fireEvent.click(screen.getByTestId('btn-agent-next-step'));
  await waitFor(() => {
    expect(screen.getByLabelText(/provider/i)).not.toBeNull();
  });
  // Wait for the model catalog to load and the first model to auto-select.
  // The hidden native select's value mirrors the `model` state that
  // validateStep2 reads, so this is deterministic (option text is not).
  await waitFor(() => {
    const select = screen.getByTestId('select-model') as HTMLSelectElement;
    expect(select.value).toBe('claude-3-7-sonnet');
  });
}

/** Advance from Step 2 (Model) to Step 3 (Capabilities). */
async function goToStep3() {
  fireEvent.click(screen.getByTestId('btn-agent-next-step'));
  await waitFor(() => {
    expect(screen.getByText('Capabilities & Integrations')).not.toBeNull();
  });
}

describe('modals/AgentConfigModal', () => {
  const mockTenant = {
    id: 'acme',
    sub: 'acme',
    name: 'Acme Corp',
    providers: [
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
    ],
  };

  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(api.providers, 'list').mockResolvedValue({
      providers: mockTenant.providers,
    });
    vi.spyOn(api.providers, 'models').mockResolvedValue({
      source: 'live',
      models: [
        { id: 'claude-3-7-sonnet', name: 'Claude 3.7 Sonnet', efforts: ['low', 'medium', 'high'] },
        { id: 'gpt-4o', name: 'GPT-4o' },
      ],
    });
    vi.spyOn(api.skills, 'list').mockResolvedValue({
      skills: [
        { id: 'sk-custom', workspace_id: 'acme', name: 'github-sweeper', enabled: true, created_at: '', updated_at: '' },
      ],
    });
  });

  it('renders configured providers and unconfigured types as disabled entries', async () => {
    const mockApiProviders = [
      {
        id: 'prov_api_only',
        workspace_id: 'acme',
        type: 'gemini',
        name: 'Gemini Custom API',
        base_url: '',
        key_set: true,
        key_hint: '1234',
        enabled: true,
        created_at: '',
        updated_at: '',
      },
    ];
    vi.spyOn(api.providers, 'list').mockResolvedValue({
      providers: mockApiProviders,
    });

    render(
      <AgentConfigModal
        tenant={mockTenant}
        onClose={vi.fn()}
        onSave={vi.fn()}
      />
    );

    await waitFor(() => {
      expect(screen.getByTestId('input-agent-name')).not.toBeNull();
    });

    await goToStep2();

    expect(api.providers.list).toHaveBeenCalledWith('acme');

    const providerSelect = screen.getByLabelText(/provider/i);
    const options = Array.from(providerSelect.querySelectorAll('option'));

    expect(options.some((o) => o.textContent?.includes('Gemini Custom API (Gemini)'))).toBe(true);

    const anthropicOption = options.find((o) => o.textContent?.includes('Anthropic (Configure in Settings → Providers)'));
    expect(anthropicOption).toBeDefined();
    expect(anthropicOption?.disabled).toBe(true);
  });

  it('auto-suggests slug in kebab-case from name and applies kebab role suggestions', async () => {
    render(
      <AgentConfigModal
        tenant={mockTenant}
        onClose={vi.fn()}
        onSave={vi.fn()}
      />
    );

    await waitFor(() => {
      expect(screen.getByTestId('input-agent-name')).not.toBeNull();
    });

    const nameInput = screen.getByTestId('input-agent-name');
    const slugInput = screen.getByTestId('input-agent-slug') as HTMLInputElement;

    fireEvent.change(nameInput, { target: { value: 'Radar Watcher' } });
    expect(slugInput.value).toBe('radar-watcher');

    // Click a role suggestion chip
    const roleChip = screen.getByText('code-reviewer');
    fireEvent.click(roleChip);

    const roleInput = screen.getByTestId('input-agent-role') as HTMLInputElement;
    expect(roleInput.value).toBe('code-reviewer');
  });

  it('blocks Step 2 until required identity fields are valid', async () => {
    render(
      <AgentConfigModal tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />
    );

    await waitFor(() => {
      expect(screen.getByTestId('input-agent-name')).not.toBeNull();
    });

    // Continue with empty required fields — must stay on Step 1 with errors
    fireEvent.click(screen.getByTestId('btn-agent-next-step'));

    await waitFor(() => {
      expect(screen.getByText(/agent name is required/i)).not.toBeNull();
    });
    expect(screen.queryByLabelText(/provider/i)).toBeNull();
  });

  it('auto-expands collapsed Advanced section on submit error when fields are invalid', async () => {
    render(
      <AgentConfigModal tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />
    );

    await waitFor(() => {
      expect(screen.getByTestId('input-agent-name')).not.toBeNull();
    });

    await goToStep2();

    // Open advanced, enter invalid max tokens (-5), then collapse it
    fireEvent.click(screen.getByTestId('btn-toggle-advanced'));
    expect(screen.getByTestId('advanced-model-section')).not.toBeNull();

    fireEvent.change(screen.getByTestId('input-max-tokens'), { target: { value: '-5' } });

    // Collapse advanced section
    fireEvent.click(screen.getByTestId('btn-toggle-advanced'));
    expect(screen.queryByTestId('advanced-model-section')).toBeNull();

    // Try to continue to Step 3 — validation must fail and re-open Advanced
    fireEvent.click(screen.getByTestId('btn-agent-next-step'));

    // Section should auto-expand and show the error
    await waitFor(() => {
      expect(screen.getByTestId('advanced-model-section')).not.toBeNull();
      expect(screen.getByText(/max tokens must be a positive integer/i)).not.toBeNull();
    });
  });

  it('walks Identity → Model → Capabilities and deploys with skipped capabilities', async () => {
    const onSave = vi.fn();
    vi.spyOn(api.agents, 'create').mockResolvedValue({
      agent: {
        id: 'agent-123',
        workspace_id: 'acme',
        slug: 'radar-agent',
        name: 'Radar Agent',
        role: 'code-reviewer',
        description: '',
        brief: 'Review all PRs',
        identity: '',
        soul: '',
        bootstrap: '',
        provider_id: 'prov_anthropic',
        model: 'claude-3-7-sonnet',
        temperature: 1.0,
        autonomy: 'approval',
        tools: [],
        skills: [],
        mcp: [],
        avatar: {},
        prompts_status: 'generating',
        created_at: '',
        updated_at: '',
      },
    });

    render(
      <AgentConfigModal
        tenant={mockTenant}
        onClose={vi.fn()}
        onSave={onSave}
      />
    );

    await waitFor(() => {
      expect(screen.getByTestId('input-agent-name')).not.toBeNull();
    });

    // Step 1 → Step 2 → Step 3
    await goToStep2();
    await goToStep3();

    // Deploy button only exists on the final step
    expect(screen.getByTestId('btn-agent-save-modal')).not.toBeNull();

    // Skip capabilities: deselect the preselected chips, then deploy
    fireEvent.click(screen.getByText('Web search'));
    fireEvent.click(screen.getByText('Deep research'));
    fireEvent.click(screen.getByTestId('btn-agent-save-modal'));

    await waitFor(() => {
      expect(api.agents.create).toHaveBeenCalledWith('acme', expect.objectContaining({
        name: 'Radar Agent',
        slug: 'radar-agent',
        role: 'code-reviewer',
        brief: 'Review all PRs',
        provider_id: 'prov_anthropic',
        model: 'claude-3-7-sonnet',
        tools: [],
        skills: [],
        mcp: [],
      }));
    });

    expect(onSave).toHaveBeenCalled();
  });

  it('deploys with the capabilities selected on Step 3', async () => {
    const onSave = vi.fn();
    vi.spyOn(api.agents, 'create').mockResolvedValue({
      agent: {
        id: 'agent-456',
        workspace_id: 'acme',
        slug: 'beacon',
        name: 'Beacon',
        role: 'triage-bot',
        description: '',
        brief: 'Triage incidents',
        identity: '',
        soul: '',
        bootstrap: '',
        provider_id: 'prov_anthropic',
        model: 'claude-3-7-sonnet',
        temperature: 1.0,
        autonomy: 'approval',
        tools: ['web'],
        skills: ['github-sweeper'],
        mcp: [],
        avatar: {},
        prompts_status: 'generating',
        created_at: '',
        updated_at: '',
      },
    });

    render(
      <AgentConfigModal
        tenant={mockTenant}
        onClose={vi.fn()}
        onSave={onSave}
      />
    );

    await waitFor(() => {
      expect(screen.getByTestId('input-agent-name')).not.toBeNull();
    });

    // Step 1 → Step 2 → Step 3
    await goToStep2({ name: 'Beacon', role: 'triage-bot', brief: 'Triage incidents' });
    await goToStep3();

    // Toggle capability chips: keep "Web search", drop the default skill, add the workspace skill
    fireEvent.click(screen.getByText('Deep research')); // deselect default
    fireEvent.click(screen.getByText('github-sweeper')); // select workspace skill

    // Autonomy lives on the Capabilities step, with a description per option
    expect(screen.getByText(/asks you before every tool call/i)).not.toBeNull();
    fireEvent.click(screen.getByRole('radio', { name: 'Suggest only' }));
    expect(screen.getByText(/proposes actions for you to run/i)).not.toBeNull();

    fireEvent.click(screen.getByTestId('btn-agent-save-modal'));

    await waitFor(() => {
      expect(api.agents.create).toHaveBeenCalledWith('acme', expect.objectContaining({
        name: 'Beacon',
        tools: ['web'],
        skills: ['github-sweeper'],
        mcp: [],
        autonomy: 'suggest',
      }));
    });

    expect(onSave).toHaveBeenCalled();
  });

  it('in edit mode shows loading state, hydrates from detail GET, and saves unchanged prompt documents', async () => {
    // Summary store draft (omits identity/soul prompt documents)
    const summaryDraft = {
      id: 'radar',
      workspace_id: 'acme',
      slug: 'radar',
      name: 'Radar',
      role: 'Reviewer',
      description: 'PR Reviewer',
      model: 'claude-3-7-sonnet',
      status: 'idle',
    };

    // Full detail response with prompt documents
    const detailAgent = {
      id: 'radar',
      workspace_id: 'acme',
      slug: 'radar',
      name: 'Radar Detail',
      role: 'Code Reviewer',
      description: 'PR Reviewer Detail',
      brief: 'Review all pull requests thoroughly',
      identity: 'System prompt identity from file',
      soul: 'Helpful and concise tone',
      bootstrap: '',
      provider_id: 'prov_anthropic',
      model: 'claude-3-7-sonnet',
      temperature: 1.0,
      autonomy: 'approval' as const,
      tools: ['web'],
      skills: ['research'],
      mcp: [],
      avatar: {},
      prompts_status: 'ready' as const,
      created_at: '',
      updated_at: '',
    };

    let resolveGet: (val: any) => void = () => {};
    const getPromise = new Promise((resolve) => {
      resolveGet = resolve;
    });
    vi.spyOn(api.agents, 'get').mockImplementation(() => getPromise as any);
    vi.spyOn(api.agents, 'patch').mockResolvedValue({
      agent: detailAgent,
    });
    vi.spyOn(api.agents, 'getMemory').mockResolvedValue({
      agent_id: 'radar',
      user_id: 'u1',
      workspace_id: 'acme',
      content: 'User prefers TypeScript over JavaScript.',
    });
    vi.spyOn(api.agents, 'deleteMemory').mockResolvedValue(undefined);

    const onSave = vi.fn();

    render(
      <AgentConfigModal
        draft={summaryDraft}
        tenant={mockTenant}
        onClose={vi.fn()}
        onSave={onSave}
      />
    );

    // Shows loading state while detail fetch is in flight
    expect(screen.getByTestId('agent-modal-loading')).not.toBeNull();
    expect(screen.getByText(/loading agent details…/i)).not.toBeNull();
    const saveBtn = screen.getByTestId('btn-agent-save-modal') as HTMLButtonElement;
    expect(saveBtn.disabled).toBe(true);

    // Resolve the detail fetch
    resolveGet({ agent: detailAgent });

    await waitFor(() => {
      expect(screen.queryByTestId('agent-modal-loading')).toBeNull();
    });

    expect(api.agents.get).toHaveBeenCalledWith('acme', 'radar');

    // Fields hydrated from detail response (Identity tab)
    const nameInput = screen.getByTestId('input-agent-name') as HTMLInputElement;
    const briefInput = screen.getByTestId('input-agent-brief') as HTMLTextAreaElement;
    expect(nameInput.value).toBe('Radar Detail');
    expect(briefInput.value).toBe('Review all pull requests thoroughly');

    // The generated prompt files live on the Prompts tab — one preview at a time
    fireEvent.click(screen.getByText('Prompts'));
    expect((screen.getByTestId('input-agent-identity') as HTMLTextAreaElement).value).toBe('System prompt identity from file');
    fireEvent.click(screen.getByTestId('prompt-file-soul'));
    expect((screen.getByTestId('input-agent-soul') as HTMLTextAreaElement).value).toBe('Helpful and concise tone');

    // Saving without edits sends prompt documents unchanged (no empty-string PATCH)
    fireEvent.click(screen.getByTestId('btn-agent-save-modal'));

    await waitFor(() => {
      expect(api.agents.patch).toHaveBeenCalledWith(
        'acme',
        'radar',
        expect.objectContaining({
          name: 'Radar Detail',
          brief: 'Review all pull requests thoroughly',
          identity: 'System prompt identity from file',
          soul: 'Helpful and concise tone',
        })
      );
    });

    expect(onSave).toHaveBeenCalled();
  });

  it('in edit mode allows editing identity & soul and resetting own-memory', async () => {
    const detailAgent = {
      id: 'radar',
      workspace_id: 'acme',
      slug: 'radar',
      name: 'Radar',
      role: 'Reviewer',
      description: 'PR Reviewer',
      brief: 'Review all PRs',
      identity: 'System prompt identity',
      soul: 'Helpful and concise',
      bootstrap: '',
      provider_id: 'prov_anthropic',
      model: 'claude-3-7-sonnet',
      temperature: 1.0,
      autonomy: 'approval' as const,
      tools: ['web'],
      skills: ['research'],
      mcp: [],
      avatar: {},
      prompts_status: 'ready' as const,
      created_at: '',
      updated_at: '',
    };

    vi.spyOn(api.agents, 'get').mockResolvedValue({
      agent: detailAgent,
    });
    vi.spyOn(api.agents, 'getMemory').mockResolvedValue({
      agent_id: 'radar',
      user_id: 'u1',
      workspace_id: 'acme',
      content: 'User prefers TypeScript over JavaScript.',
    });
    vi.spyOn(api.agents, 'deleteMemory').mockResolvedValue(undefined);
    vi.spyOn(api.agents, 'patch').mockResolvedValue({
      agent: { ...detailAgent, identity: 'Updated identity' },
    });

    const onSave = vi.fn();

    render(
      <AgentConfigModal
        draft={{ id: 'radar', name: 'Radar' }}
        tenant={mockTenant}
        onClose={vi.fn()}
        onSave={onSave}
      />
    );

    await waitFor(() => {
      expect(screen.queryByTestId('agent-modal-loading')).toBeNull();
    });

    // The generated files live on the Prompts tab
    fireEvent.click(screen.getByText('Prompts'));
    const identityInput = screen.getByTestId('input-agent-identity') as HTMLTextAreaElement;
    expect(identityInput.value).toBe('System prompt identity');

    // Switch to Memory tab
    fireEvent.click(screen.getByText('Memory'));

    await waitFor(() => {
      expect(screen.getByText('User prefers TypeScript over JavaScript.')).not.toBeNull();
    });

    // Reset memory
    const resetBtn = screen.getByTestId('btn-reset-memory');
    fireEvent.click(resetBtn);

    await waitFor(() => {
      expect(api.agents.deleteMemory).toHaveBeenCalledWith('acme', 'radar');
    });
  });

  it('lists the generated prompt files on the Prompts tab and refreshes after regenerate', async () => {
    const detailV1 = {
      id: 'radar',
      workspace_id: 'acme',
      slug: 'radar',
      name: 'Radar',
      role: 'Reviewer',
      description: 'PR Reviewer',
      brief: 'Review all PRs',
      identity: 'old identity body',
      soul: 'old soul body',
      bootstrap: 'old bootstrap body',
      provider_id: 'prov_anthropic',
      model: 'claude-3-7-sonnet',
      temperature: 1.0,
      autonomy: 'approval' as const,
      tools: ['web'],
      skills: ['research'],
      mcp: [],
      avatar: {},
      prompts_status: 'ready' as const,
      created_at: '',
      updated_at: '',
    };
    const detailV2 = {
      ...detailV1,
      identity: 'enhanced identity body',
      soul: 'enhanced soul body',
    };

    vi.spyOn(api.agents, 'get')
      .mockResolvedValueOnce({ agent: detailV1 })
      .mockResolvedValueOnce({ agent: detailV2 });
    const regenerate = vi.spyOn(useStore.getState(), 'regenerateAgent').mockResolvedValue(undefined);
    vi.spyOn(api.agents, 'getMemory').mockResolvedValue({
      agent_id: 'radar',
      user_id: 'u1',
      workspace_id: 'acme',
      content: '',
    });

    render(
      <AgentConfigModal
        draft={{ id: 'radar', name: 'Radar' }}
        tenant={mockTenant}
        onClose={vi.fn()}
        onSave={vi.fn()}
      />
    );

    await waitFor(() => {
      expect(screen.queryByTestId('agent-modal-loading')).toBeNull();
    });

    // The Prompts tab lists the generated files with status and Regenerate
    fireEvent.click(screen.getByText('Prompts'));
    expect(screen.getByTestId('agent-prompts-pane')).not.toBeNull();
    expect(screen.getByTestId('prompt-file-identity')).not.toBeNull();
    expect(screen.getByTestId('prompt-file-soul')).not.toBeNull();
    expect(screen.getByTestId('prompt-file-bootstrap')).not.toBeNull();
    expect(screen.getByText('Ready')).not.toBeNull();
    expect((screen.getByTestId('input-agent-identity') as HTMLTextAreaElement).value).toBe('old identity body');
    fireEvent.click(screen.getByTestId('prompt-file-bootstrap'));
    expect(screen.getByTestId('agent-bootstrap-doc').textContent).toContain('old bootstrap body');
    fireEvent.click(screen.getByTestId('prompt-file-identity'));

    // Regenerate asks what should change before running
    fireEvent.click(screen.getByTestId('btn-regenerate-prompts'));
    expect(screen.getByTestId('regenerate-form')).not.toBeNull();

    fireEvent.change(screen.getByTestId('input-regen-instruction'), {
      target: { value: 'make the tone sharper' },
    });
    fireEvent.click(screen.getByTestId('btn-confirm-regenerate'));

    await waitFor(() => {
      expect(regenerate).toHaveBeenCalledWith('acme', 'radar', 'make the tone sharper');
    });
    await waitFor(() => {
      expect((screen.getByTestId('input-agent-identity') as HTMLTextAreaElement).value).toBe('enhanced identity body');
    });

    expect(api.agents.get).toHaveBeenCalledTimes(2);
    expect(screen.getByTestId('agent-prompts-pane')).not.toBeNull();
    expect(screen.queryByTestId('regenerate-form')).toBeNull();
  });

  it('regenerates without an instruction when the change request is left empty', async () => {
    vi.spyOn(api.agents, 'get').mockResolvedValue({
      agent: {
        id: 'radar',
        workspace_id: 'acme',
        slug: 'radar',
        name: 'Radar',
        role: 'Reviewer',
        description: '',
        brief: 'Review all PRs',
        identity: 'old identity body',
        soul: '',
        bootstrap: '',
        provider_id: 'prov_anthropic',
        model: 'claude-3-7-sonnet',
        temperature: 1.0,
        autonomy: 'approval' as const,
        tools: [],
        skills: [],
        mcp: [],
        avatar: {},
        prompts_status: 'ready' as const,
        created_at: '',
        updated_at: '',
      },
    });
    const regenerate = vi.spyOn(useStore.getState(), 'regenerateAgent').mockResolvedValue(undefined);
    vi.spyOn(api.agents, 'getMemory').mockResolvedValue({
      agent_id: 'radar',
      user_id: 'u1',
      workspace_id: 'acme',
      content: '',
    });

    render(
      <AgentConfigModal
        draft={{ id: 'radar', name: 'Radar' }}
        tenant={mockTenant}
        onClose={vi.fn()}
        onSave={vi.fn()}
      />
    );

    await waitFor(() => {
      expect(screen.queryByTestId('agent-modal-loading')).toBeNull();
    });

    fireEvent.click(screen.getByText('Prompts'));
    fireEvent.click(screen.getByTestId('btn-regenerate-prompts'));
    fireEvent.click(screen.getByTestId('btn-confirm-regenerate'));

    await waitFor(() => {
      expect(regenerate).toHaveBeenCalledWith('acme', 'radar', undefined);
    });
  });
});
