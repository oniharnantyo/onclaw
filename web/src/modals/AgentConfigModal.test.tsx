import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { AgentConfigModal } from './AgentConfigModal';
import { api, type ApiMcpServer, type ApiWorkspaceSkill } from '../lib/api';
import { useStore } from '../store';
import { useAuthStore } from '../store/auth';

const mcpServerRow = (overrides: Partial<ApiMcpServer> = {}): ApiMcpServer => ({
  id: 'srv-gh',
  workspace_id: 'acme',
  name: 'GitHub',
  transport: 'stdio',
  command: 'npx',
  args: ['-y', '@modelcontextprotocol/server-github'],
  env: [],
  enabled: true,
  status: 'connected',
  status_error: null,
  tool_count: 24,
  created_at: '',
  updated_at: '',
  ...overrides,
});

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
    // Permission helpers read the auth store; default to no memberships so
    // "offline / mock mode" keeps every affordance visible (tests that assert
    // gating set memberships explicitly).
    useAuthStore.setState({ memberships: [] });
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
      skills: ([
        {
          id: 'sys-web-research', tier: 'system', name: 'web-research', version: '2.4.1', source: 'system',
          locked: true, enabled: true, description: 'Multi-source research briefs.', created_at: '', updated_at: '',
        },
        {
          id: 'sk-custom', workspace_id: 'acme', tier: 'workspace', name: 'github-sweeper', version: '0.3.0',
          source: 'authored', enabled: true, description: 'Sweeps PRs.', created_at: '', updated_at: '',
          dependencies: { tools: ['web.search'] },
          dependency_status: [{ kind: 'tools', name: 'web.search', status: 'missing' }],
        },
        {
          id: 'sk-off', workspace_id: 'acme', tier: 'workspace', name: 'vision', version: '1.2.0',
          source: 'upload', enabled: false, description: 'Disabled skill.', created_at: '', updated_at: '',
        },
      ] as ApiWorkspaceSkill[]),
    });
    vi.spyOn(api.agents, 'listSkills').mockResolvedValue({ skills: [] });
    vi.spyOn(api.agents, 'listMcpServers').mockResolvedValue({ servers: [] });
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [] });
    vi.spyOn(api.tools, 'list').mockResolvedValue({
      tools: [
        { key: 'ls', display_name: 'List Files', description: '', group: 'filesystem', icon_key: 'folder', configurable: false, enabled: true, configured: false, config: {} },
        { key: 'read_file', display_name: 'Read File', description: '', group: 'filesystem', icon_key: 'file', configurable: false, enabled: true, configured: false, config: {} },
        { key: 'write_file', display_name: 'Write File', description: '', group: 'filesystem', icon_key: 'file-plus', configurable: false, enabled: true, configured: false, config: {} },
        { key: 'edit_file', display_name: 'Edit File', description: '', group: 'filesystem', icon_key: 'edit', configurable: false, enabled: true, configured: false, config: {} },
        { key: 'glob', display_name: 'Glob', description: '', group: 'filesystem', icon_key: 'scan', configurable: false, enabled: true, configured: false, config: {} },
        { key: 'grep', display_name: 'Grep', description: '', group: 'filesystem', icon_key: 'compass', configurable: false, enabled: true, configured: false, config: {} },
        { key: 'execute', display_name: 'Shell', description: '', group: 'shell', icon_key: 'terminal', configurable: false, enabled: true, configured: false, config: {} },
        { key: 'web.search', display_name: 'Web Search', description: '', group: 'web', icon_key: 'search', configurable: true, enabled: true, configured: false, config: {} },
        { key: 'web.fetch', display_name: 'Web Fetch', description: '', group: 'web', icon_key: 'link', configurable: false, enabled: true, configured: false, config: {} },
        { key: 'browser', display_name: 'Browser', description: '', group: 'browser', icon_key: 'globe', configurable: true, enabled: true, configured: false, config: {} },
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
        enabled_mcps: [],
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

    // Skip capabilities: an untouched Step 3 deploys with no tools selected
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
        enabled_mcps: [],
      }));
      // No per-agent skill state ships in the payload — tiers only.
      expect((api.agents.create as any).mock.calls[0][1].skills).toBeUndefined();
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
        tools: ['web.search'],
        skills: ['github-sweeper'],
        enabled_mcps: [],
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

    // Toggle capability chips: opt into "Web Search" (also satisfies the
    // github-sweeper tool dependency warning below)
    fireEvent.click(screen.getByText('Web Search'));

    // Autonomy lives on the Capabilities step, with a description per option
    expect(screen.getByText(/asks you before every tool call/i)).not.toBeNull();
    fireEvent.click(screen.getByRole('radio', { name: 'Suggest only' }));
    expect(screen.getByText(/proposes actions for you to run/i)).not.toBeNull();

    fireEvent.click(screen.getByTestId('btn-agent-save-modal'));

    await waitFor(() => {
      expect(api.agents.create).toHaveBeenCalledWith('acme', expect.objectContaining({
        name: 'Beacon',
        tools: ['web.search'],
        enabled_mcps: [],
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
      tools: ['web.search'],
      skills: ['research'],
      enabled_mcps: [],
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
      tools: ['web.search'],
      skills: ['research'],
      enabled_mcps: [],
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
        enabled_mcps: [],
        avatar: {},
        prompts_status: 'ready' as const,
        created_at: '',
        updated_at: '',
      },
    });
    const regenerate = vi.spyOn(useStore.getState(), 'regenerateAgent').mockResolvedValue(undefined);

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

  it('auto-fills and submits context_window on create from the model catalog limit', async () => {
    // Give the model a published catalog context limit so the advanced field
    // auto-fills.
    vi.spyOn(api.providers, 'models').mockResolvedValue({
      source: 'live',
      models: [
        { id: 'claude-3-7-sonnet', name: 'Claude 3.7 Sonnet', context_limit: 200000 },
        { id: 'gpt-4o', name: 'GPT-4o' },
      ],
    });
    const onSave = vi.fn();
    vi.spyOn(api.agents, 'create').mockResolvedValue({
      agent: {
        id: 'agent-ctx',
        workspace_id: 'acme',
        slug: 'radar',
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
        autonomy: 'approval' as const,
        context_window: 200000,
        tools: ['web.search'],
        skills: ['research'],
        enabled_mcps: [],
        avatar: {},
        prompts_status: 'ready' as const,
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
    await goToStep2();

    // Advanced section is collapsed by default; open it to reveal context window
    fireEvent.click(screen.getByTestId('btn-toggle-advanced'));
    const ctxInput = screen.getByTestId('input-context-window') as HTMLInputElement;
    await waitFor(() => {
      expect(ctxInput.value).toBe('200000');
    });

    // A manual edit wins over the auto-filled value and is sent on create
    fireEvent.change(ctxInput, { target: { value: '128000' } });
    fireEvent.click(screen.getByTestId('btn-agent-next-step')); // → Step 3
    await waitFor(() => {
      expect(screen.getByText('Capabilities & Integrations')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-agent-save-modal'));

    await waitFor(() => {
      expect(api.agents.create).toHaveBeenCalledWith('acme', expect.objectContaining({
        model: 'claude-3-7-sonnet',
        context_window: 128000,
      }));
    });
    expect(onSave).toHaveBeenCalled();
  });

  it('hydrates and persists an existing context_window on edit', async () => {
    const detailAgent = {
      id: 'radar',
      workspace_id: 'acme',
      slug: 'radar',
      name: 'Radar',
      role: 'Reviewer',
      description: 'PR Reviewer',
      brief: 'Review all PRs',
      identity: 'Identity',
      soul: 'Soul',
      bootstrap: '',
      provider_id: 'prov_anthropic',
      model: 'claude-3-7-sonnet',
      temperature: 1.0,
      autonomy: 'approval' as const,
      context_window: 50000,
      tools: ['web.search'],
      skills: ['research'],
      enabled_mcps: [],
      avatar: {},
      prompts_status: 'ready' as const,
      created_at: '',
      updated_at: '',
    };
    vi.spyOn(api.agents, 'get').mockResolvedValue({ agent: detailAgent });
    vi.spyOn(api.agents, 'patch').mockResolvedValue({ agent: detailAgent });
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

    // Switch to the Model tab and open the Advanced section
    fireEvent.click(screen.getAllByText('Model')[0]);
    fireEvent.click(screen.getByTestId('btn-toggle-advanced'));
    const ctxInput = screen.getByTestId('input-context-window') as HTMLInputElement;
    await waitFor(() => {
      expect(ctxInput.value).toBe('50000');
    });

    fireEvent.click(screen.getByTestId('btn-agent-save-modal'));

    await waitFor(() => {
      expect(api.agents.patch).toHaveBeenCalledWith('acme', 'radar', expect.objectContaining({
        context_window: 50000,
      }));
    });
    expect(onSave).toHaveBeenCalled();
  });

  it('renders workspace-disabled tools greyed and unselectable', async () => {
    vi.spyOn(api.tools, 'list').mockResolvedValue({
      tools: [
        { key: 'web.search', display_name: 'Web Search', description: '', group: 'web', icon_key: 'search', configurable: true, enabled: false, configured: false, config: {} },
        { key: 'web.fetch', display_name: 'Web Fetch', description: '', group: 'web', icon_key: 'link', configurable: false, enabled: true, configured: false, config: {} },
      ],
    });
    vi.spyOn(api.agents, 'create').mockResolvedValue({
      agent: { id: 'agent-x', workspace_id: 'acme', slug: 'beacon', name: 'Beacon', role: 'triage-bot', description: '', brief: 'Triage', identity: '', soul: '', bootstrap: '', provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet', temperature: 1.0, autonomy: 'approval', tools: [], skills: [], enabled_mcps: [], avatar: {}, prompts_status: 'generating', created_at: '', updated_at: '' },
    });

    render(<AgentConfigModal tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);

    await waitFor(() => { expect(screen.getByTestId('input-agent-name')).not.toBeNull(); });
    await goToStep2({ name: 'Beacon', role: 'triage-bot', brief: 'Triage' });
    await goToStep3();

    const disabledChip = screen.getByText('Web Search').closest('button') as HTMLButtonElement;
    expect(disabledChip.disabled).toBe(true);
    expect(disabledChip.title).toBe('Disabled in Settings → Tools');

    // Clicking must not select it.
    fireEvent.click(disabledChip);
    expect(disabledChip.getAttribute('aria-pressed')).toBe('false');

    fireEvent.click(screen.getByText('Web Fetch'));

    fireEvent.click(screen.getByTestId('btn-agent-save-modal'));
    await waitFor(() => {
      expect(api.agents.create).toHaveBeenCalledWith('acme', expect.objectContaining({
        tools: ['web.fetch'],
      }));
    });
  });

  it('hydrates legacy browser.* names as the Browser chip and normalizes on save', async () => {
    const summaryDraft = {
      id: 'radar', workspace_id: 'acme', slug: 'radar', name: 'Radar', role: 'Reviewer',
      description: 'PR Reviewer', model: 'claude-3-7-sonnet', status: 'idle',
    };
    const detailAgent = {
      id: 'radar', workspace_id: 'acme', slug: 'radar', name: 'Radar Detail', role: 'Code Reviewer',
      description: 'PR Reviewer Detail', brief: 'Review all pull requests thoroughly',
      identity: '', soul: '', bootstrap: '',
      provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet', temperature: 1.0,
      autonomy: 'approval' as const,
      tools: ['web.search', 'browser.navigate', 'browser.read'],
      skills: [], enabled_mcps: [], avatar: {}, prompts_status: 'ready' as const,
      created_at: '', updated_at: '',
    };
    vi.spyOn(api.agents, 'get').mockResolvedValue({ agent: detailAgent });
    vi.spyOn(api.agents, 'patch').mockResolvedValue({ agent: detailAgent });

    render(<AgentConfigModal draft={summaryDraft} tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);

    await waitFor(() => {
      expect(screen.queryByTestId('agent-modal-loading')).toBeNull();
    });

    // Capabilities tab: the Browser chip shows selected for legacy names.
    fireEvent.click(screen.getByText('Capabilities'));
    const browserChip = screen.getByText('Browser').closest('button') as HTMLButtonElement;
    await waitFor(() => {
      expect(browserChip.getAttribute('aria-pressed')).toBe('true');
    });

    fireEvent.click(screen.getByTestId('btn-agent-save-modal'));
    await waitFor(() => {
      expect(api.agents.patch).toHaveBeenCalledWith(
        'acme',
        'radar',
        expect.objectContaining({
          tools: ['web.search', 'browser'],
        })
      );
    });
  });

  it('renders the unavailable fallback when the catalog request fails', async () => {
    vi.spyOn(api.tools, 'list').mockRejectedValue(new Error('boom'));

    render(<AgentConfigModal tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);

    await waitFor(() => { expect(screen.getByTestId('input-agent-name')).not.toBeNull(); });
    await goToStep2();
    await goToStep3();

    expect(screen.getByTestId('agent-tools-unavailable')).not.toBeNull();
  });

  it('Step 3 renders locked skill chips for system + enabled workspace skills, warnings included', async () => {
    render(<AgentConfigModal tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);

    await waitFor(() => { expect(screen.getByTestId('input-agent-name')).not.toBeNull(); });
    await goToStep2();
    await goToStep3();

    // System skill: locked, always attached
    expect(screen.getByTestId('locked-skill-web-research')).not.toBeNull();
    // Enabled workspace skill: locked chip; disabled workspace skill is absent
    expect(screen.getByTestId('locked-skill-github-sweeper')).not.toBeNull();
    expect(screen.queryByTestId('locked-skill-vision')).toBeNull();

    // Unmet tool dependency warns inline, naming the missing tool and
    // pointing at the tool chips below (no tools selected on this agent).
    const warn = screen.getByTestId('skill-dep-warn-github-sweeper');
    expect(warn.textContent).toContain('web.search');
    expect(warn.textContent).toContain('tool chips below');

    // Hint names Settings → Skills
    expect(screen.getByTestId('agent-skills-inventory').textContent).toContain('Settings → Skills');

    // New agents cannot carry agent-tier skills yet
    expect(screen.getByTestId('agent-skills-inventory').textContent).toContain('after it is deployed');
  });

  it('edit mode lists agent-tier skills and installs/removes them for writers', async () => {
    const detailAgent = {
      id: 'radar', workspace_id: 'acme', slug: 'radar', name: 'Radar', role: 'Reviewer',
      description: 'PR Reviewer', brief: 'Review all PRs', identity: '', soul: '', bootstrap: '',
      provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet', temperature: 1.0,
      autonomy: 'approval' as const, tools: [], enabled_mcps: [], avatar: {},
      prompts_status: 'ready' as const, created_at: '', updated_at: '',
    };
    vi.spyOn(api.agents, 'get').mockResolvedValue({ agent: detailAgent });
    vi.spyOn(api.agents, 'patch').mockResolvedValue({ agent: detailAgent });
    vi.spyOn(api.agents, 'listSkills').mockResolvedValue({
      skills: [
        {
          id: 'radar/pdf-sweep', tier: 'agent', name: 'pdf-sweep', version: '0.1.0', source: 'authored',
          description: 'Sweeps PDFs.', created_at: '', updated_at: '',
        },
      ],
    });
    const installSkill = vi.spyOn(api.agents, 'installSkill').mockResolvedValue({
      skill: {
        id: 'radar/log-sweep', tier: 'agent', name: 'log-sweeper', version: '0.1.0', source: 'authored',
        description: 'Sweeps logs.', created_at: '', updated_at: '',
      },
    });
    const removeSkill = vi.spyOn(api.agents, 'removeSkill').mockResolvedValue(undefined);

    render(<AgentConfigModal draft={{ id: 'radar', name: 'Radar' }} tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);

    await waitFor(() => { expect(screen.queryByTestId('agent-modal-loading')).toBeNull(); });
    fireEvent.click(screen.getByText('Capabilities'));

    // Existing agent-tier skill lists with a remove action
    expect(screen.getByTestId('agent-skill-pdf-sweep')).not.toBeNull();

    // Add installs scoped to this agent only
    fireEvent.click(screen.getByTestId('btn-add-agent-skill'));
    fireEvent.change(screen.getByTestId('input-agent-skill-name'), { target: { value: 'Log Sweeper' } });
    fireEvent.change(screen.getByTestId('input-agent-skill-body'), { target: { value: 'Sweep logs nightly.' } });
    fireEvent.click(screen.getByTestId('btn-agent-skill-install'));

    await waitFor(() => {
      expect(installSkill).toHaveBeenCalledWith('acme', 'radar', {
        name: 'log-sweeper',
        description: undefined,
        body: 'Sweep logs nightly.',
      });
    });
    await waitFor(() => {
      expect(screen.getByTestId('agent-skill-log-sweeper')).not.toBeNull();
    });

    // Remove deletes it from the agent directory
    fireEvent.click(screen.getByTestId('btn-remove-agent-skill-pdf-sweep'));
    await waitFor(() => {
      expect(removeSkill).toHaveBeenCalledWith('acme', 'radar', 'pdf-sweep');
    });
    await waitFor(() => {
      expect(screen.queryByTestId('agent-skill-pdf-sweep')).toBeNull();
    });
  });

  it('Step 3 MCP section lists workspace servers with status hints and off toggles by default', async () => {
    vi.spyOn(api.mcp, 'list').mockResolvedValue({
      servers: [
        mcpServerRow(),
        mcpServerRow({ id: 'srv-paused', name: 'Postgres', enabled: false }),
        mcpServerRow({ id: 'srv-err', name: 'Broken DB', status: 'error', status_error: 'refused', tool_count: 0 }),
      ],
    });

    render(<AgentConfigModal tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);

    await waitFor(() => { expect(screen.getByTestId('input-agent-name')).not.toBeNull(); });
    await goToStep2();
    await goToStep3();

    // Rows render from the workspace MCP endpoints with their status hints.
    expect(screen.getByTestId('agent-mcp-row-srv-gh')).not.toBeNull();
    expect(screen.getByTestId('agent-mcp-status-srv-gh').textContent).toBe('Connected');
    expect(screen.getByTestId('agent-mcp-status-srv-paused').textContent).toBe('Paused');
    expect(screen.getByTestId('agent-mcp-status-srv-err').textContent).toBe('Error');

    // Every toggle defaults to OFF — an agent opts in only explicitly.
    expect(screen.getByRole('switch', { name: 'Opt this agent into GitHub' }).getAttribute('aria-checked')).toBe('false');
    expect(screen.getByRole('switch', { name: 'Opt this agent into Postgres' }).getAttribute('aria-checked')).toBe('false');
  });

  it('opt-in toggles store and remove server ids in the draft enabled_mcps on save', async () => {
    vi.spyOn(api.mcp, 'list').mockResolvedValue({
      servers: [mcpServerRow(), mcpServerRow({ id: 'srv-slack', name: 'Slack', transport: 'sse', command: undefined, args: undefined, env: undefined, url: 'https://slack.example/sse' })],
    });
    const create = vi.spyOn(api.agents, 'create').mockResolvedValue({
      agent: {
        id: 'agent-mcp', workspace_id: 'acme', slug: 'radar', name: 'Radar Agent', role: 'code-reviewer',
        description: '', brief: 'Review all PRs', identity: '', soul: '', bootstrap: '',
        provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet', temperature: 1.0, autonomy: 'approval',
        tools: [], skills: [], enabled_mcps: [], avatar: {}, prompts_status: 'generating', created_at: '', updated_at: '',
      },
    });

    render(<AgentConfigModal tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);

    await waitFor(() => { expect(screen.getByTestId('input-agent-name')).not.toBeNull(); });
    await goToStep2();
    await goToStep3();

    // Opt into exactly one of the two servers.
    fireEvent.click(screen.getByRole('switch', { name: 'Opt this agent into GitHub' }));
    expect(screen.getByRole('switch', { name: 'Opt this agent into GitHub' }).getAttribute('aria-checked')).toBe('true');
    fireEvent.click(screen.getByRole('switch', { name: 'Opt this agent into GitHub' }));
    expect(screen.getByRole('switch', { name: 'Opt this agent into GitHub' }).getAttribute('aria-checked')).toBe('false');
    fireEvent.click(screen.getByRole('switch', { name: 'Opt this agent into Slack' }));

    fireEvent.click(screen.getByTestId('btn-agent-save-modal'));

    await waitFor(() => {
      expect(create).toHaveBeenCalledWith('acme', expect.objectContaining({
        enabled_mcps: ['srv-slack'],
      }));
    });
  });

  it('opting into a paused server warns that it contributes nothing until resumed', async () => {
    vi.spyOn(api.mcp, 'list').mockResolvedValue({
      servers: [mcpServerRow({ id: 'srv-paused', name: 'Postgres', enabled: false })],
    });

    render(<AgentConfigModal tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);

    await waitFor(() => { expect(screen.getByTestId('input-agent-name')).not.toBeNull(); });
    await goToStep2();
    await goToStep3();

    // Off: no warning.
    expect(screen.queryByTestId('agent-mcp-paused-warn-srv-paused')).toBeNull();

    fireEvent.click(screen.getByRole('switch', { name: 'Opt this agent into Postgres' }));

    const warn = screen.getByTestId('agent-mcp-paused-warn-srv-paused');
    expect(warn.textContent).toContain('contributes no tools');

    fireEvent.click(screen.getByRole('switch', { name: 'Opt this agent into Postgres' }));
    expect(screen.queryByTestId('agent-mcp-paused-warn-srv-paused')).toBeNull();
  });

  it('edit mode hydrates opted-in servers from enabled_mcps and drops removed ids on save', async () => {
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [mcpServerRow()] });
    const detailAgent = {
      id: 'radar', workspace_id: 'acme', slug: 'radar', name: 'Radar', role: 'Reviewer',
      description: 'PR Reviewer', brief: 'Review all PRs', identity: '', soul: '', bootstrap: '',
      provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet', temperature: 1.0,
      autonomy: 'approval' as const, tools: [], skills: [],
      enabled_mcps: ['srv-gh'],
      avatar: {}, prompts_status: 'ready' as const, created_at: '', updated_at: '',
    };
    vi.spyOn(api.agents, 'get').mockResolvedValue({ agent: detailAgent as any });
    vi.spyOn(api.agents, 'patch').mockResolvedValue({ agent: detailAgent as any });
    render(<AgentConfigModal draft={{ id: 'radar', name: 'Radar' }} tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);

    await waitFor(() => { expect(screen.queryByTestId('agent-modal-loading')).toBeNull(); });
    fireEvent.click(screen.getByText('Capabilities'));

    // The opted-in server's row shows selected.
    const toggle = screen.getByRole('switch', { name: 'Opt this agent into GitHub' });
    await waitFor(() => {
      expect(toggle.getAttribute('aria-checked')).toBe('true');
    });

    // Removing the selection drops the id on save.
    fireEvent.click(toggle);
    fireEvent.click(screen.getByTestId('btn-agent-save-modal'));

    await waitFor(() => {
      expect(api.agents.patch).toHaveBeenCalledWith('acme', 'radar', expect.objectContaining({
        enabled_mcps: [],
      }));
    });
  });

  it('agent MCP servers sub-list hydrates from the agent detail and adds through the shared dialog', async () => {
    vi.spyOn(api.agents, 'listMcpServers').mockResolvedValue({
      servers: [mcpServerRow({ id: 'srv-private', name: 'Private Sentry', tool_count: 3 })],
    });
    const detailAgent = {
      id: 'radar', workspace_id: 'acme', slug: 'radar', name: 'Radar', role: 'Reviewer',
      description: '', brief: 'Review all PRs', identity: '', soul: '', bootstrap: '',
      provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet', temperature: 1.0,
      autonomy: 'approval' as const, tools: [], skills: [], enabled_mcps: [],
      avatar: {}, prompts_status: 'ready' as const, created_at: '', updated_at: '',
    };
    vi.spyOn(api.agents, 'get').mockResolvedValue({ agent: detailAgent });
    const createMcp = vi.spyOn(api.agents, 'createMcpServer').mockResolvedValue({
      server: mcpServerRow({ id: 'srv-new-private', name: 'Linear Private', transport: 'streamable_http', command: undefined, args: undefined, env: undefined, url: 'https://mcp.linear.app/mcp' }),
    });

    render(<AgentConfigModal draft={{ id: 'radar', name: 'Radar' }} tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);

    await waitFor(() => { expect(screen.queryByTestId('agent-modal-loading')).toBeNull(); });
    fireEvent.click(screen.getByText('Capabilities'));

    // Hydrated private server row.
    expect(screen.getByTestId('agent-mcp-server-srv-private')).not.toBeNull();

    // Add opens the shared structured transport-branched dialog.
    fireEvent.click(screen.getByTestId('btn-add-agent-mcp'));
    await waitFor(() => {
      expect(screen.getByTestId('modal-mcp-server')).not.toBeNull();
    });

    fireEvent.change(screen.getByLabelText('Server name'), { target: { value: 'Linear Private' } });
    fireEvent.change(screen.getByLabelText('Transport'), { target: { value: 'streamable_http' } });
    fireEvent.change(screen.getByLabelText('URL'), { target: { value: 'https://mcp.linear.app/mcp' } });
    fireEvent.click(screen.getByTestId('btn-mcp-add-confirm'));

    await waitFor(() => {
      expect(createMcp).toHaveBeenCalledWith('acme', 'radar', {
        name: 'Linear Private',
        transport: 'streamable_http',
        url: 'https://mcp.linear.app/mcp',
      });
    });
    await waitFor(() => {
      expect(screen.getByTestId('agent-mcp-server-srv-new-private')).not.toBeNull();
      expect(screen.queryByTestId('modal-mcp-server')).toBeNull();
    });
  });

  it('edits a private server through the pre-filled shared dialog and removes it', async () => {
    vi.spyOn(api.agents, 'listMcpServers').mockResolvedValue({
      servers: [mcpServerRow({ id: 'srv-private', name: 'Private Sentry', tool_count: 3 })],
    });
    const detailAgent = {
      id: 'radar', workspace_id: 'acme', slug: 'radar', name: 'Radar', role: 'Reviewer',
      description: '', brief: 'Review all PRs', identity: '', soul: '', bootstrap: '',
      provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet', temperature: 1.0,
      autonomy: 'approval' as const, tools: [], skills: [], enabled_mcps: [],
      avatar: {}, prompts_status: 'ready' as const, created_at: '', updated_at: '',
    };
    vi.spyOn(api.agents, 'get').mockResolvedValue({ agent: detailAgent });
    const updateMcp = vi.spyOn(api.agents, 'updateMcpServer').mockResolvedValue({
      server: mcpServerRow({ id: 'srv-private', name: 'Sentry Renamed' }),
    });
    const deleteMcp = vi.spyOn(api.agents, 'deleteMcpServer').mockResolvedValue(undefined);

    render(<AgentConfigModal draft={{ id: 'radar', name: 'Radar' }} tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);

    await waitFor(() => { expect(screen.queryByTestId('agent-modal-loading')).toBeNull(); });
    fireEvent.click(screen.getByText('Capabilities'));

    // Edit opens the shared dialog pre-filled from the row.
    fireEvent.click(screen.getByTestId('btn-edit-agent-mcp-srv-private'));
    await waitFor(() => {
      expect(screen.getByTestId('modal-mcp-server')).not.toBeNull();
    });
    expect((screen.getByLabelText('Server name') as HTMLInputElement).value).toBe('Private Sentry');
    expect((screen.getByLabelText('Command') as HTMLInputElement).value).toBe('npx');

    fireEvent.change(screen.getByLabelText('Server name'), { target: { value: 'Sentry Renamed' } });
    fireEvent.click(screen.getByTestId('btn-mcp-save'));

    await waitFor(() => {
      expect(updateMcp).toHaveBeenCalledWith('acme', 'radar', 'srv-private', expect.objectContaining({
        name: 'Sentry Renamed',
        transport: 'stdio',
        command: 'npx',
      }));
    });
    await waitFor(() => {
      expect(screen.getByTestId('agent-mcp-server-srv-private').textContent).toContain('Sentry Renamed');
    });

    // Remove deletes it from this agent only.
    fireEvent.click(screen.getByTestId('btn-remove-agent-mcp-srv-private'));
    await waitFor(() => {
      expect(deleteMcp).toHaveBeenCalledWith('acme', 'radar', 'srv-private');
    });
    await waitFor(() => {
      expect(screen.queryByTestId('agent-mcp-server-srv-private')).toBeNull();
    });
  });

  it('hides private server write controls from holders without agents.write but keeps rows', async () => {
    useAuthStore.setState({
      memberships: [
        { workspace_id: 'acme', role_name: 'Member', role: { name: 'Member', permissions: ['agents.read'] } },
      ] as any,
    });
    vi.spyOn(api.agents, 'listMcpServers').mockResolvedValue({
      servers: [mcpServerRow({ id: 'srv-private', name: 'Private Sentry', tool_count: 3 })],
    });
    const detailAgent = {
      id: 'radar', workspace_id: 'acme', slug: 'radar', name: 'Radar', role: 'Reviewer',
      description: '', brief: 'Review all PRs', identity: '', soul: '', bootstrap: '',
      provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet', temperature: 1.0,
      autonomy: 'approval' as const, tools: [], skills: [], enabled_mcps: [],
      avatar: {}, prompts_status: 'ready' as const, created_at: '', updated_at: '',
    };
    vi.spyOn(api.agents, 'get').mockResolvedValue({ agent: detailAgent });
    render(<AgentConfigModal draft={{ id: 'radar', name: 'Radar' }} tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);

    await waitFor(() => { expect(screen.queryByTestId('agent-modal-loading')).toBeNull(); });
    fireEvent.click(screen.getByText('Capabilities'));

    // Rows still render for readers; every write affordance is absent.
    expect(screen.getByTestId('agent-mcp-server-srv-private')).not.toBeNull();
    expect(screen.queryByTestId('btn-add-agent-mcp')).toBeNull();
    expect(screen.queryByTestId('btn-edit-agent-mcp-srv-private')).toBeNull();
    expect(screen.queryByTestId('btn-remove-agent-mcp-srv-private')).toBeNull();
  });
});
