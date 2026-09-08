import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { McpServerDialog } from './McpServerDialog';
import type { ApiMcpServer } from '../lib/api';

const stdioServer = (): ApiMcpServer => ({
  id: 'srv-gh',
  workspace_id: 'acme',
  name: 'GitHub',
  transport: 'stdio',
  command: 'npx',
  args: ['-y', '@modelcontextprotocol/server-github'],
  env: [{ name: 'GITHUB_TOKEN', value_hint: 'ghp1' }],
  enabled: true,
  status: 'connected',
  status_error: null,
  tool_count: 24,
  created_at: '',
  updated_at: '',
});

const httpServer = (): ApiMcpServer => ({
  id: 'srv-http',
  workspace_id: 'acme',
  name: 'Linear',
  transport: 'streamable_http',
  url: 'https://mcp.linear.app/mcp',
  headers: [{ name: 'Authorization', value_hint: '9f04' }],
  enabled: true,
  status: 'connected',
  status_error: null,
  tool_count: 12,
  created_at: '',
  updated_at: '',
});

describe('modals/McpServerDialog', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  function renderDialog(props: Partial<Parameters<typeof McpServerDialog>[0]> = {}) {
    const onSave = vi.fn().mockResolvedValue(undefined);
    const onClose = vi.fn();
    render(<McpServerDialog existingServers={[]} onClose={onClose} onSave={onSave} {...props} />);
    return { onSave, onClose };
  }

  it('renders the stdio branch by default and swaps to URL/headers via the transport select', () => {
    render(<McpServerDialog existingServers={[]} onClose={vi.fn()} onSave={vi.fn()} />);

    // stdio controls visible, HTTP controls absent.
    expect(screen.getByLabelText('Command')).not.toBeNull();
    expect(screen.getByLabelText('Arguments')).not.toBeNull();
    expect(screen.getByText('Environment variables')).not.toBeNull();
    expect(screen.queryByLabelText('URL')).toBeNull();

    // The select carries every transport so any option can be chosen.
    const transport = screen.getByLabelText('Transport') as HTMLSelectElement;
    const values = Array.from(transport.options).map((o) => o.value);
    expect(values).toEqual(['stdio', 'streamable_http', 'sse']);

    fireEvent.change(transport, { target: { value: 'streamable_http' } });

    expect(screen.getByLabelText('URL')).not.toBeNull();
    expect(screen.getByText('Headers')).not.toBeNull();
    expect(screen.queryByLabelText('Command')).toBeNull();
    expect(screen.queryByLabelText('Arguments')).toBeNull();

    // Switching back restores the stdio controls.
    fireEvent.change(screen.getByLabelText('Transport'), { target: { value: 'sse' } });
    expect(screen.getByLabelText('URL')).not.toBeNull();
    fireEvent.change(screen.getByLabelText('Transport'), { target: { value: 'stdio' } });
    expect(screen.getByLabelText('Command')).not.toBeNull();
  });

  it('submits a structured stdio payload with parsed args and env rows, then closes', async () => {
    const { onSave, onClose } = renderDialog();

    fireEvent.change(screen.getByLabelText('Server name'), { target: { value: 'GitHub' } });
    fireEvent.change(screen.getByLabelText('Command'), { target: { value: 'npx' } });
    fireEvent.change(screen.getByLabelText('Arguments'), {
      target: { value: '-y @modelcontextprotocol/server-github' },
    });
    fireEvent.click(screen.getByTestId('btn-mcp-env-add'));
    fireEvent.change(screen.getByLabelText('Variable name'), { target: { value: 'GITHUB_TOKEN' } });
    fireEvent.change(screen.getByLabelText('Variable value'), { target: { value: ' ghp_secret ' } });

    fireEvent.click(screen.getByTestId('btn-mcp-add-confirm'));

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledWith({
        name: 'GitHub',
        transport: 'stdio',
        command: 'npx',
        args: ['-y', '@modelcontextprotocol/server-github'],
        env: [{ name: 'GITHUB_TOKEN', value: 'ghp_secret' }],
      });
    });
    await waitFor(() => {
      expect(onClose).toHaveBeenCalled();
    });
  });

  it('blocks Save until the transport-required fields are valid', () => {
    const { onSave } = renderDialog();

    // No name, no command.
    fireEvent.click(screen.getByTestId('btn-mcp-add-confirm'));
    expect(onSave).not.toHaveBeenCalled();
    expect(screen.getByTestId('mcp-name-error').textContent).toContain('required');
    expect(screen.getByTestId('mcp-command-error').textContent).toContain('required');

    fireEvent.change(screen.getByLabelText('Server name'), { target: { value: 'GitHub' } });
    fireEvent.click(screen.getByTestId('btn-mcp-add-confirm'));
    expect(onSave).not.toHaveBeenCalled();
    expect(screen.getByTestId('mcp-command-error').textContent).toContain('required');

    // Transport swap moves the requirement to the URL.
    fireEvent.change(screen.getByLabelText('Transport'), { target: { value: 'streamable_http' } });
    fireEvent.click(screen.getByTestId('btn-mcp-add-confirm'));
    expect(onSave).not.toHaveBeenCalled();
    expect(screen.getByTestId('mcp-url-error').textContent).toContain('required');
    expect(screen.queryByTestId('mcp-command-error')).toBeNull();
  });

  it('rejects duplicate server names case-insensitively and duplicate row names', () => {
    renderDialog({ existingServers: [stdioServer()] });

    fireEvent.change(screen.getByLabelText('Server name'), { target: { value: 'github' } });
    fireEvent.change(screen.getByLabelText('Command'), { target: { value: 'npx' } });
    fireEvent.click(screen.getByTestId('btn-mcp-env-add'));
    fireEvent.change(screen.getByLabelText('Variable name'), { target: { value: 'TOKEN' } });
    fireEvent.click(screen.getByTestId('btn-mcp-env-add'));
    fireEvent.change(screen.getAllByLabelText('Variable name')[1], { target: { value: 'TOKEN' } });
    fireEvent.click(screen.getByTestId('btn-mcp-add-confirm'));

    expect(screen.getByTestId('mcp-name-error').textContent).toContain('already exists');
    const rowErrs = screen.getAllByTestId('mcp-row-error');
    expect(rowErrs.length).toBe(2);
    expect(rowErrs[0].textContent).toContain('unique');
    expect(rowErrs[1].textContent).toContain('unique');
  });

  it('hydrates an edit form with stored hints and keeps secrets when values are left empty', async () => {
    const { onSave } = renderDialog({ server: httpServer() });

    expect((screen.getByLabelText('Server name') as HTMLInputElement).value).toBe('Linear');
    expect((screen.getByLabelText('Transport') as HTMLSelectElement).value).toBe('streamable_http');
    expect((screen.getByLabelText('URL') as HTMLInputElement).value).toBe('https://mcp.linear.app/mcp');

    // Write-only: the stored header value is never echoed — the hint is the
    // placeholder and the input stays empty.
    const valueInput = screen.getByLabelText('Header value') as HTMLInputElement;
    expect(valueInput.type).toBe('password');
    expect(valueInput.value).toBe('');
    expect(valueInput.placeholder).toBe('•••• 9f04');

    fireEvent.click(screen.getByTestId('btn-mcp-save'));

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledWith({
        name: 'Linear',
        transport: 'streamable_http',
        url: 'https://mcp.linear.app/mcp',
        // No `value` key — the stored secret is kept.
        headers: [{ name: 'Authorization' }],
      });
    });
  });

  it('reconfigures the form when the transport changes on edit and saves the new branch', async () => {
    const { onSave } = renderDialog({ server: stdioServer(), existingServers: [stdioServer()] });

    fireEvent.change(screen.getByLabelText('Transport'), { target: { value: 'streamable_http' } });
    fireEvent.change(screen.getByLabelText('URL'), { target: { value: 'https://mcp.github.dev/mcp' } });

    fireEvent.click(screen.getByTestId('btn-mcp-save'));

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledWith({
        name: 'GitHub',
        transport: 'streamable_http',
        url: 'https://mcp.github.dev/mcp',
        // The stdio branch's env row (kept-secret) is not carried into the new transport.
      });
    });
  });

  it('keeps the dialog open with the error when onSave rejects', async () => {
    const onSave = vi.fn().mockRejectedValue(new Error('Network connection failed.'));
    const onClose = vi.fn();
    render(
      <McpServerDialog existingServers={[]} onClose={onClose} onSave={onSave} />
    );

    fireEvent.change(screen.getByLabelText('Server name'), { target: { value: 'GitHub' } });
    fireEvent.change(screen.getByLabelText('Command'), { target: { value: 'npx' } });
    fireEvent.click(screen.getByTestId('btn-mcp-add-confirm'));

    await waitFor(() => {
      expect(screen.getByTestId('mcp-dialog-error').textContent).toContain('Network connection failed.');
    });
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByTestId('modal-mcp-server')).not.toBeNull();
  });

  it('maps 422 field details onto the matching inputs and the rest onto the dialog error', async () => {
    const { ApiError } = await import('../lib/api');
    const onSave = vi.fn().mockRejectedValue(
      new ApiError(422, 'invalid_request', 'validation failed', [
        { field: 'name', message: 'Name already used' },
        { field: 'env.0.name', message: 'Invalid env name' },
      ])
    );
    render(<McpServerDialog existingServers={[]} onClose={vi.fn()} onSave={onSave} />);

    fireEvent.change(screen.getByLabelText('Server name'), { target: { value: 'GitHub' } });
    fireEvent.change(screen.getByLabelText('Command'), { target: { value: 'npx' } });
    fireEvent.click(screen.getByTestId('btn-mcp-add-confirm'));

    await waitFor(() => {
      expect(screen.getByTestId('mcp-name-error').textContent).toContain('Name already used');
    });
    expect(screen.getByTestId('mcp-dialog-error').textContent).toContain('Invalid env name');
  });

  it('drops fully blank rows on submit instead of sending them', async () => {
    const { onSave } = renderDialog();

    fireEvent.change(screen.getByLabelText('Server name'), { target: { value: 'GitHub' } });
    fireEvent.change(screen.getByLabelText('Command'), { target: { value: 'npx' } });
    fireEvent.click(screen.getByTestId('btn-mcp-env-add'));
    fireEvent.click(screen.getByTestId('btn-mcp-add-confirm'));

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledWith({
        name: 'GitHub',
        transport: 'stdio',
        command: 'npx',
      });
    });
  });

  it('removes rows locally without submitting them', async () => {
    const { onSave } = renderDialog({ server: stdioServer() });

    const rows = screen.getAllByTestId('mcp-env-row');
    expect(rows).toHaveLength(1);
    fireEvent.click(within(rows[0]).getByLabelText('Remove variable'));
    expect(screen.queryByTestId('mcp-env-row')).toBeNull();

    fireEvent.click(screen.getByTestId('btn-mcp-save'));

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledWith({
        name: 'GitHub',
        transport: 'stdio',
        command: 'npx',
        args: ['-y', '@modelcontextprotocol/server-github'],
      });
    });
  });
});
