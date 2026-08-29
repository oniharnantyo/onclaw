import { useState, useEffect } from 'react';
import { cx } from '../../lib/helpers';
import { Icon } from '../../components/ui/Icon';
import { Chip } from '../../components/ui/Chip';
import { api, formatApiError, type ApiAdminWorkspaceItem } from '../../lib/api';
import { CreateTenantModal } from './CreateTenantModal';
import { EditTenantModal } from './EditTenantModal';

interface TenantsPaneProps {
  onToast: (text: string, kind?: string) => void;
}

export function TenantsPane({ onToast }: TenantsPaneProps) {
  const [workspaces, setWorkspaces] = useState<ApiAdminWorkspaceItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [createModalOpen, setCreateModalOpen] = useState(false);
  const [editingWorkspace, setEditingWorkspace] = useState<ApiAdminWorkspaceItem | null>(null);
  const [actionInProgress, setActionInProgress] = useState<string | null>(null);

  const fetchWorkspaces = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await api.admin.workspaces.list();
      setWorkspaces(res.workspaces || []);
    } catch (err: unknown) {
      const msg = formatApiError(err, 'Failed to load workspaces');
      setError(msg);
      onToast(msg, 'danger');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchWorkspaces();
  }, []);

  const handleToggleSuspend = async (ws: ApiAdminWorkspaceItem) => {
    const isSuspended = Boolean(ws.disabled_at);
    setActionInProgress(ws.id);
    try {
      if (isSuspended) {
        await api.admin.workspaces.enable(ws.slug || ws.id);
        setWorkspaces((prev) =>
          prev.map((item) =>
            item.id === ws.id ? { ...item, disabled_at: null } : item
          )
        );
        onToast(`Workspace "${ws.name}" restored`);
      } else {
        await api.admin.workspaces.disable(ws.slug || ws.id);
        setWorkspaces((prev) =>
          prev.map((item) =>
            item.id === ws.id ? { ...item, disabled_at: new Date().toISOString() } : item
          )
        );
        onToast(`Workspace "${ws.name}" suspended`);
      }
    } catch (err: unknown) {
      onToast(formatApiError(err, `Failed to ${isSuspended ? 'restore' : 'suspend'} workspace`), 'danger');
    } finally {
      setActionInProgress(null);
    }
  };

  return (
    <div className="space-y-6" data-od-id="pane-admin-tenants" data-testid="pane-admin-tenants">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-[18px] font-semibold text-fg">Tenants</h2>
          <p className="mt-1 text-[13px] text-muted">
            All workspaces hosted on this OnClaw instance.
          </p>
        </div>
        <button
          type="button"
          onClick={() => setCreateModalOpen(true)}
          data-od-id="btn-create-tenant"
          data-testid="btn-create-tenant"
          className="flex h-9 items-center gap-2 rounded-md bg-accent px-3.5 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)]"
        >
          <Icon name="plus" size={15} sw={2.2} /> Create tenant
        </button>
      </div>

      {loading ? (
        <div className="flex h-48 items-center justify-center rounded-lg border border-line bg-surface">
          <div className="flex items-center gap-3 text-[13px] text-muted">
            <div className="h-5 w-5 animate-spin rounded-full border-2 border-line border-t-accent" />
            Loading workspaces…
          </div>
        </div>
      ) : error ? (
        <div className="flex flex-col items-center justify-center gap-3 rounded-lg border border-line bg-surface p-8 text-center">
          <p className="text-[13px] text-danger">{error}</p>
          <button
            type="button"
            onClick={fetchWorkspaces}
            className="flex h-8 items-center rounded-md border border-line px-3 text-[12px] font-medium text-fg2 hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)]"
          >
            Retry
          </button>
        </div>
      ) : (
        <div className="overflow-hidden rounded-lg border border-line" data-od-id="tenants-table" data-testid="tenants-table">
          <div className="hidden md:grid grid-cols-[1fr_130px_90px_90px_160px] gap-3 border-b border-linesoft bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] px-4 py-2.5 font-mono text-[10px] uppercase tracking-[0.14em] text-muted">
            <span>Workspace</span>
            <span>Timezone</span>
            <span>Members</span>
            <span>Status</span>
            <span className="text-right">Actions</span>
          </div>

          <div className="divide-y divide-linesoft">
            {workspaces.map((ws) => {
              const isMaster = ws.is_master || ws.slug === 'master';
              const isSuspended = Boolean(ws.disabled_at);
              const busy = actionInProgress === ws.id;

              return (
                <div
                  key={ws.id}
                  data-od-id={'tenant-row-' + ws.slug}
                  data-testid={'tenant-row-' + ws.slug}
                  className="flex flex-col gap-2 p-4 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_3%,transparent)] md:grid md:grid-cols-[1fr_130px_90px_90px_160px] md:items-center md:gap-3 md:py-3"
                >
                  {/* Name and Slug */}
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-[14px] font-semibold text-fg">{ws.name}</span>
                      {isMaster && (
                        <span className="rounded bg-[color-mix(in_oklab,var(--accent)_16%,transparent)] px-1.5 py-0.5 text-[10px] font-bold text-accent uppercase">
                          Master
                        </span>
                      )}
                    </div>
                    <span className="font-mono text-[11px] text-muted">{ws.slug}</span>
                  </div>

                  {/* Timezone */}
                  <div className="flex items-center justify-between md:contents">
                    <span className="text-[10px] uppercase tracking-wider text-muted md:hidden">Timezone</span>
                    <span className="truncate font-mono text-[12px] text-fg2">{ws.timezone || 'UTC'}</span>
                  </div>

                  {/* Members */}
                  <div className="flex items-center justify-between md:contents">
                    <span className="text-[10px] uppercase tracking-wider text-muted md:hidden">Members</span>
                    <span className="font-mono text-[12px] text-fg2">
                      {ws.member_count} {ws.member_count === 1 ? 'member' : 'members'}
                    </span>
                  </div>

                  {/* Status */}
                  <div className="flex items-center justify-between md:contents">
                    <span className="text-[10px] uppercase tracking-wider text-muted md:hidden">Status</span>
                    <div>
                      {isSuspended ? (
                        <span className="inline-flex items-center gap-1.5 font-mono text-[11px] text-danger">
                          <Icon name="x" size={12} /> Suspended
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1.5 font-mono text-[11px] text-success">
                          <Icon name="check" size={12} /> Active
                        </span>
                      )}
                    </div>
                  </div>

                  {/* Action */}
                  <div className="flex items-center justify-end gap-1.5 pt-2 md:pt-0">
                    <button
                      type="button"
                      onClick={() => setEditingWorkspace(ws)}
                      data-od-id={'btn-edit-' + ws.slug}
                      data-testid={'btn-edit-' + ws.slug}
                      className="flex h-7 items-center gap-1 rounded-md border border-line px-2.5 text-[11.5px] font-medium text-fg2 transition-colors hover:border-muted hover:text-fg"
                    >
                      <Icon name="pencil" size={12} />
                      Edit
                    </button>
                    {isMaster ? (
                      <Chip mono className="opacity-75">Protected</Chip>
                    ) : (
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => handleToggleSuspend(ws)}
                        data-od-id={isSuspended ? 'btn-restore-' + ws.slug : 'btn-suspend-' + ws.slug}
                        data-testid={isSuspended ? 'btn-restore-' + ws.slug : 'btn-suspend-' + ws.slug}
                        className={cx(
                          'flex h-7 items-center rounded-md border px-2.5 text-[11.5px] font-medium transition-colors disabled:opacity-50',
                          isSuspended
                            ? 'border-line text-fg2 hover:border-accent hover:text-accent'
                            : 'border-[color-mix(in_oklab,var(--danger)_35%,transparent)] text-danger hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)]'
                        )}
                      >
                        {busy ? '…' : isSuspended ? 'Restore' : 'Suspend'}
                      </button>
                    )}
                  </div>
                </div>
              );
            })}

            {workspaces.length === 0 && (
              <p className="px-4 py-8 text-center text-[13px] text-muted">
                No workspaces found.
              </p>
            )}
          </div>
        </div>
      )}

      {createModalOpen && (
        <CreateTenantModal
          onClose={() => setCreateModalOpen(false)}
          onCreateSuccess={(newWs) => {
            setWorkspaces((prev) => [
              {
                id: newWs.id,
                slug: newWs.slug,
                name: newWs.name,
                timezone: newWs.timezone,
                is_master: newWs.is_master,
                disabled_at: newWs.disabled_at,
                created_at: newWs.created_at,
                updated_at: newWs.updated_at,
                member_count: newWs.member_count ?? 1,
              },
              ...prev,
            ]);
          }}
          onToast={onToast}
        />
      )}

      {editingWorkspace && (
        <EditTenantModal
          workspace={editingWorkspace}
          onClose={() => setEditingWorkspace(null)}
          onUpdateSuccess={(updated) => {
            setWorkspaces((prev) =>
              prev.map((item) => (item.id === updated.id ? { ...item, ...updated } : item))
            );
            setEditingWorkspace((prev) =>
              prev && prev.id === updated.id ? { ...prev, ...updated } : prev
            );
          }}
          onToast={onToast}
        />
      )}
    </div>
  );
}
