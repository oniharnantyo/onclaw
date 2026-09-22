import { useAuthStore } from '../store/auth';
import { useStore } from '../store';
import { request, type McpTransport } from './api';

// ---------------------------------------------------------------------------
// Workspace service connections (add-workspace-connections): recipe registry +
// connection lifecycle under /workspaces/:slug/integrations/. ONE module owns
// every shape and route so contract drift with the backend is a one-file fix.
// The token is a write-only secret — reads carry only the last-4 `token_hint`
// (spec: "Token never readable"), and the connect payload is EXACTLY
// { recipe_id, access_level, token } (design D7 — endpoint URLs are
// server-declared recipe data, never user input).
// ---------------------------------------------------------------------------

/** D5: connection metadata, driving guided scope copy and display. Actual
 * write enforcement rides the token's own scopes. */
export type ConnectionAccessLevel = 'read_only' | 'read_write';

/** D2: a service is live when its recipe says available; coming_soon recipes
 * render disabled and accept no connect requests. */
export type RecipeAvailability = 'available' | 'coming_soon';

export type RecipeAuthKind = 'pat' | 'oauth';

/** One guided token-creation step; `url` links the service's token page.
 * Mirrors domain.RecipeStep. */
export interface ApiRecipeStep {
  title: string;
  detail?: string;
  url?: string;
}

/** Recommended token scopes for one access level (domain.RecipeScopes). */
export interface ApiRecipeScopes {
  access_level: ConnectionAccessLevel;
  scopes: string[];
}

/** Read view of one recipe — mirrors domain.Recipe's JSON exactly (D2).
 * Service knowledge is server-declared; the gallery renders cards without
 * hardcoding it. `access_levels[0]` is the connect flow's default. */
export interface ApiIntegrationRecipe {
  id: string;
  /** Display service name ("GitHub"). */
  service: string;
  /** Icon identifier the gallery renders (e.g. "github"). */
  icon: string;
  auth_kind: 'pat' | 'oauth';
  availability: 'available' | 'coming_soon';
  /** The materialized server's transport constant. */
  transport: McpTransport;
  /** Remote MCP endpoint for URL transports (materialized server's URL). */
  endpoint?: string;
  /** Local command for stdio recipes. */
  command?: string;
  /** The header (URL transports) or env row (stdio) carrying the token. */
  token_header?: string;
  /** Prefix composed before the token in the secret row value ("Bearer"). */
  token_scheme?: string;
  /** Supported access levels in catalog order — the first is the default. */
  access_levels: ConnectionAccessLevel[];
  steps: ApiRecipeStep[];
  scopes: ApiRecipeScopes[];
  /** The probe declaration: the MCP tool call gating connect. */
  probe: { tool: string };
  /** Truthful coming-soon copy, or operator provisioning notes. */
  notes?: string;
}

/**
 * Read view of one connection (tasks 2.3): the recipe descriptor joined with
 * connection state, the materialized server's live status, attached agent
 * names, and the hint-only token display. `service` is the RECIPE ID the
 * connection was created from (domain.Connection) — resolve the display name
 * and icon from the recipes list with connectionServiceName/connectionIcon.
 * `server_id` is the materialized workspace MCP server — the handle agent
 * attachment toggles ride.
 */
export interface ApiConnection {
  id: string;
  workspace_id?: string;
  /** The recipe id (e.g. "github"), per domain.Connection.Service. */
  service: string;
  access_level: ConnectionAccessLevel;
  /** Read through from the materialized server row: connected|ok|error|unknown. */
  status: 'connected' | 'ok' | 'error' | 'unknown';
  status_error?: string | null;
  /** Last-4 of the stored token — the only secret shape any read carries. */
  token_hint?: string | null;
  /** The materialized workspace MCP server row. */
  server_id?: string | null;
  server_enabled?: boolean;
  tool_count?: number;
  /** Names of agents whose enabled_mcps include the materialized server. */
  attached_agents?: string[];
  created_at?: string;
  updated_at?: string;
}

/** D7: the entire connect payload — nothing else crosses the wire. */
export interface ConnectPayload {
  recipe_id: string;
  access_level: ConnectionAccessLevel;
  token: string;
}

function base(ws: string): string {
  return `/workspaces/${encodeURIComponent(ws)}/integrations`;
}

export const connectionsApi = {
  /** The registry — one entry per registered service with availability. */
  recipes: (ws: string) =>
    request<{ recipes: ApiIntegrationRecipe[] }>(`${base(ws)}/recipes`, { method: 'GET' }),

  list: (ws: string) =>
    request<{ connections: ApiConnection[] }>(`${base(ws)}/connections`, { method: 'GET' }),

  get: (ws: string, id: string) =>
    request<{ connection: ApiConnection }>(
      `${base(ws)}/connections/${encodeURIComponent(id)}`,
      { method: 'GET' }
    ),

  /** Probe-gated server-side: on failure nothing is stored and the error
   * envelope carries the upstream probe message. */
  connect: (ws: string, body: ConnectPayload) =>
    request<{ connection: ApiConnection }>(`${base(ws)}/connections`, {
      method: 'POST',
      body,
    }),

  /** D8 cascade: connection row, materialized server, and the server's
   * per-agent attachment references — the token becomes unrecoverable. */
  disconnect: (ws: string, id: string) =>
    request<void>(`${base(ws)}/connections/${encodeURIComponent(id)}`, { method: 'DELETE' }),

  /** Re-runs the recipe probe and returns the refreshed connection. */
  probe: (ws: string, id: string) =>
    request<{ connection: ApiConnection }>(
      `${base(ws)}/connections/${encodeURIComponent(id)}/probe`,
      { method: 'POST' }
    ),
};

// integrations.write holders: built-in Owner/Admin, Superadmin via its
// all-workspace-permissions set; Member never holds it and custom roles only
// by explicit grant. Same trust tier as gateways.write — mirrors
// canManageGateways in lib/gateways.ts / canWriteTools in lib/tools.ts.
export function canManageIntegrations(memberships: any[], tenant: any): boolean {
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
    perms.some((p) => p === '*' || p === 'integrations.write' || p === 'integrations.*' || p === 'workspace.*')
  );
}

export function useCanManageIntegrations(tenant: any): boolean {
  const memberships = useAuthStore((s) => s.memberships);
  const pos = useStore((s: any) => s.pos);
  if (!tenant) return canManageIntegrations(memberships, { id: pos.tenantId, sub: pos.tenantId });
  return canManageIntegrations(memberships, tenant);
}

/** Spec vocabulary: the connection view and gallery card show "Read & write". */
export function accessLevelLabel(level: ConnectionAccessLevel | string | undefined): string {
  return level === 'read_write' ? 'Read & write' : 'Read-only';
}

/** Display name for a connection: the recipe's service name when the registry
 * knows the id, else the raw id (domain.Connection.Service IS the recipe id). */
export function connectionServiceName(
  c: Pick<ApiConnection, 'service'>,
  recipes: ApiIntegrationRecipe[]
): string {
  return recipes.find((r) => r.id === c.service)?.service || c.service;
}

// The recipe's icon identifier names the service, but the in-app glyph set
// has no brand icons — fall back per service id, then to the generic plug.
const SERVICE_ICONS: Record<string, string> = {
  github: 'terminal',
  gitlab: 'globe',
  atlassian: 'file',
  slack: 'chat',
  linear: 'zap',
};

export function serviceIconKey(id: string, icon?: string): string {
  return SERVICE_ICONS[icon || ''] || SERVICE_ICONS[id] || 'plug';
}

export interface ConnectionStatusView {
  dot: string;
  label: string;
  errored: boolean;
}

// Same display contract as the MCP panes: the paused master switch wins, then
// the probed status — connected/ok green, error red, unknown gray.
export function connectionStatusView(c: Pick<ApiConnection, 'status' | 'status_error' | 'server_enabled'>): ConnectionStatusView {
  if (c.server_enabled === false) return { dot: 'bg-muted', label: 'Paused', errored: false };
  if (c.status === 'error') return { dot: 'bg-danger', label: 'Error', errored: true };
  if (c.status === 'ok' || c.status === 'connected')
    return { dot: 'bg-success', label: 'Connected', errored: false };
  return { dot: 'bg-muted', label: 'Unknown', errored: false };
}
