import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react';
import { AgentConfigModal } from './AgentConfigModal';
import { api, ApiError, type ApiMcpServer, type ApiWorkspaceSkill } from '../lib/api';
import { connectionsApi } from '../lib/connectionsApi';
import { documentsApi, type ApiReferenceDocument } from '../lib/documentsApi';
import { heartbeats, type ApiHeartbeat } from '../lib/heartbeats';
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

const OAUTH_REQUIRED_DETAIL =
  'no usable OAuth credential is stored for this server — start (or re-start) its sign-in from this agent';

/** Un-connected agent-private oauth row (add-mcp-oauth-client 7.1). */
const agentOauthRow = (overrides: Partial<ApiMcpServer> = {}): ApiMcpServer =>
  mcpServerRow({
    id: 'srv-notion',
    name: 'Notion',
    transport: 'streamable_http',
    url: 'https://mcp.notion.com/mcp',
    auth_mode: 'oauth',
    status: 'unknown',
    status_error: null,
    command: undefined,
    args: undefined,
    env: undefined,
    tool_count: 0,
    ...overrides,
  });

/** jsdom navigations need stubbing; same pattern as McpPane.test. */
function stubLocationAssign() {
  const assignMock = vi.fn();
  Object.defineProperty(window, 'location', {
    value: { ...window.location, assign: assignMock },
    writable: true,
    configurable: true,
  });
  return assignMock;
}

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
    // Curated provenance (add-skill-curation-from-traces): empty unless a
    // test overrides it — optional context that must not block the modal.
    vi.spyOn(api.curation, 'listCandidates').mockResolvedValue({ candidates: [], count: 0 });
    vi.spyOn(api.agents, 'listMcpServers').mockResolvedValue({ servers: [] });
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [] });
    // The Integrations section lists managed connections (empty unless a test
    // overrides it) — optional context that must not block the modal.
    vi.spyOn(connectionsApi, 'list').mockResolvedValue({ connections: [] });
    // The read-only reference-documents section (add-reference-documents 9.1):
    // empty unless a test overrides it — optional context that must not block
    // the modal, and never a real network call in tests.
    vi.spyOn(documentsApi, 'list').mockResolvedValue({ documents: [] });
    vi.spyOn(api.tools, 'list').mockResolvedValue({
      tools: [
        { key: 'ls', display_name: 'List Files', description: '', group: 'filesystem', icon_key: 'folder', configurable: false, enabled: true, configured: false, config: {}, toggleable: true },
        { key: 'read_file', display_name: 'Read File', description: '', group: 'filesystem', icon_key: 'file', configurable: false, enabled: true, configured: false, config: {}, toggleable: true },
        { key: 'write_file', display_name: 'Write File', description: '', group: 'filesystem', icon_key: 'file-plus', configurable: false, enabled: true, configured: false, config: {}, toggleable: true },
        { key: 'edit_file', display_name: 'Edit File', description: '', group: 'filesystem', icon_key: 'edit', configurable: false, enabled: true, configured: false, config: {}, toggleable: true },
        { key: 'glob', display_name: 'Glob', description: '', group: 'filesystem', icon_key: 'scan', configurable: false, enabled: true, configured: false, config: {}, toggleable: true },
        { key: 'grep', display_name: 'Grep', description: '', group: 'filesystem', icon_key: 'compass', configurable: false, enabled: true, configured: false, config: {}, toggleable: true },
        { key: 'execute', display_name: 'Shell', description: '', group: 'shell', icon_key: 'terminal', configurable: false, enabled: true, configured: false, config: {}, toggleable: true },
        { key: 'web.search', display_name: 'Web Search', description: '', group: 'web', icon_key: 'search', configurable: true, enabled: true, configured: false, config: {}, toggleable: true },
        { key: 'web.fetch', display_name: 'Web Fetch', description: '', group: 'web', icon_key: 'link', configurable: false, enabled: true, configured: false, config: {}, toggleable: true },
        { key: 'browser', display_name: 'Browser', description: '', group: 'browser', icon_key: 'globe', configurable: true, enabled: true, configured: false, config: {}, toggleable: true },
        { key: 'channel.post', display_name: 'Channel Post', description: '', group: 'channel', icon_key: 'message', configurable: false, enabled: true, configured: false, config: {}, toggleable: false },
        { key: 'channel.history', display_name: 'Channel History', description: '', group: 'channel', icon_key: 'history', configurable: false, enabled: true, configured: false, config: {}, toggleable: false },
        { key: 'session.close', display_name: 'Close Work Session', description: '', group: 'channel', icon_key: 'check-circle', configurable: false, enabled: true, configured: false, config: {}, toggleable: false },
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

  it('excludes decision (typesafe) configs from every model picker (5.3)', async () => {
    vi.spyOn(api.providers, 'list').mockResolvedValue({
      providers: [
        ...mockTenant.providers,
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

    // The agent model provider select lists language-model providers only —
    // the decision config never appears as configured or unconfigured.
    const providerSelect = screen.getByLabelText(/provider/i);
    const options = Array.from(providerSelect.querySelectorAll('option'));
    expect(options.some((o) => o.textContent?.includes('TypeSafe Routing'))).toBe(false);

    // The agent's own memory side-call provider select: language providers only.
    fireEvent.click(screen.getByTestId('ac-memory-sidecall-custom'));
    const sidecall = screen.getByTestId('ac-memory-sidecall-provider') as HTMLSelectElement;
    expect(Array.from(sidecall.options).map((o) => o.textContent)).not.toContain('TypeSafe Routing');
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
        provider_id: 'prov_anthropic',
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

    // Step 3 inverts (D6): every toggleable catalog chip renders selected
    // for an agent with an empty denylist — untouched, they stay enabled.
    expect(screen.getByText('Web Search').closest('button').getAttribute('aria-pressed')).toBe('true');
    expect(screen.getByText('Browser').closest('button').getAttribute('aria-pressed')).toBe('true');
    expect(screen.getByText('Shell').closest('button').getAttribute('aria-pressed')).toBe('true');

    // Skip capabilities: an untouched Step 3 deploys with an empty denylist
    // (every catalog tool enabled — scenarios: Step 3 skippable).
    fireEvent.click(screen.getByTestId('btn-agent-save-modal'));

    await waitFor(() => {
      expect(api.agents.create).toHaveBeenCalledWith('acme', expect.objectContaining({
        name: 'Radar Agent',
        slug: 'radar-agent',
        role: 'code-reviewer',
        brief: 'Review all PRs',
        provider_id: 'prov_anthropic',
        model: 'claude-3-7-sonnet',
        disabled_tools: [],
        enabled_mcps: [],
      }));
      // No per-agent skill state ships in the payload — tiers only.
      expect((api.agents.create as any).mock.calls[0][1].skills).toBeUndefined();
    });

    expect(onSave).toHaveBeenCalled();
  });

  it('toggle-off stores the denylist key and toggle-on removes it', async () => {
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
        provider_id: 'prov_anthropic',
        model: 'claude-3-7-sonnet',
        temperature: 1.0,
        autonomy: 'approval',
        disabled_tools: ['web.search'],
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

    // Chips start selected (empty denylist); toggling Web Search off adds
    // its key to the denylist...
    const webSearchChip = screen.getByText('Web Search').closest('button') as HTMLButtonElement;
    expect(webSearchChip.getAttribute('aria-pressed')).toBe('true');
    fireEvent.click(webSearchChip);
    expect(webSearchChip.getAttribute('aria-pressed')).toBe('false');

    // Autonomy lives on the Capabilities step, with a description per option
    expect(screen.getByText(/asks you before every tool call/i)).not.toBeNull();
    fireEvent.click(screen.getByRole('radio', { name: 'Suggest only' }));
    expect(screen.getByText(/proposes actions for you to run/i)).not.toBeNull();

    fireEvent.click(screen.getByTestId('btn-agent-save-modal'));

    await waitFor(() => {
      expect(api.agents.create).toHaveBeenCalledWith('acme', expect.objectContaining({
        name: 'Beacon',
        disabled_tools: ['web.search'],
        enabled_mcps: [],
        autonomy: 'suggest',
      }));
    });

    expect(onSave).toHaveBeenCalled();
  });

  it('toggle-on after toggle-off removes the key from the denylist, and Shell stores execute', async () => {
    const onSave = vi.fn();
    vi.spyOn(api.agents, 'create').mockResolvedValue({
      agent: {
        id: 'agent-457',
        workspace_id: 'acme',
        slug: 'beacon',
        name: 'Beacon',
        role: 'triage-bot',
        description: '',
        brief: 'Triage incidents',
        identity: '',
        soul: '',
        provider_id: 'prov_anthropic',
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
      <AgentConfigModal
        tenant={mockTenant}
        onClose={vi.fn()}
        onSave={onSave}
      />
    );

    await waitFor(() => {
      expect(screen.getByTestId('input-agent-name')).not.toBeNull();
    });

    await goToStep2({ name: 'Beacon', role: 'triage-bot', brief: 'Triage incidents' });
    await goToStep3();

    // Toggle off then back on: the key leaves the denylist again.
    const webFetchChip = screen.getByText('Web Fetch').closest('button') as HTMLButtonElement;
    fireEvent.click(webFetchChip);
    expect(webFetchChip.getAttribute('aria-pressed')).toBe('false');
    fireEvent.click(webFetchChip);
    expect(webFetchChip.getAttribute('aria-pressed')).toBe('true');

    // Toggling the single Shell chip off stores the reserved `execute` name.
    fireEvent.click(screen.getByText('Shell'));

    fireEvent.click(screen.getByTestId('btn-agent-save-modal'));

    await waitFor(() => {
      expect(api.agents.create).toHaveBeenCalledWith('acme', expect.objectContaining({
        disabled_tools: ['execute'],
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
      provider_id: 'prov_anthropic',
      model: 'claude-3-7-sonnet',
      temperature: 1.0,
      autonomy: 'approval' as const,
      disabled_tools: ['web.search'],
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
      provider_id: 'prov_anthropic',
      model: 'claude-3-7-sonnet',
      temperature: 1.0,
      autonomy: 'approval' as const,
      disabled_tools: ['web.search'],
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
    expect(screen.getByText('Ready')).not.toBeNull();
    expect((screen.getByTestId('input-agent-identity') as HTMLTextAreaElement).value).toBe('old identity body');
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
        provider_id: 'prov_anthropic',
        model: 'claude-3-7-sonnet',
        temperature: 1.0,
        autonomy: 'approval' as const,
        disabled_tools: [],
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
        provider_id: 'prov_anthropic',
        model: 'claude-3-7-sonnet',
        temperature: 1.0,
        autonomy: 'approval' as const,
        context_window: 200000,
        disabled_tools: ['web.search'],
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
      provider_id: 'prov_anthropic',
      model: 'claude-3-7-sonnet',
      temperature: 1.0,
      autonomy: 'approval' as const,
      context_window: 50000,
      disabled_tools: ['web.search'],
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
        { key: 'web.search', display_name: 'Web Search', description: '', group: 'web', icon_key: 'search', configurable: true, enabled: false, configured: false, config: {}, toggleable: true },
        { key: 'web.fetch', display_name: 'Web Fetch', description: '', group: 'web', icon_key: 'link', configurable: false, enabled: true, configured: false, config: {}, toggleable: true },
      ],
    });
    vi.spyOn(api.agents, 'create').mockResolvedValue({
      agent: { id: 'agent-x', workspace_id: 'acme', slug: 'beacon', name: 'Beacon', role: 'triage-bot', description: '', brief: 'Triage', identity: '', soul: '', provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet', temperature: 1.0, autonomy: 'approval', disabled_tools: [], skills: [], enabled_mcps: [], avatar: {}, prompts_status: 'generating', created_at: '', updated_at: '' },
    });

    render(<AgentConfigModal tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);

    await waitFor(() => { expect(screen.getByTestId('input-agent-name')).not.toBeNull(); });
    await goToStep2({ name: 'Beacon', role: 'triage-bot', brief: 'Triage' });
    await goToStep3();

    const disabledChip = screen.getByText('Web Search').closest('button') as HTMLButtonElement;
    expect(disabledChip.disabled).toBe(true);
    expect(disabledChip.title).toBe('Disabled in Settings → Tools');

    // Denylist inversion (D6): the gate-excluded chip renders selected (it
    // is exposed unless denylisted) but the gate locks the toggle.
    expect(disabledChip.getAttribute('aria-pressed')).toBe('true');

    // Clicking must not change it.
    fireEvent.click(disabledChip);
    expect(disabledChip.getAttribute('aria-pressed')).toBe('true');

    fireEvent.click(screen.getByText('Web Fetch'));

    fireEvent.click(screen.getByTestId('btn-agent-save-modal'));
    await waitFor(() => {
      expect(api.agents.create).toHaveBeenCalledWith('acme', expect.objectContaining({
        disabled_tools: ['web.fetch'],
      }));
    });
  });

  it('renders no chips for the non-toggleable channel tools', async () => {
    render(<AgentConfigModal tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);

    await waitFor(() => { expect(screen.getByTestId('input-agent-name')).not.toBeNull(); });
    await goToStep2();
    await goToStep3();

    // Always-on tools are context-granted at runtime — never agent-selectable.
    expect(screen.queryByText('Channel Post')).toBeNull();
    expect(screen.queryByText('Channel History')).toBeNull();
    expect(screen.queryByText('Close Work Session')).toBeNull();

    // The toggleable set still renders as chips.
    expect(screen.getByText('Web Search')).not.toBeNull();
    expect(screen.getByText('Browser')).not.toBeNull();
  });

  it('keeps a stored channel.post denylist key invisibly and preserves it on save', async () => {
    const summaryDraft = {
      id: 'radar', workspace_id: 'acme', slug: 'radar', name: 'Radar', role: 'Reviewer',
      description: 'PR Reviewer', model: 'claude-3-7-sonnet', status: 'idle',
    };
    const detailAgent = {
      id: 'radar', workspace_id: 'acme', slug: 'radar', name: 'Radar Detail', role: 'Code Reviewer',
      description: 'PR Reviewer Detail', brief: 'Review all pull requests thoroughly',
      identity: '', soul: '',
      provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet', temperature: 1.0,
      autonomy: 'approval' as const,
      disabled_tools: ['web.search', 'channel.post'],
      skills: [], enabled_mcps: [], avatar: {}, prompts_status: 'ready' as const,
      created_at: '', updated_at: '',
    };
    vi.spyOn(api.agents, 'get').mockResolvedValue({ agent: detailAgent });
    vi.spyOn(api.agents, 'patch').mockResolvedValue({ agent: detailAgent });

    render(<AgentConfigModal draft={summaryDraft} tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);

    await waitFor(() => {
      expect(screen.queryByTestId('agent-modal-loading')).toBeNull();
    });

    // No chip renders for the stored always-on key; the denied Web Search
    // chip hydrates deselected (its key sits in the denylist).
    fireEvent.click(screen.getByText('Capabilities'));
    expect(screen.queryByText('Channel Post')).toBeNull();
    const webSearchChip = screen.getByText('Web Search').closest('button') as HTMLButtonElement;
    expect(webSearchChip.getAttribute('aria-pressed')).toBe('false');

    // Saving keeps the stored keys instead of scrubbing them.
    fireEvent.click(screen.getByTestId('btn-agent-save-modal'));
    await waitFor(() => {
      expect(api.agents.patch).toHaveBeenCalledWith(
        'acme',
        'radar',
        expect.objectContaining({
          disabled_tools: ['web.search', 'channel.post'],
        })
      );
    });
  });

  it('renders the Browser chip selected for legacy browser.* names and keeps them on save', async () => {
    const summaryDraft = {
      id: 'radar', workspace_id: 'acme', slug: 'radar', name: 'Radar', role: 'Reviewer',
      description: 'PR Reviewer', model: 'claude-3-7-sonnet', status: 'idle',
    };
    const detailAgent = {
      id: 'radar', workspace_id: 'acme', slug: 'radar', name: 'Radar Detail', role: 'Code Reviewer',
      description: 'PR Reviewer Detail', brief: 'Review all pull requests thoroughly',
      identity: '', soul: '',
      provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet', temperature: 1.0,
      autonomy: 'approval' as const,
      disabled_tools: ['web.search', 'browser.navigate', 'browser.read'],
      skills: [], enabled_mcps: [], avatar: {}, prompts_status: 'ready' as const,
      created_at: '', updated_at: '',
    };
    vi.spyOn(api.agents, 'get').mockResolvedValue({ agent: detailAgent });
    vi.spyOn(api.agents, 'patch').mockResolvedValue({ agent: detailAgent });

    render(<AgentConfigModal draft={summaryDraft} tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);

    await waitFor(() => {
      expect(screen.queryByTestId('agent-modal-loading')).toBeNull();
    });

    // Capabilities tab: the Browser chip shows selected — the denylist
    // carries none of the alias, so the remaining browser tools stay exposed.
    fireEvent.click(screen.getByText('Capabilities'));
    const browserChip = screen.getByText('Browser').closest('button') as HTMLButtonElement;
    await waitFor(() => {
      expect(browserChip.getAttribute('aria-pressed')).toBe('true');
    });

    // An untouched save keeps the legacy member names (no alias collapse).
    fireEvent.click(screen.getByTestId('btn-agent-save-modal'));
    await waitFor(() => {
      expect(api.agents.patch).toHaveBeenCalledWith(
        'acme',
        'radar',
        expect.objectContaining({
          disabled_tools: ['web.search', 'browser.navigate', 'browser.read'],
        })
      );
    });
  });

  it('deselecting the Browser chip stores the browser alias and drops individual names', async () => {
    const summaryDraft = {
      id: 'radar', workspace_id: 'acme', slug: 'radar', name: 'Radar', role: 'Reviewer',
      description: 'PR Reviewer', model: 'claude-3-7-sonnet', status: 'idle',
    };
    const detailAgent = {
      id: 'radar', workspace_id: 'acme', slug: 'radar', name: 'Radar Detail', role: 'Code Reviewer',
      description: 'PR Reviewer Detail', brief: 'Review all pull requests thoroughly',
      identity: '', soul: '',
      provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet', temperature: 1.0,
      autonomy: 'approval' as const,
      disabled_tools: ['web.search', 'browser.navigate', 'browser.read'],
      skills: [], enabled_mcps: [], avatar: {}, prompts_status: 'ready' as const,
      created_at: '', updated_at: '',
    };
    vi.spyOn(api.agents, 'get').mockResolvedValue({ agent: detailAgent });
    vi.spyOn(api.agents, 'patch').mockResolvedValue({ agent: detailAgent });

    render(<AgentConfigModal draft={summaryDraft} tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);

    await waitFor(() => {
      expect(screen.queryByTestId('agent-modal-loading')).toBeNull();
    });

    fireEvent.click(screen.getByText('Capabilities'));

    // Deselecting the single Browser chip stores the `browser` alias — the
    // facade alias subsumes the individual browser.* names.
    fireEvent.click(screen.getByText('Browser').closest('button') as HTMLButtonElement);

    fireEvent.click(screen.getByTestId('btn-agent-save-modal'));
    await waitFor(() => {
      expect(api.agents.patch).toHaveBeenCalledWith(
        'acme',
        'radar',
        expect.objectContaining({
          disabled_tools: ['web.search', 'browser'],
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

    // Effective-set reading (D6): with the default empty denylist nothing is
    // unmet — web.search is exposed, so no dependency warning yet.
    expect(screen.queryByTestId('skill-dep-warn-github-sweeper')).toBeNull();

    // Denying web.search starves the skill: the warning names the missing
    // tool and points at the tool chips below.
    fireEvent.click(screen.getByText('Web Search'));
    const warn = screen.getByTestId('skill-dep-warn-github-sweeper');
    expect(warn.textContent).toContain('web.search');
    expect(warn.textContent).toContain('tool chips below');

    // Hint names Settings → Skills
    expect(screen.getByTestId('agent-skills-inventory').textContent).toContain('Settings → Skills');

    // New agents cannot carry agent-tier skills yet
    expect(screen.getByTestId('agent-skills-inventory').textContent).toContain('after it is deployed');
  });

  it('skill dependency warns when the workspace gate excludes the required tool', async () => {
    vi.spyOn(api.tools, 'list').mockResolvedValue({
      tools: [
        { key: 'web.search', display_name: 'Web Search', description: '', group: 'web', icon_key: 'search', configurable: true, enabled: false, configured: false, config: {}, toggleable: true },
      ],
    });

    render(<AgentConfigModal tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);

    await waitFor(() => { expect(screen.getByTestId('input-agent-name')).not.toBeNull(); });
    await goToStep2();
    await goToStep3();

    // The agent denylist is empty, but the workspace gate excludes
    // web.search — the required tool is missing from the effective set.
    const warn = screen.getByTestId('skill-dep-warn-github-sweeper');
    expect(warn.textContent).toContain('web.search');
    expect(warn.textContent).toContain('tool chips below');
  });

  it('edit mode lists agent-tier skills and installs/removes them for writers', async () => {
    const detailAgent = {
      id: 'radar', workspace_id: 'acme', slug: 'radar', name: 'Radar', role: 'Reviewer',
      description: 'PR Reviewer', brief: 'Review all PRs', identity: '', soul: '',
      provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet', temperature: 1.0,
      autonomy: 'approval' as const, disabled_tools: [], enabled_mcps: [], avatar: {},
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

  it('curated agent skills show provenance, probation standing, and a disabled promote affordance', async () => {
    const detailAgent = {
      id: 'radar', workspace_id: 'acme', slug: 'radar', name: 'Radar', role: 'Reviewer',
      description: 'PR Reviewer', brief: 'Review all PRs', identity: '', soul: '',
      provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet', temperature: 1.0,
      autonomy: 'approval' as const, disabled_tools: [], enabled_mcps: [], avatar: {},
      prompts_status: 'ready' as const, created_at: '', updated_at: '',
    };
    vi.spyOn(api.agents, 'get').mockResolvedValue({ agent: detailAgent });
    vi.spyOn(api.agents, 'listSkills').mockResolvedValue({
      skills: [
        {
          id: 'radar/pdf-sweep', tier: 'agent', name: 'pdf-sweep', version: '0.3.1', source: 'authored',
          description: 'Sweeps PDFs.', created_at: '', updated_at: '',
        },
      ],
    });
    // The agent-skills payload carries no curated state — the candidates list
    // is the provenance source. One provisional curated row, decided 3 days ago.
    vi.spyOn(api.curation, 'listCandidates').mockResolvedValue({
      candidates: [
        {
          id: 'cand-1', workspace_id: 'acme', agent_id: 'radar', cluster_id: 'cl-pdf', skill_name: 'pdf-sweep',
          status: 'provisional', proposed_content: '# pdf-sweep', is_edit: false,
          evidence_event_ids: ['ev-1', 'ev-2'], cited_pattern_refs: ['pdf-extraction'],
          helpful_count: 3, harmful_count: 1, use_count: 5,
          proposed_at: '2026-09-24T00:00:00Z',
          decided_at: new Date(Date.now() - 3 * 86400000).toISOString(),
          updated_at: new Date(Date.now() - 3 * 86400000).toISOString(),
        },
      ],
      count: 1,
    });

    render(<AgentConfigModal draft={{ id: 'radar', name: 'Radar' }} tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);

    await waitFor(() => { expect(screen.queryByTestId('agent-modal-loading')).toBeNull(); });
    fireEvent.click(screen.getByText('Capabilities'));

    // Curated badge with the skill's version + cited-pattern refs.
    await waitFor(() => {
      expect(screen.getByTestId('skill-curated-pdf-sweep')).not.toBeNull();
    });
    expect(screen.getByTestId('skill-curated-pdf-sweep').textContent).toContain('curated');
    expect(screen.getByTestId('skill-curated-pdf-sweep').textContent).toContain('v0.3.1');
    expect(screen.getByTestId('skill-pattern-pdf-sweep-pdf-extraction').textContent).toBe('pdf-extraction');

    // Provisional chip with the probation day count and the outcome tally.
    const provisional = screen.getByTestId('skill-provisional-pdf-sweep');
    expect(provisional.textContent).toContain('provisional');
    expect(provisional.textContent).toContain('day 3');
    expect(provisional.textContent).toContain('3 helpful');
    expect(provisional.textContent).toContain('1 harmful');

    // Promote-to-workspace stays disabled with its unmet criteria named.
    const promote = screen.getByTestId('btn-promote-pdf-sweep') as HTMLButtonElement;
    expect(promote.disabled).toBe(true);
    expect(screen.getByTestId('skill-promote-pdf-sweep').textContent).toContain(
      'needs 2+ agents converging on this procedure'
    );

    // Plain agent skills stay unbadged.
    expect(screen.queryByTestId('skill-curated-log-sweeper')).toBeNull();
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
        description: '', brief: 'Review all PRs', identity: '', soul: '',
        provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet', temperature: 1.0, autonomy: 'approval',
        disabled_tools: [], skills: [], enabled_mcps: [], avatar: {}, prompts_status: 'generating', created_at: '', updated_at: '',
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
      description: 'PR Reviewer', brief: 'Review all PRs', identity: '', soul: '',
      provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet', temperature: 1.0,
      autonomy: 'approval' as const, disabled_tools: [], skills: [],
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
      description: '', brief: 'Review all PRs', identity: '', soul: '',
      provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet', temperature: 1.0,
      autonomy: 'approval' as const, disabled_tools: [], skills: [], enabled_mcps: [],
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
      description: '', brief: 'Review all PRs', identity: '', soul: '',
      provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet', temperature: 1.0,
      autonomy: 'approval' as const, disabled_tools: [], skills: [], enabled_mcps: [],
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

  // -------------------------------------------------------------------------
  // Agent-private OAuth rows (add-mcp-oauth-client 7.1)
  // -------------------------------------------------------------------------

  it('marks an expired agent oauth row with the expired chip and a Re-authorize action', async () => {
    vi.spyOn(api.agents, 'listMcpServers').mockResolvedValue({
      servers: [agentOauthRow({ status: 'expired' })],
    });

    render(<AgentConfigModal draft={{ id: 'radar', name: 'Radar' }} tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);
    await waitFor(() => { expect(screen.queryByTestId('agent-modal-loading')).toBeNull(); });
    fireEvent.click(screen.getByText('Capabilities'));

    const status = screen.getByTestId('agent-mcp-server-status-srv-notion');
    expect(status.textContent).toBe('Expired');
    // Warning tone, matching the settings pane's expired convention.
    expect(status.className).toContain('warn');
    expect(screen.getByTestId('agent-mcp-oauth-srv-notion').textContent).toBe('Re-authorize');
  });

  it('offers the sign-in affordance on an unconnected agent oauth row and calls the agent-scoped authorize endpoint', async () => {
    const assignMock = stubLocationAssign();
    vi.spyOn(api.agents, 'listMcpServers').mockResolvedValue({
      servers: [agentOauthRow()],
    });
    const authorize = vi
      .spyOn(api.agents, 'authorizeMcpServer')
      .mockResolvedValue({ authorize_url: 'https://auth.notion.com/authorize?client_id=x' });

    render(<AgentConfigModal draft={{ id: 'radar', name: 'Radar' }} tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);
    await waitFor(() => { expect(screen.queryByTestId('agent-modal-loading')).toBeNull(); });
    fireEvent.click(screen.getByText('Capabilities'));

    const btn = screen.getByTestId('agent-mcp-oauth-srv-notion');
    expect(btn.textContent).toBe('Sign in');
    fireEvent.click(btn);

    // The begin runs against the agent-scoped endpoint with the agent id.
    await waitFor(() => {
      expect(authorize).toHaveBeenCalledWith('acme', 'radar', 'srv-notion');
    });
    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith('https://auth.notion.com/authorize?client_id=x');
    });
  });

  it('renders the needs-authorization detail with a sign-in action on a probe without a token', async () => {
    vi.spyOn(api.agents, 'listMcpServers').mockResolvedValue({
      servers: [
        agentOauthRow({ status: 'error', status_error: `mcp server requires OAuth authorization: ${OAUTH_REQUIRED_DETAIL}` }),
      ],
    });

    render(<AgentConfigModal draft={{ id: 'radar', name: 'Radar' }} tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);
    await waitFor(() => { expect(screen.queryByTestId('agent-modal-loading')).toBeNull(); });
    fireEvent.click(screen.getByText('Capabilities'));

    expect(screen.getByTestId('agent-mcp-server-status-srv-notion').textContent).toBe('Error');
    // The trimmed actionable guidance replaces the typed prefix; the full
    // detail stays on the title tooltip.
    expect(screen.getByTestId('agent-mcp-server-srv-notion').textContent).toContain(OAUTH_REQUIRED_DETAIL);
    expect(screen.getByTestId('agent-mcp-oauth-srv-notion').textContent).toBe('Sign in');
  });

  it('hides private server write controls from holders without agents.write but keeps rows', async () => {
    // Built-in Member set per the decided catalog (fix-role-permission-audit):
    // reads plus channels.read/channels.write — roles.write is gone.
    useAuthStore.setState({
      memberships: [
        {
          workspace_id: 'acme',
          role_name: 'Member',
          role: {
            name: 'Member',
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
          },
        },
      ] as any,
    });
    vi.spyOn(api.agents, 'listMcpServers').mockResolvedValue({
      servers: [mcpServerRow({ id: 'srv-private', name: 'Private Sentry', tool_count: 3 })],
    });
    const detailAgent = {
      id: 'radar', workspace_id: 'acme', slug: 'radar', name: 'Radar', role: 'Reviewer',
      description: '', brief: 'Review all PRs', identity: '', soul: '',
      provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet', temperature: 1.0,
      autonomy: 'approval' as const, disabled_tools: [], skills: [], enabled_mcps: [],
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

describe('modals/AgentConfigModal — Hooks section (integrate-agent-hooks)', () => {
  const hooksTenant = {
    id: 'acme',
    sub: 'acme',
    name: 'Acme Corp',
    providers: [],
  };

  const hookRow = (overrides: Record<string, unknown> = {}) => ({
    id: 'hook-agent-1',
    agent_id: 'radar',
    workspace_id: 'acme',
    name: 'Private Shell Gate',
    event: 'pre_tool_use',
    matcher: 'execute',
    handler_type: 'command',
    config: { command: '/usr/local/bin/gate' },
    timeout_ms: 5000,
    on_failure: 'allow',
    enabled: true,
    position: 0,
    status: 'ok',
    created_at: '2026-09-01T00:00:00Z',
    updated_at: '2026-09-01T00:00:00Z',
    ...overrides,
  });

  beforeEach(() => {
    vi.restoreAllMocks();
    useAuthStore.setState({ memberships: [] });
    vi.spyOn(api.providers, 'list').mockResolvedValue({ providers: [] });
    vi.spyOn(api.providers, 'models').mockResolvedValue({ source: 'none', models: [] });
    vi.spyOn(api.skills, 'list').mockResolvedValue({ skills: [] });
    vi.spyOn(api.agents, 'listSkills').mockResolvedValue({ skills: [] });
    vi.spyOn(api.agents, 'listMcpServers').mockResolvedValue({ servers: [] });
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [] });
    // The Integrations section lists managed connections (empty unless a test
    // overrides it) — optional context that must not block the modal.
    vi.spyOn(connectionsApi, 'list').mockResolvedValue({ connections: [] });
    // The read-only reference-documents section (add-reference-documents 9.1):
    // empty unless a test overrides it — optional context that must not block
    // the modal, and never a real network call in tests.
    vi.spyOn(documentsApi, 'list').mockResolvedValue({ documents: [] });
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools: [] });
  });

  /** Renders the edit modal with hooks resolved and opens the Hooks tab. */
  async function openHooksTab(listHooks: { instance: any[]; workspace: any[]; agent: any[] }) {
    const detailAgent = {
      id: 'radar', workspace_id: 'acme', slug: 'radar', name: 'Radar', role: 'Reviewer',
      description: '', brief: 'Review all PRs', identity: '', soul: '',
      provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet', temperature: 1.0,
      autonomy: 'approval' as const, disabled_tools: [], skills: [], enabled_mcps: [],
      avatar: {}, prompts_status: 'ready' as const, created_at: '', updated_at: '',
    };
    vi.spyOn(api.agents, 'get').mockResolvedValue({ agent: detailAgent });
    const listHooksSpy = vi.spyOn(api.agents, 'listHooks').mockResolvedValue(listHooks as any);

    render(<AgentConfigModal draft={{ id: 'radar', name: 'Radar' }} tenant={hooksTenant} onClose={vi.fn()} onSave={vi.fn()} />);
    await waitFor(() => {
      expect(screen.queryByTestId('agent-modal-loading')).toBeNull();
    });
    expect(listHooksSpy).toHaveBeenCalledWith('acme', 'radar');
    fireEvent.click(screen.getByText('Hooks'));
    await waitFor(() => {
      expect(screen.getByTestId('agent-hooks-pane')).not.toBeNull();
    });
  }

  it('shows the mandatory instance and workspace hooks read-only with level badges and no controls', async () => {
    await openHooksTab({
      instance: [hookRow({ id: 'hook-inst', name: 'Org Gate', agent_id: undefined, workspace_id: undefined, handler_type: 'http' })],
      workspace: [hookRow({ id: 'hook-ws', name: 'WS Observer', agent_id: undefined })],
      agent: [hookRow()],
    });

    // The agent's own hook is editable.
    expect(screen.getByTestId('agent-hook-hook-agent-1')).not.toBeNull();
    expect(screen.getByTestId('btn-edit-agent-hook-hook-agent-1')).not.toBeNull();

    // Instance + workspace rows render read-only with their level marked.
    expect(screen.getByTestId('agent-hook-inherited-hook-inst').textContent).toContain('Org Gate');
    expect(screen.getByTestId('agent-hook-level-hook-inst').textContent).toBe('Instance');
    expect(screen.getByTestId('agent-hook-inherited-hook-ws').textContent).toContain('WS Observer');
    expect(screen.getByTestId('agent-hook-level-hook-ws').textContent).toBe('Workspace');

    // Neither offers disable or exclusion controls (D13).
    expect(screen.queryByRole('switch', { name: 'Enable Org Gate' })).toBeNull();
    expect(screen.queryByRole('switch', { name: 'Enable WS Observer' })).toBeNull();
    expect(screen.queryByTestId('btn-edit-agent-hook-hook-inst')).toBeNull();
    expect(screen.queryByTestId('btn-remove-agent-hook-hook-ws')).toBeNull();
  });

  it('creates an agent-private hook through the shared dialog on the agent endpoints', async () => {
    await openHooksTab({ instance: [], workspace: [], agent: [] });
    expect(screen.getByTestId('agent-hooks-empty')).not.toBeNull();

    const createHook = vi.spyOn(api.agents, 'createHook').mockResolvedValue({
      hook: hookRow({ id: 'hook-new', name: 'Gate' }),
      match_count: { matched: 1, of: 12 },
    } as any);

    fireEvent.click(screen.getByTestId('btn-add-agent-hook'));
    await waitFor(() => {
      expect(screen.getByTestId('modal-hook')).not.toBeNull();
    });
    fireEvent.change(screen.getByTestId('input-hook-name'), { target: { value: 'Gate' } });
    fireEvent.change(screen.getByTestId('select-hook-handler'), { target: { value: 'http' } });
    fireEvent.change(screen.getByTestId('input-hook-url'), { target: { value: 'https://hooks.example.com/x' } });
    fireEvent.click(screen.getByTestId('btn-hook-save'));

    await waitFor(() => {
      expect(createHook).toHaveBeenCalledWith('acme', 'radar', {
        name: 'Gate',
        event: 'pre_tool_use',
        matcher: '',
        handler_type: 'http',
        config: { url: 'https://hooks.example.com/x' },
        timeout_ms: 5000,
        on_failure: 'allow',
        enabled: true,
      });
    });
    fireEvent.click(screen.getByTestId('btn-hook-done'));
    await waitFor(() => {
      expect(screen.queryByTestId('modal-hook')).toBeNull();
      expect(screen.getByTestId('agent-hook-hook-new')).not.toBeNull();
    });
  });
});

describe('modals/AgentConfigModal — Heartbeat section (add-agent-heartbeat)', () => {
  const hbTenant = {
    id: 'acme',
    sub: 'acme',
    name: 'Acme Corp',
    tz: 'UTC',
    providers: [],
  };

  const hbDetailAgent = {
    id: 'radar', workspace_id: 'acme', slug: 'radar', name: 'Radar', role: 'Reviewer',
    description: '', brief: 'Review all PRs', identity: '', soul: '',
    provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet', temperature: 1.0,
    autonomy: 'approval' as const, disabled_tools: [], skills: [], enabled_mcps: [],
    avatar: {}, prompts_status: 'ready' as const, created_at: '', updated_at: '',
  };

  const savedHeartbeat = (overrides: Partial<ApiHeartbeat> = {}): ApiHeartbeat => ({
    id: 'hb-1',
    workspace_id: 'acme',
    agent_id: 'radar',
    prompt: 'TEMPLATE',
    expr: '*/30 * * * *',
    human_label: 'every 30 minutes',
    active_start: null,
    active_end: null,
    delivery: { type: 'creator_dm' },
    enabled: true,
    next_tick_at: '2026-09-15T15:30:00Z',
    last_tick: null,
    failure_streak: 0,
    created_at: '',
    updated_at: '',
    ...overrides,
  });

  beforeEach(() => {
    vi.restoreAllMocks();
    useAuthStore.setState({ memberships: [] });
    vi.spyOn(api.providers, 'list').mockResolvedValue({ providers: [] });
    vi.spyOn(api.providers, 'models').mockResolvedValue({ source: 'none', models: [] });
    vi.spyOn(api.skills, 'list').mockResolvedValue({ skills: [] });
    vi.spyOn(api.agents, 'listSkills').mockResolvedValue({ skills: [] });
    vi.spyOn(api.agents, 'listMcpServers').mockResolvedValue({ servers: [] });
    vi.spyOn(api.agents, 'listHooks').mockResolvedValue({ instance: [], workspace: [], agent: [] });
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [] });
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools: [] });
    vi.spyOn(api.channels, 'list').mockResolvedValue({ channels: [] });
    vi.spyOn(heartbeats, 'get').mockResolvedValue({ heartbeat: null, default_prompt: 'TEMPLATE' });
  });

  /** Renders the edit modal with the agent detail resolved. */
  async function renderEditModal(opts: { onClose?: () => void; onSave?: () => void } = {}) {
    vi.spyOn(api.agents, 'get').mockResolvedValue({ agent: hbDetailAgent });
    vi.spyOn(api.agents, 'patch').mockResolvedValue({ agent: hbDetailAgent });
    render(
      <AgentConfigModal
        draft={{ id: 'radar', name: 'Radar' }}
        tenant={hbTenant}
        onClose={opts.onClose || vi.fn()}
        onSave={opts.onSave || vi.fn()}
      />
    );
    await waitFor(() => {
      expect(screen.queryByTestId('agent-modal-loading')).toBeNull();
    });
  }

  it('places the Heartbeat tab between Hooks and Prompts in edit mode', async () => {
    await renderEditModal();

    const tab = screen.getByTestId('tab-heartbeat');
    expect(tab.textContent).toBe('Heartbeat');
    expect(tab.previousElementSibling?.textContent).toBe('Hooks');
    expect(tab.nextElementSibling?.textContent).toBe('Prompts');
  });

  it('has no Heartbeat tab in create/wizard mode', async () => {
    render(<AgentConfigModal tenant={hbTenant} onClose={vi.fn()} onSave={vi.fn()} />);
    await waitFor(() => {
      expect(screen.getByTestId('input-agent-name')).not.toBeNull();
    });
    expect(screen.queryByTestId('tab-heartbeat')).toBeNull();
  });

  it('rides Save: never PUTs for a pristine pane, PUTs once once the toggle is touched', async () => {
    const putSpy = vi.spyOn(heartbeats, 'update').mockResolvedValue({ heartbeat: savedHeartbeat() });
    const onClose = vi.fn();
    await renderEditModal({ onClose });

    fireEvent.click(screen.getByTestId('tab-heartbeat'));
    // The never-created pane seeds its checklist from the server template.
    await waitFor(() => {
      expect((screen.getByTestId('heartbeat-prompt') as HTMLTextAreaElement).value).toBe('TEMPLATE');
    });

    // Pristine pane + Save: agent PATCH lands, modal closes, no heartbeat row.
    fireEvent.click(screen.getByTestId('btn-agent-save-modal'));
    await waitFor(() => {
      expect(api.agents.patch).toHaveBeenCalledTimes(1);
      expect(onClose).toHaveBeenCalledTimes(1);
    });
    expect(putSpy).not.toHaveBeenCalled();

    // Enabling the heartbeat makes the pane dirty: the next Save PUTs once.
    fireEvent.click(screen.getByRole('switch', { name: 'Enable heartbeat' }));
    fireEvent.click(screen.getByTestId('btn-agent-save-modal'));
    await waitFor(() => {
      expect(putSpy).toHaveBeenCalledTimes(1);
      expect(onClose).toHaveBeenCalledTimes(2);
    });
    expect(putSpy).toHaveBeenCalledWith('acme', 'radar', expect.objectContaining({
      enabled: true,
      expr: '*/30 * * * *',
      prompt: 'TEMPLATE',
      delivery: { type: 'creator_dm' },
    }));
  });

  it('keeps the modal open on the Heartbeat tab with the field error inline when the heartbeat PUT is rejected', async () => {
    vi.spyOn(heartbeats, 'update').mockRejectedValue(
      new ApiError(422, 'invalid_request', 'invalid request', [
        { field: 'expr', message: 'fires faster than every 5m' },
      ])
    );
    const onClose = vi.fn();
    const onSave = vi.fn();
    await renderEditModal({ onClose, onSave });

    fireEvent.click(screen.getByTestId('tab-heartbeat'));
    await waitFor(() => {
      expect((screen.getByTestId('heartbeat-prompt') as HTMLTextAreaElement).value).toBe('TEMPLATE');
    });

    // Enable + save: the agent PATCH succeeds first and is not rolled back;
    // the heartbeat 422 keeps the modal open with the server's field message.
    fireEvent.click(screen.getByRole('switch', { name: 'Enable heartbeat' }));
    fireEvent.click(screen.getByTestId('btn-agent-save-modal'));

    await waitFor(() => {
      expect(heartbeats.update).toHaveBeenCalledTimes(1);
      expect(onSave).toHaveBeenCalledTimes(1);
    });
    expect(onClose).not.toHaveBeenCalled();
    const err = screen.getByTestId('heartbeat-expr-error');
    expect(err.textContent).toContain('fires faster than every 5m');
  });
});

describe('modals/AgentConfigModal — workspace default inherit (refactor-workspace-settings)', () => {
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
    ],
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

  beforeEach(() => {
    vi.restoreAllMocks();
    useAuthStore.setState({ memberships: [] });
    vi.spyOn(api.providers, 'list').mockResolvedValue({ providers: mockTenant.providers });
    vi.spyOn(api.providers, 'models').mockResolvedValue({
      source: 'live',
      models: [
        { id: 'claude-3-7-sonnet', name: 'Claude 3.7 Sonnet', efforts: ['low', 'medium', 'high'] },
        { id: 'gpt-4o', name: 'GPT-4o' },
      ],
    });
    vi.spyOn(api.skills, 'list').mockResolvedValue({ skills: [] });
    vi.spyOn(api.agents, 'listSkills').mockResolvedValue({ skills: [] });
    vi.spyOn(api.agents, 'listMcpServers').mockResolvedValue({ servers: [] });
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [] });
    // The Integrations section lists managed connections (empty unless a test
    // overrides it) — optional context that must not block the modal.
    vi.spyOn(connectionsApi, 'list').mockResolvedValue({ connections: [] });
    // The read-only reference-documents section (add-reference-documents 9.1):
    // empty unless a test overrides it — optional context that must not block
    // the modal, and never a real network call in tests.
    vi.spyOn(documentsApi, 'list').mockResolvedValue({ documents: [] });
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools: [] });
  });

  async function goToStep2WithDefault(
    defaultModel: unknown,
    props: { onSave?: (agent: any) => void; onClose?: () => void } = {}
  ) {
    vi.spyOn(api.workspaces, 'get').mockResolvedValue(workspacePayload(defaultModel) as any);
    render(
      <AgentConfigModal
        tenant={mockTenant}
        onClose={props.onClose || vi.fn()}
        onSave={props.onSave || vi.fn()}
      />
    );
    await waitFor(() => {
      expect(screen.getByTestId('input-agent-name')).not.toBeNull();
    });
    fireEvent.change(screen.getByTestId('input-agent-name'), { target: { value: 'Radar Agent' } });
    fireEvent.change(screen.getByTestId('input-agent-role'), { target: { value: 'code-reviewer' } });
    fireEvent.change(screen.getByTestId('input-agent-brief'), { target: { value: 'Review all PRs' } });
    fireEvent.click(screen.getByTestId('btn-agent-next-step'));
    await waitFor(() => {
      expect(screen.getByLabelText(/provider/i)).not.toBeNull();
    });
    await waitFor(() => {
      const select = screen.getByTestId('select-model') as HTMLSelectElement;
      expect(select.value).toBe('claude-3-7-sonnet');
    });
  }

  it('offers "Workspace default (inherit)" only while the workspace payload carries a default model', async () => {
    await goToStep2WithDefault({ provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet' });

    const providerSelect = screen.getByLabelText(/provider/i);
    expect(
      Array.from(providerSelect.querySelectorAll('option')).some((o) =>
        o.textContent?.includes('Workspace default (inherit)')
      )
    ).toBe(true);

    // No default in the payload → the option disappears.
    vi.spyOn(api.workspaces, 'get').mockResolvedValue(workspacePayload(null) as any);
    cleanup();
    render(<AgentConfigModal tenant={mockTenant} onClose={vi.fn()} onSave={vi.fn()} />);
    await waitFor(() => {
      expect(screen.getByTestId('input-agent-name')).not.toBeNull();
    });
    fireEvent.change(screen.getByTestId('input-agent-name'), { target: { value: 'Radar Agent' } });
    fireEvent.change(screen.getByTestId('input-agent-role'), { target: { value: 'code-reviewer' } });
    fireEvent.change(screen.getByTestId('input-agent-brief'), { target: { value: 'Review all PRs' } });
    fireEvent.click(screen.getByTestId('btn-agent-next-step'));
    await waitFor(() => {
      expect((screen.getByTestId('select-model') as HTMLSelectElement).value).toBe('claude-3-7-sonnet');
    });
    expect(screen.queryByTestId('option-inherit-workspace-default')).toBeNull();
    // Pinned path unchanged: a provider and model pair is required.
    expect(screen.getByTestId('select-model')).not.toBeNull();
  });

  it('selecting inherit hides the model combobox and effort dropdown and saves the empty pair', async () => {
    const onSave = vi.fn();
    const create = vi.spyOn(api.agents, 'create').mockResolvedValue({
      agent: {
        id: 'agent-inh',
        workspace_id: 'acme',
        slug: 'radar-agent',
        name: 'Radar Agent',
        role: 'code-reviewer',
        description: '',
        brief: 'Review all PRs',
        identity: '',
        soul: '',
        provider_id: null,
        model: '',
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
    } as any);

    await goToStep2WithDefault(
      { provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet' },
      { onSave }
    );

    // Pinned path renders the model combobox and the effort dropdown first.
    expect(screen.getByTestId('select-effort')).not.toBeNull();

    fireEvent.change(screen.getByTestId('select-agent-provider'), {
      target: { value: '__workspace_default__' },
    });

    // Both the model combobox and the effort dropdown are gone.
    expect(screen.queryByTestId('select-model')).toBeNull();
    expect(screen.queryByTestId('select-effort')).toBeNull();
    expect(screen.getByTestId('inherit-default-hint')).not.toBeNull();

    fireEvent.click(screen.getByTestId('btn-agent-next-step'));
    await waitFor(() => {
      expect(screen.getByText('Capabilities & Integrations')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-agent-save-modal'));

    await waitFor(() => {
      expect(create).toHaveBeenCalledWith(
        'acme',
        expect.objectContaining({ provider_id: '', model: '' })
      );
      expect(onSave).toHaveBeenCalled();
    });
  });

  it('keeps "Provider is required" from firing on the inherit path while still gating the pinned path', async () => {
    await goToStep2WithDefault({ provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet' });

    fireEvent.change(screen.getByTestId('select-agent-provider'), {
      target: { value: '__workspace_default__' },
    });
    fireEvent.click(screen.getByTestId('btn-agent-next-step'));

    // No pair errors — the wizard advances to Step 3.
    await waitFor(() => {
      expect(screen.getByText('Capabilities & Integrations')).not.toBeNull();
    });
    expect(screen.queryByText(/provider is required/i)).toBeNull();
    expect(screen.queryByText(/model is required/i)).toBeNull();
  });
});

// Reference documents (add-reference-documents 9.1): the Capabilities surface
// lists promoted + agent-attached documents read-only — scope badges,
// index-status chips, a preview link into a standalone modal
// (rework-document-chat-surfaces 1.2) — with an empty
// state, and the section stays out entirely when the fetch fails.
describe('modals/AgentConfigModal — reference documents section (9.1)', () => {
  beforeEach(() => {
    useAuthStore.setState({ memberships: [] });
    vi.spyOn(api.providers, 'list').mockResolvedValue({ providers: [] });
    vi.spyOn(api.providers, 'models').mockResolvedValue({ source: 'none', models: [] });
    vi.spyOn(api.skills, 'list').mockResolvedValue({ skills: [] });
    vi.spyOn(api.agents, 'listSkills').mockResolvedValue({ skills: [] });
    vi.spyOn(api.agents, 'listMcpServers').mockResolvedValue({ servers: [] });
    vi.spyOn(api.agents, 'listHooks').mockResolvedValue({ agent: [], instance: [], workspace: [] });
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [] });
    vi.spyOn(connectionsApi, 'list').mockResolvedValue({ connections: [] });
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools: [] });
  });

  const doc = (over: Partial<ApiReferenceDocument> = {}): ApiReferenceDocument => ({
    id: 'doc-1',
    name: 'runbook.pdf',
    description: 'Incident runbook',
    mime: 'application/pdf',
    size: 1024,
    url: '/files/acme/docs/runbook.pdf',
    indexStatus: 'ready',
    scope: 'attached',
    pageCount: 12,
    agents: ['radar'],
    channels: [],
    createdAt: '2026-09-01T00:00:00Z',
    ...over,
  });

  const detailAgent = {
    id: 'radar', workspace_id: 'acme', slug: 'radar', name: 'Radar', role: 'Reviewer',
    description: '', brief: 'Review all PRs', identity: '', soul: '',
    provider_id: 'prov_anthropic', model: 'claude-3-7-sonnet', temperature: 1.0,
    autonomy: 'approval' as const, disabled_tools: [], skills: [], enabled_mcps: [],
    avatar: {}, prompts_status: 'ready' as const, created_at: '', updated_at: '',
  };

  async function openCapabilities(docs: ApiReferenceDocument[]) {
    vi.spyOn(api.agents, 'get').mockResolvedValue({ agent: detailAgent });
    vi.spyOn(documentsApi, 'list').mockResolvedValue({ documents: docs });
    render(<AgentConfigModal draft={{ id: 'radar', name: 'Radar' }} tenant={{ id: 'acme', sub: 'acme', name: 'Acme' }} onClose={vi.fn()} onSave={vi.fn()} />);
    await waitFor(() => {
      expect(screen.queryByTestId('agent-modal-loading')).toBeNull();
    });
    fireEvent.click(screen.getByText('Capabilities'));
    await waitFor(() => {
      expect(screen.getByText('Reference documents')).not.toBeNull();
    });
  }

  it('lists promoted and agent-attached documents with badges — nothing else, no edit controls', async () => {
    await openCapabilities([
      doc({ scope: 'workspace', agents: [] }),
      doc({ id: 'doc-2', name: 'radar-only.md', mime: 'text/markdown', scope: 'attached', agents: ['radar'], indexStatus: 'no_text_layer' }),
      doc({ id: 'doc-3', name: 'elsewhere.md', mime: 'text/markdown', scope: 'attached', agents: ['other-agent'] }),
    ]);
    const section = screen.getByTestId('agent-documents-section');
    expect(screen.getByTestId('agent-document-doc-1')).not.toBeNull();
    expect(screen.getByTestId('agent-document-scope-doc-1').textContent).toBe('ALL AGENTS');
    expect(screen.getByTestId('agent-document-doc-2')).not.toBeNull();
    expect(screen.getByTestId('agent-document-scope-doc-2').textContent).toBe('Attached');
    expect(screen.getByTestId('agent-document-index-doc-2').textContent).toBe('No text layer');
    // A document attached to another agent is not this agent's context.
    expect(screen.queryByTestId('agent-document-doc-3')).toBeNull();

    // Read-only: the section's only buttons are the two Preview links.
    const buttons = Array.from(section.querySelectorAll('button'));
    expect(buttons).toHaveLength(2);
    buttons.forEach((b) => expect(b.textContent).toBe('Preview'));
    // No toggle/attach/promote affordances anywhere in the section.
    expect(section.querySelectorAll('input[type="checkbox"]')).toHaveLength(0);
  });

  it('a preview link renders the document source in a standalone modal (1.2)', async () => {
    await openCapabilities([doc()]);
    fireEvent.click(screen.getByTestId('agent-document-preview-doc-1'));

    // pdf rides the iframe source — no fetch needed.
    const modal = screen.getByTestId('modal-document-preview');
    expect(modal).not.toBeNull();
    const frame = modal.querySelector(
      'iframe[data-od-id="panel-document-frame"]'
    ) as HTMLIFrameElement;
    expect(frame).not.toBeNull();
    expect(frame.getAttribute('src')).toContain('/files/acme/docs/runbook.pdf');

    // Panel-less surface: the panel slice stays untouched — no tab minted.
    const panel = useStore.getState().panel;
    expect(panel.open).toBe(false);
    expect(panel.tabs).toHaveLength(0);

    // Closing the preview returns to the capabilities step. Scoped to the
    // preview modal — the agent modal has its own Close dialog button.
    fireEvent.click(modal.querySelector('button[aria-label="Close dialog"]')!);
    await waitFor(() => {
      expect(screen.queryByTestId('modal-document-preview')).toBeNull();
    });
    expect(screen.getByTestId('agent-documents-section')).not.toBeNull();
  });

  it('an empty library renders the empty state pointing at Settings → Documents', async () => {
    await openCapabilities([]);
    expect(screen.getByTestId('agent-documents-empty').textContent).toContain('Settings → Documents');
  });

  it('a failed fetch silently omits the section (AgentConnectionsSection precedent)', async () => {
    vi.spyOn(api.agents, 'get').mockResolvedValue({ agent: detailAgent });
    vi.spyOn(documentsApi, 'list').mockRejectedValue(new Error('offline'));
    render(<AgentConfigModal draft={{ id: 'radar', name: 'Radar' }} tenant={{ id: 'acme', sub: 'acme', name: 'Acme' }} onClose={vi.fn()} onSave={vi.fn()} />);
    await waitFor(() => {
      expect(screen.queryByTestId('agent-modal-loading')).toBeNull();
    });
    fireEvent.click(screen.getByText('Capabilities'));
    await waitFor(() => {
      expect(screen.getByText('Capabilities & Integrations')).not.toBeNull();
    });
    expect(screen.queryByTestId('agent-documents-section')).toBeNull();
  });
});
