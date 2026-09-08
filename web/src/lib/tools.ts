import { useAuthStore } from '../store/auth';
import { useStore } from '../store';

// tools.write holders: Owner/Admin/Superadmin via the built-in catalog grants —
// the MCP registry and workspace tool gate live behind the same permission.
// Members hold tools.read only and get read-only surfaces. Mirrors
// canWriteSkills in lib/skills.ts.
export function canWriteTools(memberships: any[], tenant: any): boolean {
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
    perms.some((p) => p === '*' || p === 'tools.write' || p === 'tools.*' || p === 'workspace.*')
  );
}

export function useCanWriteTools(tenant: any): boolean {
  const memberships = useAuthStore((s) => s.memberships);
  const pos = useStore((s: any) => s.pos);
  if (!tenant) return canWriteTools(memberships, { id: pos.tenantId, sub: pos.tenantId });
  return canWriteTools(memberships, tenant);
}
