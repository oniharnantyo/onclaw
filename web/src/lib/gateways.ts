import { useAuthStore } from '../store/auth';
import { useStore } from '../store';
import type {
  ApiGatewayConfig,
  ApiGatewayLink,
  ApiWhatsAppGatewayConfig,
  ApiWhatsAppHealth,
} from './api';

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

export type GatewayState = 'connected' | 'linked' | 'paused' | 'error' | 'unconfigured';
export type GatewayDotColor = 'accent' | 'success' | 'danger' | 'muted';

export interface GatewayStatusView {
  label: string;
  dot: string;
  dotColor: GatewayDotColor;
  errored: boolean;
  state: GatewayState;
}

// Telegram gateway status mapping (revamp-gateways-sidebar D1):
// Unconfigured -> 'Not set up' (○ muted)
// Configured + status_error -> 'Error' (● danger)
// Configured + disabled -> 'Paused' (○ muted)
// Configured + enabled -> 'Connected' (● accent/success)
export function telegramGatewayStatusView(
  g: ApiGatewayConfig | null | undefined
): GatewayStatusView {
  if (!g || (!g.token_hint && !g.bot_username)) {
    return {
      label: 'Not set up',
      dot: 'bg-muted',
      dotColor: 'muted',
      errored: false,
      state: 'unconfigured',
    };
  }
  if (g.status_error) {
    return {
      label: 'Error',
      dot: 'bg-danger',
      dotColor: 'danger',
      errored: true,
      state: 'error',
    };
  }
  if (!g.enabled) {
    return {
      label: 'Paused',
      dot: 'bg-muted',
      dotColor: 'muted',
      errored: false,
      state: 'paused',
    };
  }
  return {
    label: 'Connected',
    dot: 'bg-success',
    dotColor: 'accent',
    errored: false,
    state: 'connected',
  };
}

// WhatsApp gateway status mapping (revamp-gateways-sidebar D1):
// Unconfigured -> 'Not set up' (○ muted)
// Configured + disabled -> 'Paused' (○ muted)
// Configured + health error -> 'Error' (● danger)
// Configured + enabled + health ok -> 'Connected' (multi-device: 'Linked') (● accent/success)
export function whatsappGatewayStatusView(
  config: ApiWhatsAppGatewayConfig | null | undefined,
  health: ApiWhatsAppHealth | null | undefined,
  _link?: ApiGatewayLink | null | undefined
): GatewayStatusView {
  const configured = Boolean(config && (config.lane || config.has_credentials));
  if (!configured) {
    return {
      label: 'Not set up',
      dot: 'bg-muted',
      dotColor: 'muted',
      errored: false,
      state: 'unconfigured',
    };
  }
  if (config && !config.enabled) {
    return {
      label: 'Paused',
      dot: 'bg-muted',
      dotColor: 'muted',
      errored: false,
      state: 'paused',
    };
  }
  if (health?.status === 'error') {
    return {
      label: 'Error',
      dot: 'bg-danger',
      dotColor: 'danger',
      errored: true,
      state: 'error',
    };
  }
  if (health?.status === 'ok') {
    const isLinked = config?.lane === 'multi_device';
    return {
      label: isLinked ? 'Linked' : 'Connected',
      dot: 'bg-success',
      dotColor: 'accent',
      errored: false,
      state: isLinked ? 'linked' : 'connected',
    };
  }
  return {
    label: 'Not set up',
    dot: 'bg-muted',
    dotColor: 'muted',
    errored: false,
    state: 'unconfigured',
  };
}

// Display status for the connection card: backward compatible alias for Telegram.
export function gatewayStatusView(g: ApiGatewayConfig | null | undefined): GatewayStatusView {
  return telegramGatewayStatusView(g);
}

// Problem-first default platform selector (revamp-gateways-sidebar D6):
// Error (TG > WA) > Configured (TG > WA) > unconfigured (TG)
export function defaultGatewayPlatform(
  tgStatus: GatewayStatusView,
  waStatus: GatewayStatusView
): 'telegram' | 'whatsapp' {
  if (tgStatus.state === 'error') return 'telegram';
  if (waStatus.state === 'error') return 'whatsapp';
  if (tgStatus.state !== 'unconfigured') return 'telegram';
  if (waStatus.state !== 'unconfigured') return 'whatsapp';
  return 'telegram';
}

// mm:ss countdown for the pairing-token expiry (bounded at one hour).
export function formatCountdown(expiresAt: string, now: number = Date.now()): string {
  const remaining = Math.max(0, Math.floor((Date.parse(expiresAt) - now) / 1000));
  const m = Math.floor(remaining / 60);
  const s = remaining % 60;
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
}

// Client-side privacy mask for a linked WhatsApp identity (bare digits):
// keep the two-digit country prefix and the last two digits, dot out the
// rest — e.g. 6281234567890 → +62•••••••••90. Nothing full is ever shown.
export function maskWhatsAppId(id: string | null | undefined): string {
  const digits = (id || '').replace(/\D/g, '');
  if (!digits) return '';
  if (digits.length <= 4) return `+${digits}`;
  return `+${digits.slice(0, 2)}${'•'.repeat(digits.length - 4)}${digits.slice(-2)}`;
}
