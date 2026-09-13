import { useAuthStore } from '../store/auth';
import { useStore } from '../store';
import type { ApiGatewayConfig } from './api';

// gateways.write holders: Owner/Admin/Superadmin via the built-in catalog
// grants — gateway config, bindings, and enable/disable live behind the same
// admin permission. Members hold the pairing flow only. Mirrors canWriteSkills
// in lib/skills.ts / canWriteTools in lib/tools.ts.
export function canManageGateways(memberships: any[], tenant: any): boolean {
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
    perms.some((p) => p === '*' || p === 'gateways.write' || p === 'gateways.*' || p === 'workspace.*')
  );
}

export function useCanManageGateways(tenant: any): boolean {
  const memberships = useAuthStore((s) => s.memberships);
  const pos = useStore((s: any) => s.pos);
  if (!tenant) return canManageGateways(memberships, { id: pos.tenantId, sub: pos.tenantId });
  return canManageGateways(memberships, tenant);
}

export interface GatewayStatusView {
  label: string;
  errored: boolean;
}

// Display status for the connection card: a stored token (hint present) means
// the bot is connected — enabled gateways are live, the rest disabled. No
// hint and no username means unconfigured. A server-side error message wins.
export function gatewayStatusView(g: ApiGatewayConfig | null | undefined): GatewayStatusView {
  if (!g || (!g.token_hint && !g.bot_username)) return { label: 'Not connected', errored: false };
  if (g.status_error) return { label: 'Error', errored: true };
  return g.enabled ? { label: 'Connected', errored: false } : { label: 'Disabled', errored: false };
}

// mm:ss countdown for the pairing-token expiry (bounded at one hour).
export function formatCountdown(expiresAt: string, now: number = Date.now()): string {
  const remaining = Math.max(0, Math.floor((Date.parse(expiresAt) - now) / 1000));
  const m = Math.floor(remaining / 60);
  const s = remaining % 60;
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
}
