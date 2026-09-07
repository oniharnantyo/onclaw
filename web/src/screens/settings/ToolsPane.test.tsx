import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
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
    config_schema: [
      {
        key: 'provider',
        label: 'Provider',
        type: 'enum',
        required: true,
        help: 'Which search backend this workspace uses.',
        options: [
          { value: 'duckduckgo', label: 'DuckDuckGo' },
          { value: 'tavily', label: 'Tavily' },
        ],
      },
      { key: 'api_key', label: 'API key', type: 'secret', required: false, help: 'Stored encrypted; never shown again.' },
    ],
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
    config: { headless: true, max_pages: 3 },
    config_schema: [
      { key: 'headless', label: 'Headless', type: 'boolean', required: false },
      { key: 'max_pages', label: 'Max pages', type: 'number', required: false },
    ],
  },
];

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

  it('renders the config dialog per schema, never echoing secrets', async () => {
    const tools = catalogTools();
    tools[2].config = { headless: true, max_pages: 3, api_key: { hint: '7f3a' } };
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools });

    render(<ToolsPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-tool-config-web.search')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-tool-config-web.search'));

    // One labeled input per schema field, rendered generically.
    await waitFor(() => {
      expect(screen.getByTestId('tool-config-dialog')).not.toBeNull();
    });
    expect(screen.getByTestId('tool-config-field-provider')).not.toBeNull();
    expect(screen.getByTestId('tool-config-field-api_key')).not.toBeNull();

    // The secret input is a password field; no stored value is echoed.
    const secretInput = screen.getByLabelText(/API key/i) as HTMLInputElement;
    expect(secretInput.type).toBe('password');
    expect(secretInput.value).toBe('');
  });

  it('surfaces 422 validation details as inline field errors', async () => {
    vi.spyOn(api.tools, 'list').mockResolvedValue({ tools: catalogTools() });
    vi.spyOn(api.tools, 'update').mockRejectedValue(
      new ApiError(422, 'invalid_request', 'web.search provider "tavily" requires an API key', [
        { field: 'api_key', message: 'web.search provider "tavily" requires an API key' },
      ])
    );

    render(<ToolsPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-tool-config-web.search')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-tool-config-web.search'));

    await waitFor(() => {
      expect(screen.getByTestId('tool-config-field-provider')).not.toBeNull();
    });
    fireEvent.change(screen.getByLabelText(/Provider/i), { target: { value: 'tavily' } });
    fireEvent.click(screen.getByTestId('btn-tool-config-save'));

    await waitFor(() => {
      expect(
        screen.getByTestId('tool-config-field-error-api_key')
      ).not.toBeNull();
    });
    expect(screen.getByTestId('tool-config-field-error-api_key').textContent).toContain('requires an API key');
  });
});
