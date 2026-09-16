import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { ToolsPane } from './ToolsPane';
import { api, ApiError, type ApiToolSettings } from '../../lib/api';

const mockTenant = { id: 'acme', sub: 'acme', name: 'Acme Corp' };

const catalogTools = (): ApiToolSettings[] => [
  {
    key: 'ls',
    display_name: 'List Files',
    description: 'List directory contents in the agent workspace.',
    group: 'filesystem',
    icon_key: 'folder',
    configurable: false,
    enabled: true,
    configured: false,
    config: {},
    toggleable: true,
  },
  {
    key: 'web.search',
    display_name: 'Web Search',
    description: 'Search the web and return top results.',
    group: 'web',
    icon_key: 'search',
    configurable: true,
    enabled: false,
    configured: false,
    config: {},
    toggleable: true,
  },
  {
    key: 'browser',
    display_name: 'Browser',
    description: 'Drive a real browser.',
    group: 'browser',
    icon_key: 'globe',
    configurable: true,
    enabled: true,
    configured: true,
    config: { headless: true, max_pages: 3, api_key: { hint: '7f3a' } },
    config_schema: [
      { key: 'headless', label: 'Headless', type: 'boolean', required: false },
      { key: 'max_pages', label: 'Max pages', type: 'number', required: false },
      { key: 'api_key', label: 'API key', type: 'secret', required: false },
    ],
    toggleable: true,
  },
  {
    key: 'channel.post',
    display_name: 'Channel Post',
    description: 'Post a message into the channel the agent is running in.',
    group: 'channel',
    icon_key: 'message',
    configurable: false,
    enabled: true,
    configured: false,
    config: {},
    toggleable: false,
  },
  {
    key: 'channel.history',
    display_name: 'Channel History',
    description: "Page back through the channel's earlier messages.",
    group: 'channel',
    icon_key: 'history',
    configurable: false,
    enabled: true,
    configured: false,
    config: {},
    toggleable: false,
  },
  {
    key: 'session.close',
    display_name: 'Close Work Session',
    description: "Close the channel's active work session with a stored summary.",
    group: 'channel',
    icon_key: 'check-circle',
    configurable: false,
    enabled: true,
    configured: false,
    config: {},
    toggleable: false,
  },
];

// Ordered stack fixture: rows 1-3 in rotation, row 4 standby.
const stackConfig = (): Record<string, unknown> => ({
  entries: [
    { id: 'e1', name: 'Tavily 1', provider: 'tavily', api_key_hint: 'ab12' },
    { id: 'e2', name: 'Tavily 2', provider: 'tavily', api_key_hint: '9f04' },
    { id: 'e3', name: 'Exa 1', provider: 'exa', api_key_hint: 'c7e1' },
    { id: 'e4', name: 'Brave 1', provider: 'brave', api_key_hint: '41d0' },
  ],
  request_timeout_seconds: 10,
});

const nameValue = (row: HTMLElement): string =>
  (within(row).getByLabelText('Name') as HTMLInputElement).value;

async function openDialog(toolKey: string) {
  await waitFor(() => {
    expect(screen.getByTestId('btn-tool-config-' + toolKey)).not.toBeNull();
  });
  fireEvent.click(screen.getByTestId('btn-tool-config-' + toolKey));
  await waitFor(() => {
    expect(screen.getByTestId('tool-config-dialog')).not.toBeNull();
  });
}

describe('screens/settings/ToolsPane', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders the flat tool list with toggles from the catalog', async () => {
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools: catalogTools() });

    render(<ToolsPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('tool-web.search')).not.toBeNull();
    });
    expect(screen.getByText('List Files')).not.toBeNull();
    expect(screen.getByText('Web Search')).not.toBeNull();

    // Non-configurable tools have no gear button.
    expect(screen.queryByTestId('btn-tool-config-ls')).toBeNull();
    expect(screen.getByTestId('btn-tool-config-web.search')).not.toBeNull();
  });

  it('renders the Channel Tool section with always-on badges and no toggle or gear', async () => {
    const tools = catalogTools();
    // Even a configurable non-toggleable entry must render no gear.
    tools[3].configurable = true;
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools });

    render(<ToolsPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('tool-channel.post')).not.toBeNull();
    });

    // Exactly the non-toggleable payload entries, under the section header.
    const section = screen.getByTestId('channel-tool-section');
    expect(within(section).getByText('Channel Tool')).not.toBeNull();
    expect(within(section).getByTestId('tool-channel.post')).not.toBeNull();
    expect(within(section).getByTestId('tool-channel.history')).not.toBeNull();
    expect(within(section).getByTestId('tool-session.close')).not.toBeNull();
    expect(within(section).queryByTestId('tool-ls')).toBeNull();
    expect(within(section).queryByTestId('tool-web.search')).toBeNull();

    // Badge copy: the facilitator-only special case for session.close.
    expect(within(section).getByTestId('tool-always-on-badge-channel.post').textContent).toBe('Always on · channel runs');
    expect(within(section).getByTestId('tool-always-on-badge-channel.history').textContent).toBe('Always on · channel runs');
    expect(within(section).getByTestId('tool-always-on-badge-session.close').textContent).toBe('Always on · facilitator only');

    // Badge rows carry neither a toggle nor a gear — always.
    for (const key of ['channel.post', 'channel.history', 'session.close']) {
      const row = screen.getByTestId('tool-' + key);
      expect(within(row).queryByRole('switch')).toBeNull();
      expect(within(row).queryByTestId('btn-tool-config-' + key)).toBeNull();
    }
  });

  it('renders the always-on badge even when the payload reports a stale disabled row', async () => {
    const tools = catalogTools();
    tools[4].enabled = false; // channel.history — stale row from before the flag
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools });

    render(<ToolsPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('tool-channel.history')).not.toBeNull();
    });

    // Membership and badge come from toggleable, not enabled.
    expect(screen.getByTestId('channel-tool-section')).not.toBeNull();
    expect(screen.getByTestId('tool-always-on-badge-channel.history')).not.toBeNull();
    // Badge rows never dim on enabled state.
    expect(screen.getByTestId('tool-channel.history').className).not.toContain('opacity-70');
  });

  it('renders the flat list below the section from the toggleable entries only', async () => {
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools: catalogTools() });

    render(<ToolsPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('tool-ls')).not.toBeNull();
    });

    // Toggleable rows keep their toggles and carry no badge.
    const lsRow = screen.getByTestId('tool-ls');
    expect(within(lsRow).getByRole('switch', { name: 'Enable List Files' })).not.toBeNull();
    expect(within(lsRow).queryByTestId('tool-always-on-badge-ls')).toBeNull();

    // Every toggleable row renders outside the Channel Tool section.
    const section = screen.getByTestId('channel-tool-section');
    for (const key of ['ls', 'web.search', 'browser']) {
      expect(within(section).queryByTestId('tool-' + key)).toBeNull();
    }
  });

  it('toggle drives PATCH and reports failures as toasts', async () => {
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools: catalogTools() });
    const update = vi.spyOn(api.tools, 'update').mockResolvedValue({
      tool: { ...catalogTools()[0], enabled: false },
    });
    const onToast = vi.fn();

    render(<ToolsPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('tool-ls')).not.toBeNull();
    });
    fireEvent.click(screen.getByRole('switch', { name: 'Enable List Files' }));

    await waitFor(() => {
      expect(update).toHaveBeenCalledWith('acme', 'ls', { enabled: false });
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('Disabled List Files');
    });
  });

  it('shows the provider-stack summary line when entries exist', async () => {
    const tools = catalogTools();
    tools[1].config = stackConfig();
    tools[1].configured = true;
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools });

    render(<ToolsPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('tool-web.search')).not.toBeNull();
    });
    expect(screen.getByTestId('web-search-summary').textContent).toBe('4 configured · 3 stacked');
  });

  it('renders the generic config dialog per schema, never echoing secrets', async () => {
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools: catalogTools() });

    render(<ToolsPane tenant={mockTenant} />);

    await openDialog('browser');

    // One labeled input per schema field, rendered generically.
    expect(screen.getByTestId('tool-config-field-headless')).not.toBeNull();
    expect(screen.getByTestId('tool-config-field-max_pages')).not.toBeNull();
    expect(screen.getByTestId('tool-config-field-api_key')).not.toBeNull();

    // The secret input is a password field; no stored value is echoed.
    const secretInput = screen.getByLabelText(/API key/i) as HTMLInputElement;
    expect(secretInput.type).toBe('password');
    expect(secretInput.value).toBe('');
  });

  it('surfaces 422 validation details as inline field errors', async () => {
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools: catalogTools() });
    vi.spyOn(api.tools, 'update').mockRejectedValue(
      new ApiError(422, 'invalid_request', 'max_pages must be at least 1', [
        { field: 'max_pages', message: 'max_pages must be at least 1' },
      ])
    );

    render(<ToolsPane tenant={mockTenant} />);

    await openDialog('browser');
    fireEvent.click(screen.getByTestId('btn-tool-config-save'));

    await waitFor(() => {
      expect(screen.getByTestId('tool-config-field-error-max_pages')).not.toBeNull();
    });
    expect(screen.getByTestId('tool-config-field-error-max_pages').textContent).toContain('at least 1');
  });

  it('renders show_if fields only while the controlling field matches', async () => {
    const tools = catalogTools();
    tools[2].config_schema!.push({
      key: 'cdp_url',
      label: 'Remote CDP URL',
      type: 'text',
      required: false,
      help: 'A set CDP URL makes headless irrelevant.',
      show_if: { field: 'headless', equals: 'false' },
    });
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools });
    const update = vi.spyOn(api.tools, 'update').mockResolvedValue({ tool: tools[2] });

    render(<ToolsPane tenant={mockTenant} />);

    await openDialog('browser');

    // No value yet — the conditional field is hidden.
    expect(screen.queryByTestId('tool-config-field-cdp_url')).toBeNull();

    fireEvent.change(screen.getByLabelText(/Headless/i), { target: { value: 'false' } });
    expect(screen.getByTestId('tool-config-field-cdp_url')).not.toBeNull();

    // A value typed while visible is dropped once the field hides again.
    fireEvent.change(screen.getByLabelText(/Remote CDP URL/i), { target: { value: 'http://127.0.0.1:9222' } });
    fireEvent.change(screen.getByLabelText(/Headless/i), { target: { value: 'true' } });
    expect(screen.queryByTestId('tool-config-field-cdp_url')).toBeNull();

    fireEvent.click(screen.getByTestId('btn-tool-config-save'));
    await waitFor(() => {
      expect(update).toHaveBeenCalledWith('acme', 'browser', { config: { headless: 'true' } });
    });
  });

  it('opens the provider-stack editor with rotation labels, hint placeholders, and no DuckDuckGo', async () => {
    const tools = catalogTools();
    tools[1].config = stackConfig();
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools });

    render(<ToolsPane tenant={mockTenant} />);

    await openDialog('web.search');

    const rows = screen.getAllByTestId('web-search-row');
    expect(rows).toHaveLength(4);
    expect(within(rows[0]).getByTestId('web-search-rotation').textContent).toBe('in rotation');
    expect(within(rows[2]).getByTestId('web-search-rotation').textContent).toBe('in rotation');
    expect(within(rows[3]).getByTestId('web-search-rotation').textContent).toBe('(standby)');
    expect(rows[3].className).toContain('opacity-60');

    // The stored secret is write-only: password input, hint as placeholder.
    const keyInput = within(rows[0]).getByLabelText('API key') as HTMLInputElement;
    expect(keyInput.type).toBe('password');
    expect(keyInput.placeholder).toBe('•••• ab12');
    expect(keyInput.value).toBe('');

    // DuckDuckGo is deleted from the registry options.
    expect(screen.queryByRole('option', { name: 'DuckDuckGo' })).toBeNull();
  });

  it('re-evaluates in-rotation/standby labels immediately on reorder', async () => {
    const tools = catalogTools();
    tools[1].config = stackConfig();
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools });
    const update = vi.spyOn(api.tools, 'update').mockResolvedValue({ tool: tools[1] });

    render(<ToolsPane tenant={mockTenant} />);

    await openDialog('web.search');

    // Bounds: first row cannot move up, last cannot move down.
    let rows = screen.getAllByTestId('web-search-row');
    expect(within(rows[0]).getByRole('button', { name: 'Move up' }).hasAttribute('disabled')).toBe(true);
    expect(within(rows[3]).getByRole('button', { name: 'Move down' }).hasAttribute('disabled')).toBe(true);

    // Promote the standby row: Brave 1 enters the window, Exa 1 drops out.
    fireEvent.click(within(rows[3]).getByRole('button', { name: 'Move up' }));

    rows = screen.getAllByTestId('web-search-row');
    expect(nameValue(rows[2])).toBe('Brave 1');
    expect(within(rows[2]).getByTestId('web-search-rotation').textContent).toBe('in rotation');
    expect(nameValue(rows[3])).toBe('Exa 1');
    expect(within(rows[3]).getByTestId('web-search-rotation').textContent).toBe('(standby)');

    // Reordering is local until Save.
    expect(update).not.toHaveBeenCalled();
  });

  it('submits the ordered list; known ids with empty keys keep stored secrets', async () => {
    const tools = catalogTools();
    tools[1].config = {
      entries: [
        { id: 'e1', name: 'Tavily 1', provider: 'tavily', api_key_hint: 'ab12' },
        { id: 'e2', name: 'Exa 1', provider: 'exa', api_key_hint: 'c7e1' },
      ],
      request_timeout_seconds: 15,
    };
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools });
    const update = vi.spyOn(api.tools, 'update').mockResolvedValue({ tool: tools[1] });

    render(<ToolsPane tenant={mockTenant} />);

    await openDialog('web.search');

    // Append a brand-new row (no id) with a typed key.
    fireEvent.click(screen.getByTestId('btn-web-search-add'));
    const rows = screen.getAllByTestId('web-search-row');
    expect(rows).toHaveLength(3);
    fireEvent.change(within(rows[2]).getByLabelText('Name'), { target: { value: 'Brave 1' } });
    fireEvent.change(within(rows[2]).getByLabelText('Provider'), { target: { value: 'brave' } });
    fireEvent.change(within(rows[2]).getByLabelText('API key'), { target: { value: 'brk_123' } });

    fireEvent.click(screen.getByTestId('btn-tool-config-save'));

    await waitFor(() => {
      expect(update).toHaveBeenCalledWith('acme', 'web.search', {
        config: {
          entries: [
            // Known ids with no typed key submit without api_key — keep stored.
            { id: 'e1', name: 'Tavily 1', provider: 'tavily' },
            { id: 'e2', name: 'Exa 1', provider: 'exa' },
            { name: 'Brave 1', provider: 'brave', api_key: 'brk_123' },
          ],
          request_timeout_seconds: 15,
        },
      });
    });
  });

  it('blocks Save with an inline error when a new row has no name', async () => {
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools: catalogTools() });
    const update = vi.spyOn(api.tools, 'update').mockResolvedValue({ tool: catalogTools()[1] });

    render(<ToolsPane tenant={mockTenant} />);

    await openDialog('web.search');
    expect(screen.getByTestId('web-search-empty').textContent).toBe(
      'No providers configured — web.search will error until you add one.'
    );

    fireEvent.click(screen.getByTestId('btn-web-search-add'));
    const row = screen.getAllByTestId('web-search-row')[0];
    fireEvent.change(within(row).getByLabelText('Provider'), { target: { value: 'tavily' } });
    fireEvent.change(within(row).getByLabelText('API key'), { target: { value: 'tvly_key' } });
    fireEvent.click(screen.getByTestId('btn-tool-config-save'));

    expect(screen.getByTestId('web-search-row-error').textContent).toContain('Name is required');
    expect(update).not.toHaveBeenCalled();
    expect(screen.getByTestId('tool-config-dialog')).not.toBeNull();
  });

  it('blocks Save when a new row is missing the credential its provider requires', async () => {
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools: catalogTools() });
    const update = vi.spyOn(api.tools, 'update').mockResolvedValue({ tool: catalogTools()[1] });

    render(<ToolsPane tenant={mockTenant} />);

    await openDialog('web.search');
    fireEvent.click(screen.getByTestId('btn-web-search-add'));
    const row = screen.getAllByTestId('web-search-row')[0];
    fireEvent.change(within(row).getByLabelText('Name'), { target: { value: 'Exa 2' } });
    fireEvent.change(within(row).getByLabelText('Provider'), { target: { value: 'exa' } });
    fireEvent.click(screen.getByTestId('btn-tool-config-save'));

    expect(screen.getByTestId('web-search-row-error').textContent).toContain('API key is required for exa');
    expect(update).not.toHaveBeenCalled();
  });

  it('blocks Save on duplicate names case-insensitively', async () => {
    const tools = catalogTools();
    tools[1].config = { entries: [{ id: 'e1', name: 'Tavily 1', provider: 'tavily', api_key_hint: 'ab12' }] };
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools });
    const update = vi.spyOn(api.tools, 'update').mockResolvedValue({ tool: tools[1] });

    render(<ToolsPane tenant={mockTenant} />);

    await openDialog('web.search');
    fireEvent.click(screen.getByTestId('btn-web-search-add'));
    const rows = screen.getAllByTestId('web-search-row');
    fireEvent.change(within(rows[1]).getByLabelText('Name'), { target: { value: 'tavily 1' } });
    fireEvent.change(within(rows[1]).getByLabelText('Provider'), { target: { value: 'tavily' } });
    fireEvent.change(within(rows[1]).getByLabelText('API key'), { target: { value: 'k' } });
    fireEvent.click(screen.getByTestId('btn-tool-config-save'));

    const errs = screen.getAllByTestId('web-search-row-error');
    expect(errs).toHaveLength(2);
    expect(errs[0].textContent).toContain('Name must be unique');
    expect(errs[1].textContent).toContain('Name must be unique');
    expect(update).not.toHaveBeenCalled();
    expect(screen.getByTestId('tool-config-dialog')).not.toBeNull();
  });

  it('removes optimistically and restores the row on Undo without persisting', async () => {
    const tools = catalogTools();
    tools[1].config = {
      entries: [
        { id: 'e1', name: 'Tavily 1', provider: 'tavily', api_key_hint: 'ab12' },
        { id: 'e2', name: 'Exa 1', provider: 'exa', api_key_hint: 'c7e1' },
      ],
    };
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools });
    const update = vi.spyOn(api.tools, 'update').mockResolvedValue({ tool: tools[1] });

    render(<ToolsPane tenant={mockTenant} />);

    await openDialog('web.search');

    fireEvent.click(screen.getAllByTestId('btn-web-search-remove')[0]);

    let rows = screen.getAllByTestId('web-search-row');
    expect(rows).toHaveLength(1);
    expect(nameValue(rows[0])).toBe('Exa 1');
    expect(screen.getByTestId('web-search-undo-bar').textContent).toContain('Removed Tavily 1');
    expect(update).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId('web-search-undo'));

    rows = screen.getAllByTestId('web-search-row');
    expect(rows).toHaveLength(2);
    expect(nameValue(rows[0])).toBe('Tavily 1');
    expect(nameValue(rows[1])).toBe('Exa 1');
    expect(screen.queryByTestId('web-search-undo-bar')).toBeNull();
    expect(update).not.toHaveBeenCalled();
  });

  it('swaps the credential field to Base URL only for searxng rows', async () => {
    const tools = catalogTools();
    tools[1].config = { entries: [{ id: 'e1', name: 'Tavily 1', provider: 'tavily', api_key_hint: 'ab12' }] };
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools });

    render(<ToolsPane tenant={mockTenant} />);

    await openDialog('web.search');
    const row = screen.getAllByTestId('web-search-row')[0];
    expect(within(row).getByLabelText('API key')).not.toBeNull();
    expect(within(row).queryByLabelText('Base URL')).toBeNull();

    fireEvent.change(within(row).getByLabelText('Provider'), { target: { value: 'searxng' } });
    expect(within(row).getByLabelText('Base URL')).not.toBeNull();
    expect(within(row).queryByLabelText('API key')).toBeNull();
    fireEvent.change(within(row).getByLabelText('Base URL'), { target: { value: 'http://searxng:8080' } });

    // Back to a key provider: the base URL field disappears again.
    fireEvent.change(within(row).getByLabelText('Provider'), { target: { value: 'exa' } });
    expect(within(row).queryByLabelText('Base URL')).toBeNull();
    expect(within(row).getByLabelText('API key')).not.toBeNull();
  });

  it('maps server 422 details onto the offending row, dialog level otherwise', async () => {
    const tools = catalogTools();
    tools[1].config = {
      entries: [
        { id: 'e1', name: 'Tavily 1', provider: 'tavily', api_key_hint: 'ab12' },
        { id: 'e2', name: 'Exa 1', provider: 'exa', api_key_hint: 'c7e1' },
      ],
    };
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools });
    vi.spyOn(api.tools, 'update').mockRejectedValue(
      new ApiError(422, 'invalid_request', 'validation failed', [
        { field: 'entries.1.name', message: 'Name already used' },
        { field: 'entries', message: 'Too many entries' },
      ])
    );

    render(<ToolsPane tenant={mockTenant} />);

    await openDialog('web.search');
    fireEvent.click(screen.getByTestId('btn-tool-config-save'));

    const rows = screen.getAllByTestId('web-search-row');
    await waitFor(() => {
      expect(within(rows[1]).queryByTestId('web-search-row-error')).not.toBeNull();
    });
    expect(within(rows[1]).getByTestId('web-search-row-error').textContent).toContain('Name already used');
    expect(within(rows[0]).queryByTestId('web-search-row-error')).toBeNull();
    expect(screen.getByTestId('tool-config-error').textContent).toContain('Too many entries');
    expect(screen.getByTestId('tool-config-dialog')).not.toBeNull();
  });
});
