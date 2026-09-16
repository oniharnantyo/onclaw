/**
 * @vitest-environment jsdom
 */
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { HooksPane } from './HooksPane';
import { api, ApiError, type ApiHook, type ApiHookExecution } from '../../lib/api';

const mockTenant = { id: 'acme', sub: 'acme', name: 'Acme Corp' };

const hookRow = (overrides: Partial<ApiHook> = {}): ApiHook => ({
  id: 'hook-gate',
  workspace_id: 'acme',
  name: 'Policy Gate',
  event: 'pre_tool_use',
  matcher: 'web.fetch',
  handler_type: 'http',
  config: {
    url: 'https://hooks.example.com/onclaw',
    headers: [{ name: 'Authorization', value: '4321' }],
  },
  timeout_ms: 5000,
  on_failure: 'allow',
  enabled: true,
  position: 0,
  status: 'ok',
  created_at: '2026-09-01T00:00:00Z',
  updated_at: '2026-09-01T00:00:00Z',
  ...overrides,
});

const cheapRow = () =>
  hookRow({
    id: 'hook-cheap',
    name: 'Cheap First',
    event: 'run_started',
    matcher: '',
    position: 1,
  });

const erroredRow = () =>
  hookRow({
    id: 'hook-broken',
    name: 'Broken Command',
    handler_type: 'command',
    config: { command: '/usr/local/bin/gate' },
    status: 'error',
    status_error: 'exit status 1: last delivery failed',
    position: 2,
  });

const instanceRow = (): ApiHook => ({
  id: 'hook-instance',
  key: 'org-policy',
  source: 'managed',
  name: 'Org Mandatory Gate',
  event: 'user_prompt_submit',
  matcher: '*',
  handler_type: 'http',
  config: { url: 'https://example.com/org-gate' },
  timeout_ms: 5000,
  on_failure: 'block',
  enabled: true,
  position: 0,
  status: 'ok',
  created_at: '2026-09-01T00:00:00Z',
  updated_at: '2026-09-01T00:00:00Z',
});

const executionRow = (overrides: Partial<ApiHookExecution> = {}): ApiHookExecution => ({
  id: 'exec-1',
  hook_id: 'hook-gate',
  hook_name: 'Policy Gate',
  hook_level: 'workspace',
  workspace_id: 'acme',
  event: 'pre_tool_use',
  decision: 'block',
  duration_ms: 240,
  detail: 'blocked by policy',
  created_at: '2026-09-08T09:14:00Z',
  ...overrides,
});

describe('screens/settings/HooksPane', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(api.hooks, 'list').mockResolvedValue({ instance: [], hooks: [] });
    vi.spyOn(api.hooks, 'executions').mockResolvedValue({ executions: [] });
  });

  it('renders ordered rows with position numbers, mono chips, level badge, and the order hint', async () => {
    vi.spyOn(api.hooks, 'list').mockResolvedValue({
      instance: [],
      hooks: [hookRow(), cheapRow()],
    });

    render(<HooksPane tenant={mockTenant} canWrite />);

    await waitFor(() => {
      expect(screen.getByTestId('hook-hook-gate')).not.toBeNull();
    });
    // Evaluation order = list order (D14): position numbers follow the list.
    expect(screen.getByTestId('hook-position-hook-gate').textContent).toBe('1');
    expect(screen.getByTestId('hook-position-hook-cheap').textContent).toBe('2');
    // Row content: name, event + handler + matcher chips (mono), level.
    expect(screen.getByText('Policy Gate')).not.toBeNull();
    expect(screen.getByText('pre_tool_use')).not.toBeNull();
    expect(screen.getAllByText('Webhook').length).toBeGreaterThanOrEqual(2);
    // The row summary carries the plain matcher string (D19).
    expect(screen.getByText('web.fetch')).not.toBeNull();
    expect(screen.getAllByText('Workspace').length).toBeGreaterThanOrEqual(2);
    // The blocking seam is marked, the observer is not.
    expect(screen.getByText('can block')).not.toBeNull();
    // The first-block-wins helper line sits near the list.
    expect(screen.getByTestId('hooks-order-hint').textContent).toContain('Top to bottom — first block wins');
    // Healthy status dot label.
    expect(screen.getByTestId('hook-status-hook-gate').textContent).toBe('Healthy');
  });

  it('shows an errored hook in red with its failure detail as the tooltip target', async () => {
    vi.spyOn(api.hooks, 'list').mockResolvedValue({ instance: [], hooks: [erroredRow()] });

    render(<HooksPane tenant={mockTenant} canWrite />);

    await waitFor(() => {
      expect(screen.getByTestId('hook-status-hook-broken').textContent).toBe('Error');
    });
    expect(screen.getByText('exit status 1: last delivery failed')).not.toBeNull();
  });

  it('lists instance hooks read-only with their level badge and no controls', async () => {
    vi.spyOn(api.hooks, 'list').mockResolvedValue({ instance: [instanceRow()], hooks: [hookRow()] });

    render(<HooksPane tenant={mockTenant} canWrite />);

    await waitFor(() => {
      expect(screen.getByTestId('hook-instance-hook-instance')).not.toBeNull();
    });
    expect(screen.getByText('Org Mandatory Gate')).not.toBeNull();
    // The read-only section header and the row's level badge both mark it.
    expect(screen.getByTestId('hook-instance-hook-instance').textContent).toContain('Instance');
    // The row matcher renders as the plain string — "*" = every occurrence
    // (the gallery's match-all reading).
    expect(screen.getByTestId('hook-instance-hook-instance').textContent).toContain('*');
    // Mandatory visibility, no control from below (D13): the instance row
    // carries neither a toggle nor edit/delete affordances.
    expect(screen.queryByRole('switch', { name: 'Enable Org Mandatory Gate' })).toBeNull();
    expect(screen.queryByTestId('btn-edit-hook-instance')).toBeNull();
    expect(screen.queryByTestId('hook-delete-hook-instance')).toBeNull();
    // Workspace controls still exist.
    expect(screen.getByRole('switch', { name: 'Enable Policy Gate' })).not.toBeNull();
  });

  it('toggles a hook via PATCH and toasts the outcome', async () => {
    vi.spyOn(api.hooks, 'list').mockResolvedValue({ instance: [], hooks: [hookRow()] });
    const update = vi.spyOn(api.hooks, 'update').mockResolvedValue({
      hook: hookRow({ enabled: false }),
      match_count: { matched: 1, of: 24 },
    });
    const onToast = vi.fn();

    render(<HooksPane tenant={mockTenant} onToast={onToast} canWrite />);

    await waitFor(() => {
      expect(screen.getByRole('switch', { name: 'Enable Policy Gate' })).not.toBeNull();
    });
    fireEvent.click(screen.getByRole('switch', { name: 'Enable Policy Gate' }));

    await waitFor(() => {
      expect(update).toHaveBeenCalledWith('acme', 'hook-gate', { enabled: false });
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('Policy Gate disabled — it no longer fires');
    });
    expect(screen.getByTestId('hook-status-hook-gate').textContent).toBe('Disabled');
  });

  it('deletes a hook through confirmation and keeps the audit trail note', async () => {
    vi.spyOn(api.hooks, 'list').mockResolvedValue({ instance: [], hooks: [hookRow()] });
    const del = vi.spyOn(api.hooks, 'delete').mockResolvedValue(undefined);
    const onToast = vi.fn();

    render(<HooksPane tenant={mockTenant} onToast={onToast} canWrite />);

    await waitFor(() => {
      expect(screen.getByTestId('hook-delete-hook-gate')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('hook-delete-hook-gate'));
    expect(screen.getByTestId('modal-hook-delete').textContent).toContain('execution history is preserved');
    expect(del).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId('btn-hook-delete-confirm'));
    await waitFor(() => {
      expect(del).toHaveBeenCalledWith('acme', 'hook-gate');
    });
    await waitFor(() => {
      expect(screen.queryByTestId('hook-hook-gate')).toBeNull();
    });
  });

  it('reorders by drag and persists the new execution order', async () => {
    vi.spyOn(api.hooks, 'list').mockResolvedValue({ instance: [], hooks: [hookRow(), cheapRow()] });
    const reorder = vi.spyOn(api.hooks, 'reorder').mockResolvedValue(undefined);

    render(<HooksPane tenant={mockTenant} canWrite />);

    await waitFor(() => {
      expect(screen.getByTestId('hook-hook-cheap')).not.toBeNull();
    });

    // Drag row 2 (Cheap First) onto row 1 (Policy Gate).
    const target = screen.getByTestId('hook-hook-gate');
    fireEvent.dragStart(screen.getByTestId('hook-hook-cheap'));
    fireEvent.dragOver(target);
    fireEvent.drop(target);

    await waitFor(() => {
      expect(reorder).toHaveBeenCalledWith('acme', ['hook-cheap', 'hook-gate']);
    });
    await waitFor(() => {
      expect(screen.getByTestId('hook-position-hook-cheap').textContent).toBe('1');
      expect(screen.getByTestId('hook-position-hook-gate').textContent).toBe('2');
    });
  });

  it('expands a row to its per-hook execution history', async () => {
    vi.spyOn(api.hooks, 'list').mockResolvedValue({ instance: [], hooks: [hookRow()] });
    const history = vi.spyOn(api.hooks, 'hookExecutions').mockResolvedValue({
      executions: [
        executionRow(),
        executionRow({ id: 'exec-2', decision: 'allow', duration_ms: 1800, detail: '' }),
      ],
    });

    render(<HooksPane tenant={mockTenant} canWrite />);

    await waitFor(() => {
      expect(screen.getByTestId('hook-hook-gate')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('hook-expand-hook-gate'));

    await waitFor(() => {
      expect(history).toHaveBeenCalledWith('acme', 'hook-gate', 50);
    });
    // Decision, event, duration, detail, and time all render per record.
    expect(screen.getByTestId('hook-exec-exec-1')).not.toBeNull();
    expect(screen.getByTestId('hook-exec-exec-2')).not.toBeNull();
    expect(screen.getAllByTestId('hook-decision-block').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('blocked by policy')).not.toBeNull();
  });

  it('surfaces the workspace-wide recent-executions audit trail', async () => {
    vi.spyOn(api.hooks, 'list').mockResolvedValue({ instance: [], hooks: [] });
    vi.spyOn(api.hooks, 'executions').mockResolvedValue({
      executions: [executionRow({ hook_name: 'Deleted Gate', hook_id: null })],
    });

    render(<HooksPane tenant={mockTenant} canWrite />);

    await waitFor(() => {
      expect(screen.getByTestId('hooks-recent-executions')).not.toBeNull();
    });
    // Records of hooks that have since been deleted still surface (D16).
    expect(screen.getByText('Deleted Gate')).not.toBeNull();
    expect(screen.getByTestId('hook-decision-block')).not.toBeNull();
  });

  it('creates a hook through the dialog with the plain matcher and live count', async () => {
    vi.spyOn(api.hooks, 'list').mockResolvedValue({ instance: [], hooks: [] });
    vi.spyOn(api.tools, 'list').mockResolvedValue({
      tools: [
        { key: 'web.fetch', display_name: 'Web Fetch', description: '', group: 'web', icon_key: 'link', configurable: false, enabled: true, configured: false, config: {}, toggleable: true },
        { key: 'execute', display_name: 'Shell', description: '', group: 'core', icon_key: 'terminal', configurable: false, enabled: true, configured: false, config: {}, toggleable: true },
      ],
    });
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [] });
    vi.spyOn(api.providers, 'list').mockResolvedValue({ providers: [] });
    const create = vi.spyOn(api.hooks, 'create').mockResolvedValue({
      hook: hookRow({ id: 'hook-new', name: 'Gate', position: 0 }),
      match_count: { matched: 1, of: 24 },
    });
    const onToast = vi.fn();

    render(<HooksPane tenant={mockTenant} onToast={onToast} canWrite />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-hook-empty-add')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-hook-empty-add'));
    await waitFor(() => {
      expect(screen.getByTestId('modal-hook')).not.toBeNull();
    });

    // The matcher section is ONE text input — no segmented picker, no mode
    // dropdown, no chips (D19).
    expect(screen.queryByTestId('btn-hook-matcher-picker')).toBeNull();
    expect(screen.queryByTestId('btn-hook-matcher-regex')).toBeNull();
    expect(screen.queryByTestId('select-hook-mode')).toBeNull();

    fireEvent.change(screen.getByTestId('input-hook-name'), { target: { value: 'Gate' } });

    // The event select carries all five events; switch to pre_tool_use stays.
    const eventSelect = screen.getByTestId('select-hook-event');
    fireEvent.change(eventSelect, { target: { value: 'pre_tool_use' } });
    expect(screen.getByTestId('hook-event-helper').textContent).toContain('can block');

    // The optional `if` gate renders on tool events only (D20), with the
    // gallery's helper copy and placeholder.
    const ifInput = screen.getByTestId('input-hook-if') as HTMLInputElement;
    expect(ifInput.placeholder).toBe('read_file(secret*)');
    expect(screen.getByTestId('hook-if-helper').textContent).toContain('Format: Tool(pattern)');
    fireEvent.change(eventSelect, { target: { value: 'run_finished' } });
    expect(screen.queryByTestId('input-hook-if')).toBeNull();
    fireEvent.change(eventSelect, { target: { value: 'pre_tool_use' } });
    fireEvent.change(screen.getByTestId('input-hook-if'), { target: { value: 'read_file(secret*)' } });

    // Typing the matcher updates the live count over the visible candidates:
    // the 12 built-in/catalog tools + the browser family = 13, and the
    // "web.*" family hits web.search + web.fetch (D19).
    const matcher = screen.getByTestId('input-hook-matcher');
    fireEvent.change(matcher, { target: { value: 'web.*' } });
    await waitFor(() => {
      expect(screen.getByTestId('hook-match-count').textContent).toBe('Matches 2 of 19 tools');
    });
    // The static syntax helper sits under the input.
    expect(screen.getByTestId('hook-matcher-helper').textContent).toContain('Empty or * = every occurrence');

    // Switch handler to http — fill the URL and save.
    fireEvent.change(screen.getByTestId('select-hook-handler'), { target: { value: 'http' } });
    fireEvent.change(screen.getByTestId('input-hook-url'), { target: { value: 'https://hooks.example.com/onclaw' } });
    fireEvent.click(screen.getByTestId('btn-hook-save'));

    await waitFor(() => {
      expect(create).toHaveBeenCalledWith('acme', {
        name: 'Gate',
        event: 'pre_tool_use',
        matcher: 'web.*',
        if: 'read_file(secret*)',
        handler_type: 'http',
        config: { url: 'https://hooks.example.com/onclaw' },
        timeout_ms: 5000,
        on_failure: 'allow',
        enabled: true,
      });
    });
    // Done closes and the row lands in the list.
    fireEvent.click(screen.getByTestId('btn-hook-done'));
    await waitFor(() => {
      expect(screen.queryByTestId('modal-hook')).toBeNull();
      expect(screen.getByTestId('hook-hook-new')).not.toBeNull();
    });
    expect(onToast).toHaveBeenCalledWith('Gate added');
  });

  it('blocks a prompt-evaluator save client-side while the matcher is match-all', async () => {
    vi.spyOn(api.hooks, 'list').mockResolvedValue({ instance: [], hooks: [] });
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools: [] });
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [] });
    vi.spyOn(api.providers, 'list').mockResolvedValue({
      providers: [{ id: 'prov-a', workspace_id: 'acme', type: 'anthropic', name: 'Anthropic', key_set: true, key_hint: '', enabled: true, created_at: '', updated_at: '' }],
    });
    const create = vi.spyOn(api.hooks, 'create');

    render(<HooksPane tenant={mockTenant} canWrite />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-hook-empty-add')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-hook-empty-add'));
    await waitFor(() => {
      expect(screen.getByTestId('modal-hook')).not.toBeNull();
    });

    fireEvent.change(screen.getByTestId('input-hook-name'), { target: { value: 'Eval' } });
    fireEvent.change(screen.getByTestId('select-hook-event'), { target: { value: 'run_finished' } });
    fireEvent.change(screen.getByTestId('select-hook-handler'), { target: { value: 'prompt' } });
    fireEvent.change(screen.getByTestId('select-hook-provider'), { target: { value: 'prov-a' } });
    fireEvent.change(screen.getByTestId('input-hook-model'), { target: { value: 'claude-sonnet-5' } });
    fireEvent.change(screen.getByTestId('input-hook-prompt'), { target: { value: 'Block failures on prod agents.' } });
    fireEvent.click(screen.getByTestId('btn-hook-save'));

    expect(create).not.toHaveBeenCalled();
    expect(screen.getByTestId('modal-hook').textContent).toContain('Prompt evaluators require a matcher');
  });

  it('sends regex-tier matcher strings through the same single input', async () => {
    vi.spyOn(api.hooks, 'list').mockResolvedValue({ instance: [], hooks: [] });
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools: [] });
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [] });
    vi.spyOn(api.providers, 'list').mockResolvedValue({ providers: [] });
    const create = vi.spyOn(api.hooks, 'create').mockResolvedValue({
      hook: hookRow({ id: 'hook-regex', name: 'Regex Gate', matcher: '^web\\.' }),
      match_count: null,
    });

    render(<HooksPane tenant={mockTenant} canWrite />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-hook-empty-add')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-hook-empty-add'));
    await waitFor(() => {
      expect(screen.getByTestId('modal-hook')).not.toBeNull();
    });

    fireEvent.change(screen.getByTestId('input-hook-name'), { target: { value: 'Regex Gate' } });
    // No mode switch — the regex tier is just another string in the input.
    fireEvent.change(screen.getByTestId('input-hook-matcher'), { target: { value: '^web\\.' } });
    // Switch to http handler and fill its URL.
    fireEvent.change(screen.getByTestId('select-hook-handler'), { target: { value: 'http' } });
    fireEvent.change(screen.getByTestId('input-hook-url'), { target: { value: 'https://hooks.example.com/regex' } });
    fireEvent.click(screen.getByTestId('btn-hook-save'));

    await waitFor(() => {
      expect(create).toHaveBeenCalledWith('acme', expect.objectContaining({
        name: 'Regex Gate',
        matcher: '^web\\.',
      }));
    });
  });

  it('rejects an invalid regex-tier matcher client-side', async () => {
    vi.spyOn(api.hooks, 'list').mockResolvedValue({ instance: [], hooks: [] });
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools: [] });
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [] });
    vi.spyOn(api.providers, 'list').mockResolvedValue({ providers: [] });
    const create = vi.spyOn(api.hooks, 'create');

    render(<HooksPane tenant={mockTenant} canWrite />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-hook-empty-add')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-hook-empty-add'));
    await waitFor(() => {
      expect(screen.getByTestId('modal-hook')).not.toBeNull();
    });

    fireEvent.change(screen.getByTestId('input-hook-name'), { target: { value: 'Broken' } });
    fireEvent.change(screen.getByTestId('select-hook-handler'), { target: { value: 'http' } });
    fireEvent.change(screen.getByTestId('input-hook-matcher'), { target: { value: '(' } });
    fireEvent.change(screen.getByTestId('input-hook-url'), { target: { value: 'https://hooks.example.com/x' } });
    fireEvent.click(screen.getByTestId('btn-hook-save'));

    expect(create).not.toHaveBeenCalled();
    expect(screen.getByTestId('hook-matcher-error').textContent).toContain('not a valid regular expression');
  });

  it('edits a hook keeping the stored secret when the header value is left empty', async () => {
    vi.spyOn(api.hooks, 'list').mockResolvedValue({ instance: [], hooks: [hookRow()] });
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools: [] });
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [] });
    vi.spyOn(api.providers, 'list').mockResolvedValue({ providers: [] });
    const update = vi.spyOn(api.hooks, 'update').mockResolvedValue({
      hook: hookRow({ name: 'Policy Gate II' }),
      match_count: { matched: 1, of: 24 },
    });

    render(<HooksPane tenant={mockTenant} canWrite />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-edit-hook-gate')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-edit-hook-gate'));
    await waitFor(() => {
      expect(screen.getByTestId('modal-hook')).not.toBeNull();
    });

    // Write-only: the stored header value is never echoed, only hinted.
    const valueInput = screen.getByTestId('input-hook-header-value') as HTMLInputElement;
    expect(valueInput.type).toBe('password');
    expect(valueInput.value).toBe('');
    expect(valueInput.placeholder).toContain('4321');

    fireEvent.change(screen.getByTestId('input-hook-name'), { target: { value: 'Policy Gate II' } });
    fireEvent.click(screen.getByTestId('btn-hook-save'));

    await waitFor(() => {
      expect(update).toHaveBeenCalledWith('acme', 'hook-gate', expect.objectContaining({
        name: 'Policy Gate II',
        config: {
          url: 'https://hooks.example.com/onclaw',
          // Empty value omitted — the stored secret is kept (keep-stored merge).
          headers: [{ name: 'Authorization' }],
        },
      }));
    });
  });

  it('runs the dry-run test panel and renders the decision badge and handler detail', async () => {
    vi.spyOn(api.hooks, 'list').mockResolvedValue({ instance: [], hooks: [hookRow()] });
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools: [] });
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [] });
    vi.spyOn(api.providers, 'list').mockResolvedValue({ providers: [] });
    const test = vi.spyOn(api.hooks, 'test').mockResolvedValue({
      decision: 'block',
      reason: 'not allowed in prod',
      duration_ms: 312,
      detail: { exit_code: 2 },
    });

    render(<HooksPane tenant={mockTenant} canWrite />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-edit-hook-gate')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-edit-hook-gate'));
    await waitFor(() => {
      expect(screen.getByTestId('modal-hook')).not.toBeNull();
    });

    fireEvent.click(screen.getByTestId('btn-hook-test-toggle'));
    // The "real handler, nothing recorded" note is stated.
    expect(screen.getByTestId('hook-test-note').textContent).toContain('Executes the real handler');
    // Payload preview is read-only display.
    expect(screen.getByTestId('hook-test-preview').textContent).toContain('pre_tool_use');

    // Synthetic overrides: event stays pre_tool_use; set the tool name.
    fireEvent.change(screen.getByTestId('select-hook-test-event'), { target: { value: 'pre_tool_use' } });
    fireEvent.change(screen.getByTestId('input-hook-test-tool'), { target: { value: 'execute' } });
    fireEvent.click(screen.getByTestId('btn-hook-test-run'));

    await waitFor(() => {
      expect(test).toHaveBeenCalledWith('acme', expect.objectContaining({
        id: 'hook-gate',
        event: 'pre_tool_use',
        overrides: { tool_name: 'execute' },
      }));
    });
    await waitFor(() => {
      expect(screen.getByTestId('hook-test-decision').textContent).toBe('block');
    });
    expect(screen.getByTestId('hook-test-result').textContent).toContain('312 ms');
    expect(screen.getByTestId('hook-test-exit-code').textContent).toBe('exit 2');
    expect(screen.getByTestId('hook-test-result').textContent).toContain('not allowed in prod');
  });

  it('shows the load error state with a retry action', async () => {
    vi.spyOn(api.hooks, 'list').mockRejectedValue(new ApiError(500, 'error', 'database unreachable'));

    render(<HooksPane tenant={mockTenant} canWrite />);

    await waitFor(() => {
      expect(screen.getByText("Couldn't load hooks")).not.toBeNull();
    });
    expect(screen.getByText('database unreachable')).not.toBeNull();
  });

  it('hides every write control without hooks.write but keeps reads', async () => {
    vi.spyOn(api.hooks, 'list').mockResolvedValue({ instance: [instanceRow()], hooks: [hookRow()] });

    render(<HooksPane tenant={mockTenant} canWrite={false} />);

    await waitFor(() => {
      expect(screen.getByTestId('hook-hook-gate')).not.toBeNull();
    });
    expect(screen.queryByTestId('btn-hook-add')).toBeNull();
    expect(screen.queryByTestId('btn-hook-empty-add')).toBeNull();
    expect(screen.queryByRole('switch', { name: 'Enable Policy Gate' })).toBeNull();
    expect(screen.queryByTestId('btn-edit-hook-gate')).toBeNull();
    expect(screen.queryByTestId('hook-delete-hook-gate')).toBeNull();
    expect(screen.queryByTestId('hook-handle-hook-gate')).toBeNull();
    // Rows, status, and the expandable history still render.
    expect(screen.getByTestId('hook-status-hook-gate').textContent).toBe('Healthy');
    expect(screen.getByTestId('hook-instance-hook-instance')).not.toBeNull();
    fireEvent.click(screen.getByTestId('hook-expand-hook-gate'));
    expect(screen.getByText('Execution history')).not.toBeNull();
  });
});
