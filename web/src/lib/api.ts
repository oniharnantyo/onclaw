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
  tools: string[];
  skills: string[];
  mcp: string[];
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
  tools?: string[];
  skills?: string[];
  mcp?: string[];
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
  tools?: string[];
  skills?: string[];
  mcp?: string[];
  avatar?: Record<string, any>;
}

export interface ApiWorkspaceSkill {
  id: string;
  workspace_id: string;
  name: string;
  description?: string;
  body?: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface ApiAgentMemory {
  agent_id: string;
  user_id: string;
  workspace_id: string;
  content: string;
  created_at?: string | null;
  updated_at?: string | null;
}

export interface ApiModel {
  id: string;
  name: string;
  efforts?: string[];
  supports_temperature?: boolean;
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
const API_ORIGIN = (import.meta.env.VITE_API_URL ?? '').replace(/\/+$/, '');

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
    getMemory: (ws: string, agent: string) =>
      request<ApiAgentMemory>(`/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}/memory`, {
        method: 'GET',
      }),
    deleteMemory: (ws: string, agent: string) =>
      request<void>(`/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}/memory`, {
        method: 'DELETE',
      }),
  },
  skills: {
    list: (ws: string) =>
      request<{ skills: ApiWorkspaceSkill[] }>(`/workspaces/${encodeURIComponent(ws)}/skills`, {
        method: 'GET',
      }),
    get: (ws: string, id: string) =>
      request<{ skill: ApiWorkspaceSkill }>(`/workspaces/${encodeURIComponent(ws)}/skills/${encodeURIComponent(id)}`, {
        method: 'GET',
      }),
    create: (ws: string, body: { name: string; body?: string; description?: string; enabled?: boolean }) =>
      request<{ skill: ApiWorkspaceSkill }>(`/workspaces/${encodeURIComponent(ws)}/skills`, {
        method: 'POST',
        body,
      }),
    patch: (ws: string, id: string, body: { name?: string; body?: string; description?: string; enabled?: boolean }) =>
      request<{ skill: ApiWorkspaceSkill }>(`/workspaces/${encodeURIComponent(ws)}/skills/${encodeURIComponent(id)}`, {
        method: 'PATCH',
        body,
      }),
    delete: (ws: string, id: string) =>
      request<void>(`/workspaces/${encodeURIComponent(ws)}/skills/${encodeURIComponent(id)}`, {
        method: 'DELETE',
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

