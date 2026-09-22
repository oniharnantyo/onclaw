import { useMemo } from 'react';
import { useParams, useLocation, useNavigate } from 'react-router-dom';
import { Icon } from '../../components/ui/Icon';
import { ErrorState } from '../../components/ErrorState';
import { useStore } from '../../store';
import { useIsAdmin } from '../../store/auth';
import { TenantsPane } from './TenantsPane';
import { UsersPane } from './UsersPane';
import { OAuthAppsPane } from './OAuthAppsPane';

export interface AdminViewProps {
  screen?: 'workspaces' | 'accounts' | 'tenants' | 'users' | 'oauth-apps';
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
      if (propScreen === 'oauth-apps') return 'oauth-apps';
      return propScreen === 'accounts' || propScreen === 'users' ? 'accounts' : 'workspaces';
    }
    const path = location.pathname.toLowerCase();
    if (path.includes('/oauth')) {
      return 'oauth-apps';
    }
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
        <ErrorState
          variant="full"
          icon="shield"
          title="Not authorized"
          description="You must be an administrator in the master workspace to access the Admin area."
          primaryAction={{
            label: 'Return to workspace',
            onClick: () => navigate('/'),
            'data-testid': 'btn-return-workspace',
          }}
        />
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
          ) : activeScreen === 'oauth-apps' ? (
            <OAuthAppsPane onToast={toast} />
          ) : (
            <UsersPane onToast={toast} />
          )}
        </div>
      </div>
    </div>
  );
}

