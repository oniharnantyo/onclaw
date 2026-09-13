import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { GatewaysPane } from './GatewaysPane';
import {
  api,
  ApiError,
  type ApiAgent,
  type ApiGatewayBinding,
  type ApiGatewayConfig,
  type ApiGatewayLink,
} from '../../lib/api';

const mockTenant = { id: 'acme', sub: 'acme', name: 'Acme Corp' };

const gateway = (overrides: Partial<ApiGatewayConfig> = {}): ApiGatewayConfig => ({
  id: 'gw1',
  workspace_id: 'acme',
  platform: 'telegram',
  enabled: true,
  bot_username: 'onclaw_bot',
  token_hint: 'ab12',
  default_agent_id: 'a_atlas',
  transport: 'long_polling',
  webhook_url: '',
  status_error: null,
  created_at: '',
  updated_at: '',
  ...overrides,
});

const agents = (): ApiAgent[] =>
  [
    { id: 'a_atlas', slug: 'atlas', name: 'Atlas' },
    { id: 'a_beacon', slug: 'beacon', name: 'Beacon' },
  ].map((a) => ({
    id: a.id,
    workspace_id: 'acme',
    slug: a.slug,
    name: a.name,
    role: '',
    description: '',
    brief: '',
    identity: '',
    soul: '',
    bootstrap: '',
    provider_id: 'p1',
    model: 'm1',
    temperature: 1,
    autonomy: 'approval' as const,
    tools: [],
    enabled_mcps: [],
    avatar: {},
    prompts_status: 'ready' as const,
    created_at: '',
    updated_at: '',
  }));

const bindings = (): ApiGatewayBinding[] => [
  {
    id: 'b1',
    platform: 'telegram',
    platform_chat_id: '-100123',
    chat_title: 'Ops',
    agent_id: 'a_beacon',
    created_at: '2026-09-01T00:00:00Z',
  },
];

const myLink = (): ApiGatewayLink => ({
  platform_user_id: '593821092',
  username: 'onih',
  display_name: 'Oni',
  linked_at: '2026-09-12T00:00:00Z',
});

function mockGatewayApis(overrides: {
  gateway?: ApiGatewayConfig | null;
  bindings?: ApiGatewayBinding[];
  link?: ApiGatewayLink | null;
} = {}) {
  vi.spyOn(api.gateways.telegram, 'getConfig').mockResolvedValue({
    gateway: overrides.gateway === undefined ? gateway() : overrides.gateway,
  });
  vi.spyOn(api.gateways.telegram.bindings, 'list').mockResolvedValue({
    bindings: overrides.bindings ?? [],
  });
  vi.spyOn(api.gateways.telegram.links, 'getMine').mockResolvedValue({
    link: overrides.link === undefined ? null : overrides.link,
  });
  vi.spyOn(api.agents, 'list').mockResolvedValue({ agents: agents() });
}

describe('screens/settings/GatewaysPane', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
    });
  });

  it('renders the connected gateway, status, username, and token hint from the API', async () => {
    mockGatewayApis();

    render(<GatewaysPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('gateway-status').textContent).toBe('Connected');
    });
    expect(screen.getByTestId('gateway-username').textContent).toBe('@onclaw_bot');
    expect(screen.getByTestId('gateway-token-hint').textContent).toContain('••••ab12');
    // Write-only: the plaintext token is never rendered
    expect(screen.queryByText(/123:ABC/)).toBeNull();
    expect(screen.getByTestId('select-default-agent')).not.toBeNull();
  });

  it('renders the disabled status when the gateway is configured but disabled', async () => {
    mockGatewayApis({ gateway: gateway({ enabled: false }) });

    render(<GatewaysPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('gateway-status').textContent).toBe('Disabled');
    });
  });

  it('requires a token before connecting and saves it write-only', async () => {
    mockGatewayApis({ gateway: null });
    const updateSpy = vi
      .spyOn(api.gateways.telegram, 'updateConfig')
      .mockResolvedValue({ gateway: gateway() });
    const onToast = vi.fn();

    render(<GatewaysPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-connect-bot')).not.toBeNull();
    });

    // Empty submit is blocked inline, not via a disabled button
    fireEvent.click(screen.getByTestId('btn-connect-bot'));
    expect(screen.getByTestId('gateway-token-error').textContent).toContain('required');
    expect(updateSpy).not.toHaveBeenCalled();

    fireEvent.change(screen.getByTestId('input-gateway-token'), {
      target: { value: '123:ABC' },
    });
    fireEvent.click(screen.getByTestId('btn-connect-bot'));

    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalledWith('acme', { token: '123:ABC' });
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('Bot connected as @onclaw_bot');
    });
    // Write-only: the field is cleared after saving
    expect((screen.getByTestId('input-gateway-token') as HTMLInputElement).value).toBe('');
  });

  it('saves the default agent choice', async () => {
    mockGatewayApis();
    const updateSpy = vi
      .spyOn(api.gateways.telegram, 'updateConfig')
      .mockResolvedValue({ gateway: gateway({ default_agent_id: 'a_beacon' }) });

    render(<GatewaysPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('select-default-agent')).not.toBeNull();
    });
    fireEvent.change(screen.getByTestId('select-default-agent'), {
      target: { value: 'a_beacon' },
    });

    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalledWith('acme', { default_agent_id: 'a_beacon' });
    });
  });

  it('switches transport to webhook and reveals the webhook URL field', async () => {
    mockGatewayApis();
    const updateSpy = vi
      .spyOn(api.gateways.telegram, 'updateConfig')
      .mockResolvedValue({
        gateway: gateway({ transport: 'webhook', webhook_url: 'https://example.com/hook' }),
      });

    render(<GatewaysPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('seg-webhook')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('seg-webhook'));

    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalledWith('acme', { transport: 'webhook' });
    });
    await waitFor(() => {
      expect(screen.getByTestId('input-webhook-url')).not.toBeNull();
    });
  });

  it('enable and disable hit the dedicated endpoints', async () => {
    mockGatewayApis();
    const enableSpy = vi
      .spyOn(api.gateways.telegram, 'enable')
      .mockResolvedValue({ gateway: gateway() });
    const disableSpy = vi
      .spyOn(api.gateways.telegram, 'disable')
      .mockResolvedValue({ gateway: gateway({ enabled: false }) });

    render(<GatewaysPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByRole('switch', { name: 'Enable gateway' })).not.toBeNull();
    });
    fireEvent.click(screen.getByRole('switch', { name: 'Enable gateway' }));

    await waitFor(() => {
      expect(disableSpy).toHaveBeenCalledWith('acme');
    });

    await waitFor(() => {
      expect(screen.getByRole('switch', { name: 'Enable gateway' }).getAttribute('aria-checked')).toBe('false');
    });
    fireEvent.click(screen.getByRole('switch', { name: 'Enable gateway' }));

    await waitFor(() => {
      expect(enableSpy).toHaveBeenCalledWith('acme');
    });
  });

  it('lists group bindings with the bound agent and unlinks via DELETE', async () => {
    mockGatewayApis({ bindings: bindings() });
    const removeSpy = vi.spyOn(api.gateways.telegram.bindings, 'remove').mockResolvedValue(undefined);
    const onToast = vi.fn();

    render(<GatewaysPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('binding-b1')).not.toBeNull();
    });
    const row = screen.getByTestId('binding-b1').textContent || '';
    expect(row).toContain('Ops');
    expect(row).toContain('-100123');
    expect(row).toContain('Beacon');

    fireEvent.click(screen.getByTestId('btn-unlink-b1'));

    await waitFor(() => {
      expect(removeSpy).toHaveBeenCalledWith('acme', 'b1');
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('Ops unlinked');
    });
    await waitFor(() => {
      expect(screen.queryByTestId('binding-b1')).toBeNull();
    });
  });

  it('surfaces a binding conflict as a danger toast', async () => {
    mockGatewayApis({ bindings: bindings() });
    vi.spyOn(api.gateways.telegram.bindings, 'remove').mockRejectedValue(
      new ApiError(409, 'conflict', 'That group is already bound')
    );
    const onToast = vi.fn();

    render(<GatewaysPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-unlink-b1')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-unlink-b1'));

    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith(
        'Binding conflict — the group is bound to another agent. Reload and retry.',
        'danger'
      );
    });
  });

  it('shows the copyable bind command for the selected agent', async () => {
    mockGatewayApis();
    const onToast = vi.fn();

    render(<GatewaysPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('bind-command').textContent).toBe('/bind@onclaw_bot atlas');
    });

    fireEvent.change(screen.getByTestId('select-bind-agent'), { target: { value: 'a_beacon' } });
    expect(screen.getByTestId('bind-command').textContent).toBe('/bind@onclaw_bot beacon');

    fireEvent.click(screen.getByTestId('btn-copy-bind'));
    await waitFor(() => {
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('/bind@onclaw_bot beacon');
      expect(onToast).toHaveBeenCalledWith('Bind command copied to clipboard');
    });
  });

  describe('pairing modal', () => {
    const expires = () => new Date(Date.now() + 60 * 60 * 1000).toISOString();

    it('mints a token and shows the copyable /start command with a live countdown', async () => {
      mockGatewayApis();
      const createSpy = vi
        .spyOn(api.gateways.telegram.pairing, 'create')
        .mockResolvedValue({ token: { token: 'pt_abc123', expires_at: expires() } });
      const onToast = vi.fn();

      render(<GatewaysPane tenant={mockTenant} onToast={onToast} />);

      await waitFor(() => {
        expect(screen.getByTestId('btn-open-pairing')).not.toBeNull();
      });
      fireEvent.click(screen.getByTestId('btn-open-pairing'));

      await waitFor(() => {
        expect(createSpy).toHaveBeenCalledWith('acme');
        expect(screen.getByTestId('pairing-command').textContent).toBe('/start pt_abc123');
      });
      // Live expiry countdown in mm:ss, under the one-hour bound
      expect(screen.getByTestId('pairing-countdown').textContent).toMatch(/Expires in \d{2}:\d{2}/);
      expect(screen.queryByTestId('btn-regenerate-pairing')).toBeNull();

      fireEvent.click(screen.getByTestId('btn-copy-pairing'));
      await waitFor(() => {
        expect(navigator.clipboard.writeText).toHaveBeenCalledWith('/start pt_abc123');
        expect(onToast).toHaveBeenCalledWith('Pairing command copied to clipboard');
      });
    });

    it('revokes the pending token and closes', async () => {
      mockGatewayApis();
      vi.spyOn(api.gateways.telegram.pairing, 'create').mockResolvedValue({
        token: { token: 'pt_abc123', expires_at: expires() },
      });
      const revokeSpy = vi.spyOn(api.gateways.telegram.pairing, 'revoke').mockResolvedValue(undefined);
      const onToast = vi.fn();

      render(<GatewaysPane tenant={mockTenant} onToast={onToast} />);

      await waitFor(() => {
        expect(screen.getByTestId('btn-open-pairing')).not.toBeNull();
      });
      fireEvent.click(screen.getByTestId('btn-open-pairing'));

      await waitFor(() => {
        expect(screen.getByTestId('pairing-command')).not.toBeNull();
      });

      fireEvent.click(screen.getByTestId('btn-revoke-pairing'));

      await waitFor(() => {
        expect(revokeSpy).toHaveBeenCalledWith('acme', 'pt_abc123');
        expect(onToast).toHaveBeenCalledWith('Pairing token revoked');
        expect(screen.queryByTestId('modal-pairing')).toBeNull();
      });
    });

    it('offers regeneration once the token expires', async () => {
      mockGatewayApis();
      const createSpy = vi
        .spyOn(api.gateways.telegram.pairing, 'create')
        .mockResolvedValue({
          token: { token: 'pt_abc123', expires_at: new Date(Date.now() - 1000).toISOString() },
        });

      render(<GatewaysPane tenant={mockTenant} />);

      await waitFor(() => {
        expect(screen.getByTestId('btn-open-pairing')).not.toBeNull();
      });
      fireEvent.click(screen.getByTestId('btn-open-pairing'));

      await waitFor(() => {
        expect(screen.getByTestId('pairing-countdown').textContent).toContain('Expired');
        expect(screen.getByTestId('btn-regenerate-pairing')).not.toBeNull();
      });
      fireEvent.click(screen.getByTestId('btn-regenerate-pairing'));
      await waitFor(() => {
        expect(createSpy).toHaveBeenCalledTimes(2);
      });
    });
  });

  it('shows the linked identity and unpairs via the member endpoint', async () => {
    mockGatewayApis({ link: myLink() });
    const removeMineSpy = vi.spyOn(api.gateways.telegram.links, 'removeMine').mockResolvedValue(undefined);
    const onToast = vi.fn();

    render(<GatewaysPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('gateway-link-identity').textContent).toContain('Oni');
    });
    expect(screen.getByTestId('gateway-link-identity').textContent).toContain('@onih');

    fireEvent.click(screen.getByTestId('btn-unpair'));

    await waitFor(() => {
      expect(removeMineSpy).toHaveBeenCalledWith('acme');
      expect(onToast).toHaveBeenCalledWith('Telegram account unlinked');
    });
  });

  it('hides admin controls for non-admins but keeps the pairing flow', async () => {
    mockGatewayApis();
    const onToast = vi.fn();

    render(<GatewaysPane tenant={mockTenant} onToast={onToast} canWrite={false} />);

    await waitFor(() => {
      expect(screen.getByTestId('gateway-pairing-section')).not.toBeNull();
    });
    expect(screen.queryByTestId('gateway-admin-section')).toBeNull();
    expect(screen.queryByTestId('input-gateway-token')).toBeNull();
    expect(screen.queryByRole('switch', { name: 'Enable gateway' })).toBeNull();
    expect(screen.getByTestId('btn-open-pairing')).not.toBeNull();
  });
});
