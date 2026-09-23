import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { McpServerDialog } from './McpServerDialog';
import {
  ApiError,
  type ApiMcpServer,
  type McpOAuthFlowApi,
  type McpServerPayload,
} from '../lib/api';

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

  // -----------------------------------------------------------------------
  // Auth mode, launch presets, and the OAuth sign-in step
  // (add-mcp-oauth-client 7.2/7.3)
  // -----------------------------------------------------------------------

  const oauthServer = (): ApiMcpServer => ({
    ...httpServer(),
    auth_mode: 'oauth',
    oauth_client_id: 'my-byo-app',
    oauth_client_secret_hint: '9f04',
  });

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

  const flowApi = (overrides: Partial<McpOAuthFlowApi> = {}): McpOAuthFlowApi => ({
    authorize: vi.fn().mockResolvedValue({ authorize_url: 'https://auth.example.com/authorize?x=1' }),
    beginDevice: vi.fn().mockResolvedValue({
      device_session: 'sealed-blob',
      user_code: 'WDJB-MJHT',
      verification_uri: 'https://example.com/activate',
      expires_in: 600,
      interval: 0,
    }),
    pollDevice: vi.fn().mockResolvedValue({ status: 'pending' }),
    ...overrides,
  });

  /** Fills the add form into an oauth-mode streamable_http server and submits;
   * resolves once the sign-in step replaces the form. */
  async function submitOauthAdd(
    onSave: (payload: McpServerPayload) => Promise<ApiMcpServer | void>,
    oauthApi: McpOAuthFlowApi,
    opts: { onAuthorized?: () => void; url?: string } = {}
  ) {
    render(
      <McpServerDialog
        existingServers={[]}
        onClose={vi.fn()}
        onSave={onSave}
        oauthApi={oauthApi}
        onAuthorized={opts.onAuthorized}
      />
    );

    fireEvent.change(screen.getByLabelText('Server name'), { target: { value: 'Notion' } });
    fireEvent.change(screen.getByLabelText('Transport'), { target: { value: 'streamable_http' } });
    fireEvent.change(screen.getByLabelText('URL'), {
      target: { value: opts.url ?? 'https://mcp.notion.com/mcp' },
    });
    fireEvent.change(screen.getByLabelText('Authentication mode'), { target: { value: 'oauth' } });
    fireEvent.click(screen.getByTestId('btn-mcp-add-confirm'));

    await waitFor(() => {
      expect(screen.getByTestId('mcp-oauth-step')).not.toBeNull();
    });
  }

  it('reveals the BYO client rows only when the auth mode is oauth on a URL transport', () => {
    render(<McpServerDialog existingServers={[]} onClose={vi.fn()} onSave={vi.fn()} />);

    // stdio carries no auth mode — OAuth is a URL-transport-only mode.
    expect(screen.queryByLabelText('Authentication mode')).toBeNull();

    fireEvent.change(screen.getByLabelText('Transport'), { target: { value: 'streamable_http' } });
    const select = screen.getByLabelText('Authentication mode') as HTMLSelectElement;
    expect(Array.from(select.options).map((o) => o.value)).toEqual(['none', 'oauth']);
    expect(select.value).toBe('none');
    expect(screen.queryByTestId('mcp-oauth-client')).toBeNull();

    fireEvent.change(select, { target: { value: 'oauth' } });
    expect(screen.getByTestId('mcp-oauth-client')).not.toBeNull();
    expect(screen.getByLabelText('Client ID')).not.toBeNull();
    expect(screen.getByLabelText('Client secret')).not.toBeNull();
    expect(screen.getByTestId('mcp-oauth-client').textContent).toContain(
      'Leave blank to auto-register (DCR) where the provider supports it; bring-your-own app otherwise.'
    );

    // Switching back to none drops the BYO rows again.
    fireEvent.change(screen.getByLabelText('Authentication mode'), { target: { value: 'none' } });
    expect(screen.queryByTestId('mcp-oauth-client')).toBeNull();
  });

  it('prefills the Notion preset into the URL, transport, and auth mode, then submits oauth', async () => {
    const { onSave } = renderDialog();

    fireEvent.click(screen.getByTestId('mcp-preset-notion'));

    expect((screen.getByLabelText('Server name') as HTMLInputElement).value).toBe('Notion');
    expect((screen.getByLabelText('Transport') as HTMLSelectElement).value).toBe('streamable_http');
    expect((screen.getByLabelText('URL') as HTMLInputElement).value).toBe('https://mcp.notion.com/mcp');
    expect((screen.getByLabelText('Authentication mode') as HTMLSelectElement).value).toBe('oauth');
    expect(screen.getByTestId('mcp-oauth-client')).not.toBeNull();
    expect(screen.getByTestId('mcp-preset-notion').getAttribute('title')).toContain(
      'dynamic client registration'
    );

    fireEvent.click(screen.getByTestId('btn-mcp-add-confirm'));

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledWith({
        name: 'Notion',
        transport: 'streamable_http',
        url: 'https://mcp.notion.com/mcp',
        auth_mode: 'oauth',
      });
    });
  });

  it('prefills the Sentry preset with the {org} placeholder and its slug help text', () => {
    renderDialog();

    fireEvent.click(screen.getByTestId('mcp-preset-sentry'));

    expect((screen.getByLabelText('URL') as HTMLInputElement).value).toBe(
      'https://mcp.sentry.dev/mcp/{org}'
    );
    expect(screen.getByTestId('mcp-url-org-help').textContent).toBe(
      'replace {org} with your Sentry organization slug'
    );
    expect(screen.getByTestId('mcp-preset-sentry').getAttribute('title')).toContain(
      'organization slug'
    );
  });

  it('hydrates an oauth edit with the stored client id and secret hint, keeping a blank secret', async () => {
    const { onSave } = renderDialog({ server: oauthServer() });

    expect((screen.getByLabelText('Authentication mode') as HTMLSelectElement).value).toBe('oauth');
    expect((screen.getByLabelText('Client ID') as HTMLInputElement).value).toBe('my-byo-app');
    const secret = screen.getByLabelText('Client secret') as HTMLInputElement;
    expect(secret.type).toBe('password');
    expect(secret.value).toBe('');
    // Write-only: the stored secret surfaces only as its last-4 hint.
    expect(secret.placeholder).toBe('•••• 9f04');

    fireEvent.click(screen.getByTestId('btn-mcp-save'));

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledWith({
        name: 'Linear',
        transport: 'streamable_http',
        url: 'https://mcp.linear.app/mcp',
        headers: [{ name: 'Authorization' }],
        auth_mode: 'oauth',
        oauth_client_id: 'my-byo-app',
        // No oauth_client_secret key — the stored secret is kept.
      });
    });
  });

  it('sends the explicit clearing auth mode when an oauth row switches back to none', async () => {
    const { onSave } = renderDialog({ server: oauthServer() });

    fireEvent.change(screen.getByLabelText('Authentication mode'), { target: { value: 'none' } });
    expect(screen.queryByTestId('mcp-oauth-client')).toBeNull();
    fireEvent.click(screen.getByTestId('btn-mcp-save'));

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledWith({
        name: 'Linear',
        transport: 'streamable_http',
        url: 'https://mcp.linear.app/mcp',
        headers: [{ name: 'Authorization' }],
        // Explicit non-oauth mode clears the stored BYO client server-side.
        auth_mode: 'none',
      });
    });
  });

  it('keeps the dialog open with the sign-in step after saving an oauth server', async () => {
    const assignMock = stubLocationAssign();
    const oauthApi = flowApi();
    const onSave = vi
      .fn()
      .mockResolvedValue({ ...httpServer(), id: 'srv-new', auth_mode: 'oauth' as const });
    await submitOauthAdd(onSave, oauthApi);

    expect(onSave).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId('btn-mcp-add-confirm')).toBeNull();

    fireEvent.click(screen.getByTestId('btn-mcp-signin'));

    await waitFor(() => {
      expect(oauthApi.authorize).toHaveBeenCalledWith('srv-new');
    });
    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith('https://auth.example.com/authorize?x=1');
    });
  });

  it('falls back to the device flow when the instance is headless', async () => {
    const oauthApi = flowApi({
      authorize: vi
        .fn()
        .mockRejectedValue(
          new ApiError(
            400,
            'invalid_request',
            'OAuth sign-in is unavailable because the instance public base URL is not configured; use the device authorization flow'
          )
        ),
    });
    const onSave = vi.fn().mockResolvedValue({ ...httpServer(), id: 'srv-new', auth_mode: 'oauth' as const });
    await submitOauthAdd(onSave, oauthApi);

    fireEvent.click(screen.getByTestId('btn-mcp-signin'));

    // The browser hand-off is tried first, then the paste-back screen opens
    // on the headless signal.
    await waitFor(() => {
      expect(oauthApi.authorize).toHaveBeenCalledWith('srv-new');
    });
    await waitFor(() => {
      expect(oauthApi.beginDevice).toHaveBeenCalledWith('srv-new');
    });
    expect(screen.getByTestId('mcp-device-code').textContent).toBe('WDJB-MJHT');
    expect(screen.getByTestId('link-mcp-device-verify').getAttribute('href')).toBe(
      'https://example.com/activate'
    );
  });

  it('runs the device paste-back loop: pending then completed, and refreshes the caller', async () => {
    const onAuthorized = vi.fn();
    const oauthApi = flowApi({
      pollDevice: vi
        .fn()
        .mockResolvedValueOnce({ status: 'pending' })
        .mockResolvedValue({ status: 'completed' }),
    });
    const onSave = vi.fn().mockResolvedValue({ ...httpServer(), id: 'srv-new', auth_mode: 'oauth' as const });
    await submitOauthAdd(onSave, oauthApi, { onAuthorized });

    fireEvent.click(screen.getByTestId('btn-mcp-device'));

    await waitFor(() => {
      expect(oauthApi.beginDevice).toHaveBeenCalledWith('srv-new');
    });
    expect(screen.getByTestId('mcp-device-code').textContent).toBe('WDJB-MJHT');

    await waitFor(() => {
      expect(oauthApi.pollDevice).toHaveBeenCalledWith('srv-new', 'sealed-blob');
    });
    await waitFor(
      () => {
        expect(screen.getByTestId('mcp-device-status').textContent).toContain('Authorized');
      },
      { timeout: 3000 }
    );
    expect(onAuthorized).toHaveBeenCalled();
    expect(screen.queryByTestId('btn-mcp-flow-done')).not.toBeNull();
  });

  it('surfaces a denied device authorization with the provider detail', async () => {
    const oauthApi = flowApi({
      pollDevice: vi.fn().mockResolvedValue({ status: 'denied', detail: 'the user declined' }),
    });
    const onSave = vi.fn().mockResolvedValue({ ...httpServer(), id: 'srv-new', auth_mode: 'oauth' as const });
    await submitOauthAdd(onSave, oauthApi);

    fireEvent.click(screen.getByTestId('btn-mcp-device'));

    await waitFor(
      () => {
        expect(screen.getByTestId('mcp-device-status').textContent).toContain(
          'The authorization was denied — the user declined.'
        );
      },
      { timeout: 3000 }
    );
    // A denial is retryable with a fresh code, and never a completion.
    expect(screen.getByTestId('btn-mcp-device-restart')).not.toBeNull();
    expect(screen.queryByTestId('btn-mcp-flow-done')).toBeNull();
  });
});
