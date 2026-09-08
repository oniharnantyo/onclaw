import { useConnectionStore } from '../store/connection';

export const TOKEN_STORAGE_KEY = 'od_token';

export function getToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_STORAGE_KEY);
  } catch {
    return null;
  }
}

export function setToken(token: string): void {
  try {
    localStorage.setItem(TOKEN_STORAGE_KEY, token);
  } catch {
    // LocalStorage might be disabled or unavailable
  }
}

export function clearToken(): void {
  try {
    localStorage.removeItem(TOKEN_STORAGE_KEY);
  } catch {
    // LocalStorage might be disabled or unavailable
  }
}

export interface ApiErrorDetail {
  field?: string;
  message: string;
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly details: ApiErrorDetail[];
  readonly requestId?: string;

  constructor(
    status: number,
    code: string,
    message: string,
    details: ApiErrorDetail[] = [],
    requestId?: string
  ) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.details = details;
    this.requestId = requestId;
  }
}

export function formatApiError(error: unknown, fallbackMessage = 'An unexpected error occurred'): string {
  if (error instanceof ApiError) {
    switch (error.code) {
      case 'last_owner_protected':
        return 'The last owner cannot be removed or demoted.';
      case 'conflict':
        return error.message || 'A conflict occurred with that name or email.';
      case 'forbidden':
        return error.message || "You don't have permission to perform this action.";
      case 'unauthenticated':
        return 'Invalid email or password.';
      case 'invalid_request':
        return error.message || 'Invalid request parameters.';
      case 'not_found':
        return error.message || 'Resource not found.';
      case 'payload_too_large':
        return 'The uploaded file exceeds the size limit.';
      case 'network':
        return 'Network connection failed. Please check your connection.';
      default:
        return error.message || fallbackMessage;
    }
  }
  if (error instanceof Error) {
    if (error.name === 'TypeError' && error.message.toLowerCase().includes('fetch')) {
      return 'Network connection failed. Please check your connection.';
    }
    return error.message || fallbackMessage;
  }
  return fallbackMessage;
}

// API models
export interface ApiUser {
  id: string;
  email: string;
  name: string;
  avatar_key?: string | null;
  avatar_url?: string | null;
  disabled_at?: string | null;
  is_superadmin?: boolean;
  created_at: string;
  updated_at: string;
  membership_count?: number;
}

export interface ApiWorkspace {
  id: string;
  slug: string;
  name: string;
  timezone: string;
  is_master: boolean;
  disabled_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface ApiRole {
  id: string;
  workspace_id: string;
  name: string;
  is_owner: boolean;
  permissions: string[];
  built_in: boolean;
  created_at: string;
}

export interface ApiMember {
  workspace_id: string;
  user_id: string;
  role_id: string;
  role?: ApiRole;
  created_at: string;
}

export interface ApiMemberView {
  workspace_id: string;
  workspace_slug?: string;
  workspace_name?: string;
  user_id: string;
  email: string;
  name: string;
  avatar_key?: string | null;
  avatar_url?: string | null;
  role_id: string;
  role_name: string;
  role?: ApiRole;
  workspace?: ApiWorkspace;
  invited?: boolean;
  joined_at: string;
}

export interface ApiMemberItem {
  user_id: string;
  email: string;
  name: string;
  avatar_key?: string | null;
  avatar_url?: string | null;
  role_id: string;
  role_name: string;
  role?: ApiRole;
  invited?: boolean;
  joined_at: string;
}

export interface ApiAdminWorkspaceItem {
  id: string;
  slug: string;
  name: string;
  timezone: string;
  is_master: boolean;
  disabled_at?: string | null;
  created_at: string;
  updated_at: string;
  member_count: number;
}

export interface ApiProviderConfig {
  id: string;
  workspace_id: string;
  type: string;
  name: string;
  base_url?: string;
  key_set: boolean;
  key_hint: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface ApiProviderVerifyResult {
  ok: boolean;
  error?: string;
}

export interface ApiToolConfigFieldOption {
  value: string;
  label: string;
}

export interface ApiToolConfigField {
  key: string;
  label: string;
  type: 'secret' | 'text' | 'number' | 'boolean' | 'enum';
  required: boolean;
  help?: string;
  default?: unknown;
  options?: ApiToolConfigFieldOption[];
  /** Rendered only while the named sibling field currently holds `equals`. */
  show_if?: { field: string; equals: string };
}

export interface ApiToolSettings {
  key: string;
  display_name: string;
  description: string;
  group: string;
  icon_key: string;
  configurable: boolean;
  config_schema?: ApiToolConfigField[];
  enabled: boolean;
  configured: boolean;
  config: Record<string, unknown>;
}

// ---------------------------------------------------------------------------
// web.search provider stacks (config shape locked in design D1/D5)
// ---------------------------------------------------------------------------

/** One ordered web.search provider entry. Config VIEW payloads never include
 * `api_key` — only the last-4 `api_key_hint` nested per entry. UPSERT payloads
 * carry `api_key` only when a secret is entered; a known `id` with an omitted
 * or empty `api_key` means "keep the stored secret". SearXNG entries carry
 * `base_url` instead of a key. */
export interface ApiSearchEntry {
  id?: string;
  name: string;
  provider: string;
  api_key?: string;
  base_url?: string;
  api_key_hint?: string;
}

export interface ApiWebSearchConfig {
  entries: ApiSearchEntry[];
  /** Positive integer, default 10, max 60 — bounds each failover attempt. */
  request_timeout_seconds?: number;
}

export type AgentAutonomy = 'approval' | 'suggest' | 'full';
export type PromptsStatus = 'generating' | 'ready' | 'failed';

export interface ApiAgent {
  id: string;
  workspace_id: string;
  slug: string;
  name: string;
  role: string;
  description: string;
  brief: string;
  identity: string;
  soul: string;
  bootstrap: string;
  provider_id: string;
  model: string;
  temperature: number;
  max_tokens?: number | null;
  effort?: string | null;
  autonomy: AgentAutonomy;
  context_window?: number | null;
  /** Server-computed read-only echoes for the context meter (context meter change). */
  effective_context_window?: number;
  /** Token count at which the backend summarizes — the meter's warn threshold. */
  summarization_trigger_tokens?: number;
  tools: string[];
  /** Agent-tier skill names when the server lists them; never a payload field. */
  skills?: string[];
  /** MCP server UUIDs the agent opts into (design D1). */
  enabled_mcps: string[];
  avatar: Record<string, any>;
  prompts_status: PromptsStatus;
  prompts_error?: string | null;
  created_by?: string | null;
  updated_by?: string | null;
  created_at: string;
  updated_at: string;
}

export interface CreateAgentPayload {
  name: string;
  slug: string;
  role: string;
  description?: string;
  brief: string;
  provider_id?: string;
  model: string;
  temperature?: number;
  max_tokens?: number;
  effort?: string;
  autonomy?: AgentAutonomy;
  context_window?: number | null;
  tools?: string[];
  skills?: string[];
  enabled_mcps?: string[];
  avatar?: Record<string, any>;
}

export interface PatchAgentPayload {
  name?: string;
  slug?: string;
  role?: string;
  description?: string;
  brief?: string;
  identity?: string;
  soul?: string;
  provider_id?: string;
  model?: string;
  temperature?: number;
  max_tokens?: number;
  effort?: string;
  autonomy?: AgentAutonomy;
  context_window?: number | null;
  tools?: string[];
  skills?: string[];
  enabled_mcps?: string[];
  avatar?: Record<string, any>;
}

// ---------------------------------------------------------------------------
// Workspace skills (registry + system tier)
// ---------------------------------------------------------------------------

export type SkillSource = 'authored' | 'upload' | 'git' | 'fork' | 'system';
export type SkillTier = 'system' | 'workspace' | 'agent';

export interface ApiSkillDependencies {
  tools?: string[];
  binaries?: string[];
  python?: string[];
}

export interface ApiSkillDependencyStatus {
  kind: 'tools' | 'binaries' | 'python';
  name: string;
  status: 'met' | 'missing' | 'unprovisioned';
  /** Per-platform install command hint (binaries). */
  install_hint?: string;
  detail?: string;
}

export interface ApiWorkspaceSkill {
  id: string;
  workspace_id?: string;
  tier: SkillTier;
  name: string;
  description?: string;
  version: string;
  source: SkillSource;
  /** System-tier entries are locked: no toggle, edit, or uninstall exists. */
  locked?: boolean;
  enabled?: boolean;
  dependencies?: ApiSkillDependencies;
  dependency_status?: ApiSkillDependencyStatus[];
  /** SKILL.md body — present on detail/edit responses. */
  body?: string;
  created_at: string;
  updated_at: string;
}

/** A skill directory discovered inside an uploaded archive or fetched git tree. */
export interface ApiDiscoveredSkill {
  name: string;
  description?: string;
  dependencies?: ApiSkillDependencies;
  dependency_status?: ApiSkillDependencyStatus[];
}

/** Archive inspection result: tree preview + inferred dependency report. */
export interface ApiSkillInspectResult {
  skill: ApiDiscoveredSkill;
  files: string[];
}

/** Install options shared by every source on the dependency review step. */
export interface SkillInstallOptions {
  /** Pre-checked: add missing tools to the workspace gate and every agent allowlist. */
  enable_everywhere?: boolean;
  /** Auto-provision python packages into the shared workspace venv. */
  provision_python?: boolean;
  /** Same-name import: replace tree, bump version, update row. */
  overwrite?: boolean;
}

export type CreateSkillPayload =
  | ({ source: 'authored'; name: string; description?: string; body: string } & SkillInstallOptions)
  | ({ source: 'git'; url: string; ref?: string; token?: string; names: string[] } & SkillInstallOptions)
  | ({ source: 'fork'; system_skill: string } & SkillInstallOptions);

export interface ApiSkillInstallResult {
  skill: ApiWorkspaceSkill;
  dependency_status?: ApiSkillDependencyStatus[];
}

/** USER.md / WORKSPACE.md payload (change agent-memory). The size cap is
 * server-owned — every read carries `max_chars` and the UI never hardcodes it. */
export interface ApiMemory {
  content: string;
  max_chars: number;
  updated_at: string | null;
}

export interface ApiModel {
  id: string;
  name: string;
  efforts?: string[];
  supports_temperature?: boolean;
  context_limit?: number | null;
}

export interface ApiModelsResult {
  source: 'live' | 'catalog' | 'none';
  models: ApiModel[];
}

export interface CreateWorkspacePayload {
  name: string;
  slug: string;
  timezone?: string;
  provider?: {
    type: string;
    name: string;
    base_url?: string;
    key?: string;
    enabled?: boolean;
  };
  starter_agent?: CreateAgentPayload;
}

export interface CreateWorkspaceResult {
  workspace: ApiWorkspace;
  role: ApiRole;
  member: ApiMember;
  provider?: ApiProviderConfig;
  starter_agent?: ApiAgent;
}

// ---------------------------------------------------------------------------
// MCP servers (workspace registry + agent-private; change integrate-mcp-servers)
// ---------------------------------------------------------------------------

export type McpTransport = 'stdio' | 'streamable_http' | 'sse';

/** Probe outcome persisted on the row. The spec's vocabulary is `connected`;
 * `ok` is accepted as its terse alias and `unknown` marks a never-probed row. */
export type McpServerStatus = 'connected' | 'ok' | 'error' | 'unknown';

/** Read view of one env-var/header row. Secret values are write-only and never
 * round-trip — reads carry only the plaintext `value_hint` beside the name. */
export interface ApiMcpSecretRow {
  name: string;
  value_hint?: string | null;
}

export interface ApiMcpServer {
  id: string;
  workspace_id?: string;
  /** Set on agent-private servers only. */
  agent_id?: string;
  name: string;
  transport: McpTransport;
  /** stdio transport: executable plus whitespace-split args. */
  command?: string;
  args?: string[];
  /** stdio transport: env-var rows (secrets hinted, never echoed). */
  env?: ApiMcpSecretRow[];
  /** streamable_http / sse transports. */
  url?: string;
  /** streamable_http / sse transports: header rows (secrets hinted, never echoed). */
  headers?: ApiMcpSecretRow[];
  /** Master switch — a paused server contributes no tools despite agent opt-in. */
  enabled: boolean;
  status: McpServerStatus;
  status_error?: string | null;
  tool_count: number;
  created_at: string;
  updated_at: string;
}

/** Write-side env/header row. Values are write-only: an omitted or empty
 * `value` keeps the stored secret; a non-empty value replaces it. */
export interface ApiMcpSecretRowInput {
  name: string;
  value?: string;
}

export interface McpServerPayload {
  name: string;
  transport: McpTransport;
  command?: string;
  args?: string[];
  env?: ApiMcpSecretRowInput[];
  url?: string;
  headers?: ApiMcpSecretRowInput[];
  enabled?: boolean;
}

export type PatchMcpServerPayload = Partial<McpServerPayload>;


type UnauthorizedHandler = () => void;
const unauthorizedHandlers = new Set<UnauthorizedHandler>();

export function onUnauthorized(handler: UnauthorizedHandler): () => void {
  unauthorizedHandlers.add(handler);
  return () => {
    unauthorizedHandlers.delete(handler);
  };
}

function notifyUnauthorized() {
  clearToken();
  unauthorizedHandlers.forEach((handler) => {
    try {
      handler();
    } catch (err) {
      console.error('Error in onUnauthorized handler:', err);
    }
  });
}

// Absolute API origin from build-time env; empty string keeps requests same-origin.
export const API_ORIGIN = (import.meta.env.VITE_API_URL ?? '').replace(/\/+$/, '');

const API_BASE = `${API_ORIGIN}/api/v1`;

export interface RequestOptions extends Omit<RequestInit, 'body'> {
  body?: any;
  skipAuth?: boolean;
}

export async function request<T = any>(endpoint: string, options: RequestOptions = {}): Promise<T> {
  const url = endpoint.startsWith('http://') || endpoint.startsWith('https://')
    ? endpoint
    : endpoint.startsWith(API_BASE)
      ? endpoint
      : `${API_BASE}${endpoint.startsWith('/') ? '' : '/'}${endpoint}`;

  const headers = new Headers(options.headers || {});

  if (!options.skipAuth) {
    const token = getToken();
    if (token && !headers.has('Authorization')) {
      headers.set('Authorization', `Bearer ${token}`);
    }
  }

  let body = options.body;
  if (body !== undefined && !(body instanceof FormData) && typeof body !== 'string') {
    if (!headers.has('Content-Type')) {
      headers.set('Content-Type', 'application/json');
    }
    body = JSON.stringify(body);
  }

  let response: Response;
  try {
    response = await fetch(url, {
      ...options,
      headers,
      body,
    });
  } catch {
    useConnectionStore.getState().reportFailure();
    throw new ApiError(0, 'network', 'Network connection failed. Please check your connection.');
  }

  if (response.status === 0) {
    useConnectionStore.getState().reportFailure();
    throw new ApiError(0, 'network', 'Network connection failed. Please check your connection.');
  }

  useConnectionStore.getState().reportSuccess();

  if (response.status === 204) {
    return undefined as unknown as T;
  }

  let data: any = null;
  const contentType = response.headers.get('Content-Type') || '';
  if (contentType.includes('application/json')) {
    try {
      data = await response.json();
    } catch {
      // ignore json parse error
    }
  }

  if (!response.ok) {
    const errorPayload = data && data.error ? data.error : null;
    const code = errorPayload?.code || (response.status === 401 ? 'unauthenticated' : response.status === 403 ? 'forbidden' : response.status === 404 ? 'not_found' : response.status === 409 ? 'conflict' : 'error');
    const message = errorPayload?.message || response.statusText || 'Request failed';
    const details = errorPayload?.details || [];
    const requestId = errorPayload?.request_id || response.headers.get('X-Request-ID') || undefined;

    if (response.status === 401 && !endpoint.includes('/auth/login')) {
      notifyUnauthorized();
    }

    throw new ApiError(response.status, code, message, details, requestId);
  }

  return data as T;
}

// ---------------------------------------------------------------------------
// Workspace API keys (native settings surface; plaintext returned once)
// ---------------------------------------------------------------------------

export interface ApiWorkspaceKey {
  id: string;
  name: string;
  key_prefix: string;
  key_suffix: string;
  created_by: string;
  created_at: string;
  revoked_at?: string | null;
}

export const apiKeys = {
  list: (wsSlug: string) =>
    request<{ api_keys: ApiWorkspaceKey[] }>(`/workspaces/${encodeURIComponent(wsSlug)}/api-keys`),
  create: (wsSlug: string, name: string) =>
    request<{ key: string; api_key: ApiWorkspaceKey }>(
      `/workspaces/${encodeURIComponent(wsSlug)}/api-keys`,
      { method: 'POST', body: { name } },
    ),
  revoke: (wsSlug: string, id: string) =>
    request<void>(`/workspaces/${encodeURIComponent(wsSlug)}/api-keys/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),
  // JWT-authenticated chat key exchange: mints a workspace-scoped key for the
  // caller (membership of any role suffices); plaintext returned once.
  exchange: (wsSlug: string) =>
    request<{ key: string; api_key: ApiWorkspaceKey }>(
      `/workspaces/${encodeURIComponent(wsSlug)}/api-keys/exchange`,
      { method: 'POST', body: {} },
    ),
};

export const api = {
  request,
  onUnauthorized,
  auth: {
    login: (body: { email: string; password: string; provider?: string }) =>
      request<{ token: string; user: ApiUser }>('/auth/login', {
        method: 'POST',
        body,
        skipAuth: true,
      }),
    logout: () =>
      request<void>('/auth/logout', {
        method: 'POST',
      }),
    me: () =>
      request<{ user: ApiUser; memberships: ApiMemberView[] }>('/auth/me', {
        method: 'GET',
      }),
  },
  workspaces: {
    list: () =>
      request<{ workspaces: ApiMemberView[] }>('/workspaces', {
        method: 'GET',
      }),
    create: (body: CreateWorkspacePayload) =>
      request<CreateWorkspaceResult>('/workspaces', {
        method: 'POST',
        body,
      }),
    get: (ws: string) =>
      request<{ workspace: ApiWorkspace; role: ApiRole; my_role: ApiRole; member: ApiMember }>(`/workspaces/${encodeURIComponent(ws)}`, {
        method: 'GET',
      }),
    patch: (ws: string, body: { name?: string; timezone?: string }) =>
      request<{ workspace: ApiWorkspace }>(`/workspaces/${encodeURIComponent(ws)}`, {
        method: 'PATCH',
        body,
      }),
  },
  members: {
    list: (ws: string) =>
      request<{ members: ApiMemberItem[] }>(`/workspaces/${encodeURIComponent(ws)}/members`, {
        method: 'GET',
      }),
    add: (ws: string, body: { email: string; role_id: string }) =>
      request<{ member: ApiMember; user: ApiUser; role: ApiRole }>(`/workspaces/${encodeURIComponent(ws)}/members`, {
        method: 'POST',
        body,
      }),
    patch: (ws: string, uid: string, body: { role_id: string }) =>
      request<{ member: ApiMember; role: ApiRole }>(`/workspaces/${encodeURIComponent(ws)}/members/${encodeURIComponent(uid)}`, {
        method: 'PATCH',
        body,
      }),
    remove: (ws: string, uid: string) =>
      request<void>(`/workspaces/${encodeURIComponent(ws)}/members/${encodeURIComponent(uid)}`, {
        method: 'DELETE',
      }),
  },
  roles: {
    list: (ws: string) =>
      request<{ roles: ApiRole[] }>(`/workspaces/${encodeURIComponent(ws)}/roles`, {
        method: 'GET',
      }),
  },
  tools: {
    list: (ws: string) =>
      request<{ tools: ApiToolSettings[] }>(`/workspaces/${encodeURIComponent(ws)}/tools`, {
        method: 'GET',
      }),
    update: (ws: string, key: string, body: { enabled?: boolean; config?: Record<string, unknown> }) =>
      request<{ tool: ApiToolSettings }>(
        `/workspaces/${encodeURIComponent(ws)}/tools/${encodeURIComponent(key)}`,
        {
          method: 'PATCH',
          body,
        }
      ),
  },
  providers: {
    list: (ws: string) =>
      request<{ providers: ApiProviderConfig[] }>(`/workspaces/${encodeURIComponent(ws)}/providers`, {
        method: 'GET',
      }),
    create: (
      ws: string,
      body: { type: string; name: string; base_url?: string; key?: string; enabled?: boolean }
    ) =>
      request<{ provider: ApiProviderConfig }>(`/workspaces/${encodeURIComponent(ws)}/providers`, {
        method: 'POST',
        body,
      }),
    patch: (
      ws: string,
      id: string,
      body: { name?: string; base_url?: string; key?: string; enabled?: boolean }
    ) =>
      request<{ provider: ApiProviderConfig }>(`/workspaces/${encodeURIComponent(ws)}/providers/${encodeURIComponent(id)}`, {
        method: 'PATCH',
        body,
      }),
    delete: (ws: string, id: string) =>
      request<void>(`/workspaces/${encodeURIComponent(ws)}/providers/${encodeURIComponent(id)}`, {
        method: 'DELETE',
      }),
    verify: (ws: string, id: string) =>
      request<ApiProviderVerifyResult>(`/workspaces/${encodeURIComponent(ws)}/providers/${encodeURIComponent(id)}/verify`, {
        method: 'POST',
      }),
    models: (ws: string, id: string) =>
      request<ApiModelsResult>(`/workspaces/${encodeURIComponent(ws)}/providers/${encodeURIComponent(id)}/models`, {
        method: 'GET',
      }),
    modelsPreview: (body: { type: string; base_url?: string; key?: string; api_key?: string }) =>
      request<ApiModelsResult>('/providers/models-preview', {
        method: 'POST',
        body,
      }),
  },
  // Workspace MCP registry (tools.read for reads, tools.write for writes —
  // gating is server-side; the pane mirrors it for affordance visibility).
  mcp: {
    list: (ws: string) =>
      request<{ servers: ApiMcpServer[] }>(`/workspaces/${encodeURIComponent(ws)}/mcp-servers`, {
        method: 'GET',
      }),
    create: (ws: string, body: McpServerPayload) =>
      request<{ server: ApiMcpServer }>(`/workspaces/${encodeURIComponent(ws)}/mcp-servers`, {
        method: 'POST',
        body,
      }),
    update: (ws: string, id: string, body: PatchMcpServerPayload) =>
      request<{ server: ApiMcpServer }>(
        `/workspaces/${encodeURIComponent(ws)}/mcp-servers/${encodeURIComponent(id)}`,
        { method: 'PATCH', body }
      ),
    delete: (ws: string, id: string) =>
      request<void>(`/workspaces/${encodeURIComponent(ws)}/mcp-servers/${encodeURIComponent(id)}`, {
        method: 'DELETE',
      }),
    // Bounded (~10s) re-probe; the response carries the refreshed row —
    // status, tool count, and on failure the connection error message.
    probe: (ws: string, id: string) =>
      request<{ server: ApiMcpServer }>(
        `/workspaces/${encodeURIComponent(ws)}/mcp-servers/${encodeURIComponent(id)}/probe`,
        { method: 'POST' }
      ),
  },
  agents: {
    list: (ws: string) =>
      request<{ agents: ApiAgent[] }>(`/workspaces/${encodeURIComponent(ws)}/agents`, {
        method: 'GET',
      }),
    get: (ws: string, agent: string) =>
      request<{ agent: ApiAgent }>(`/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}`, {
        method: 'GET',
      }),
    create: (ws: string, body: CreateAgentPayload) =>
      request<{ agent: ApiAgent }>(`/workspaces/${encodeURIComponent(ws)}/agents`, {
        method: 'POST',
        body,
      }),
    patch: (ws: string, agent: string, body: PatchAgentPayload) =>
      request<{ agent: ApiAgent }>(`/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}`, {
        method: 'PATCH',
        body,
      }),
    delete: (ws: string, agent: string) =>
      request<void>(`/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}`, {
        method: 'DELETE',
      }),
    regenerate: (ws: string, agent: string, instruction?: string) => {
      const trimmed = instruction?.trim();
      return request<{ agent: ApiAgent }>(`/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}/regenerate`, {
        method: 'POST',
        ...(trimmed ? { body: { instruction: trimmed } } : {}),
      });
    },
    resolveApproval: (ws: string, agent: string, sessionId: string, interruptId: string, approved: boolean) =>
      request<{ resumed: boolean; interrupt_id: string; approved: boolean }>(
        `/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}/sessions/${encodeURIComponent(sessionId)}/approvals/${encodeURIComponent(interruptId)}`,
        { method: 'POST', body: { approved } },
      ),
    // Translated transcript events for an agent session (server-authoritative history).
    sessionEvents: (ws: string, agent: string, sessionId: string) =>
      request<{ events: any[]; next: string }>(
        `/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}/sessions/${encodeURIComponent(sessionId)}/events`,
        { method: 'GET' },
      ),
    // Cancels the live run for a session; the :turn segment is addressed by
    // splitting the in-flight response id (resp_<session>_<turn>) client-side.
    cancelRun: (ws: string, agent: string, sessionId: string, turnId: string) =>
      request<{ cancelled: boolean }>(
        `/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}/sessions/${encodeURIComponent(sessionId)}/runs/${encodeURIComponent(turnId)}/cancel`,
        { method: 'POST' },
      ),
    // Agent-tier skills: bodies live in the agent's own skills directory;
    // install/remove are the only operations (presence is the state).
    listSkills: (ws: string, agent: string) =>
      request<{ skills: ApiWorkspaceSkill[] }>(`/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}/skills`, {
        method: 'GET',
      }),
    installSkill: (ws: string, agent: string, body: { name: string; description?: string; body: string }) =>
      request<{ skill: ApiWorkspaceSkill }>(`/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}/skills`, {
        method: 'POST',
        body,
      }),
    removeSkill: (ws: string, agent: string, name: string) =>
      request<void>(`/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}/skills/${encodeURIComponent(name)}`, {
        method: 'DELETE',
      }),
    // Agent-private MCP servers (agents.write): same payload/response shapes
    // as the workspace registry, scoped under the agent and invisible to the
    // workspace pane.
    listMcpServers: (ws: string, agent: string) =>
      request<{ servers: ApiMcpServer[] }>(
        `/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}/mcp-servers`,
        { method: 'GET' }
      ),
    createMcpServer: (ws: string, agent: string, body: McpServerPayload) =>
      request<{ server: ApiMcpServer }>(
        `/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}/mcp-servers`,
        { method: 'POST', body }
      ),
    updateMcpServer: (ws: string, agent: string, id: string, body: PatchMcpServerPayload) =>
      request<{ server: ApiMcpServer }>(
        `/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}/mcp-servers/${encodeURIComponent(id)}`,
        { method: 'PATCH', body }
      ),
    deleteMcpServer: (ws: string, agent: string, id: string) =>
      request<void>(
        `/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}/mcp-servers/${encodeURIComponent(id)}`,
        { method: 'DELETE' }
      ),
    probeMcpServer: (ws: string, agent: string, id: string) =>
      request<{ server: ApiMcpServer }>(
        `/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}/mcp-servers/${encodeURIComponent(id)}/probe`,
        { method: 'POST' }
      ),
  },
  skills: {
    // List includes locked system-tier entries alongside registry rows.
    list: (ws: string) =>
      request<{ skills: ApiWorkspaceSkill[] }>(`/workspaces/${encodeURIComponent(ws)}/skills`, {
        method: 'GET',
      }),
    get: (ws: string, name: string) =>
      request<{ skill: ApiWorkspaceSkill }>(`/workspaces/${encodeURIComponent(ws)}/skills/${encodeURIComponent(name)}`, {
        method: 'GET',
      }),
    create: (ws: string, body: CreateSkillPayload) =>
      request<ApiSkillInstallResult>(`/workspaces/${encodeURIComponent(ws)}/skills`, {
        method: 'POST',
        body,
      }),
    // Upload source: multipart with the zip archive under the `archive` field.
    createUpload: (
      ws: string,
      payload: { archive: File | Blob; name?: string } & SkillInstallOptions
    ) => {
      const form = new FormData();
      form.append('archive', payload.archive);
      if (payload.name) form.append('name', payload.name);
      if (payload.enable_everywhere !== undefined) form.append('enable_everywhere', String(payload.enable_everywhere));
      if (payload.provision_python !== undefined) form.append('provision_python', String(payload.provision_python));
      if (payload.overwrite !== undefined) form.append('overwrite', String(payload.overwrite));
      return request<ApiSkillInstallResult>(`/workspaces/${encodeURIComponent(ws)}/skills`, {
        method: 'POST',
        body: form,
      });
    },
    // Metadata/body update (PUT).
    update: (ws: string, name: string, body: { description?: string; body?: string }) =>
      request<{ skill: ApiWorkspaceSkill }>(`/workspaces/${encodeURIComponent(ws)}/skills/${encodeURIComponent(name)}`, {
        method: 'PUT',
        body,
      }),
    // Master enable/disable switch (PATCH).
    setEnabled: (ws: string, name: string, enabled: boolean) =>
      request<{ skill: ApiWorkspaceSkill }>(`/workspaces/${encodeURIComponent(ws)}/skills/${encodeURIComponent(name)}`, {
        method: 'PATCH',
        body: { enabled },
      }),
    // Uninstall (DELETE): removes the registry row and the body tree.
    uninstall: (ws: string, name: string) =>
      request<void>(`/workspaces/${encodeURIComponent(ws)}/skills/${encodeURIComponent(name)}`, {
        method: 'DELETE',
      }),
    // Dependency re-check: re-probes LookPath/imports and updates the row.
    recheck: (ws: string, name: string) =>
      request<{ skill: ApiWorkspaceSkill }>(`/workspaces/${encodeURIComponent(ws)}/skills/${encodeURIComponent(name)}/dependencies/recheck`, {
        method: 'POST',
      }),
    // Archive inspection: validates SKILL.md-at-root, returns the tree preview
    // and the inferred dependency report without installing anything.
    inspectUpload: (ws: string, archive: File | Blob) => {
      const form = new FormData();
      form.append('archive', archive);
      return request<ApiSkillInspectResult>(`/workspaces/${encodeURIComponent(ws)}/skills/inspect`, {
        method: 'POST',
        body: form,
      });
    },
    // Git/URL discovery: shallow fetch, scan for SKILL.md directories.
    inspectGit: (ws: string, body: { url: string; ref?: string; token?: string }) =>
      request<{ skills: ApiDiscoveredSkill[] }>(`/workspaces/${encodeURIComponent(ws)}/skills/inspect/git`, {
        method: 'POST',
        body,
      }),
  },

  users: {
    patchMe: (body: { name?: string; avatar_url?: string; avatar_key?: string; clear_avatar?: boolean }) =>
      request<{ user: ApiUser }>('/users/me', {
        method: 'PATCH',
        body,
      }),
    uploadAvatar: (file: File | Blob) => {
      const form = new FormData();
      form.append('file', file);
      return request<{ avatar_url: string; user: ApiUser }>('/users/me/avatar', {
        method: 'POST',
        body: form,
      });
    },
  },
  // Per-user (USER.md) and shared (WORKSPACE.md) memory. Routes key on the
  // workspace slug like every other workspace-scoped call; the user pair is
  // self-scoped (`/me/memory`), the workspace pair follows the settings
  // write permission server-side (Members get 403 on the shared PUT).
  memory: {
    getMine: (ws: string) =>
      request<ApiMemory>(`/workspaces/${encodeURIComponent(ws)}/me/memory`, {
        method: 'GET',
      }),
    updateMine: (ws: string, body: { content: string }) =>
      request<ApiMemory>(`/workspaces/${encodeURIComponent(ws)}/me/memory`, {
        method: 'PUT',
        body,
      }),
    getWorkspace: (ws: string) =>
      request<ApiMemory>(`/workspaces/${encodeURIComponent(ws)}/memory`, {
        method: 'GET',
      }),
    updateWorkspace: (ws: string, body: { content: string }) =>
      request<ApiMemory>(`/workspaces/${encodeURIComponent(ws)}/memory`, {
        method: 'PUT',
        body,
      }),
  },
  admin: {
    workspaces: {
      list: () =>
        request<{ workspaces: ApiAdminWorkspaceItem[] }>('/admin/workspaces', {
          method: 'GET',
        }),
      create: (body: { name: string; slug: string; timezone?: string; owner_email: string }) =>
        request<{ workspace: ApiAdminWorkspaceItem; role: ApiRole; member: ApiMember; user: ApiUser }>('/admin/workspaces', {
          method: 'POST',
          body,
        }),
      patch: (ws: string, body: { name?: string; timezone?: string }) =>
        request<{ workspace: ApiWorkspace }>(`/admin/workspaces/${encodeURIComponent(ws)}`, {
          method: 'PATCH',
          body,
        }),
      transferOwner: (ws: string, body: { user_id?: string; email?: string }) =>
        request<{ workspace: ApiWorkspace; owner: ApiUser; role: ApiRole }>(`/admin/workspaces/${encodeURIComponent(ws)}/owner`, {
          method: 'PATCH',
          body,
        }),
      listMembers: (ws: string) =>
        request<{ members: ApiMemberItem[] }>(`/admin/workspaces/${encodeURIComponent(ws)}/members`, {
          method: 'GET',
        }),
      addMember: (ws: string, body: { user_id?: string; email?: string; role_id?: string; role_name?: string }) =>
        request<{ member: ApiMember; user: ApiUser; role: ApiRole }>(`/admin/workspaces/${encodeURIComponent(ws)}/members`, {
          method: 'POST',
          body,
        }),
      disable: (ws: string) =>
        request<{ workspace: ApiWorkspace }>(`/admin/workspaces/${encodeURIComponent(ws)}/disable`, {
          method: 'POST',
        }),
      enable: (ws: string) =>
        request<{ workspace: ApiWorkspace }>(`/admin/workspaces/${encodeURIComponent(ws)}/enable`, {
          method: 'POST',
        }),
    },
    users: {
      list: () =>
        request<{ users: ApiUser[] }>('/admin/users', {
          method: 'GET',
        }),
      create: (body: { email: string; name?: string; password?: string }) =>
        request<{ user: ApiUser }>('/admin/users', {
          method: 'POST',
          body,
        }),
      disable: (uid: string) =>
        request<{ user: ApiUser }>(`/admin/users/${encodeURIComponent(uid)}/disable`, {
          method: 'POST',
        }),
      enable: (uid: string) =>
        request<{ user: ApiUser }>(`/admin/users/${encodeURIComponent(uid)}/enable`, {
          method: 'POST',
        }),
    },
    superadmins: {
      grant: (body: { user_id?: string; email?: string }) =>
        request<{ member: ApiMember; user: ApiUser; role: ApiRole }>('/admin/superadmins', {
          method: 'POST',
          body,
        }),
      revoke: (uid: string) =>
        request<void>(`/admin/superadmins/${encodeURIComponent(uid)}`, {
          method: 'DELETE',
        }),
    },
  },
};

export async function pollAgentPromptsStatus(
  workspaceId: string,
  agentIdOrSlug: string,
  options?: {
    intervalMs?: number;
    maxAttempts?: number;
    signal?: AbortSignal;
    onUpdate?: (agent: ApiAgent) => void;
  }
): Promise<ApiAgent> {
  const intervalMs = options?.intervalMs ?? 1000;
  const maxAttempts = options?.maxAttempts ?? 60;
  let attempts = 0;

  while (attempts < maxAttempts) {
    if (options?.signal?.aborted) {
      throw new Error('Polling aborted');
    }
    const res = await api.agents.get(workspaceId, agentIdOrSlug);
    const agent = res.agent;
    if (options?.onUpdate) {
      options.onUpdate(agent);
    }
    if (agent.prompts_status !== 'generating') {
      return agent;
    }
    attempts++;
    await new Promise((resolve) => setTimeout(resolve, intervalMs));
  }
  const finalRes = await api.agents.get(workspaceId, agentIdOrSlug);
  return finalRes.agent;
}

