import { useState, useEffect } from 'react';
import { cx } from '../../lib/helpers';
import { Icon } from '../../components/ui/Icon';
import { Avatar } from '../../components/ui/Avatar';
import { ErrorState } from '../../components/ErrorState';
import { api, formatApiError, ApiError, type ApiUser } from '../../lib/api';
import { useAuthStore } from '../../store/auth';
import { CreateUserModal } from './CreateUserModal';
import serverErrorSvg from '../../assets/server-error.svg';

interface UsersPaneProps {
  onToast: (text: string, kind?: string) => void;
}

export function UsersPane({ onToast }: UsersPaneProps) {
  const [users, setUsers] = useState<ApiUser[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<ApiError | Error | null>(null);
  const [createModalOpen, setCreateModalOpen] = useState(false);
  const [confirmDisableId, setConfirmDisableId] = useState<string | null>(null);
  const [actionInProgress, setActionInProgress] = useState<string | null>(null);

  const currentUser = useAuthStore((s) => s.user);

  const fetchUsers = async () => {
    setLoading(true);
    setLoadError(null);
    try {
      const res = await api.admin.users.list();
      setUsers(res.users || []);
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 0) {
        // Status 0 keeps the loading/empty state (handled by ConnectionBanner)
        return;
      }
      const apiErr = err instanceof Error ? err : new Error(String(err));
      setLoadError(apiErr);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchUsers();
  }, []);

  const handlePromote = async (u: ApiUser) => {
    setActionInProgress(`promote-${u.id}`);
    try {
      await api.admin.superadmins.grant({ user_id: u.id });
      setUsers((prev) =>
        prev.map((item) =>
          item.id === u.id
            ? {
                ...item,
                is_superadmin: true,
              }
            : item
        )
      );
      onToast(`Granted superadmin to "${u.name || u.email}"`);
    } catch (err: unknown) {
      onToast(formatApiError(err, 'Failed to promote user'), 'danger');
    } finally {
      setActionInProgress(null);
    }
  };

  const handleDemote = async (u: ApiUser) => {
    setActionInProgress(`demote-${u.id}`);
    try {
      await api.admin.superadmins.revoke(u.id);
      setUsers((prev) =>
        prev.map((item) =>
          item.id === u.id
            ? {
                ...item,
                is_superadmin: false,
              }
            : item
        )
      );
      onToast(`Revoked superadmin from "${u.name || u.email}"`);
    } catch (err: unknown) {
      onToast(formatApiError(err, 'Failed to demote user'), 'danger');
    } finally {
      setActionInProgress(null);
    }
  };

  const handleToggleDisable = async (u: ApiUser) => {
    const isMe = currentUser?.id === u.id;
    const isDisabled = Boolean(u.disabled_at);

    if (isMe && !isDisabled) {
      onToast('You cannot disable your own account', 'danger');
      return;
    }

    if (!isDisabled && confirmDisableId !== u.id) {
      setConfirmDisableId(u.id);
      setTimeout(() => {
        setConfirmDisableId((current) => (current === u.id ? null : current));
      }, 4000);
      return;
    }

    setConfirmDisableId(null);
    setActionInProgress(`disable-${u.id}`);

    try {
      if (isDisabled) {
        await api.admin.users.enable(u.id);
        setUsers((prev) =>
          prev.map((item) =>
            item.id === u.id ? { ...item, disabled_at: null } : item
          )
        );
        onToast(`User "${u.email}" enabled`);
      } else {
        await api.admin.users.disable(u.id);
        setUsers((prev) =>
          prev.map((item) =>
            item.id === u.id ? { ...item, disabled_at: new Date().toISOString() } : item
          )
        );
        onToast(`User "${u.email}" disabled`);
      }
    } catch (err: unknown) {
      onToast(formatApiError(err, `Failed to ${isDisabled ? 'enable' : 'disable'} user`), 'danger');
    } finally {
      setActionInProgress(null);
    }
  };

  return (
    <div className="space-y-6" data-od-id="pane-admin-users" data-testid="pane-admin-users">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-[18px] font-semibold text-fg">Users</h2>
          <p className="mt-1 text-[13px] text-muted">
            All user identities across this OnClaw instance.
          </p>
        </div>
        <button
          type="button"
          onClick={() => setCreateModalOpen(true)}
          data-od-id="btn-create-user"
          data-testid="btn-create-user"
          className="flex h-9 items-center gap-2 rounded-md bg-accent px-3.5 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)]"
        >
          <Icon name="plus" size={15} sw={2.2} /> Create user
        </button>
      </div>

      {loading ? (
        <div className="flex h-48 items-center justify-center rounded-lg border border-line bg-surface">
          <div className="flex items-center gap-3 text-[13px] text-muted">
            <div className="h-5 w-5 animate-spin rounded-full border-2 border-line border-t-accent" />
            Loading users…
          </div>
        </div>
      ) : loadError ? (
        <div className="flex min-h-[320px] items-center justify-center rounded-lg border border-line bg-surface p-6">
          <ErrorState
            variant="full"
            illustration={serverErrorSvg}
            title="Couldn't load users"
            description="A server error occurred while loading users."
            status={loadError instanceof ApiError ? loadError.status : 500}
            detail={loadError.message}
            primaryAction={{
              label: 'Retry',
              onClick: fetchUsers,
            }}
          />
        </div>
      ) : (
        <div className="overflow-hidden rounded-lg border border-line" data-od-id="users-table" data-testid="users-table">
          <div className="hidden md:grid grid-cols-[1fr_120px_110px_90px_90px_160px] gap-3 border-b border-linesoft bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] px-4 py-2.5 font-mono text-[10px] uppercase tracking-[0.14em] text-muted">
            <span>User</span>
            <span>Superadmin</span>
            <span>Created</span>
            <span>Workspaces</span>
            <span>Status</span>
            <span className="text-right">Actions</span>
          </div>

          <div className="divide-y divide-linesoft">
            {users.map((u) => {
              const isMe = currentUser?.id === u.id;
              const isDisabled = Boolean(u.disabled_at);
              const busy = Boolean(actionInProgress && actionInProgress.endsWith(u.id));
              const isConfirming = confirmDisableId === u.id;

              return (
                <div
                  key={u.id}
                  data-od-id={'user-row-' + u.id}
                  data-testid={'user-row-' + u.id}
                  className="flex flex-col gap-2 p-4 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_3%,transparent)] md:grid md:grid-cols-[1fr_120px_110px_90px_90px_160px] md:items-center md:gap-3 md:py-3"
                >
                  {/* User info */}
                  <div className="flex items-center gap-3 min-w-0">
                    <Avatar name={u.name || u.email} src={u.avatar_url} kind={isMe ? 'you' : 'other'} />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="truncate text-[14px] font-medium text-fg">
                          {u.name || u.email.split('@')[0]}
                        </span>
                        {isMe && (
                          <span className="rounded bg-[color-mix(in_oklab,var(--fg)_10%,transparent)] px-1.5 py-0.2 text-[10px] font-medium text-muted">
                            You
                          </span>
                        )}
                      </div>
                      <span className="truncate font-mono text-[11px] text-muted block">{u.email}</span>
                    </div>
                  </div>

                  {/* Superadmin badge */}
                  <div className="flex items-center justify-between md:contents">
                    <span className="text-[10px] uppercase tracking-wider text-muted md:hidden">Superadmin</span>
                    <div>
                      {u.is_superadmin ? (
                        <span
                          data-od-id={'badge-superadmin-' + u.id}
                          data-testid={'badge-superadmin-' + u.id}
                          className="inline-flex items-center gap-1.5 rounded-full bg-[color-mix(in_oklab,var(--accent)_16%,transparent)] px-2.5 py-0.5 font-mono text-[11px] font-semibold text-accent"
                        >
                          <Icon name="shield" size={12} /> Superadmin
                        </span>
                      ) : (
                        <span className="font-mono text-[12px] text-muted">—</span>
                      )}
                    </div>
                  </div>

                  {/* Created Date */}
                  <div className="flex items-center justify-between md:contents">
                    <span className="text-[10px] uppercase tracking-wider text-muted md:hidden">Created</span>
                    <span className="font-mono text-[12px] text-fg2">
                      {u.created_at ? new Date(u.created_at).toLocaleDateString() : '—'}
                    </span>
                  </div>

                  {/* Workspaces */}
                  <div className="flex items-center justify-between md:contents">
                    <span className="text-[10px] uppercase tracking-wider text-muted md:hidden">Workspaces</span>
                    <span className="font-mono text-[12px] text-fg2">
                      {u.membership_count ?? 0} {(u.membership_count ?? 0) === 1 ? 'workspace' : 'workspaces'}
                    </span>
                  </div>

                  {/* Status */}
                  <div className="flex items-center justify-between md:contents">
                    <span className="text-[10px] uppercase tracking-wider text-muted md:hidden">Status</span>
                    <div>
                      {isDisabled ? (
                        <span className="inline-flex items-center gap-1.5 font-mono text-[11px] text-danger">
                          <Icon name="x" size={12} /> Disabled
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1.5 font-mono text-[11px] text-success">
                          <Icon name="check" size={12} /> Active
                        </span>
                      )}
                    </div>
                  </div>

                  {/* Action */}
                  <div className="flex items-center justify-end gap-2 pt-2 md:pt-0">
                    {u.is_superadmin ? (
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => handleDemote(u)}
                        data-od-id={'btn-demote-user-' + u.id}
                        data-testid={'btn-demote-user-' + u.id}
                        className="flex h-7 items-center rounded-md border border-[color-mix(in_oklab,var(--danger)_35%,transparent)] px-2.5 text-[11.5px] font-medium text-danger transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] disabled:opacity-50"
                      >
                        {actionInProgress === `demote-${u.id}` ? '…' : 'Demote'}
                      </button>
                    ) : (
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => handlePromote(u)}
                        data-od-id={'btn-promote-user-' + u.id}
                        data-testid={'btn-promote-user-' + u.id}
                        className="flex h-7 items-center rounded-md border border-line px-2.5 text-[11.5px] font-medium text-fg2 transition-colors hover:border-accent hover:text-accent disabled:opacity-50"
                      >
                        {actionInProgress === `promote-${u.id}` ? '…' : 'Promote'}
                      </button>
                    )}

                    <button
                      type="button"
                      disabled={busy || (isMe && !isDisabled)}
                      onClick={() => handleToggleDisable(u)}
                      data-od-id={isDisabled ? 'btn-enable-user-' + u.id : 'btn-disable-user-' + u.id}
                      data-testid={isDisabled ? 'btn-enable-user-' + u.id : 'btn-disable-user-' + u.id}
                      title={isMe && !isDisabled ? 'You cannot disable your own account' : undefined}
                      className={cx(
                        'flex h-7 items-center rounded-md border px-2.5 text-[11.5px] font-medium transition-colors disabled:opacity-50',
                        isDisabled
                          ? 'border-line text-fg2 hover:border-accent hover:text-accent'
                          : isConfirming
                          ? 'border-danger bg-danger text-white hover:bg-[color-mix(in_oklab,var(--danger)_90%,black)]'
                          : 'border-[color-mix(in_oklab,var(--danger)_35%,transparent)] text-danger hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)]'
                      )}
                    >
                      {actionInProgress === `disable-${u.id}` ? '…' : isDisabled ? 'Enable' : isConfirming ? 'Confirm disable' : 'Disable'}
                    </button>
                  </div>
                </div>
              );
            })}

            {users.length === 0 && (
              <p className="px-4 py-8 text-center text-[13px] text-muted">
                No users found.
              </p>
            )}
          </div>
        </div>
      )}

      {createModalOpen && (
        <CreateUserModal
          onClose={() => setCreateModalOpen(false)}
          onCreateSuccess={(newUser) => {
            setUsers((prev) => [{ ...newUser, is_superadmin: Boolean(newUser.is_superadmin) }, ...prev]);
          }}
          onToast={onToast}
        />
      )}
    </div>
  );
}

