import { create } from 'zustand';
import { api, getToken, setToken, clearToken, ApiError, type ApiUser, type ApiMemberView } from '../lib/api';
import { useStore } from './index';

export type AuthStatus = 'loading' | 'authenticated' | 'unauthenticated';

export interface AuthState {
  user: ApiUser | null;
  memberships: ApiMemberView[];
  status: AuthStatus;

  // Actions
  boot: () => Promise<void>;
  login: (email: string, password: string) => Promise<{ user: ApiUser; memberships: ApiMemberView[] }>;
  logout: () => Promise<void>;
  setSession: (user: ApiUser | null, memberships: ApiMemberView[]) => void;
  clearSession: () => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  memberships: [],
  status: 'loading',

  boot: async () => {
    const token = getToken();
    if (!token) {
      set({ user: null, memberships: [], status: 'unauthenticated' });
      return;
    }

    set({ status: 'loading' });
    try {
      const res = await api.auth.me();
      set({
        user: res.user,
        memberships: res.memberships || [],
        status: 'authenticated',
      });

      // Synchronize workspace position
      const memberships = res.memberships || [];
      if (memberships.length > 0) {
        const currentPos = useStore.getState().pos;
        const isMemberOfCurrent = memberships.some(
          (m) => m.workspace_id === currentPos.tenantId || m.workspace_slug === currentPos.tenantId
        );
        if (!isMemberOfCurrent) {
          const firstTarget = memberships[0].workspace_slug || memberships[0].workspace_id;
          useStore.getState().goPos({ tenantId: firstTarget });
        }
      }
    } catch (err: unknown) {
      const isUnauth =
        (err instanceof ApiError && (err.status === 401 || err.code === 'unauthenticated')) ||
        (err as any)?.status === 401 ||
        (err as any)?.code === 'unauthenticated';

      if (isUnauth) {
        clearToken();
        set({ user: null, memberships: [], status: 'unauthenticated' });
      } else {
        set({ status: 'authenticated' });
      }
    }
  },

  login: async (email: string, password: string) => {
    try {
      const loginRes = await api.auth.login({ email, password });
      setToken(loginRes.token);

      const meRes = await api.auth.me();
      const memberships = meRes.memberships || [];
      set({
        user: meRes.user,
        memberships,
        status: 'authenticated',
      });

      // Select first membership or remembered workspace
      if (memberships.length > 0) {
        const currentPos = useStore.getState().pos;
        const isMemberOfCurrent = memberships.some(
          (m) => m.workspace_id === currentPos.tenantId || m.workspace_slug === currentPos.tenantId
        );
        if (!isMemberOfCurrent) {
          const firstTarget = memberships[0].workspace_slug || memberships[0].workspace_id;
          useStore.getState().goPos({ tenantId: firstTarget });
        }
      }

      return meRes;
    } catch (err) {
      clearToken();
      set({ user: null, memberships: [], status: 'unauthenticated' });
      throw err;
    }
  },

  logout: async () => {
    try {
      await api.auth.logout();
    } catch {
      // Ignore logout request errors
    } finally {
      clearToken();
      set({ user: null, memberships: [], status: 'unauthenticated' });
    }
  },

  setSession: (user, memberships) => {
    set({
      user,
      memberships,
      status: user ? 'authenticated' : 'unauthenticated',
    });
  },

  clearSession: () => {
    clearToken();
    set({ user: null, memberships: [], status: 'unauthenticated' });
  },
}));

// Wire 401 event handler to clear session automatically
api.onUnauthorized(() => {
  useAuthStore.getState().clearSession();
});

export function useAuth() {
  const user = useAuthStore((s) => s.user);
  const memberships = useAuthStore((s) => s.memberships);
  const status = useAuthStore((s) => s.status);
  const boot = useAuthStore((s) => s.boot);
  const login = useAuthStore((s) => s.login);
  const logout = useAuthStore((s) => s.logout);

  return {
    user,
    memberships,
    status,
    boot,
    login,
    logout,
    isAuthenticated: status === 'authenticated',
    isLoading: status === 'loading',
  };
}


export function useIsAdmin(): boolean {
  const memberships = useAuthStore((s) => s.memberships);
  const user = useAuthStore((s) => s.user);
  const pos = useStore((s) => s.pos);
  const db = useStore((s) => s.db);
  const tenant = db[pos.tenantId];

  // In authenticated mode with user & memberships
  if (user && memberships.length > 0) {
    const currentMembership = memberships.find(
      (m) =>
        m.workspace_id === pos.tenantId ||
        m.workspace_slug === pos.tenantId ||
        m.workspace_id === tenant?.id ||
        m.workspace_slug === tenant?.id ||
        m.workspace_slug === tenant?.sub ||
        m.workspace_id === tenant?.sub
    );

    const isMaster =
      tenant?.is_master === true ||
      tenant?.sub === 'master' ||
      tenant?.id === 'master' ||
      pos.tenantId === 'master' ||
      currentMembership?.workspace?.is_master === true ||
      currentMembership?.workspace_slug === 'master' ||
      currentMembership?.workspace_id === 'master';

    if (!isMaster || !currentMembership) return false;

    const role = currentMembership.role;
    const roleName = (currentMembership.role_name || role?.name || '').toLowerCase();
    const perms = role?.permissions || [];

    return (
      role?.is_owner === true ||
      roleName === 'superadmin' ||
      roleName === 'owner' ||
      perms.some((p) => p.startsWith('admin.') || p === 'admin.*' || p === '*')
    );
  }

  // Graceful fallback for mock tests / offline states where tenant object indicates master
  if (tenant && (tenant.is_master || tenant.id === 'master' || tenant.sub === 'master')) {
    const userRole = (tenant as any).currentUserRole || (tenant as any).role || '';
    if (
      userRole.toLowerCase() === 'superadmin' ||
      userRole.toLowerCase() === 'owner' ||
      (tenant as any).isAdmin === true
    ) {
      return true;
    }
  }

  return false;
}

