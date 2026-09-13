import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  api,
  type ApiAgent,
  type ApiGatewayBinding,
  type ApiGatewayConfig,
  type ApiGatewayLink,
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

  const gateway: ApiGatewayConfig = {
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
  };

  it('calls gateway config CRUD correctly', async () => {
    mockJson({ gateway: null });
    await api.gateways.telegram.getConfig('acme');
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/telegram');
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('GET');

    mockJson({ gateway });
    await api.gateways.telegram.updateConfig('acme', {
      token: '123:ABC',
      default_agent_id: 'a_atlas',
      transport: 'webhook',
      webhook_url: 'https://example.com/hook',
    });
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/telegram');
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('PUT');
    expect((globalThis.fetch as any).mock.calls[0][1].body).toBe(
      JSON.stringify({
        token: '123:ABC',
        default_agent_id: 'a_atlas',
        transport: 'webhook',
        webhook_url: 'https://example.com/hook',
      })
    );
  });

  it('calls enable, disable, and test correctly', async () => {
    mockJson({ gateway: { ...gateway, enabled: false } });
    await api.gateways.telegram.disable('acme');
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/telegram/disable');
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');

    mockJson({ gateway });
    await api.gateways.telegram.enable('acme');
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/telegram/enable');
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');

    mockJson({ ok: true, bot_username: 'onclaw_bot' });
    await api.gateways.telegram.test('acme');
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/telegram/test');
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');
  });

  it('calls binding list, create, and delete correctly', async () => {
    const bindings: ApiGatewayBinding[] = [
      {
        id: 'b1',
        platform: 'telegram',
        platform_chat_id: '-100123',
        chat_title: 'Ops',
        agent_id: 'a_atlas',
        created_at: '',
      },
    ];
    mockJson({ bindings });
    await api.gateways.telegram.bindings.list('acme');
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/gateways/telegram/bindings');
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('GET');

    mockJson({ binding: bindings[0] });
    await api.gateways.telegram.bindings.create('acme', {
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
      api.gateways.telegram.bindings.create('acme', { agent_id: 'a_beacon', platform_chat_id: '-100123' })
    ).rejects.toMatchObject({ status: 409, code: 'conflict' });
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
        bootstrap: '',
        provider_id: 'p1',
        model: 'm1',
        temperature: 1,
        autonomy: 'approval',
        tools: [],
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
