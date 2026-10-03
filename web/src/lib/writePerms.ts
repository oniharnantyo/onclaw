import { useAuthStore } from '../store/auth';
import { useStore } from '../store';

// Write-permission gating helpers for the fix-role-permission-audit sweep.
// Same derivation shape and same contract as the per-domain canWrite* family —
// canWriteTools in lib/tools.ts, canWriteSkills in lib/skills.ts,
// canManageIntegrations in lib/connectionsApi.ts — for the permissions whose
// surfaces had no helper yet (members.*, providers.write, workspace.write,
// hooks.write, scheduler.write). This is the same helper family, not a new
// permission framework: the derivation is shared, the named wrappers keep the
// canWrite* call sites.
//
// Decided catalog (fix-role-permission-audit): `roles.write` is gone, and the
// built-in Member set gains channels.read + channels.write on top of its read
// permissions — neither affects these checks.
//
// Mock mode (no membership rows) keeps every affordance visible, exactly like
// the rest of the family.

function hasWritePerm(memberships: any[], tenant: any, perm: string): boolean {
  if (!memberships.length) return true; // offline / mock mode — affordances stay visible
  const tenantId = tenant?.sub || tenant?.id;
  const mem = memberships.find(
    (m) =>
      m.workspace_id === tenantId ||
      m.workspace_slug === tenantId ||
      m.workspace_id === tenant?.id ||
      m.workspace_slug === tenant?.sub
  );
  if (!mem) return false;
  const role = mem.role;
  const roleName = (mem.role_name || role?.name || '').toLowerCase();
  const perms: string[] = role?.permissions || [];
  return (
    role?.is_owner === true ||
    roleName === 'superadmin' ||
    roleName === 'owner' ||
    roleName === 'admin' ||
    perms.some((p) => p === '*' || p === perm || p === perm.replace('.write', '.*') || p === 'workspace.*')
  );
}

function useWritePerm(tenant: any, perm: string): boolean {
  const memberships = useAuthStore((s) => s.memberships);
  const pos = useStore((s: any) => s.pos);
  if (!tenant) return hasWritePerm(memberships, { id: pos.tenantId, sub: pos.tenantId }, perm);
  return hasWritePerm(memberships, tenant, perm);
}

// members.write: invite members and change roles (Members pane).
export function canWriteMembers(memberships: any[], tenant: any): boolean {
  return hasWritePerm(memberships, tenant, 'members.write');
}
export function useCanWriteMembers(tenant: any): boolean {
  return useWritePerm(tenant, 'members.write');
}

// members.remove: remove members (Members pane). Separated from members.write
// because the catalog grants them independently (Owner/Admin hold both).
export function canRemoveMembers(memberships: any[], tenant: any): boolean {
  return hasWritePerm(memberships, tenant, 'members.remove');
}
export function useCanRemoveMembers(tenant: any): boolean {
  return useWritePerm(tenant, 'members.remove');
}

// providers.write: create, edit, enable/disable, and delete provider configs.
export function canWriteProviders(memberships: any[], tenant: any): boolean {
  return hasWritePerm(memberships, tenant, 'providers.write');
}
export function useCanWriteProviders(tenant: any): boolean {
  return useWritePerm(tenant, 'providers.write');
}

// workspace.write: workspace settings (name, timezone, default model) and the
// memory surfaces (shared memory, Memory pane mutations).
export function canWriteWorkspace(memberships: any[], tenant: any): boolean {
  return hasWritePerm(memberships, tenant, 'workspace.write');
}
export function useCanWriteWorkspace(tenant: any): boolean {
  return useWritePerm(tenant, 'workspace.write');
}

// hooks.write: hook lifecycle (workspace Hooks pane and the agent-config
// hooks section). Previously shared with tools.write — the audit re-gated it
// to its own catalog permission.
export function canWriteHooks(memberships: any[], tenant: any): boolean {
  return hasWritePerm(memberships, tenant, 'hooks.write');
}
export function useCanWriteHooks(tenant: any): boolean {
  return useWritePerm(tenant, 'hooks.write');
}

// scheduler.write: create, edit, run-now, pause/resume, and delete schedules.
export function canWriteSchedulers(memberships: any[], tenant: any): boolean {
  return hasWritePerm(memberships, tenant, 'scheduler.write');
}
export function useCanWriteSchedulers(tenant: any): boolean {
  return useWritePerm(tenant, 'scheduler.write');
}
