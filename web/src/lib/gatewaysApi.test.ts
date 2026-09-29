import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  api,
  type ApiAgent,
  type ApiGatewayBinding,
  type ApiGatewayConfig,
  type ApiGatewayLink,
  type ApiWhatsAppGatewayConfig,
} from './api';

describe('lib/api gateway endpoints', () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  function mockJson(payload: unknown) {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Headers({ 'Content-Type': 'application/json' }),
      json: async () => payload,
    } as any);
  }

  function mockNoContent() {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 204,
      headers: new Headers(),
    } as any);
  }

  const tgGateway: ApiGatewayConfig = {
    id: 'gw1',
    workspace_id: 'acme',
    platform: 'telegram',
    identity: 'onclaw_bot',
    agent_id: 'a_atlas',
    bot_username: 'onclaw_bot',
    token_hint: 'ab12',
    enabled: true,
    transport: 'long_polling',
    webhook_url: '',
    status_error: null,
    created_at: '',
    updated_at: '',
  };

  describe('telegram gateway endpoints', () => {
    it('calls gateway list, get, create, update, and delete correctly', async () => {
      mockJson([tgGateway]);
      const listRes = await api.gateways.telegram.list('acme');
      expect(listRes).toEqual([tgGateway]);
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/telegram');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('GET');

      mockJson(tgGateway);
      const getRes = await api.gateways.telegram.get('acme', 'gw1');
      expect(getRes).toEqual(tgGateway);
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/telegram/gw1');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('GET');

      mockJson(tgGateway);
      const createRes = await api.gateways.telegram.create('acme', {
        token: '123:ABC',
        agent_id: 'a_atlas',
        transport: 'webhook',
        webhook_url: 'https://example.com/hook',
      });
      expect(createRes).toEqual(tgGateway);
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/telegram');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');
      expect((globalThis.fetch as any).mock.calls[0][1].body).toBe(
        JSON.stringify({
          token: '123:ABC',
          agent_id: 'a_atlas',
          transport: 'webhook',
          webhook_url: 'https://example.com/hook',
        })
      );

      mockJson({ ...tgGateway, agent_id: 'a_beacon' });
      const updateRes = await api.gateways.telegram.update('acme', 'gw1', {
        agent_id: 'a_beacon',
      });
      expect(updateRes.agent_id).toBe('a_beacon');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/telegram/gw1');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('PUT');
      expect((globalThis.fetch as any).mock.calls[0][1].body).toBe(
        JSON.stringify({ agent_id: 'a_beacon' })
      );

      mockNoContent();
      await api.gateways.telegram.delete('acme', 'gw1');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/telegram/gw1');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('DELETE');
    });

    it('calls enable, disable, and test correctly with account id', async () => {
      mockNoContent();
      await api.gateways.telegram.disable('acme', 'gw1');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/telegram/gw1/disable');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');

      mockNoContent();
      await api.gateways.telegram.enable('acme', 'gw1');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/telegram/gw1/enable');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');

      mockJson({ ok: true, bot_username: 'onclaw_bot' });
      await api.gateways.telegram.test('acme', 'gw1');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/telegram/gw1/test');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');
    });

    it('calls binding list, create, and delete correctly', async () => {
      const bindings: ApiGatewayBinding[] = [
        {
          id: 'b1',
          gateway_id: 'gw1',
          platform: 'telegram',
          platform_chat_id: '-100123',
          chat_title: 'Ops',
          agent_id: 'a_atlas',
          created_at: '',
        },
      ];
      mockJson(bindings);
      await api.gateways.telegram.bindings.list('acme');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/telegram/bindings');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('GET');

      mockJson(bindings[0]);
      await api.gateways.telegram.bindings.create('acme', {
        gateway_id: 'gw1',
        agent_id: 'a_atlas',
        platform_chat_id: '-100123',
        chat_title: 'Ops',
      });
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/telegram/bindings');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');

      mockNoContent();
      await api.gateways.telegram.bindings.remove('acme', 'b1');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe(
        '/api/v1/workspaces/acme/gateways/telegram/bindings/b1'
      );
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('DELETE');
    });

    it('calls pairing token generate/revoke and link unpair correctly', async () => {
      mockJson({ token: { token: 'pt_123', expires_at: '2026-09-12T13:00:00Z' } });
      await api.gateways.telegram.pairing.create('acme');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe(
        '/api/v1/workspaces/acme/gateways/telegram/pairing-tokens'
      );
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');

      mockNoContent();
      await api.gateways.telegram.pairing.revoke('acme', 'pt_123');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe(
        '/api/v1/workspaces/acme/gateways/telegram/pairing-tokens/pt_123'
      );
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('DELETE');

      const link: ApiGatewayLink = {
        platform_user_id: '593821092',
        username: 'onih',
        display_name: 'Oni',
        linked_at: '2026-09-12T00:00:00Z',
      };
      mockJson({ link });
      await api.gateways.telegram.links.getMine('acme');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe(
        '/api/v1/workspaces/acme/gateways/telegram/links/me'
      );
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('GET');

      mockNoContent();
      await api.gateways.telegram.links.removeMine('acme');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe(
        '/api/v1/workspaces/acme/gateways/telegram/links/me'
      );
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('DELETE');

      mockNoContent();
      await api.gateways.telegram.links.remove('acme', 'u_bob');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe(
        '/api/v1/workspaces/acme/gateways/telegram/links/u_bob'
      );
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('DELETE');
    });

    it('propagates 409 binding conflicts as ApiError conflict', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 409,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => ({ error: { code: 'conflict', message: 'That group is already bound' } }),
      } as any);

      await expect(
        api.gateways.telegram.bindings.create('acme', { gateway_id: 'gw1', agent_id: 'a_beacon', platform_chat_id: '-100123' })
      ).rejects.toMatchObject({ status: 409, code: 'conflict' });
    });
  });

  describe('whatsapp gateway endpoints', () => {
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

    it('calls list, get, create, update, delete correctly with four write-only cloud fields', async () => {
      mockJson([waConfig()]);
      await api.gateways.whatsapp.list('acme');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/whatsapp');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('GET');

      mockJson(waConfig());
      await api.gateways.whatsapp.get('acme', 'wa1');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/whatsapp/wa1');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('GET');

      mockJson(waConfig());
      await api.gateways.whatsapp.create('acme', {
        lane: 'cloud_api',
        agent_id: 'a_atlas',
        access_token: 'EAAG...',
        phone_number_id: '123456789012345',
        app_secret: 'shh',
        verify_token: 'vt',
      });
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/whatsapp');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');
      expect((globalThis.fetch as any).mock.calls[0][1].body).toBe(
        JSON.stringify({
          lane: 'cloud_api',
          agent_id: 'a_atlas',
          access_token: 'EAAG...',
          phone_number_id: '123456789012345',
          app_secret: 'shh',
          verify_token: 'vt',
        })
      );

      mockJson(waConfig({ agent_id: 'a_beacon' }));
      await api.gateways.whatsapp.update('acme', 'wa1', {
        agent_id: 'a_beacon',
      });
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/whatsapp/wa1');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('PUT');
      expect((globalThis.fetch as any).mock.calls[0][1].body).toBe(
        JSON.stringify({ agent_id: 'a_beacon' })
      );

      mockNoContent();
      await api.gateways.whatsapp.delete('acme', 'wa1');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/whatsapp/wa1');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('DELETE');
    });

    it('calls enable, disable, and health correctly with account id', async () => {
      mockNoContent();
      await api.gateways.whatsapp.disable('acme', 'wa1');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/whatsapp/wa1/disable');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');

      mockNoContent();
      await api.gateways.whatsapp.enable('acme', 'wa1');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/whatsapp/wa1/enable');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');

      mockJson({ status: 'ok' });
      await expect(api.gateways.whatsapp.health('acme', 'wa1')).resolves.toEqual({ status: 'ok' });
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/whatsapp/wa1/health');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('GET');
    });

    it('calls pairing start/status/regenerate/logout correctly with account id', async () => {
      const waiting = { status: 'waiting', qr_data_url: 'data:image/png;base64,qq', pair_code: '4821-9376' };
      mockJson(waiting);
      await expect(api.gateways.whatsapp.pairing.start('acme', 'wa1')).resolves.toEqual(waiting);
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe(
        '/api/v1/workspaces/acme/gateways/whatsapp/wa1/pairing/start'
      );
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');
      expect((globalThis.fetch as any).mock.calls[0][1].body).toBe(JSON.stringify({}));

      // Pair-code mode (design D12): the phone digits ride the body and the
      // server answers the 8-digit code.
      mockJson(waiting);
      await expect(api.gateways.whatsapp.pairing.start('acme', 'wa1', '6281234567890')).resolves.toEqual(waiting);
      expect((globalThis.fetch as any).mock.calls[0][1].body).toBe(
        JSON.stringify({ phone: '6281234567890' })
      );

      mockJson(waiting);
      await expect(api.gateways.whatsapp.pairing.status('acme', 'wa1')).resolves.toMatchObject({ status: 'waiting' });
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe(
        '/api/v1/workspaces/acme/gateways/whatsapp/wa1/pairing/status'
      );
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('GET');

      mockJson(waiting);
      await api.gateways.whatsapp.pairing.regenerate('acme', 'wa1');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe(
        '/api/v1/workspaces/acme/gateways/whatsapp/wa1/pairing/regenerate'
      );
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');

      mockJson({ status: 'logged_out' });
      await expect(api.gateways.whatsapp.pairing.logout('acme', 'wa1')).resolves.toEqual({ status: 'logged_out' });
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe(
        '/api/v1/workspaces/acme/gateways/whatsapp/wa1/pairing/logout'
      );
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');
    });

    it('calls member pairing tokens and the self link correctly', async () => {
      mockJson({ token: { token: 'wa_pt1', expires_at: '2026-09-14T13:00:00Z' } });
      await api.gateways.whatsapp.pairingTokens.create('acme');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe(
        '/api/v1/workspaces/acme/gateways/whatsapp/pairing-tokens'
      );
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');

      mockNoContent();
      await api.gateways.whatsapp.pairingTokens.revoke('acme', 'wa_pt1');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe(
        '/api/v1/workspaces/acme/gateways/whatsapp/pairing-tokens/wa_pt1'
      );
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('DELETE');

      const link: ApiGatewayLink = {
        platform_user_id: '6281234567890',
        username: null,
        display_name: 'Oni',
        linked_at: '2026-09-12T00:00:00Z',
      };
      mockJson({ link });
      await api.gateways.whatsapp.links.getMine('acme');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe(
        '/api/v1/workspaces/acme/gateways/whatsapp/links/me'
      );
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('GET');

      mockNoContent();
      await api.gateways.whatsapp.links.removeMine('acme');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe(
        '/api/v1/workspaces/acme/gateways/whatsapp/links/me'
      );
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('DELETE');
    });

    it('propagates 422 lane validation details as ApiError details', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 422,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => ({
          error: {
            code: 'invalid_request',
            message: 'invalid gateway configuration',
            details: [{ field: 'access_token', message: 'access_token is required' }],
          },
        }),
      } as any);

      await expect(api.gateways.whatsapp.create('acme', { lane: 'cloud_api', agent_id: 'a_atlas' })).rejects.toMatchObject({
        status: 422,
        details: [{ field: 'access_token', message: 'access_token is required' }],
      });
    });
  });

  it('agents.list stays reachable for the pane pickers', async () => {
    const agents: ApiAgent[] = [
      {
        id: 'a_atlas',
        workspace_id: 'acme',
        slug: 'atlas',
        name: 'Atlas',
        role: '',
        description: '',
        brief: '',
        identity: '',
        soul: '',
        provider_id: 'p1',
        model: 'm1',
        temperature: 1,
        autonomy: 'approval',
        disabled_tools: [],
        enabled_mcps: [],
        avatar: {},
        prompts_status: 'ready',
        created_at: '',
        updated_at: '',
      },
    ];
    mockJson({ agents });
    await expect(api.agents.list('acme')).resolves.toEqual({ agents });
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/agents');
  });
});
