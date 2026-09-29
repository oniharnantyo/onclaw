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
  type ApiWhatsAppGatewayConfig,
  type ApiWhatsAppHealth,
} from '../../lib/api';

const mockTenant = { id: 'acme', sub: 'acme', name: 'Acme Corp' };

const gateway = (overrides: Partial<ApiGatewayConfig> = {}): ApiGatewayConfig => ({
  id: 'gw1',
  workspace_id: 'acme',
  platform: 'telegram',
  identity: 'onclaw_bot',
  bot_username: 'onclaw_bot',
  token_hint: 'ab12',
  agent_id: 'a_atlas',
  enabled: true,
  transport: 'long_polling',
  webhook_url: '',
  status_error: null,
  created_at: '',
  updated_at: '',
  ...overrides,
});

const gateway2 = (overrides: Partial<ApiGatewayConfig> = {}): ApiGatewayConfig => ({
  id: 'gw2',
  workspace_id: 'acme',
  platform: 'telegram',
  identity: 'beacon_bot',
  bot_username: 'beacon_bot',
  token_hint: 'cd34',
  agent_id: 'a_beacon',
  enabled: true,
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
    provider_id: 'p1',
    model: 'm1',
    temperature: 1,
    autonomy: 'approval' as const,
    disabled_tools: [],
    enabled_mcps: [],
    avatar: {},
    prompts_status: 'ready' as const,
    created_at: '',
    updated_at: '',
  }));

const bindings = (): ApiGatewayBinding[] => [
  {
    id: 'b1',
    gateway_id: 'gw1',
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

const waConfig = (overrides: Partial<ApiWhatsAppGatewayConfig> = {}): ApiWhatsAppGatewayConfig => ({
  id: 'wa1',
  platform: 'whatsapp',
  lane: 'cloud_api',
  identity: 'wa_bot',
  agent_id: 'a_atlas',
  enabled: true,
  bot_username: null,
  transport: null,
  webhook_url: '',
  has_credentials: true,
  ...overrides,
});

const waLink = (): ApiGatewayLink => ({
  platform_user_id: '6281234567890',
  username: null,
  display_name: 'Oni',
  linked_at: '2026-09-12T00:00:00Z',
});

function mockGatewayApis(overrides: {
  tgGateways?: ApiGatewayConfig[];
  bindings?: ApiGatewayBinding[];
  link?: ApiGatewayLink | null;
  waGateways?: ApiWhatsAppGatewayConfig[];
  waHealthMap?: Record<string, ApiWhatsAppHealth>;
  waLink?: ApiGatewayLink | null;
} = {}) {
  const tgList = overrides.tgGateways === undefined ? [gateway()] : overrides.tgGateways;
  const waList = overrides.waGateways === undefined ? [] : overrides.waGateways;

  vi.spyOn(api.gateways.telegram, 'list').mockResolvedValue(tgList);
  vi.spyOn(api.gateways.telegram, 'get').mockImplementation(async (_ws, id) => {
    const found = tgList.find((g) => g.id === id);
    if (found) return found;
    throw new ApiError(404, 'not_found', 'gateway not found');
  });
  vi.spyOn(api.gateways.telegram.bindings, 'list').mockResolvedValue(overrides.bindings ?? []);
  vi.spyOn(api.gateways.telegram.links, 'getMine').mockResolvedValue({
    link: overrides.link === undefined ? null : overrides.link,
  });

  vi.spyOn(api.gateways.whatsapp, 'list').mockResolvedValue(waList);
  vi.spyOn(api.gateways.whatsapp, 'get').mockImplementation(async (_ws, id) => {
    const found = waList.find((g) => g.id === id);
    if (found) return found;
    throw new ApiError(404, 'not_found', 'gateway not found');
  });
  vi.spyOn(api.gateways.whatsapp, 'health').mockImplementation(async (_ws, id) => {
    if (overrides.waHealthMap && overrides.waHealthMap[id]) {
      return overrides.waHealthMap[id];
    }
    return { status: 'unconfigured' };
  });
  vi.spyOn(api.gateways.whatsapp.links, 'getMine').mockResolvedValue({
    link: overrides.waLink === undefined ? null : overrides.waLink,
  });
  vi.spyOn(api.agents, 'list').mockResolvedValue({ agents: agents() });
}

const openWhatsAppTab = async () => {
  const btn = screen.getByTestId('tab-whatsapp') || screen.queryByTestId(/tab-whatsapp-/);
  fireEvent.click(btn);
  await waitFor(() => {
    expect(screen.getByTestId('wa-pairing-section')).not.toBeNull();
  });
};

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

  it('renders the paused status when the gateway is configured but disabled', async () => {
    mockGatewayApis({ tgGateways: [gateway({ enabled: false })] });

    render(<GatewaysPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('gateway-status').textContent).toBe('Paused');
    });
  });

  it('sidebar lists multiple bot rows with status dots and @usernames, switching between them', async () => {
    mockGatewayApis({
      tgGateways: [
        gateway({ id: 'gw1', bot_username: 'bot_one', token_hint: '1111', agent_id: 'a_atlas', enabled: true }),
        gateway2({ id: 'gw2', bot_username: 'bot_two', token_hint: '2222', agent_id: 'a_beacon', enabled: false }),
      ],
    });

    render(<GatewaysPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('sidebar-item-telegram-gw1')).not.toBeNull();
      expect(screen.getByTestId('sidebar-item-telegram-gw2')).not.toBeNull();
    });

    // Both bots listed with usernames
    expect(screen.getByTestId('sidebar-item-telegram-gw1').textContent).toContain('@bot_one');
    expect(screen.getByTestId('sidebar-item-telegram-gw2').textContent).toContain('@bot_two');

    // Status dots
    expect(screen.getByTestId('sidebar-dot-telegram-gw1').className).toContain('bg-success');
    expect(screen.getByTestId('sidebar-dot-telegram-gw2').className).toContain('bg-muted');

    // Initially gw1 is selected
    expect(screen.getByTestId('gateway-username').textContent).toBe('@bot_one');

    // Click gw2 in sidebar to switch
    fireEvent.click(screen.getByTestId('sidebar-item-telegram-gw2'));

    await waitFor(() => {
      expect(screen.getByTestId('gateway-username').textContent).toBe('@bot_two');
      expect(screen.getByTestId('gateway-status').textContent).toBe('Paused');
    });
  });

  it('＋ Add a bot opens Telegram Connect Wizard modal requiring agent selection before saving', async () => {
    mockGatewayApis();
    const createSpy = vi
      .spyOn(api.gateways.telegram, 'create')
      .mockResolvedValue(gateway2({ bot_username: 'new_bot' }));
    const onToast = vi.fn();

    render(<GatewaysPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-add-telegram-bot')).not.toBeNull();
    });

    fireEvent.click(screen.getByTestId('btn-add-telegram-bot'));

    await waitFor(() => {
      expect(screen.getByTestId('modal-telegram-wizard')).not.toBeNull();
    });

    // Clear agent to test validation
    fireEvent.change(screen.getByTestId('wizard-select-telegram-agent'), {
      target: { value: '' },
    });
    fireEvent.click(screen.getByTestId('btn-submit-telegram-wizard'));

    expect(screen.getByTestId('wizard-token-error').textContent).toContain('required');
    expect(screen.getByTestId('wizard-agent-error').textContent).toContain('required');
    expect(createSpy).not.toHaveBeenCalled();

    // Fill in valid details
    fireEvent.change(screen.getByTestId('wizard-input-telegram-token'), {
      target: { value: '123456:NEW_BOT_TOKEN' },
    });
    fireEvent.change(screen.getByTestId('wizard-select-telegram-agent'), {
      target: { value: 'a_beacon' },
    });

    fireEvent.click(screen.getByTestId('btn-submit-telegram-wizard'));

    await waitFor(() => {
      expect(createSpy).toHaveBeenCalledWith('acme', {
        token: '123456:NEW_BOT_TOKEN',
        agent_id: 'a_beacon',
        transport: 'long_polling',
        webhook_url: undefined,
      });
    });

    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('Bot connected as @new_bot');
      expect(screen.queryByTestId('modal-telegram-wizard')).toBeNull();
    });
  });

  it('rotates bot token for the currently selected bot', async () => {
    mockGatewayApis();
    const updateSpy = vi
      .spyOn(api.gateways.telegram, 'update')
      .mockResolvedValue(gateway({ token_hint: '9999' }));
    const onToast = vi.fn();

    render(<GatewaysPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('input-gateway-token')).not.toBeNull();
    });

    fireEvent.change(screen.getByTestId('input-gateway-token'), {
      target: { value: '123:NEW_TOKEN' },
    });
    fireEvent.click(screen.getByTestId('btn-rotate-token'));

    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalledWith('acme', 'gw1', { token: '123:NEW_TOKEN' });
      expect(onToast).toHaveBeenCalledWith('Bot token updated');
    });
    expect((screen.getByTestId('input-gateway-token') as HTMLInputElement).value).toBe('');
  });

  it('saves the bound agent change calling update with agent_id', async () => {
    mockGatewayApis();
    const updateSpy = vi
      .spyOn(api.gateways.telegram, 'update')
      .mockResolvedValue(gateway({ agent_id: 'a_beacon' }));

    render(<GatewaysPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('select-default-agent')).not.toBeNull();
    });
    fireEvent.change(screen.getByTestId('select-default-agent'), {
      target: { value: 'a_beacon' },
    });

    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalledWith('acme', 'gw1', { agent_id: 'a_beacon' });
    });
  });

  it('tests bot connection via test button', async () => {
    mockGatewayApis();
    const testSpy = vi
      .spyOn(api.gateways.telegram, 'test')
      .mockResolvedValue({ ok: true, bot_username: 'onclaw_bot' });
    const onToast = vi.fn();

    render(<GatewaysPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-test-gateway')).not.toBeNull();
    });

    fireEvent.click(screen.getByTestId('btn-test-gateway'));

    await waitFor(() => {
      expect(testSpy).toHaveBeenCalledWith('acme', 'gw1');
      expect(onToast).toHaveBeenCalledWith('Connection verified — bot is @onclaw_bot');
    });
  });

  it('deletes a bot via delete button', async () => {
    mockGatewayApis();
    const deleteSpy = vi.spyOn(api.gateways.telegram, 'delete').mockResolvedValue(undefined);
    const onToast = vi.fn();

    render(<GatewaysPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-delete-bot')).not.toBeNull();
    });

    fireEvent.click(screen.getByTestId('btn-delete-bot'));

    await waitFor(() => {
      expect(deleteSpy).toHaveBeenCalledWith('acme', 'gw1');
      expect(onToast).toHaveBeenCalledWith('Telegram bot deleted');
    });
  });

  it('switches transport to webhook immediately when valid https URL is stored', async () => {
    mockGatewayApis({
      tgGateways: [gateway({ transport: 'long_polling', webhook_url: 'https://example.com/hook' })],
    });
    const updateSpy = vi
      .spyOn(api.gateways.telegram, 'update')
      .mockResolvedValue(gateway({ transport: 'webhook', webhook_url: 'https://example.com/hook' }));
    const onToast = vi.fn();

    render(<GatewaysPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('seg-webhook')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('seg-webhook'));

    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalledWith('acme', 'gw1', {
        transport: 'webhook',
        webhook_url: 'https://example.com/hook',
      });
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('Transport switched to webhook');
      expect(screen.getByTestId('input-webhook-url')).not.toBeNull();
    });
  });

  it('reveals inline URL input without immediate PUT when switching to webhook without stored URL, then saves', async () => {
    mockGatewayApis({
      tgGateways: [gateway({ transport: 'long_polling', webhook_url: '' })],
    });
    const updateSpy = vi
      .spyOn(api.gateways.telegram, 'update')
      .mockResolvedValue(gateway({ transport: 'webhook', webhook_url: 'https://example.com/api/v1/webhooks/telegram/gw1' }));
    const onToast = vi.fn();

    render(<GatewaysPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('seg-webhook')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('seg-webhook'));

    expect(updateSpy).not.toHaveBeenCalled();
    expect(screen.getByTestId('input-webhook-url')).not.toBeNull();

    fireEvent.click(screen.getByTestId('btn-save-webhook'));
    expect(screen.getByTestId('webhook-error').textContent).toContain('required');

    fireEvent.change(screen.getByTestId('input-webhook-url'), {
      target: { value: 'https://example.com/api/v1/webhooks/telegram/gw1' },
    });
    fireEvent.click(screen.getByTestId('btn-save-webhook'));

    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalledWith('acme', 'gw1', {
        transport: 'webhook',
        webhook_url: 'https://example.com/api/v1/webhooks/telegram/gw1',
      });
      expect(onToast).toHaveBeenCalledWith('Transport switched to webhook');
    });
  });

  it('enable and disable hit dedicated endpoints with account id', async () => {
    mockGatewayApis();
    const enableSpy = vi.spyOn(api.gateways.telegram, 'enable').mockResolvedValue(undefined);
    const disableSpy = vi.spyOn(api.gateways.telegram, 'disable').mockResolvedValue(undefined);

    render(<GatewaysPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByRole('switch', { name: 'Enable gateway' })).not.toBeNull();
    });
    fireEvent.click(screen.getByRole('switch', { name: 'Enable gateway' }));

    await waitFor(() => {
      expect(disableSpy).toHaveBeenCalledWith('acme', 'gw1');
    });

    await waitFor(() => {
      expect(screen.getByRole('switch', { name: 'Enable gateway' }).getAttribute('aria-checked')).toBe('false');
    });
    fireEvent.click(screen.getByRole('switch', { name: 'Enable gateway' }));

    await waitFor(() => {
      expect(enableSpy).toHaveBeenCalledWith('acme', 'gw1');
    });
  });

  it('lists group bindings displaying owning bot and unlinks via DELETE', async () => {
    mockGatewayApis({ bindings: bindings() });
    const removeSpy = vi.spyOn(api.gateways.telegram.bindings, 'remove').mockResolvedValue(undefined);
    const onToast = vi.fn();

    render(<GatewaysPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('binding-b1')).not.toBeNull();
    });
    expect(screen.getByTestId('binding-bot-b1').textContent).toBe('@onclaw_bot');
    const row = screen.getByTestId('binding-b1').textContent || '';
    expect(row).toContain('Ops');
    expect(row).toContain('-100123');
    expect(row).toContain('Beacon');

    fireEvent.click(screen.getByTestId('btn-unlink-b1'));

    await waitFor(() => {
      expect(removeSpy).toHaveBeenCalledWith('acme', 'b1');
      expect(onToast).toHaveBeenCalledWith('Ops unlinked');
      expect(screen.queryByTestId('binding-b1')).toBeNull();
    });
  });

  it('creates a group binding selecting owning bot, agent, and chat ID', async () => {
    mockGatewayApis({
      tgGateways: [gateway(), gateway2()],
    });
    const createSpy = vi.spyOn(api.gateways.telegram.bindings, 'create').mockResolvedValue({
      id: 'b2',
      gateway_id: 'gw2',
      platform: 'telegram',
      platform_chat_id: '-100999',
      chat_title: 'Support',
      agent_id: 'a_beacon',
      created_at: '',
    });
    const onToast = vi.fn();

    render(<GatewaysPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-open-add-binding')).not.toBeNull();
    });

    fireEvent.click(screen.getByTestId('btn-open-add-binding'));

    await waitFor(() => {
      expect(screen.getByTestId('modal-create-binding')).not.toBeNull();
    });

    fireEvent.change(screen.getByTestId('select-create-binding-bot'), { target: { value: 'gw2' } });
    fireEvent.change(screen.getByTestId('select-create-binding-agent'), { target: { value: 'a_beacon' } });
    fireEvent.change(screen.getByTestId('input-binding-chat-id'), { target: { value: '-100999' } });
    fireEvent.change(screen.getByTestId('input-binding-chat-title'), { target: { value: 'Support' } });

    fireEvent.click(screen.getByTestId('btn-submit-binding'));

    await waitFor(() => {
      expect(createSpy).toHaveBeenCalledWith('acme', {
        gateway_id: 'gw2',
        agent_id: 'a_beacon',
        platform_chat_id: '-100999',
        chat_title: 'Support',
      });
      expect(onToast).toHaveBeenCalledWith('Group bound successfully');
      expect(screen.queryByTestId('modal-create-binding')).toBeNull();
    });
  });

  it('shows the copyable bind command with bot username', async () => {
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
      expect(screen.getByTestId('pairing-countdown').textContent).toMatch(/Expires in \d{2}:\d{2}/);

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

  describe('whatsapp pane', () => {
    it('renders the idle unconfigured state behind the WhatsApp tab and allows opening wizard', async () => {
      mockGatewayApis();

      render(<GatewaysPane tenant={mockTenant} />);

      await waitFor(() => {
        expect(screen.getByTestId('gateway-admin-section')).not.toBeNull();
      });
      expect(screen.queryByTestId('wa-gateway-admin-section')).toBeNull();

      fireEvent.click(screen.getByTestId('tab-whatsapp'));

      await waitFor(() => {
        expect(screen.getByTestId('wa-unconfigured')).not.toBeNull();
      });
      expect(screen.getByTestId('btn-connect-wa')).not.toBeNull();
    });

    it('WhatsApp Connect Wizard creates account requiring lane and agent', async () => {
      mockGatewayApis();
      const createSpy = vi.spyOn(api.gateways.whatsapp, 'create').mockResolvedValue(waConfig());
      const onToast = vi.fn();

      render(<GatewaysPane tenant={mockTenant} onToast={onToast} />);
      await openWhatsAppTab();

      fireEvent.click(screen.getByTestId('btn-connect-wa'));

      await waitFor(() => {
        expect(screen.getByTestId('modal-whatsapp-wizard')).not.toBeNull();
      });

      // Clear agent
      fireEvent.change(screen.getByTestId('wizard-select-wa-agent'), { target: { value: '' } });
      fireEvent.click(screen.getByTestId('btn-submit-wa-wizard'));

      expect(screen.getByTestId('wizard-wa-agent-error').textContent).toContain('required');
      expect(screen.getByTestId('wizard-wa-cred-error').textContent).toContain('required');
      expect(createSpy).not.toHaveBeenCalled();

      // Fill in
      fireEvent.change(screen.getByTestId('wizard-select-wa-agent'), { target: { value: 'a_atlas' } });
      fireEvent.change(screen.getByTestId('wizard-input-wa-access-token'), { target: { value: 'EAAG123' } });
      fireEvent.change(screen.getByTestId('wizard-input-wa-phone-number-id'), { target: { value: '12345' } });
      fireEvent.change(screen.getByTestId('wizard-input-wa-app-secret'), { target: { value: 'secret' } });
      fireEvent.change(screen.getByTestId('wizard-input-wa-verify-token'), { target: { value: 'verify' } });

      fireEvent.click(screen.getByTestId('btn-submit-wa-wizard'));

      await waitFor(() => {
        expect(createSpy).toHaveBeenCalledWith('acme', {
          lane: 'cloud_api',
          agent_id: 'a_atlas',
          access_token: 'EAAG123',
          phone_number_id: '12345',
          app_secret: 'secret',
          verify_token: 'verify',
        });
        expect(onToast).toHaveBeenCalledWith('WhatsApp account connected');
        expect(screen.queryByTestId('modal-whatsapp-wizard')).toBeNull();
      });
    });

    it('renders the connected cloud lane: form, webhook block, and updates credentials', async () => {
      mockGatewayApis({
        waGateways: [waConfig()],
        waHealthMap: { wa1: { status: 'ok' } },
      });
      const updateSpy = vi
        .spyOn(api.gateways.whatsapp, 'update')
        .mockResolvedValue(waConfig());
      const onToast = vi.fn();

      render(<GatewaysPane tenant={mockTenant} onToast={onToast} />);
      await openWhatsAppTab();

      await waitFor(() => {
        expect(screen.getByTestId('wa-gateway-status').textContent).toBe('Connected');
      });
      expect(screen.getByTestId('wa-cloud-form')).not.toBeNull();
      expect(screen.getByTestId('wa-webhook-url').textContent).toBe(
        `${window.location.origin}/api/v1/webhooks/whatsapp/wa1`
      );

      fireEvent.change(screen.getByTestId('wa-input-access-token'), { target: { value: 'EAAG-new' } });
      fireEvent.click(screen.getByTestId('wa-btn-save'));

      await waitFor(() => {
        expect(updateSpy).toHaveBeenCalledWith('acme', 'wa1', {
          access_token: 'EAAG-new',
        });
        expect(onToast).toHaveBeenCalledWith('WhatsApp gateway updated');
      });
    });

    it('updates the bound agent for WhatsApp account', async () => {
      mockGatewayApis({ waGateways: [waConfig()] });
      const updateSpy = vi
        .spyOn(api.gateways.whatsapp, 'update')
        .mockResolvedValue(waConfig({ agent_id: 'a_beacon' }));

      render(<GatewaysPane tenant={mockTenant} />);
      await openWhatsAppTab();

      await waitFor(() => {
        expect(screen.getByTestId('wa-select-default-agent')).not.toBeNull();
      });
      fireEvent.change(screen.getByTestId('wa-select-default-agent'), { target: { value: 'a_beacon' } });

      await waitFor(() => {
        expect(updateSpy).toHaveBeenCalledWith('acme', 'wa1', { agent_id: 'a_beacon' });
      });
    });

    it('renders the md pairing state: ban-risk notice, QR, pair code, live status', async () => {
      mockGatewayApis({
        waGateways: [waConfig({ id: 'wa_md', lane: 'multi_device', has_credentials: false })],
        waHealthMap: { wa_md: { status: 'error', detail: 'device disconnected' } },
      });
      vi.spyOn(api.gateways.whatsapp.pairing, 'status').mockResolvedValue({
        status: 'waiting',
        qr_data_url: 'data:image/png;base64,qq',
        pair_code: '4821-9376',
      });
      const regenerateSpy = vi
        .spyOn(api.gateways.whatsapp.pairing, 'regenerate')
        .mockResolvedValue({ status: 'waiting', qr_data_url: 'data:image/png;base64,qq2', pair_code: '1111-2222' });

      render(<GatewaysPane tenant={mockTenant} />);
      await openWhatsAppTab();

      await waitFor(() => {
        expect(screen.getByTestId('wa-md-card')).not.toBeNull();
      });
      expect(screen.getByTestId('wa-md-warning').textContent).toContain('Meta may ban the account');
      const qr = await screen.findByTestId('wa-qr-image');
      expect((qr as HTMLImageElement).src).toContain('data:image/png');
      expect(screen.getByTestId('wa-pair-code').textContent).toBe('4821-9376');
      expect(screen.getByTestId('wa-pairing-status').textContent).toContain('Waiting for scan');

      fireEvent.click(screen.getByTestId('wa-btn-regenerate'));
      await waitFor(() => {
        expect(regenerateSpy).toHaveBeenCalledWith('acme', 'wa_md');
        expect(screen.getByTestId('wa-pair-code').textContent).toBe('1111-2222');
      });
    });

    it('deletes WhatsApp account', async () => {
      mockGatewayApis({ waGateways: [waConfig()] });
      const deleteSpy = vi.spyOn(api.gateways.whatsapp, 'delete').mockResolvedValue(undefined);
      const onToast = vi.fn();

      render(<GatewaysPane tenant={mockTenant} onToast={onToast} />);
      await openWhatsAppTab();

      await waitFor(() => {
        expect(screen.getByTestId('wa-btn-delete')).not.toBeNull();
      });

      fireEvent.click(screen.getByTestId('wa-btn-delete'));

      await waitFor(() => {
        expect(deleteSpy).toHaveBeenCalledWith('acme', 'wa1');
        expect(onToast).toHaveBeenCalledWith('WhatsApp account deleted');
      });
    });
  });

  describe('sidebar frame, status synchronization, and responsive collapse', () => {
    it('mobile chip row carries status dots and switches active platform', async () => {
      mockGatewayApis({
        tgGateways: [gateway({ enabled: false })],
        waGateways: [waConfig({ lane: 'cloud_api', enabled: true })],
        waHealthMap: { wa1: { status: 'ok' } },
      });

      render(<GatewaysPane tenant={mockTenant} />);

      await waitFor(() => {
        expect(screen.getByTestId('tab-telegram-gw1')).not.toBeNull();
        expect(screen.getByTestId('tab-whatsapp-wa1')).not.toBeNull();
      });

      expect(screen.getByTestId('tab-telegram-gw1').textContent).toContain('Paused');
      expect(screen.getByTestId('tab-whatsapp-wa1').textContent).toContain('Connected');

      fireEvent.click(screen.getByTestId('tab-whatsapp-wa1'));
      await waitFor(() => {
        expect(screen.getByTestId('wa-gateway-admin-section')).not.toBeNull();
      });

      fireEvent.click(screen.getByTestId('tab-telegram-gw1'));
      await waitFor(() => {
        expect(screen.getByTestId('gateway-admin-section')).not.toBeNull();
      });
    });

    it('problem-first default lands on the platform with an error', async () => {
      mockGatewayApis({
        tgGateways: [gateway({ enabled: true })],
        waGateways: [waConfig({ lane: 'cloud_api', enabled: true })],
        waHealthMap: { wa1: { status: 'error', detail: 'Token expired' } },
      });

      render(<GatewaysPane tenant={mockTenant} />);

      await waitFor(() => {
        expect(screen.getByTestId('wa-gateway-admin-section')).not.toBeNull();
      });
      expect(screen.getByTestId('wa-gateway-status').textContent).toBe('Error');
    });
  });
});
