import { describe, it, expect, beforeEach, vi } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useAuthStore, useIsAdmin } from './auth';
import { useStore } from './index';
import { api, getToken, setToken, ApiError } from '../lib/api';

describe('store/auth', () => {
  beforeEach(() => {
    localStorage.clear();
    useAuthStore.setState({
      user: null,
      memberships: [],
      status: 'loading',
    });
    vi.restoreAllMocks();
  });

  it('boot() with no token transitions to unauthenticated', async () => {
    await useAuthStore.getState().boot();

    const state = useAuthStore.getState();
    expect(state.status).toBe('unauthenticated');
    expect(state.user).toBeNull();
    expect(state.memberships).toEqual([]);
  });

  it('boot() with valid token hydrates session and sets authenticated', async () => {
    setToken('valid-token');
    const mockUser = { id: 'u1', email: 'test@example.com', name: 'Test User', created_at: '', updated_at: '' };
    const mockMemberships = [
      {
        workspace_id: 'ws1',
        workspace_slug: 'acme',
        workspace_name: 'Acme',
        user_id: 'u1',
        email: 'test@example.com',
        name: 'Test User',
        role_id: 'r1',
        role_name: 'Owner',
        joined_at: '',
      },
    ];

    vi.spyOn(api.auth, 'me').mockResolvedValue({
      user: mockUser,
      memberships: mockMemberships,
    });

    await useAuthStore.getState().boot();

    const state = useAuthStore.getState();
    expect(state.status).toBe('authenticated');
    expect(state.user).toEqual(mockUser);
    expect(state.memberships).toEqual(mockMemberships);
  });

  it('boot() with invalid/expired token clears token and transitions to unauthenticated', async () => {
    setToken('invalid-token');
    vi.spyOn(api.auth, 'me').mockRejectedValue(new ApiError(401, 'unauthenticated', 'Unauthorized'));

    await useAuthStore.getState().boot();

    const state = useAuthStore.getState();
    expect(state.status).toBe('unauthenticated');
    expect(state.user).toBeNull();
    expect(getToken()).toBeNull();
  });

  it('boot() with network error preserves token and sets authenticated status', async () => {
    setToken('saved-token');
    vi.spyOn(api.auth, 'me').mockRejectedValue(new ApiError(0, 'network', 'Network connection failed'));

    await useAuthStore.getState().boot();

    const state = useAuthStore.getState();
    expect(state.status).toBe('authenticated');
    expect(getToken()).toBe('saved-token');
  });

  it('login() saves token, hydrates session, and sets authenticated', async () => {
    const mockUser = { id: 'u1', email: 'user@example.com', name: 'User', created_at: '', updated_at: '' };
    const mockMemberships = [
      {
        workspace_id: 'ws1',
        workspace_slug: 'acme',
        workspace_name: 'Acme',
        user_id: 'u1',
        email: 'user@example.com',
        name: 'User',
        role_id: 'r1',
        role_name: 'Owner',
        joined_at: '',
      },
    ];

    vi.spyOn(api.auth, 'login').mockResolvedValue({
      token: 'issued-token-123',
      user: mockUser,
    });
    vi.spyOn(api.auth, 'me').mockResolvedValue({
      user: mockUser,
      memberships: mockMemberships,
    });

    await useAuthStore.getState().login('user@example.com', 'password123');

    const state = useAuthStore.getState();
    expect(state.status).toBe('authenticated');
    expect(state.user).toEqual(mockUser);
    expect(state.memberships).toEqual(mockMemberships);
    expect(getToken()).toBe('issued-token-123');
  });

  it('login() failure clears token and throws error', async () => {
    vi.spyOn(api.auth, 'login').mockRejectedValue(new Error('Invalid email or password'));

    await expect(useAuthStore.getState().login('bad@example.com', 'wrongpass')).rejects.toThrow();

    const state = useAuthStore.getState();
    expect(state.status).toBe('unauthenticated');
    expect(state.user).toBeNull();
    expect(getToken()).toBeNull();
  });

  it('logout() clears session and token', async () => {
    setToken('my-token');
    useAuthStore.setState({
      user: { id: 'u1', email: 'u@example.com', name: 'U', created_at: '', updated_at: '' },
      memberships: [],
      status: 'authenticated',
    });

    vi.spyOn(api.auth, 'logout').mockResolvedValue(undefined);

    await useAuthStore.getState().logout();

    const state = useAuthStore.getState();
    expect(state.status).toBe('unauthenticated');
    expect(state.user).toBeNull();
    expect(getToken()).toBeNull();
  });

  describe('useIsAdmin', () => {
    const mockAdminUser = {
      id: 'u_super',
      email: 'admin@master.dev',
      name: 'Super Admin',
      created_at: '',
      updated_at: '',
    };

    const mockMasterMembership = {
      workspace_id: 'master',
      workspace_slug: 'master',
      workspace_name: 'Master Control',
      user_id: 'u_super',
      email: 'admin@master.dev',
      name: 'Super Admin',
      role_id: 'r_superadmin',
      role_name: 'Superadmin',
      role: {
        id: 'r_superadmin',
        workspace_id: 'master',
        name: 'Superadmin',
        is_owner: true,
        permissions: ['*'],
        built_in: true,
        created_at: '',
      },
      workspace: {
        id: 'master',
        slug: 'master',
        name: 'Master Control',
        timezone: 'UTC',
        is_master: true,
        created_at: '',
        updated_at: '',
      },
      joined_at: '',
    };

    it('returns false when unauthenticated', () => {
      useAuthStore.setState({ user: null, memberships: [] });
      useStore.setState({ pos: { tenantId: 'acme', view: 'chats', chatId: '', showContext: false } });

      const { result } = renderHook(() => useIsAdmin());
      expect(result.current).toBe(false);
    });

    it('returns false when authenticated in a non-master workspace', () => {
      useAuthStore.setState({
        user: { id: 'u_user', email: 'user@acme.dev', name: 'User', created_at: '', updated_at: '' },
        memberships: [
          {
            workspace_id: 'acme',
            workspace_slug: 'acme',
            workspace_name: 'Acme Corp',
            user_id: 'u_user',
            email: 'user@acme.dev',
            name: 'User',
            role_id: 'r_admin',
            role_name: 'Admin',
            role: {
              id: 'r_admin',
              workspace_id: 'acme',
              name: 'Admin',
              is_owner: false,
              permissions: ['workspace.*'],
              built_in: true,
              created_at: '',
            },
            workspace: {
              id: 'acme',
              slug: 'acme',
              name: 'Acme Corp',
              timezone: 'UTC',
              is_master: false,
              created_at: '',
              updated_at: '',
            },
            joined_at: '',
          },
        ],
      });
      useStore.setState({ pos: { tenantId: 'acme', view: 'chats', chatId: '', showContext: false } });

      const { result } = renderHook(() => useIsAdmin());
      expect(result.current).toBe(false);
    });

    it('returns true when user is in master workspace with superadmin role', () => {
      useAuthStore.setState({
        user: mockAdminUser,
        memberships: [mockMasterMembership],
      });
      useStore.setState({ pos: { tenantId: 'master', view: 'chats', chatId: '', showContext: false } });

      const { result } = renderHook(() => useIsAdmin());
      expect(result.current).toBe(true);
    });

    it('returns true when role has is_owner true in master workspace', () => {
      useAuthStore.setState({
        user: mockAdminUser,
        memberships: [
          {
            ...mockMasterMembership,
            role_name: 'Custom Master Owner',
            role: {
              id: 'r_owner_custom',
              workspace_id: 'master',
              name: 'Custom Master Owner',
              is_owner: true,
              permissions: ['chat.*'],
              built_in: false,
              created_at: '',
            },
          },
        ],
      });
      useStore.setState({ pos: { tenantId: 'master', view: 'chats', chatId: '', showContext: false } });

      const { result } = renderHook(() => useIsAdmin());
      expect(result.current).toBe(true);
    });

    it('returns true when role permissions include admin.* in master workspace', () => {
      useAuthStore.setState({
        user: mockAdminUser,
        memberships: [
          {
            ...mockMasterMembership,
            role_name: 'Admin Staff',
            role: {
              id: 'r_staff',
              workspace_id: 'master',
              name: 'Admin Staff',
              is_owner: false,
              permissions: ['admin.users', 'admin.workspaces'],
              built_in: false,
              created_at: '',
            },
          },
        ],
      });
      useStore.setState({ pos: { tenantId: 'master', view: 'chats', chatId: '', showContext: false } });

      const { result } = renderHook(() => useIsAdmin());
      expect(result.current).toBe(true);
    });

    it('returns false when user is in master workspace but has standard member role without admin permissions', () => {
      useAuthStore.setState({
        user: mockAdminUser,
        memberships: [
          {
            ...mockMasterMembership,
            role_name: 'Member',
            role: {
              id: 'r_member',
              workspace_id: 'master',
              name: 'Member',
              is_owner: false,
              permissions: ['chat.send', 'chat.read'],
              built_in: true,
              created_at: '',
            },
          },
        ],
      });
      useStore.setState({ pos: { tenantId: 'master', view: 'chats', chatId: '', showContext: false } });

      const { result } = renderHook(() => useIsAdmin());
      expect(result.current).toBe(false);
    });

    it('falls back to offline/mock tenant properties when unauthenticated', () => {
      useAuthStore.setState({ user: null, memberships: [] });
      useStore.setState({
        pos: { tenantId: 'master-mock', view: 'chats', chatId: '', showContext: false },
        db: {
          'master-mock': {
            id: 'master-mock',
            sub: 'master',
            name: 'Master Mock',
            is_master: true,
            currentUserRole: 'superadmin',
            tz: 'UTC',
            agents: [],
            channels: [],
            people: [],
            threads: {},
            cron: [],
            runs: [],
          } as any,
        },
      });

      const { result } = renderHook(() => useIsAdmin());
      expect(result.current).toBe(true);
    });
  });
});
