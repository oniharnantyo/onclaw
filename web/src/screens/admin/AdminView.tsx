import { useMemo } from 'react';
import { useParams, useLocation, useNavigate } from 'react-router-dom';
import { Icon } from '../../components/ui/Icon';
import { useStore } from '../../store';
import { useIsAdmin } from '../../store/auth';
import { TenantsPane } from './TenantsPane';
import { UsersPane } from './UsersPane';

export interface AdminViewProps {
  screen?: 'workspaces' | 'accounts' | 'tenants' | 'users';
  tenant?: any;
}

export function AdminView({ screen: propScreen }: AdminViewProps) {
  const { tab } = useParams<{ tab?: string }>();
  const location = useLocation();
  const navigate = useNavigate();
  const isAdmin = useIsAdmin();
  const toast = useStore((s: any) => s.toast);

  const activeScreen = useMemo(() => {
    if (propScreen) {
      return propScreen === 'accounts' || propScreen === 'users' ? 'accounts' : 'workspaces';
    }
    const path = location.pathname.toLowerCase();
    if (path.includes('/accounts') || path.includes('/users') || path.includes('/superadmins')) {
      return 'accounts';
    }
    if (tab === 'accounts' || tab === 'users' || tab === 'superadmins') {
      return 'accounts';
    }
    return 'workspaces';
  }, [propScreen, location.pathname, tab]);

  if (!isAdmin) {
    return (
      <div
        data-od-id="admin-unauthorized"
        data-testid="admin-unauthorized"
        className="flex h-full flex-1 flex-col items-center justify-center bg-surface p-6 text-center"
      >
        <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--danger)_15%,transparent)] text-danger">
          <Icon name="shield" size={24} />
        </div>
        <h2 className="text-[18px] font-semibold text-fg">Not authorized</h2>
        <p className="mt-2 max-w-sm text-[13px] leading-5 text-muted">
          You must be an administrator in the master workspace to access the Admin area.
        </p>
        <button
          type="button"
          onClick={() => navigate('/')}
          data-od-id="btn-return-workspace"
          data-testid="btn-return-workspace"
          className="mt-5 inline-flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)]"
        >
          Return to workspace
        </button>
      </div>
    );
  }

  return (
    <div
      data-od-id="admin-view"
      data-testid="admin-view"
      className="flex h-full flex-1 overflow-hidden bg-surface"
    >
      <div className="od-scroll flex-1 overflow-y-auto p-6 md:p-8">
        <div className="mx-auto max-w-5xl">
          {activeScreen === 'workspaces' ? (
            <TenantsPane onToast={toast} />
          ) : (
            <UsersPane onToast={toast} />
          )}
        </div>
      </div>
    </div>
  );
}

