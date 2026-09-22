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

/** Connection kind (add-connection-http, D6): declared by the recipe, never
 * chosen by the user. "mcp" materializes a workspace MCP server; "http"
 * contributes its declared verb tools over a pinned base URL with no server
 * row. Absent on the wire means the original "mcp". */
export type ConnectionKind = 'mcp' | 'http';

/** One typed parameter of a declared verb (add-connection-http). `in` names
 * where the value binds — "path" or "query". */
export interface ApiRecipeVerbParam {
  name: string;
  type: string;
  required?: boolean;
  in: string;
}

/** One recipe-declared verb tool (D1 verbs-only surface): the entire
 * agent-facing tool surface of an HTTP-kind connection. `name` is the
 * service-prefixed tool name ("figma.get_comments"); `path` is a template
 * under the recipe's base URL. */
export interface ApiRecipeVerb {
  name: string;
  method: string;
  path: string;
  description?: string;
  params?: ApiRecipeVerbParam[];
}

/** One guided token-creation step; `url` links the service's token page.
 * Mirrors domain.RecipeStep. */
export interface ApiRecipeStep {
  title: string;
  detail?: string;
  url?: string;
}

/** Recommended token scopes for one access level (domain.RecipeScopes). For
 * oauth recipes these are the scopes consent is requested for. */
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
  /** The materialized server's transport constant (MCP-kind recipes). HTTP-kind
   * recipes have no MCP transport — the field is absent. */
  transport?: McpTransport;
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
  /** Connection kind (add-connection-http, D6) — absent/"mcp" materializes a
   * server; "http" contributes the declared verbs over `base_url`. */
  kind?: ConnectionKind;
  /** HTTP-kind only: the pinned API root every verb joins to. Server-declared
   * recipe data — never user input (D7 SSRF property). */
  base_url?: string;
  /** HTTP-kind only: the header the stored token is attached under. */
  /** HTTP-kind only: the entire declared verb tool surface (D1 verbs-only). */
  verbs?: ApiRecipeVerb[];
  /** The probe declaration: the MCP tool call gating connect. */
  probe: { tool?: string; method?: string; path?: string };
  /** OAuth-only (add-connection-oauth): the provider's authorization endpoint
   * the connect hand-off redirects through. Server-declared — never built
   * client-side. */
  authorize_url?: string;
  /** OAuth-only: the token endpoint the backend exchanges the code at. */
  token_url?: string;
  /** OAuth-only: how an instance admin registers the provider app. */
  app_registration_guidance?: string;
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
  /** Read through from the materialized server row: connected|ok|error|
   * unknown — plus `expired` (D6): the OAuth token set lapsed after a failed
   * refresh and the connection needs reauthorization. */
  status: 'connected' | 'ok' | 'error' | 'unknown' | 'expired';
  status_error?: string | null;
  /** OAuth token lifecycle (add-connection-oauth): when the access token
   * expires (refresh happens server-side within the recipe's margin). */
  expires_at?: string | null;
  /** Scopes the provider actually granted at consent time. */
  granted_scopes?: string[];
  /** Last-4 of the stored token — the only secret shape any read carries. */
  token_hint?: string | null;
  /** The materialized workspace MCP server row. HTTP-kind connections
   * (add-connection-http) have none — the field is absent/null and
   * `server_enabled` is structurally false (no server row exists to pause);
   * agent attachment then rides the raw connection id. */
  server_id?: string | null;
  server_enabled?: boolean;
  tool_count?: number;
  /** Names of agents whose enabled_mcps include the materialized server. */
  attached_agents?: string[];
  created_at?: string;
  updated_at?: string;
}

/** D7: the entire connect payload — nothing else crosses the wire. `token` is
 * omitted for oauth recipes: connect starts the consent hand-off instead of
 * storing a pasted secret. */
export interface ConnectPayload {
  recipe_id: string;
  access_level: ConnectionAccessLevel;
  token?: string;
}

/** Connect resolves one of two ways: a PAT connect returns the created
 * connection (probe-gated); an oauth connect returns the provider's authorize
 * URL the browser is redirected to (the connection is created later, by the
 * OAuth callback). */
export interface ConnectResult {
  connection?: ApiConnection;
  authorize_url?: string;
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
   * envelope carries the upstream probe message. For oauth recipes the 200
   * instead carries { authorize_url } and the browser is redirected. */
  connect: (ws: string, body: ConnectPayload) =>
    request<ConnectResult>(`${base(ws)}/connections`, {
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

  /** Reauthorization (add-connection-oauth): starts a fresh consent flow for
   * an expired connection — the token set is replaced in place on callback,
   * keeping the connection id, materialized server, and attachments. */
  reauthorize: (ws: string, id: string) =>
    request<{ authorize_url: string }>(
      `${base(ws)}/connections/${encodeURIComponent(id)}/reauthorize`,
      { method: 'POST' }
    ),
};

// ---------------------------------------------------------------------------
// Instance OAuth app registration (add-connection-oauth, instance-admin
// capability): one app per provider, instance-scoped. The client secret is
// write-only — reads carry only the last-4 `client_secret_hint`.
// ---------------------------------------------------------------------------

/** One instance-registered OAuth app (domain.OAuthApp JSON). */
export interface ApiOAuthApp {
  provider: string;
  client_id: string;
  /** Last 4 of the stored secret — the full secret never returns. */
  client_secret_hint?: string | null;
  /** The exact callback URI to configure at the provider. */
  redirect_uri: string;
  created_at?: string;
  updated_at?: string;
}

/** Body of the register/update PUT — both credentials, every save. */
export interface OAuthAppPayload {
  client_id: string;
  client_secret: string;
}

/** Master-tenant guarded (instance admin), like api.admin.*. Lives here so
 * every connections/OAuth shape and route stays in one client module. */
export const adminOAuthAppsApi = {
  /** Every supported provider row — registered ones carry client_id and the
   * secret hint; unregistered ones are empty stubs with the redirect URI. */
  list: () => request<{ apps: ApiOAuthApp[] }>('/admin/oauth-apps', { method: 'GET' }),

  get: (provider: string) =>
    request<{ app: ApiOAuthApp }>(`/admin/oauth-apps/${encodeURIComponent(provider)}`, {
      method: 'GET',
    }),

  /** Registers or updates the provider app. The secret is stored encrypted;
   * the response echoes the row with the last-4 hint only. */
  save: (provider: string, body: OAuthAppPayload) =>
    request<{ app: ApiOAuthApp }>(`/admin/oauth-apps/${encodeURIComponent(provider)}`, {
      method: 'PUT',
      body,
    }),
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

/** D6: kind is recipe-declared; absent means the original MCP kind. */
export function recipeKind(r: Pick<ApiIntegrationRecipe, 'kind'> | undefined): ConnectionKind {
  return r?.kind === 'http' ? 'http' : 'mcp';
}

/** A connection's kind, resolved from its recipe registry. A connection whose
 * recipe is unknown falls back to its shape: no materialized server = http. */
export function connectionKind(
  c: Pick<ApiConnection, 'service' | 'server_id'>,
  recipes: ApiIntegrationRecipe[]
): ConnectionKind {
  const recipe = recipes.find((r) => r.id === c.service);
  if (recipe) return recipeKind(recipe);
  return c.server_id ? 'mcp' : 'http';
}

/** The handle agent attachment stores in `enabled_mcps` for a connection: the
 * materialized server id when one exists (MCP kind), else the raw connection
 * id (HTTP kind contributes tools directly — no server row). */
export function connectionAttachId(c: Pick<ApiConnection, 'id' | 'server_id'>): string {
  return c.server_id || c.id;
}

/** Chip label for a connection kind. */
export function connectionKindLabel(kind: ConnectionKind): string {
  return kind === 'http' ? 'HTTP' : 'MCP';
}

/** Compact declared-verb-surface copy for gallery cards:
 * "6 tools — figma.get_comments, figma.list_files, figma.post_comment +3 more".
 * The full verb list renders in the connect dialog. Empty for recipes that
 * declare no verbs. */
export function verbSurfaceCopy(r: Pick<ApiIntegrationRecipe, 'verbs'>): string {
  const names = (r.verbs || []).map((v) => v.name);
  if (names.length === 0) return '';
  const head = names.slice(0, 3).join(', ');
  const rest = names.length - 3;
  return `${names.length} ${names.length === 1 ? 'tool' : 'tools'} — ${head}${rest > 0 ? ` +${rest} more` : ''}`;
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
  /** D6: the OAuth token set lapsed — the card offers Reauthorize instead of
   * treating the connection as plain broken. */
  expired: boolean;
}

// Same display contract as the MCP panes: the paused master switch wins, then
// the probed status — connected/ok green, expired amber (recoverable), error
// red, unknown gray. Kind-aware (add-connection-http): an HTTP-kind connection
// has no materialized server, so its `server_enabled` is structurally false —
// that is not a workspace pause, and the probe status alone decides.
export function connectionStatusView(
  c: Pick<ApiConnection, 'status' | 'status_error' | 'server_enabled'>,
  kind: ConnectionKind = 'mcp'
): ConnectionStatusView {
  if (kind === 'mcp' && c.server_enabled === false)
    return { dot: 'bg-muted', label: 'Paused', errored: false, expired: false };
  if (c.status === 'expired')
    return { dot: 'bg-warn', label: 'Expired', errored: false, expired: true };
  if (c.status === 'error') return { dot: 'bg-danger', label: 'Error', errored: true, expired: false };
  if (c.status === 'ok' || c.status === 'connected')
    return { dot: 'bg-success', label: 'Connected', errored: false, expired: false };
  return { dot: 'bg-muted', label: 'Unknown', errored: false, expired: false };
}
