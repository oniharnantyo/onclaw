import { useRef, useEffect } from "react";
import { cx } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { useAuthStore, useIsAdmin } from "../../store/auth";
import type { ApiMemberView } from "../../lib/api";

export interface WorkspaceSwitcherProps {
  open: boolean;
  db?: Record<string, any>;
  currentId?: string;
  onPick: (id: string, membership?: ApiMemberView) => void;
  onClose: () => void;
  memberships?: ApiMemberView[];
  onCreateWorkspace?: () => void;
}

export function WorkspaceSwitcher({
  open,
  db,
  currentId,
  onPick,
  onClose,
  memberships: propMemberships,
  onCreateWorkspace,
}: WorkspaceSwitcherProps) {
  const ref = useRef<HTMLDivElement>(null);
  const storeMemberships = useAuthStore((s) => s.memberships);
  const authMemberships = propMemberships || storeMemberships;
  // Workspace creation is instance-admin-only (fix-role-permission-audit):
  // master-workspace members holding admin.workspaces.write — exactly the
  // useIsAdmin gate the backend enforces on POST /workspaces. No other role
  // sees a creation entry.
  const isAdmin = useIsAdmin();
  const canCreate = Boolean(onCreateWorkspace) && isAdmin;

  useEffect(() => {
    if (!open) return;
    const h = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        onClose();
      }
    };
    window.addEventListener('mousedown', h);
    return () => window.removeEventListener('mousedown', h);
  }, [open, onClose]);

  if (!open) return null;

  const hasRealMemberships = authMemberships && authMemberships.length > 0;

  return (
    <div
      ref={ref}
      data-od-id="ws-switcher-popover"
      data-testid="ws-switcher-popover"
      className="od-pop fixed left-[76px] top-[14px] z-40 w-64 overflow-hidden rounded-md border border-line bg-surface shadow-[var(--elev-raised)]"
    >
      <div className="flex items-center justify-between border-b border-linesoft px-3.5 pt-3 pb-2 text-[11px] font-semibold uppercase tracking-wider text-muted">
        <span>Workspaces</span>
        {canCreate && (
          <button
            type="button"
            data-testid="btn-switcher-create-ws"
            onClick={() => {
              onClose();
              onCreateWorkspace!();
            }}
            className="flex items-center gap-1 rounded text-[11px] font-medium text-accent hover:underline lowercase tracking-normal"
          >
            <Icon name="plus" size={12} />
            new
          </button>
        )}
      </div>
      <ul className="py-1.5 max-h-72 overflow-y-auto">
        {hasRealMemberships ? (
          authMemberships.map((m: ApiMemberView) => {
            const wsId = m.workspace_slug || m.workspace_id;
            const wsName = m.workspace_name || m.workspace?.name || m.workspace_slug || 'Workspace';
            const isCurrent = currentId === wsId || currentId === m.workspace_id || currentId === m.workspace_slug;
            const isSuspended = Boolean(m.workspace?.disabled_at || (m as any).disabled_at);
            const roleName = m.role_name || m.role?.name || 'Member';
            const initials = wsName
              .split(/\s+/)
              .map((w: string) => w[0])
              .slice(0, 2)
              .join('')
              .toUpperCase();

            return (
              <li key={m.workspace_id || wsId}>
                <button
                  type="button"
                  disabled={isSuspended}
                  onClick={() => {
                    if (!isSuspended) {
                      onPick(wsId, m);
                    }
                  }}
                  data-od-id={'ws-option-' + wsId}
                  className={cx(
                    'flex w-full items-center gap-2.5 px-3.5 py-2 text-left transition-colors',
                    isSuspended
                      ? 'cursor-not-allowed opacity-60'
                      : isCurrent
                        ? 'bg-[color-mix(in_oklab,var(--accent)_13%,transparent)]'
                        : 'hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)]'
                  )}
                >
                  <span
                    className={cx(
                      'flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-[11px] font-semibold',
                      isSuspended
                        ? 'bg-muted text-surface'
                        : isCurrent
                          ? 'bg-accent text-accenton'
                          : 'bg-warm text-fg2'
                    )}
                  >
                    {initials}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-[13px] font-medium text-fg">
                      {wsName}
                    </span>
                    <span className="block truncate font-mono text-[10px] text-muted">
                      {m.workspace_slug ? `${m.workspace_slug}.onclaw.app` : ''}
                    </span>
                  </span>
                  {isSuspended ? (
                    <span className="rounded-[5px] bg-[color-mix(in_oklab,var(--danger)_18%,transparent)] px-1.5 py-0.5 text-[10px] font-semibold text-danger">
                      Suspended
                    </span>
                  ) : (
                    <>
                      {isCurrent && <Icon name="check" size={14} className="shrink-0 text-accent" />}
                      <span className="rounded-[5px] bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] px-1.5 py-0.5 text-[10px] font-semibold text-muted">
                        {roleName}
                      </span>
                    </>
                  )}
                </button>
              </li>
            );
          })
        ) : db ? (
          Object.values(db).map((t: any) => (
            <li key={t.id}>
              <button
                type="button"
                onClick={() => onPick(t.id)}
                data-od-id={'ws-option-' + t.id}
                className={cx(
                  'flex w-full items-center gap-2.5 px-3.5 py-2 text-left transition-colors',
                  t.id === currentId
                    ? 'bg-[color-mix(in_oklab,var(--accent)_13%,transparent)]'
                    : 'hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)]'
                )}
              >
                <span
                  className={cx(
                    'flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-[11px] font-semibold',
                    t.id === currentId ? 'bg-accent text-accenton' : 'bg-warm text-fg2'
                  )}
                >
                  {t.name.split(' ').map((w: any) => w[0]).join('')}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13px] font-medium text-fg">{t.name}</span>
                  <span className="block font-mono text-[10px] text-muted">
                    {t.agents?.length || 0} agents · {t.schedules?.filter((c: any) => c.enabled).length || 0} schedules
                  </span>
                </span>
                {t.id === currentId && <Icon name="check" size={14} className="shrink-0 text-accent" />}
              </button>
            </li>
          ))
        ) : (
          <li className="px-3.5 py-2 text-[12px] text-muted">No workspaces found</li>
        )}
      </ul>
      {canCreate && (
        <div className="border-t border-linesoft p-1.5">
          <button
            type="button"
            data-testid="btn-create-workspace-footer"
            onClick={() => {
              onClose();
              onCreateWorkspace!();
            }}
            className="flex w-full items-center gap-2 rounded px-2.5 py-1.5 text-[12px] font-medium text-fg2 hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
          >
            <Icon name="plus" size={13} className="text-muted" />
            Create new workspace
          </button>
        </div>
      )}
    </div>
  );
}



