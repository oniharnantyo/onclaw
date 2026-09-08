import { useAuthStore } from '../store/auth';
import { useStore } from '../store';

// agents.write holders: Owner/Admin/Superadmin via the built-in catalog grants.
// Gates agent-private MCP servers (integrate-mcp-servers D9): whoever can edit
// the agent can wire its private integrations. Mirrors canWriteTools in
// lib/tools.ts / canWriteSkills in lib/skills.ts.
export function canWriteAgents(memberships: any[], tenant: any): boolean {
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
    perms.some((p) => p === '*' || p === 'agents.write' || p === 'agents.*' || p === 'workspace.*')
  );
}

export function useCanWriteAgents(tenant: any): boolean {
  const memberships = useAuthStore((s) => s.memberships);
  const pos = useStore((s: any) => s.pos);
  if (!tenant) return canWriteAgents(memberships, { id: pos.tenantId, sub: pos.tenantId });
  return canWriteAgents(memberships, tenant);
}
