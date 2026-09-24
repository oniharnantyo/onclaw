/**
 * @vitest-environment jsdom
 */
// ConnectionEditDialog tests (add-connection-edit 5.1): initial attached
// state per attach id, token-first save ordering (D3), the full desired-id
// set on save, the OAuth-kind Reauthorize variant, and the unchanged/disabled
// + all-or-nothing failure handling.
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { ConnectionEditDialog } from './ConnectionEditDialog';
import { connectionsApi, type ApiConnection, type ApiIntegrationRecipe } from '../lib/connectionsApi';
import { api, ApiError } from '../lib/api';

// This environment's jsdom exposes no localStorage — install the stub BEFORE
// the token-carrying imports (same mode as src/lib/panel/filesApi.test.ts).
const backing = new Map<string, string>();
(globalThis as any).localStorage = {
  getItem: (k: string) => (backing.has(k) ? backing.get(k)! : null),
  setItem: (k: string, v: string) => void backing.set(k, String(v)),
  removeItem: (k: string) => void backing.delete(k),
  clear: () => void backing.clear(),
  key: (i: number) => Array.from(backing.keys())[i] ?? null,
  get length() { return backing.size; },
};

const tenant = { id: 'acme', sub: 'acme' };

const githubRecipe = (overrides: Partial<ApiIntegrationRecipe> = {}): ApiIntegrationRecipe => ({
  id: 'github',
  service: 'GitHub',
  icon: 'github',
  auth_kind: 'pat',
  availability: 'available',
  transport: 'streamable_http',
  access_levels: ['read_only', 'read_write'],
  steps: [],
  scopes: [],
  probe: { tool: 'list-repositories' },
  ...overrides,
});

const atlassianRecipe = (overrides: Partial<ApiIntegrationRecipe> = {}): ApiIntegrationRecipe => ({
  id: 'atlassian',
  service: 'Atlassian',
  icon: 'atlassian',
  auth_kind: 'oauth',
  availability: 'available',
  transport: 'sse',
  access_levels: ['read_only'],
  steps: [],
  scopes: [],
  probe: { tool: 'get-visible-issues' },
  ...overrides,
});

const connectionRow = (overrides: Partial<ApiConnection> = {}): ApiConnection => ({
  id: 'conn-gh',
  workspace_id: 'acme',
  service: 'github',
  access_level: 'read_only',
  status: 'connected',
  status_error: null,
  token_hint: 'a1b2',
  server_id: 'srv-gh',
  server_enabled: true,
  tool_count: 24,
  attached_agents: ['Atlas'],
  origin: 'https://github.example.com',
  created_at: '',
  updated_at: '',
  ...overrides,
});

const agentsFixture = () =>
  [
    { id: 'a1', slug: 'atlas', name: 'Atlas', enabled_mcps: ['srv-gh'] },
    { id: 'a2', slug: 'beacon', name: 'Beacon', enabled_mcps: [] },
    { id: 'a3', slug: 'cronus', name: 'Cronus', enabled_mcps: ['other-srv'] },
  ] as any[];

function renderDialog(
  connection: ApiConnection,
  recipes: ApiIntegrationRecipe[],
  props: Partial<Parameters<typeof ConnectionEditDialog>[0]> = {}
) {
  return render(
    <ConnectionEditDialog
      tenant={tenant}
      connection={connection}
      recipes={recipes}
      onClose={vi.fn()}
      onToast={vi.fn()}
      onSaved={vi.fn()}
      {...props}
    />
  );
}

/** jsdom navigations need stubbing; same pattern as IntegrationsSection.test. */
function stubLocationAssign() {
  const assignMock = vi.fn();
  Object.defineProperty(window, 'location', {
    value: { ...window.location, assign: assignMock },
    writable: true,
    configurable: true,
  });
  return assignMock;
}

describe('modals/ConnectionEditDialog', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(api.agents, 'list').mockResolvedValue({ agents: agentsFixture() });
    vi.spyOn(connectionsApi, 'setAgents').mockResolvedValue({ connection: connectionRow() });
    vi.spyOn(connectionsApi, 'replaceToken').mockResolvedValue({ connection: connectionRow() });
    vi.spyOn(connectionsApi, 'reauthorize').mockResolvedValue({
      authorize_url: 'https://auth.example.com/authorize',
    });
  });

  it('renders the workspace agents with their initial attached state and disables save while unchanged', async () => {
    renderDialog(connectionRow(), [githubRecipe()]);

    await waitFor(() => {
      expect(screen.getByTestId('attach-agents-list')).not.toBeNull();
    });
    // Attached state rides the attach id (the materialized server id for MCP
    // kind), not the connection id — Cronus holds an unrelated server. The
    // picker is a multi-select combobox now: open it and read aria-selected
    // off the option rows (keyed by agent id).
    fireEvent.click(screen.getByTestId('attach-agents-trigger'));
    expect(screen.getByTestId('multiselect-option-a1').getAttribute('aria-selected')).toBe('true');
    expect(screen.getByTestId('multiselect-option-a2').getAttribute('aria-selected')).toBe('false');
    expect(screen.getByTestId('multiselect-option-a3').getAttribute('aria-selected')).toBe('false');

    // Display-only meta line: origin and access level as mono chips.
    const meta = screen.getByTestId('edit-connection-meta');
    expect(meta.textContent).toContain('https://github.example.com');
    expect(meta.textContent).toContain('Read-only');

    // Unchanged state (no toggle, empty token) — save stays disabled.
    expect((screen.getByTestId('btn-edit-save') as HTMLButtonElement).disabled).toBe(true);
  });

  it('saves only the agent diff when the token field stays empty — no replace call', async () => {
    const replaceToken = vi.spyOn(connectionsApi, 'replaceToken');
    const setAgents = vi.spyOn(connectionsApi, 'setAgents').mockResolvedValue({ connection: connectionRow() });
    const onClose = vi.fn();
    const onToast = vi.fn();
    const onSaved = vi.fn();

    renderDialog(connectionRow(), [githubRecipe()], { onClose, onToast, onSaved });
    await waitFor(() => {
      expect(screen.getByTestId('attach-agents-list')).not.toBeNull();
    });

    // Detach Atlas, attach Beacon — the desired set is exactly a2. One open
    // covers both: toggling a row keeps the popover open.
    fireEvent.click(screen.getByTestId('attach-agents-trigger'));
    fireEvent.click(screen.getByTestId('multiselect-option-a1'));
    fireEvent.click(screen.getByTestId('multiselect-option-a2'));
    fireEvent.click(screen.getByTestId('btn-edit-save'));

    await waitFor(() => {
      expect(setAgents).toHaveBeenCalledWith('acme', 'conn-gh', ['a2']);
    });
    // Empty submission keeps the stored token.
    expect(replaceToken).not.toHaveBeenCalled();
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('GitHub updated');
      expect(onSaved).toHaveBeenCalled();
      expect(onClose).toHaveBeenCalled();
    });
  });

  it('replaces the token before saving agents when both changed (D3 ordering)', async () => {
    const replaceToken = vi.spyOn(connectionsApi, 'replaceToken').mockResolvedValue({ connection: connectionRow() });
    const setAgents = vi.spyOn(connectionsApi, 'setAgents').mockResolvedValue({ connection: connectionRow() });

    renderDialog(connectionRow(), [githubRecipe()]);
    await waitFor(() => {
      expect(screen.getByTestId('attach-agents-list')).not.toBeNull();
    });

    fireEvent.change(screen.getByTestId('input-edit-token'), { target: { value: 'new-ghp' } });
    fireEvent.click(screen.getByTestId('btn-edit-save'));

    await waitFor(() => {
      expect(setAgents).toHaveBeenCalled();
    });
    expect(replaceToken).toHaveBeenCalledWith('acme', 'conn-gh', 'new-ghp');
    expect(replaceToken.mock.invocationCallOrder[0]).toBeLessThan(setAgents.mock.invocationCallOrder[0]);
  });

  it('aborts the whole save on a failed token probe — agents untouched, error inline', async () => {
    const replaceToken = vi
      .spyOn(connectionsApi, 'replaceToken')
      .mockRejectedValue(new ApiError(400, 'invalid_request', 'probe failed: Bad credentials'));
    const setAgents = vi.spyOn(connectionsApi, 'setAgents');
    const onClose = vi.fn();

    renderDialog(connectionRow(), [githubRecipe()], { onClose });
    await waitFor(() => {
      expect(screen.getByTestId('attach-agents-list')).not.toBeNull();
    });

    fireEvent.click(screen.getByTestId('attach-agents-trigger'));
    fireEvent.click(screen.getByTestId('multiselect-option-a2'));
    fireEvent.change(screen.getByTestId('input-edit-token'), { target: { value: 'bad-ghp' } });
    fireEvent.click(screen.getByTestId('btn-edit-save'));

    await waitFor(() => {
      expect(screen.getByTestId('edit-error').textContent).toContain('probe failed: Bad credentials');
    });
    expect(replaceToken).toHaveBeenCalledWith('acme', 'conn-gh', 'bad-ghp');
    // All-or-nothing: the failed probe leaves attachment untouched.
    expect(setAgents).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
  });

  it('surfaces a failed agent save after a successful token replace and notes the token changed', async () => {
    vi.spyOn(connectionsApi, 'replaceToken').mockResolvedValue({ connection: connectionRow() });
    vi.spyOn(connectionsApi, 'setAgents').mockRejectedValue(new ApiError(422, 'invalid_request', 'unknown agent'));
    const onClose = vi.fn();
    const onToast = vi.fn();

    renderDialog(connectionRow(), [githubRecipe()], { onClose, onToast });
    await waitFor(() => {
      expect(screen.getByTestId('attach-agents-list')).not.toBeNull();
    });

    // Attach Beacon (a2): one trigger click opens the picker, then tick the
    // row keyed by agent id — toggling keeps the popover open.
    fireEvent.click(screen.getByTestId('attach-agents-trigger'));
    fireEvent.click(screen.getByTestId('multiselect-option-a2'));
    fireEvent.change(screen.getByTestId('input-edit-token'), { target: { value: 'new-ghp' } });
    fireEvent.click(screen.getByTestId('btn-edit-save'));

    await waitFor(() => {
      expect(screen.getByTestId('edit-error').textContent).toContain('unknown agent');
    });
    // The token DID change — the toast says so instead of swallowing it.
    expect(onToast).toHaveBeenCalledWith(expect.stringContaining('token was saved'), 'danger');
    expect(onClose).not.toHaveBeenCalled();
  });

  it('renders Reauthorize instead of a token field for an OAuth-kind connection', async () => {
    const assignMock = stubLocationAssign();
    const authorizeUrl = 'https://auth.atlassian.com/authorize?state=r9';
    const reauthorize = vi.spyOn(connectionsApi, 'reauthorize').mockResolvedValue({ authorize_url: authorizeUrl });
    vi.spyOn(api.agents, 'list').mockResolvedValue({
      agents: [{ id: 'a1', slug: 'atlas', name: 'Atlas', enabled_mcps: ['srv-atl'] }] as any[],
    });

    renderDialog(
      connectionRow({ id: 'conn-atl', service: 'atlassian', server_id: 'srv-atl', token_hint: null }),
      [atlassianRecipe()]
    );
    await waitFor(() => {
      expect(screen.getByTestId('attach-agents-list')).not.toBeNull();
    });
    expect(screen.queryByTestId('input-edit-token')).toBeNull();
    expect(screen.getByTestId('btn-edit-reauthorize')).not.toBeNull();
    // OAuth connections still carry a materialized server — attachment rides
    // its id like any other MCP-kind row. Open the picker and read the
    // attached state off the option row keyed by agent id.
    fireEvent.click(screen.getByTestId('attach-agents-trigger'));
    expect(screen.getByTestId('multiselect-option-a1').getAttribute('aria-selected')).toBe('true');

    fireEvent.click(screen.getByTestId('btn-edit-reauthorize'));
    await waitFor(() => {
      expect(reauthorize).toHaveBeenCalledWith('acme', 'conn-atl');
    });
    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith(authorizeUrl);
    });
  });
});
