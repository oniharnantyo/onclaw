import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  api,
  getToken,
  setToken,
  clearToken,
  ApiError,
  formatApiError,
  listAgentSessions,
  deleteAgentSession,
  TOKEN_STORAGE_KEY,
} from './api';
import { useConnectionStore } from '../store/connection';
import { useStore } from '../store';

// This environment's jsdom exposes no localStorage (opaque origin — same mode
// behind the other store suites); install a minimal stub so token handling and
// the store import work.
const backing = new Map<string, string>();
if (typeof (globalThis as any).localStorage === 'undefined' || true) {
  (globalThis as any).localStorage = {
    getItem: (k: string) => (backing.has(k) ? backing.get(k)! : null),
    setItem: (k: string, v: string) => void backing.set(k, String(v)),
    removeItem: (k: string) => void backing.delete(k),
    clear: () => void backing.clear(),
    key: (i: number) => Array.from(backing.keys())[i] ?? null,
    get length() { return backing.size; },
  };
}

describe('lib/api', () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  describe('Token management', () => {
    it('sets, gets, and clears token in localStorage', () => {
      expect(getToken()).toBeNull();
      setToken('test-token-123');
      expect(getToken()).toBe('test-token-123');
      expect(localStorage.getItem(TOKEN_STORAGE_KEY)).toBe('test-token-123');
      clearToken();
      expect(getToken()).toBeNull();
    });
  });

  describe('Request handling & headers', () => {
    it('attaches Authorization header when token exists', async () => {
      setToken('jwt-abc-123');
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => ({ status: 'ok' }),
      } as any);

      const res = await api.request('/test');
      expect(res).toEqual({ status: 'ok' });

      expect(globalThis.fetch).toHaveBeenCalledTimes(1);
      const [url, init] = (globalThis.fetch as any).mock.calls[0];
      expect(url).toBe('/api/v1/test');
      expect((init.headers as Headers).get('Authorization')).toBe('Bearer jwt-abc-123');
      expect((init.headers as Headers).get('Content-Type')).toBeNull();
    });

    it('sets Content-Type: application/json for object bodies', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => ({ id: '1' }),
      } as any);

      await api.request('/test', {
        method: 'POST',
        body: { name: 'Acme' },
      });

      const [, init] = (globalThis.fetch as any).mock.calls[0];
      expect((init.headers as Headers).get('Content-Type')).toBe('application/json');
      expect(init.body).toBe(JSON.stringify({ name: 'Acme' }));
    });

    it('handles 204 No Content without parsing JSON', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 204,
        headers: new Headers(),
      } as any);

      const res = await api.request('/test', { method: 'DELETE' });
      expect(res).toBeUndefined();
    });

    it('skips Authorization header when skipAuth is true', async () => {
      setToken('jwt-abc-123');
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => ({ token: 'new-token' }),
      } as any);

      await api.request('/auth/login', {
        method: 'POST',
        body: { email: 'a@b.com', password: 'pass' },
        skipAuth: true,
      });

      const [, init] = (globalThis.fetch as any).mock.calls[0];
      expect((init.headers as Headers).get('Authorization')).toBeNull();
    });
  });

  describe('Error handling & envelope parsing', () => {
    it('parses error envelope with code, message, and details', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 409,
        statusText: 'Conflict',
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => ({
          error: {
            code: 'conflict',
            message: 'Workspace slug already exists',
            details: [{ field: 'slug', message: 'already taken' }],
          },
        }),
      } as any);

      await expect(api.request('/workspaces', { method: 'POST', body: { slug: 'acme' } })).rejects.toSatisfy(
        (err: any) => {
          expect(err).toBeInstanceOf(ApiError);
          expect(err.status).toBe(409);
          expect(err.code).toBe('conflict');
          expect(err.message).toBe('Workspace slug already exists');
          expect(err.details).toEqual([{ field: 'slug', message: 'already taken' }]);
          return true;
        }
      );
    });

    it('handles network failure as ApiError with code "network"', async () => {
      globalThis.fetch = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'));

      await expect(api.request('/auth/me')).rejects.toSatisfy((err: any) => {
        expect(err).toBeInstanceOf(ApiError);
        expect(err.status).toBe(0);
        expect(err.code).toBe('network');
        expect(err.message).toContain('Network connection failed');
        return true;
      });
    });

    it('invokes onUnauthorized and clears token on 401 response', async () => {
      setToken('expired-token');
      const unauthorizedCallback = vi.fn();
      const unsubscribe = api.onUnauthorized(unauthorizedCallback);

      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 401,
        statusText: 'Unauthorized',
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => ({
          error: {
            code: 'unauthenticated',
            message: 'token expired',
          },
        }),
      } as any);

      await expect(api.auth.me()).rejects.toBeInstanceOf(ApiError);

      expect(unauthorizedCallback).toHaveBeenCalledTimes(1);
      expect(getToken()).toBeNull();

      unsubscribe();
    });

    it('does not trigger onUnauthorized for /auth/login failures', async () => {
      const unauthorizedCallback = vi.fn();
      const unsubscribe = api.onUnauthorized(unauthorizedCallback);

      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 401,
        statusText: 'Unauthorized',
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => ({
          error: {
            code: 'unauthenticated',
            message: 'invalid email or password',
          },
        }),
      } as any);

      await expect(api.auth.login({ email: 'bad@user.com', password: 'bad' })).rejects.toBeInstanceOf(ApiError);

      expect(unauthorizedCallback).not.toHaveBeenCalled();
      unsubscribe();
    });

    it('captures request_id from error envelope into ApiError.requestId', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        statusText: 'Internal Server Error',
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => ({
          error: {
            code: 'internal',
            message: 'Database failure',
            request_id: 'req_envelope_12345',
          },
        }),
      } as any);

      await expect(api.request('/test')).rejects.toSatisfy((err: any) => {
        expect(err).toBeInstanceOf(ApiError);
        expect(err.status).toBe(500);
        expect(err.code).toBe('internal');
        expect(err.requestId).toBe('req_envelope_12345');
        return true;
      });
    });

    it('captures request_id from X-Request-ID response header if omitted from error envelope', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        statusText: 'Internal Server Error',
        headers: new Headers({
          'Content-Type': 'application/json',
          'X-Request-ID': 'req_header_67890',
        }),
        json: async () => ({
          error: {
            code: 'internal',
            message: 'Server error',
          },
        }),
      } as any);

      await expect(api.request('/test')).rejects.toSatisfy((err: any) => {
        expect(err).toBeInstanceOf(ApiError);
        expect(err.status).toBe(500);
        expect(err.requestId).toBe('req_header_67890');
        return true;
      });
    });

    it('sets connection store degraded to true on network failure and false on success', async () => {
      useConnectionStore.getState().reset();
      expect(useConnectionStore.getState().degraded).toBe(false);

      globalThis.fetch = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'));

      await expect(api.request('/test')).rejects.toThrow();
      expect(useConnectionStore.getState().degraded).toBe(true);

      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => ({ ok: true }),
      } as any);

      await api.request('/test');
      expect(useConnectionStore.getState().degraded).toBe(false);
    });

    it('classifies status 0 as network error and sets connection degraded', async () => {
      useConnectionStore.getState().reset();

      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 0,
        statusText: '',
        headers: new Headers(),
      } as any);

      await expect(api.request('/test')).rejects.toSatisfy((err: any) => {
        expect(err).toBeInstanceOf(ApiError);
        expect(err.status).toBe(0);
        expect(err.code).toBe('network');
        return true;
      });

      expect(useConnectionStore.getState().degraded).toBe(true);
    });

    it('suppresses network toasts while connection store is degraded', () => {
      useStore.setState({ ui: { ...useStore.getState().ui, toasts: [] } });

      // When healthy, network toast is accepted
      useConnectionStore.getState().reset();
      useStore.getState().toast('Network connection failed. Please check your connection.', 'danger');
      expect(useStore.getState().ui.toasts.length).toBe(1);

      // Clear toasts and degrade connection
      useStore.setState({ ui: { ...useStore.getState().ui, toasts: [] } });
      useConnectionStore.getState().reportFailure();

      // Now network toasts should be suppressed
      useStore.getState().toast('Network connection failed. Please check your connection.', 'danger');
      expect(useStore.getState().ui.toasts.length).toBe(0);

      useStore.getState().toast('Network error occurred', 'network');
      expect(useStore.getState().ui.toasts.length).toBe(0);

      // Non-network toast is still displayed
      useStore.getState().toast('Settings saved', 'success');
      expect(useStore.getState().ui.toasts.length).toBe(1);
    });
  });

  describe('formatApiError', () => {
    it('maps known error codes correctly', () => {
      expect(formatApiError(new ApiError(409, 'last_owner_protected', 'server msg'))).toBe(
        'The last owner cannot be removed or demoted.'
      );
      expect(formatApiError(new ApiError(403, 'forbidden', 'Custom forbidden'))).toBe(
        'Custom forbidden'
      );
      expect(formatApiError(new ApiError(401, 'unauthenticated', 'unauthenticated'))).toBe(
        'Invalid email or password.'
      );
      expect(formatApiError(new ApiError(0, 'network', 'Network error'))).toBe(
        'Network connection failed. Please check your connection.'
      );
    });

    it('formats fetch TypeErrors as network errors', () => {
      expect(formatApiError(new TypeError('Failed to fetch'))).toBe(
        'Network connection failed. Please check your connection.'
      );
    });

    it('falls back to error message or default fallback', () => {
      expect(formatApiError(new Error('Something blew up'))).toBe('Something blew up');
      expect(formatApiError('plain string')).toBe('An unexpected error occurred');
    });
  });

  describe('Resource endpoints mapping', () => {
    it('calls auth endpoints correctly', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => ({ token: 't1', user: { id: 'u1' } }),
      } as any);

      await api.auth.login({ email: 'user@example.com', password: 'password123' });
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/auth/login');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');

      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 204,
        headers: new Headers(),
      } as any);
      await api.auth.logout();
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/auth/logout');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');

      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => ({ user: { id: 'u1' }, memberships: [] }),
      } as any);
      await api.auth.me();
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/auth/me');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('GET');
    });

    it('calls workspaces and members endpoints correctly', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => ({ workspaces: [] }),
      } as any);
      await api.workspaces.list();
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces');

      await api.workspaces.create({ name: 'Acme', slug: 'acme', timezone: 'UTC' });
      expect((globalThis.fetch as any).mock.calls[1][0]).toBe('/api/v1/workspaces');
      expect((globalThis.fetch as any).mock.calls[1][1].method).toBe('POST');

      await api.workspaces.get('acme');
      expect((globalThis.fetch as any).mock.calls[2][0]).toBe('/api/v1/workspaces/acme');

      await api.workspaces.patch('acme', { name: 'Acme Inc' });
      expect((globalThis.fetch as any).mock.calls[3][0]).toBe('/api/v1/workspaces/acme');
      expect((globalThis.fetch as any).mock.calls[3][1].method).toBe('PATCH');

      await api.members.list('acme');
      expect((globalThis.fetch as any).mock.calls[4][0]).toBe('/api/v1/workspaces/acme/members');

      await api.members.add('acme', { email: 'bob@acme.com', role_id: 'r-admin' });
      expect((globalThis.fetch as any).mock.calls[5][0]).toBe('/api/v1/workspaces/acme/members');
      expect((globalThis.fetch as any).mock.calls[5][1].method).toBe('POST');

      await api.members.patch('acme', 'u2', { role_id: 'r-member' });
      expect((globalThis.fetch as any).mock.calls[6][0]).toBe('/api/v1/workspaces/acme/members/u2');
      expect((globalThis.fetch as any).mock.calls[6][1].method).toBe('PATCH');

      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 204,
        headers: new Headers(),
      } as any);
      await api.members.remove('acme', 'u2');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/members/u2');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('DELETE');
    });

    it('calls admin endpoints correctly', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => ({ workspaces: [] }),
      } as any);

      await api.admin.workspaces.list();
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/admin/workspaces');

      await api.admin.workspaces.patch('acme', { name: 'Acme Renamed', timezone: 'UTC' });
      expect((globalThis.fetch as any).mock.calls[1][0]).toBe('/api/v1/admin/workspaces/acme');
      expect((globalThis.fetch as any).mock.calls[1][1].method).toBe('PATCH');

      await api.admin.workspaces.transferOwner('acme', { user_id: 'u2' });
      expect((globalThis.fetch as any).mock.calls[2][0]).toBe('/api/v1/admin/workspaces/acme/owner');
      expect((globalThis.fetch as any).mock.calls[2][1].method).toBe('PATCH');

      await api.admin.workspaces.listMembers('acme');
      expect((globalThis.fetch as any).mock.calls[3][0]).toBe('/api/v1/admin/workspaces/acme/members');

      await api.admin.workspaces.addMember('acme', { user_id: 'u3', role_name: 'Admin' });
      expect((globalThis.fetch as any).mock.calls[4][0]).toBe('/api/v1/admin/workspaces/acme/members');
      expect((globalThis.fetch as any).mock.calls[4][1].method).toBe('POST');

      await api.admin.workspaces.disable('acme');
      expect((globalThis.fetch as any).mock.calls[5][0]).toBe('/api/v1/admin/workspaces/acme/disable');
      expect((globalThis.fetch as any).mock.calls[5][1].method).toBe('POST');

      await api.admin.users.list();
      expect((globalThis.fetch as any).mock.calls[6][0]).toBe('/api/v1/admin/users');

      await api.admin.superadmins.grant({ email: 'super@acme.com' });
      expect((globalThis.fetch as any).mock.calls[7][0]).toBe('/api/v1/admin/superadmins');
      expect((globalThis.fetch as any).mock.calls[7][1].method).toBe('POST');
    });

    it('calls providers endpoints correctly', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => ({ providers: [] }),
      } as any);

      await api.providers.list('acme');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/providers');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('GET');

      await api.providers.create('acme', {
        type: 'openai',
        name: 'OpenAI Main',
        key: 'sk-test',
      });
      expect((globalThis.fetch as any).mock.calls[1][0]).toBe('/api/v1/workspaces/acme/providers');
      expect((globalThis.fetch as any).mock.calls[1][1].method).toBe('POST');
      expect(JSON.parse((globalThis.fetch as any).mock.calls[1][1].body)).toEqual({
        type: 'openai',
        name: 'OpenAI Main',
        key: 'sk-test',
      });

      await api.providers.patch('acme', 'prov-1', {
        name: 'OpenAI Updated',
        enabled: false,
      });
      expect((globalThis.fetch as any).mock.calls[2][0]).toBe('/api/v1/workspaces/acme/providers/prov-1');
      expect((globalThis.fetch as any).mock.calls[2][1].method).toBe('PATCH');

      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 204,
        headers: new Headers(),
      } as any);
      await api.providers.delete('acme', 'prov-1');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/providers/prov-1');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('DELETE');

      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => ({ ok: true }),
      } as any);
      const verifyRes = await api.providers.verify('acme', 'prov-1');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/providers/prov-1/verify');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');
      expect(verifyRes).toEqual({ ok: true });

      // models endpoint
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => ({ source: 'live', models: [{ id: 'claude-3-7-sonnet', name: 'Claude 3.7 Sonnet' }] }),
      } as any);
      const modelsRes = await api.providers.models('acme', 'prov-1');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/providers/prov-1/models');
      expect(modelsRes.source).toBe('live');

      // modelsPreview endpoint
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => ({ source: 'catalog', models: [{ id: 'gpt-4o', name: 'GPT-4o' }] }),
      } as any);
      const previewRes = await api.providers.modelsPreview({ type: 'openai', key: 'sk-test' });
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/providers/models-preview');
      expect(previewRes.source).toBe('catalog');
    });

    it('calls agents endpoints correctly', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => ({ agents: [] }),
      } as any);

      await api.agents.list('acme');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/agents');

      await api.agents.get('acme', 'radar');
      expect((globalThis.fetch as any).mock.calls[1][0]).toBe('/api/v1/workspaces/acme/agents/radar');

      await api.agents.create('acme', {
        name: 'Radar',
        slug: 'radar',
        role: 'Research',
        brief: 'Do research',
        model: 'gpt-4o',
      });
      expect((globalThis.fetch as any).mock.calls[2][0]).toBe('/api/v1/workspaces/acme/agents');
      expect((globalThis.fetch as any).mock.calls[2][1].method).toBe('POST');

      await api.agents.patch('acme', 'radar', { name: 'Radar 2' });
      expect((globalThis.fetch as any).mock.calls[3][0]).toBe('/api/v1/workspaces/acme/agents/radar');
      expect((globalThis.fetch as any).mock.calls[3][1].method).toBe('PATCH');

      await api.agents.regenerate('acme', 'radar');
      expect((globalThis.fetch as any).mock.calls[4][0]).toBe('/api/v1/workspaces/acme/agents/radar/regenerate');
      expect((globalThis.fetch as any).mock.calls[4][1].method).toBe('POST');

      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 204,
        headers: new Headers(),
      } as any);
      await api.agents.delete('acme', 'radar');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/agents/radar');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('DELETE');
    });

    it('calls skills endpoints correctly', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => ({ skills: [] }),
      } as any);

      await api.skills.list('acme');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/skills');

      await api.skills.get('acme', 'sk-1');
      expect((globalThis.fetch as any).mock.calls[1][0]).toBe('/api/v1/workspaces/acme/skills/sk-1');

      await api.skills.create('acme', { source: 'authored', name: 'search', body: '...' });
      expect((globalThis.fetch as any).mock.calls[2][0]).toBe('/api/v1/workspaces/acme/skills');
      expect((globalThis.fetch as any).mock.calls[2][1].method).toBe('POST');

      await api.skills.setEnabled('acme', 'sk-1', false);
      expect((globalThis.fetch as any).mock.calls[3][0]).toBe('/api/v1/workspaces/acme/skills/sk-1');
      expect((globalThis.fetch as any).mock.calls[3][1].method).toBe('PATCH');

      await api.skills.recheck('acme', 'sk-1');
      expect((globalThis.fetch as any).mock.calls[4][0]).toBe('/api/v1/workspaces/acme/skills/sk-1/dependencies/recheck');
      expect((globalThis.fetch as any).mock.calls[4][1].method).toBe('POST');

      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 204,
        headers: new Headers(),
      } as any);
      await api.skills.uninstall('acme', 'sk-1');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/skills/sk-1');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('DELETE');
    });

    it('polls prompt status until ready or failed', async () => {
      const responses = [
        { agent: { id: 'a1', slug: 'a1', prompts_status: 'generating' } },
        { agent: { id: 'a1', slug: 'a1', prompts_status: 'generating' } },
        { agent: { id: 'a1', slug: 'a1', prompts_status: 'ready', identity: 'I am ready' } },
      ];
      let callCount = 0;
      globalThis.fetch = vi.fn().mockImplementation(async () => ({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => responses[Math.min(callCount++, responses.length - 1)],
      }));

      const { pollAgentPromptsStatus } = await import('./api');
      const onUpdate = vi.fn();

      const finalAgent = await pollAgentPromptsStatus('acme', 'a1', {
        intervalMs: 10,
        maxAttempts: 10,
        onUpdate,
      });

      expect(finalAgent.prompts_status).toBe('ready');
      expect(onUpdate).toHaveBeenCalledTimes(3);
    });
  });

  describe('Agent session index endpoints (agent-session-index)', () => {
    it('lists agent sessions via GET /workspaces/:slug/agents/:agent/sessions', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => ({
          sessions: [
            { id: 'row-1', session_id: 'sess_b', title: 'Fix the login bug', created_at: '2026-01-01T00:00:00Z', last_active_at: '2026-01-02T00:00:00Z', running: false },
            { id: 'row-2', session_id: 'sess_a', title: 'Old chat', created_at: '2025-12-01T00:00:00Z', last_active_at: '2025-12-31T00:00:00Z', running: true },
          ],
        }),
      } as any);

      const res = await listAgentSessions('acme', 'atlas');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/agents/atlas/sessions');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('GET');
      expect(res.sessions).toHaveLength(2);
      expect(res.sessions[0].session_id).toBe('sess_b');
      expect(res.sessions[1].running).toBe(true);
    });

    it('soft-deletes a session via DELETE and tolerates the 204', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 204,
        headers: new Headers(),
      } as any);

      await expect(deleteAgentSession('acme', 'atlas', 'sess_1')).resolves.toBeUndefined();
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/agents/atlas/sessions/sess_1');
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('DELETE');
    });

    it('maps auth/permission/absence failures through the shared error envelope', async () => {
      for (const [status, code] of [[401, 'unauthenticated'], [403, 'forbidden'], [404, 'not_found']] as const) {
        globalThis.fetch = vi.fn().mockResolvedValue({
          ok: false,
          status,
          statusText: 'Err',
          headers: new Headers({ 'Content-Type': 'application/json' }),
          json: async () => ({ error: { code, message: 'boom' } }),
        } as any);

        await expect(listAgentSessions('acme', 'atlas')).rejects.toSatisfy((err: any) => {
          expect(err).toBeInstanceOf(ApiError);
          expect(err.status).toBe(status);
          expect(err.code).toBe(code);
          return true;
        });
      }
    });
  });

  describe('MCP OAuth flow endpoints (add-mcp-oauth-client)', () => {
    const okJson = (payload: any) =>
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: async () => payload,
      } as any);

    it('posts the browser authorize begin and returns the provider URL', async () => {
      globalThis.fetch = okJson({ authorize_url: 'https://auth.example.com/authorize?x=1' });

      const res = await api.mcp.authorize('acme', 'srv-1');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe(
        '/api/v1/workspaces/acme/mcp-servers/srv-1/oauth/authorize'
      );
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');
      expect(res.authorize_url).toContain('authorize');
    });

    it('begins the device flow and echoes the sealed session to the poll', async () => {
      globalThis.fetch = okJson({
        device_session: 'sealed-blob',
        user_code: 'WDJB-MJHT',
        verification_uri: 'https://example.com/activate',
        verification_uri_complete: 'https://example.com/activate?user_code=WDJB-MJHT',
        expires_in: 600,
        interval: 5,
      });

      const begin = await api.mcp.beginDevice('acme', 'srv-1');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe(
        '/api/v1/workspaces/acme/mcp-servers/srv-1/oauth/device'
      );
      expect(begin.user_code).toBe('WDJB-MJHT');
      expect(begin.interval).toBe(5);

      globalThis.fetch = okJson({ status: 'pending' });
      const poll = await api.mcp.pollDevice('acme', 'srv-1', 'sealed-blob');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe(
        '/api/v1/workspaces/acme/mcp-servers/srv-1/oauth/device/poll'
      );
      expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');
      expect(JSON.parse((globalThis.fetch as any).mock.calls[0][1].body)).toEqual({
        device_session: 'sealed-blob',
      });
      expect(poll.status).toBe('pending');
    });

    it('mirrors the agent-private scope under /agents/:agent/mcp-servers', async () => {
      globalThis.fetch = okJson({ authorize_url: 'https://auth.example.com/authorize?x=2' });
      await api.agents.authorizeMcpServer('acme', 'atlas', 'srv-1');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe(
        '/api/v1/workspaces/acme/agents/atlas/mcp-servers/srv-1/oauth/authorize'
      );

      globalThis.fetch = okJson({
        device_session: 's2',
        user_code: 'ABCD-EFGH',
        verification_uri: 'https://example.com/activate',
        expires_in: 600,
        interval: 5,
      });
      await api.agents.beginDeviceMcpServer('acme', 'atlas', 'srv-1');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe(
        '/api/v1/workspaces/acme/agents/atlas/mcp-servers/srv-1/oauth/device'
      );

      globalThis.fetch = okJson({ status: 'slow_down' });
      await api.agents.pollDeviceMcpServer('acme', 'atlas', 'srv-1', 's2');
      expect((globalThis.fetch as any).mock.calls[0][0]).toBe(
        '/api/v1/workspaces/acme/agents/atlas/mcp-servers/srv-1/oauth/device/poll'
      );
      expect(JSON.parse((globalThis.fetch as any).mock.calls[0][1].body)).toEqual({
        device_session: 's2',
      });
    });
  });
});


