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

/** Workspace default model pair (refactor-workspace-settings D2): both
 * fields set pins the default agents may inherit; null means no default. */
export interface ApiDefaultModel {
  provider_id: string;
  model: string;
}

export interface ApiWorkspace {
  id: string;
  slug: string;
  name: string;
  timezone: string;
  is_master: boolean;
  default_model?: ApiDefaultModel | null;
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
  /** Community-catalog provider id stored on compatible gateways (D3 hint). */
  catalog_provider?: string;
  /** Host-derived suggestion; empty when not derivable. */
  suggested_catalog_provider?: string;
  created_at: string;
  updated_at: string;
}

export interface ApiProviderVerifyResult {
  ok: boolean;
  error?: string;
}

// ---------------------------------------------------------------------------
// Workspace blob-storage configuration (attachments design D16): the settings
// Storage pane backend. The secret is write-only — reads carry only the
// last-4 `secret_hint`, and a PUT/probe with an empty or hint-echoed
// `secret_access_key` keeps the stored secret server-side. The default when
// no row exists is `{"driver": "local"}`.
// ---------------------------------------------------------------------------

export interface ApiWorkspaceStorageConfig {
  driver: 'local' | 's3';
  endpoint?: string;
  region?: string;
  bucket?: string;
  access_key_id?: string;
  use_path_style?: boolean;
  /** Last-4 of the stored secret; present on stored s3 configs only. */
  secret_hint?: string;
  updated_at?: string;
}

export interface WorkspaceStoragePayload {
  driver: 'local' | 's3';
  endpoint?: string;
  region?: string;
  bucket?: string;
  access_key_id?: string;
  /** Write-only: empty or the masked hint keeps the stored secret. */
  secret_access_key?: string;
  use_path_style?: boolean;
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
  /** False for always-on tools (design D1): the pane sections them with an
   * always-on badge, the agent picker drops them, and they cannot be disabled. */
  toggleable: boolean;
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
  /** Memory side-call override: both empty = inherit the workspace memory
   * setting, then the model this agent runs. */
  memory_sidecall_provider_id?: string;
  memory_sidecall_model?: string;
  temperature: number;
  max_tokens?: number | null;
  effort?: string | null;
  autonomy: AgentAutonomy;
  context_window?: number | null;
  /** Server-computed read-only echoes for the context meter (context meter change). */
  effective_context_window?: number;
  /** Token count at which the backend summarizes — the meter's warn threshold. */
  summarization_trigger_tokens?: number;
  /** Server-computed read-only input-modality capability of the agent's
   * model (fix-image-attachment-lane D5): tri-state per kind, `unknown`
   * when the catalog cannot answer. */
  input_modalities?: {
    image: 'supported' | 'unsupported' | 'unknown';
    pdf: 'supported' | 'unsupported' | 'unknown';
  };
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
  memory_sidecall_provider_id?: string;
  memory_sidecall_model?: string;
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
  /** Both fields set pins a memory side-call model; both empty clears back
   * to inherit. Send neither to leave the current choice untouched. */
  memory_sidecall_provider_id?: string;
  memory_sidecall_model?: string;
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

// ---------------------------------------------------------------------------
// Extracted memory (integrate-agent-zero-memory tasks 5.1): the curated fact
// store, the episodic timeline, the consolidator's morning report, and the
// workspace memory settings record. Shapes mirror the handler JSON exactly.
// ---------------------------------------------------------------------------

/** Within-tenant visibility tier (D4): shared = tenant-wide, user = one
 * member, agent = one agent. */
export type ApiMemoryVisibility = 'shared' | 'user' | 'agent';

export type ApiMemoryOrigin = 'manual' | 'dialogue' | 'infer' | 'doc';

/** One curated fact with its provenance birth tuple (D5) and update
 * pointers (D6). */
export interface ApiMemoryNote {
  id: string;
  workspace_id: string;
  visibility: ApiMemoryVisibility;
  user_id: string | null;
  agent_id: string | null;
  origin: ApiMemoryOrigin;
  event_time: string;
  learned_at: string;
  source_event_id: string;
  content: string;
  importance: number;
  pinned: boolean;
  topic: string | null;
  conflict_flag: string | null;
  supersedes: string | null;
  superseded_by: string | null;
  promoted_by: string | null;
  promoted_at: string | null;
  tombstoned_at: string | null;
}

/** One raw-evidence link on a note (D12's multi-evidence). */
export interface ApiMemoryNoteEvidence {
  source_event_id: string;
  added_at: string;
}

/** Workspace-wide per-tier counts — chips, never content. */
export interface ApiMemoryNoteCounts {
  shared: number;
  user: number;
  agent: number;
}

export interface ApiMemoryNoteList {
  notes: ApiMemoryNote[];
  counts: ApiMemoryNoteCounts;
  viewer_user_id: string;
}

export interface ApiMemoryNoteDetail {
  note: ApiMemoryNote;
  evidence: ApiMemoryNoteEvidence[];
}

export interface ApiMemoryNotesQuery {
  q?: string;
  visibility?: ApiMemoryVisibility;
  topic?: string;
  from?: string;
  until?: string;
  include_superseded?: boolean;
}

/** One episodic gist (D3): the summarized window of a session. */
export interface ApiMemoryEvent {
  id: string;
  workspace_id: string;
  agent_id: string;
  session_id: string;
  turn_id: string;
  visibility: ApiMemoryVisibility;
  user_id: string | null;
  origin: ApiMemoryOrigin;
  event_time: string;
  learned_at: string;
  source_event_id: string;
  description: string;
  outcome: string;
  participants: { kind: string; id: string }[];
  tombstoned_at: string | null;
}

export interface ApiMemoryEventsQuery {
  session_id?: string;
  visibility?: ApiMemoryVisibility;
  from?: string;
  until?: string;
}

/** One doc-over-notes precedence review item (D7). */
export interface ApiMemoryConflictFlag {
  note_id: string;
  document: string;
  excerpt: string;
  flagged_at: string;
}

/** One consolidation merge (D12). */
export interface ApiMemoryMergeRecord {
  canonical_id: string;
  merged_ids: string[];
}

/** The consolidator's morning report (tasks 6.3). */
export interface ApiMorningReport {
  generated_at: string;
  conflicts: ApiMemoryConflictFlag[];
  merges: ApiMemoryMergeRecord[];
  extraction_failures: number;
}

/** The workspace memory settings record (D16). The embedding provider IS a
 * workspace provider — endpoint and credential live on the provider record;
 * the settings pin provider, model, and the known dimension only. */
export interface ApiMemorySettings {
  visibility_posture: 'narrow' | 'org-shared';
  ingestion_enabled: boolean;
  /** The intent gate's classification budget in ms; always resolved — absent
   * or out-of-contract stored values come back as the 4000 default. */
  gate_budget_ms: number;
  /** The workspace-level memory side-call model; null = agent default
   * (each agent's own run model, or its own override). */
  side_call_model: { provider_id: string; model: string } | null;
  embedding: {
    provider_id: string;
    model: string;
    dimension: number;
  } | null;
}

export interface ApiMemorySettingsUpdate {
  visibility_posture?: 'narrow' | 'org-shared';
  ingestion_enabled?: boolean;
  /** The intent gate's classification budget in ms; must sit in
   * [500, 20000]. Absent leaves the stored value untouched. */
  gate_budget_ms?: number;
  /** Absent leaves the stored choice; null clears to agent default. */
  side_call_model?: { provider_id: string; model: string } | null;
  embedding?: {
    provider_id?: string;
    model?: string;
    dimension?: number;
  };
}

export interface ApiMemorySettingsTest {
  provider_id: string;
  model: string;
  dimension?: number;
}

export interface ApiMemorySettingsTestResult {
  ok: boolean;
  dimension: number;
}

export interface ApiModel {
  id: string;
  name: string;
  efforts?: string[];
  supports_temperature?: boolean;
  context_limit?: number | null;
  /** Capability flags from the model catalog — true ONLY when the catalog
   * affirmatively supports the capability; absent/false otherwise. */
  image_input?: boolean;
  pdf_input?: boolean;
  reasoning?: boolean;
  tool_call?: boolean;
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

// ---------------------------------------------------------------------------
// Agent lifecycle hooks (change integrate-agent-hooks): workspace/agent-level
// CRUD under /workspaces/:slug/..., plus the workspace-wide dry-run and audit
// surfaces. Secret row values inside config (http headers, command env) are
// write-only: reads carry only the last-4 hint, and a create/update echoing
// the hint (or an empty value) keeps the stored secret server-side.
// ---------------------------------------------------------------------------

export type HookEvent =
  | 'run_started'
  | 'user_prompt_submit'
  | 'pre_tool_use'
  | 'post_tool_use'
  | 'run_finished';

export type HookHandlerType = 'http' | 'command' | 'mcp_tool' | 'prompt' | 'script';

export type HookFailurePolicy = 'allow' | 'block';

export type HookStatus = 'ok' | 'error';

/** Matcher: one plain string (D19 — Claude Code parity). Empty or "*" selects
 * every occurrence; comma/pipe/space-separated charset-valid entries match
 * exactly or by family ("web.*"); anything else reads as an unanchored RE2
 * regex (<= 256 chars). */
export type ApiHookMatcher = string;

/** Write-side secret row (http header / command env). An omitted or empty
 * `value` keeps the stored secret; echoing the masked hint does too. */
export interface ApiHookSecretRowInput {
  name: string;
  value?: string;
}

/** Handler config shapes — one per handler_type (pinned by save validation). */
export interface ApiHookHttpConfig {
  url: string;
  headers?: ApiHookSecretRowInput[];
}

export interface ApiHookCommandConfig {
  command: string;
  args?: string[];
  env?: ApiHookSecretRowInput[];
  cwd?: string;
}

export interface ApiHookMcpToolConfig {
  /** Workspace MCP server id (D21 rename: server_id → server). */
  server: string;
  /** Tool name on that server (D21 rename: tool_name → tool). */
  tool: string;
  /** Structured key/value rows (values may use the closed ${event.*} set). */
  input?: Record<string, unknown>;
}

export interface ApiHookPromptConfig {
  provider: string;
  model: string;
  /** Policy prompt (D21 rename: prompt_template → prompt). */
  prompt: string;
  max_invocations_per_run?: number;
}

/** In-process sandboxed JavaScript (D22): one string inside the config JSONB,
 * invoked as `(function(input){ … })` — no env rows, no cwd, no files. */
export interface ApiHookScriptConfig {
  script: string;
}

/** Read view of a hook at any level. Secret row values arrive masked as their
 * last-4 hints; the masked config round-trips (echoing it keeps the stored
 * secrets). */
export interface ApiHook {
  id: string;
  workspace_id?: string;
  agent_id?: string;
  /** Instance rows only (builtin key). */
  key?: string;
  source?: 'builtin' | 'managed';
  version?: number;
  name: string;
  event: HookEvent;
  matcher: ApiHookMatcher;
  /** Optional input-level gate (D20), pre_tool_use/post_tool_use
   * only: `ToolName(pattern)` — narrows by the serialized tool input JSON. */
  if?: string;
  handler_type: HookHandlerType;
  config?: any;
  timeout_ms: number;
  on_failure: HookFailurePolicy;
  enabled: boolean;
  position: number;
  status: HookStatus;
  status_error?: string;
  created_at: string;
  updated_at: string;
}

/** Save-time matcher report (D8): how many of the event's currently available
 * values the matcher selects. Absent when the count source failed. */
export interface ApiHookMatchCount {
  matched: number;
  of: number;
}

export interface ApiHookSaveResult {
  hook: ApiHook;
  match_count?: ApiHookMatchCount | null;
}

export interface ApiHookPayload {
  name: string;
  event: HookEvent;
  matcher?: ApiHookMatcher;
  /** Input gate `ToolName(pattern)` — tool events only (D20). */
  if?: string;
  handler_type: HookHandlerType;
  config?: unknown;
  timeout_ms?: number;
  on_failure?: HookFailurePolicy;
  enabled?: boolean;
}

export type PatchHookPayload = Partial<ApiHookPayload>;

/** One audit record (D16). hook_id is nullable — records survive hook
 * deletion with the name denormalized. */
export interface ApiHookExecution {
  id: string;
  hook_id?: string | null;
  hook_name: string;
  hook_level: 'instance' | 'workspace' | 'agent';
  workspace_id: string;
  event: HookEvent;
  decision: string;
  duration_ms: number;
  exit_code?: number | null;
  http_status?: number | null;
  detail?: string;
  token_count?: number | null;
  origin?: string;
  created_at: string;
}

/** Dry-run result (D18): a REAL handler execution of a synthetic event that
 * records nothing. decision is allow | block | failure. */
export interface ApiHookTestResult {
  decision: string;
  reason?: string;
  duration_ms: number;
  detail?: {
    exit_code?: number;
    http_status?: number;
    token_count?: number;
  };
  /** Captured console.log/console.error of a script dry run (D22) — present
   * for script hooks only. */
  console_lines?: string[];
  error?: string;
}

/** Synthetic event knobs for the dry run. event overrides the hook's own
 * event only for the simulated delivery. */
export interface ApiHookTestOverrides {
  event?: HookEvent;
  tool_name?: string;
  tool_args?: string;
  origin?: string;
  status?: string;
}

export interface ApiHookTestPayload extends ApiHookPayload {
  /** Saved hook id — enables the server's keep-stored secret merge for the
   * dry run of an existing hook's current config. */
  id?: string;
  overrides?: ApiHookTestOverrides;
}

// ---------------------------------------------------------------------------
// Agent channels (change integrate-agent-channels): team rooms where humans
// and agents share one attributed feed. Wire shapes mirror the server's
// snake_case JSON (domain.Channel / ChannelMember / ChannelMessage).
// ---------------------------------------------------------------------------

/** Discriminates the two roster/author kinds sharing a channel. */
export type ApiChannelMemberType = 'user' | 'agent';

export interface ApiChannel {
  id: string;
  workspace_id: string;
  /** Display name ("Production Ops"). */
  name: string;
  /** URL + #handle form ("ops"); workspace-unique, case-insensitive. */
  slug: string;
  purpose: string;
  /** Freeform CHANNEL.md section (design D8 — textarea exception). */
  conventions: string;
  created_by?: string | null;
  created_at: string;
  updated_at: string;
}

/** One heterogeneous roster row (design D5): exactly one of user_id/agent_id.
 * Wire: the roster read view — display_name/handle are resolved server-side
 * (agent name+slug, user name+dashed handle) and the raw row's workspace_id
 * never crosses the wire. */
export interface ApiChannelMember {
  id: string;
  channel_id: string;
  member_type: ApiChannelMemberType;
  user_id?: string;
  agent_id?: string;
  /** Resolved display name — the agent's name or the user's name. */
  display_name: string;
  /** Resolved @handle — agent slug / dashed user name; the mention key. */
  handle: string;
  specialization: string;
  added_at: string;
}

/** One resolved @handle reference on a message's mentions list. */
export interface ApiChannelMention {
  type: ApiChannelMemberType;
  id: string;
  handle: string;
}

/** Run footprint written onto agent-authored feed messages at run finish (D9). */
export interface ApiChannelRunSummary {
  tools?: Record<string, number>;
  duration_ms: number;
}

export interface ApiChannelMessage {
  id: string;
  workspace_id: string;
  channel_id: string;
  /** Monotonic per-channel feed cursor — the SSE dedup + gap-recovery key. */
  seq: number;
  author_type: ApiChannelMemberType;
  author_user_id?: string;
  author_agent_id?: string;
  body: string;
  mentions: ApiChannelMention[];
  session_id?: string | null;
  turn_id?: string | null;
  run_summary?: ApiChannelRunSummary | null;
  root_message_id?: string | null;
  chain_depth: number;
  created_at: string;
}

export interface CreateChannelPayload {
  name: string;
  slug: string;
  purpose?: string;
  conventions?: string;
}

export type PatchChannelPayload = Partial<CreateChannelPayload>;

export interface AddChannelMemberPayload {
  member_type: ApiChannelMemberType;
  /** Required when member_type is 'user'. */
  user_id?: string;
  /** Required when member_type is 'agent'. */
  agent_id?: string;
  specialization?: string;
}

export interface PatchChannelMemberPayload {
  specialization: string;
}

export interface PostChannelMessagePayload {
  body: string;
}

// ---------------------------------------------------------------------------
// Workspace Telegram gateway (change integrate-telegram-gateway and multi-bot-gateways):
// multiple bot gateway accounts per workspace under /workspaces/:slug/gateways/telegram.
// The bot token is a write-only secret — every read carries only the last-4 `token_hint`;
// a PUT with an omitted or empty `token` keeps the stored secret server-side.
// ---------------------------------------------------------------------------

export type GatewayTransport = 'webhook' | 'long_polling';

export interface ApiGatewayConfig {
  id: string;
  workspace_id?: string;
  platform: string;
  identity: string;
  agent_id: string;
  bot_username?: string | null;
  token_hint?: string;
  enabled: boolean;
  transport: GatewayTransport;
  webhook_url?: string;
  status_error?: string | null;
  created_at?: string;
  updated_at?: string;
}

export interface CreateGatewayPayload {
  token: string;
  agent_id: string;
  transport?: GatewayTransport;
  webhook_url?: string;
}

export interface UpdateGatewayPayload {
  token?: string;
  agent_id?: string | null;
  transport?: GatewayTransport;
  webhook_url?: string;
}

export interface ApiGatewayBinding {
  id: string;
  gateway_id: string;
  platform: string;
  platform_chat_id: string;
  chat_title?: string | null;
  agent_id: string;
  created_by?: string | null;
  created_at: string;
}

export interface CreateGatewayBindingPayload {
  gateway_id: string;
  agent_id: string;
  platform_chat_id: string;
  chat_title?: string;
}

export interface ApiGatewayLink {
  /** Immutable platform user id — usernames are display-only (design D6). */
  platform_user_id: string;
  username?: string | null;
  display_name?: string | null;
  linked_at: string;
}

export interface ApiPairingToken {
  /** One-time crypto-random token, single-use, consumed on use. */
  token: string;
  expires_at: string;
}

// ---------------------------------------------------------------------------
// Workspace WhatsApp gateway (change add-whatsapp-gateway and multi-bot-gateways):
// multiple WhatsApp gateway accounts per workspace under /workspaces/:slug/gateways/whatsapp
// with an explicit lane — 'cloud_api' or 'multi_device'.
// Secrets are never echoed: reads carry only `has_credentials`; a PUT with
// omitted or empty credential fields keeps the stored envelope.
// ---------------------------------------------------------------------------

export type GatewayLane = 'cloud_api' | 'multi_device';

export interface ApiWhatsAppGatewayConfig {
  id: string;
  platform: string;
  lane?: GatewayLane | null;
  identity: string;
  agent_id: string;
  enabled: boolean;
  bot_username?: string | null;
  transport?: GatewayTransport | null;
  webhook_url?: string;
  has_credentials?: boolean;
  status_error?: string | null;
  created_at?: string;
  updated_at?: string;
}

export interface CreateWhatsAppGatewayPayload {
  lane: GatewayLane;
  agent_id: string;
  access_token?: string;
  phone_number_id?: string;
  app_secret?: string;
  verify_token?: string;
  transport?: GatewayTransport;
  webhook_url?: string;
}

export interface UpdateWhatsAppGatewayPayload {
  agent_id?: string;
  access_token?: string;
  phone_number_id?: string;
  app_secret?: string;
  verify_token?: string;
  transport?: GatewayTransport;
  webhook_url?: string;
}

export interface ApiWhatsAppHealth {
  status: 'ok' | 'error' | 'unconfigured';
  detail?: string;
  /** Dead deliveries for this gateway (design D4) — absent when unconfigured. */
  dead_outbox?: number;
}

export interface ApiWhatsAppPairing {
  status: 'waiting' | 'connected' | 'logged_out' | 'not_started';
  /** QR image as a data URL while a pairing session is live. */
  qr_data_url?: string;
  /** Raw QR payload when no data URL is provided. */
  qr?: string;
  /** 8-digit pairing code, e.g. "4821-9376". */
  pair_code?: string;
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
    patch: (ws: string, body: { name?: string; timezone?: string; default_model?: ApiDefaultModel | null }) =>
      request<{ workspace: ApiWorkspace }>(`/workspaces/${encodeURIComponent(ws)}`, {
        method: 'PATCH',
        body,
      }),
  },
  // Workspace blob-storage configuration (storage pane; Owner/Admin gated
  // server-side — Members get 403 on all three). `probe` is the Test
  // connection affordance: verifies connectivity for the submitted values and
  // persists nothing.
  storage: {
    get: (ws: string) =>
      request<ApiWorkspaceStorageConfig>(`/workspaces/${encodeURIComponent(ws)}/storage`, {
        method: 'GET',
      }),
    update: (ws: string, body: WorkspaceStoragePayload) =>
      request<ApiWorkspaceStorageConfig>(`/workspaces/${encodeURIComponent(ws)}/storage`, {
        method: 'PUT',
        body,
      }),
    probe: (ws: string, body: WorkspaceStoragePayload) =>
      request<{ ok: boolean }>(`/workspaces/${encodeURIComponent(ws)}/storage/probe`, {
        method: 'POST',
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
      body: { type: string; name: string; base_url?: string; key?: string; enabled?: boolean; catalog_provider?: string }
    ) =>
      request<{ provider: ApiProviderConfig }>(`/workspaces/${encodeURIComponent(ws)}/providers`, {
        method: 'POST',
        body,
      }),
    patch: (
      ws: string,
      id: string,
      body: { name?: string; base_url?: string; key?: string; enabled?: boolean; catalog_provider?: string }
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
    /** Draft-credential verification (refactor-workspace-settings D5): tests
     * the UNSAVED form values against the provider and persists nothing.
     * `provider_id` names the config being edited so a blank `key` falls back
     * to its stored key server-side; the endpoint 400s when no credential is
     * expressible. */
    verifyDraft: (
      ws: string,
      body: { type: string; base_url?: string; key?: string; catalog_provider?: string; provider_id?: string }
    ) =>
      request<ApiProviderVerifyResult>(`/workspaces/${encodeURIComponent(ws)}/providers/verify-draft`, {
        method: 'POST',
        body,
      }),
    models: (ws: string, id: string) =>
      request<ApiModelsResult>(`/workspaces/${encodeURIComponent(ws)}/providers/${encodeURIComponent(id)}/models`, {
        method: 'GET',
      }),
    /** Optional catalog-mapping hint (D3) so compatible gateways resolve
     * against the community catalog's provider entries. */
    modelsPreview: (body: { type: string; base_url?: string; key?: string; api_key?: string; catalog_provider?: string }) =>
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
  // Workspace agent lifecycle hooks (hooks.read / hooks.write; gating is
  // server-side). The list response also carries the read-only instance
  // section — instance hooks are mandatory visibility, no control from below.
  hooks: {
    list: (ws: string) =>
      request<{ instance: ApiHook[]; hooks: ApiHook[] }>(`/workspaces/${encodeURIComponent(ws)}/hooks`, {
        method: 'GET',
      }),
    create: (ws: string, body: ApiHookPayload) =>
      request<ApiHookSaveResult>(`/workspaces/${encodeURIComponent(ws)}/hooks`, {
        method: 'POST',
        body,
      }),
    update: (ws: string, id: string, body: PatchHookPayload) =>
      request<ApiHookSaveResult>(`/workspaces/${encodeURIComponent(ws)}/hooks/${encodeURIComponent(id)}`, {
        method: 'PATCH',
        body,
      }),
    delete: (ws: string, id: string) =>
      request<void>(`/workspaces/${encodeURIComponent(ws)}/hooks/${encodeURIComponent(id)}`, {
        method: 'DELETE',
      }),
    // The list order IS the execution order (D14): payload is the level's
    // hook ids in their new order.
    reorder: (ws: string, ids: string[]) =>
      request<void>(`/workspaces/${encodeURIComponent(ws)}/hooks/reorder`, {
        method: 'POST',
        body: { ids },
      }),
    // Dry run (D18): executes the handler against a synthetic event, records
    // nothing. Accepts a full definition (saved or unsaved); `id` merges the
    // stored secrets when testing an existing hook.
    test: (ws: string, body: ApiHookTestPayload) =>
      request<ApiHookTestResult>(`/workspaces/${encodeURIComponent(ws)}/hooks/test`, {
        method: 'POST',
        body,
      }),
    // Workspace-wide audit trail; also surfaces records of deleted hooks.
    executions: (ws: string, limit = 50) =>
      request<{ executions: ApiHookExecution[] }>(
        `/workspaces/${encodeURIComponent(ws)}/hooks/executions?limit=${limit}`,
        { method: 'GET' }
      ),
    // Per-hook history (unknown/deleted ids answer an empty list, not 404).
    hookExecutions: (ws: string, id: string, limit = 50) =>
      request<{ executions: ApiHookExecution[] }>(
        `/workspaces/${encodeURIComponent(ws)}/hooks/${encodeURIComponent(id)}/executions?limit=${limit}`,
        { method: 'GET' }
      ),
  },
  // Agent channels (integrate-agent-channels D11): workspace-scoped CRUD,
  // the heterogeneous human+agent roster, and the seq-cursored feed.
  // Gating is server-side (channels.read / channels.write).
  channels: {
    list: (ws: string) =>
      request<{ channels: ApiChannel[] }>(`/workspaces/${encodeURIComponent(ws)}/channels`, {
        method: 'GET',
      }),
    create: (ws: string, body: CreateChannelPayload) =>
      request<{ channel: ApiChannel }>(`/workspaces/${encodeURIComponent(ws)}/channels`, {
        method: 'POST',
        body,
      }),
    get: (ws: string, id: string) =>
      request<{ channel: ApiChannel }>(`/workspaces/${encodeURIComponent(ws)}/channels/${encodeURIComponent(id)}`, {
        method: 'GET',
      }),
    update: (ws: string, id: string, body: PatchChannelPayload) =>
      request<{ channel: ApiChannel }>(`/workspaces/${encodeURIComponent(ws)}/channels/${encodeURIComponent(id)}`, {
        method: 'PATCH',
        body,
      }),
    delete: (ws: string, id: string) =>
      request<void>(`/workspaces/${encodeURIComponent(ws)}/channels/${encodeURIComponent(id)}`, {
        method: 'DELETE',
      }),
    // One heterogeneous roster (humans + agents) consumed as a unit (design D5).
    members: {
      list: (ws: string, id: string) =>
        request<{ members: ApiChannelMember[] }>(`/workspaces/${encodeURIComponent(ws)}/channels/${encodeURIComponent(id)}/members`, {
          method: 'GET',
        }),
      add: (ws: string, id: string, body: AddChannelMemberPayload) =>
        request<{ member: ApiChannelMember }>(`/workspaces/${encodeURIComponent(ws)}/channels/${encodeURIComponent(id)}/members`, {
          method: 'POST',
          body,
        }),
      patch: (ws: string, id: string, mid: string, body: PatchChannelMemberPayload) =>
        request<{ member: ApiChannelMember }>(
          `/workspaces/${encodeURIComponent(ws)}/channels/${encodeURIComponent(id)}/members/${encodeURIComponent(mid)}`,
          { method: 'PATCH', body }
        ),
      remove: (ws: string, id: string, mid: string) =>
        request<void>(`/workspaces/${encodeURIComponent(ws)}/channels/${encodeURIComponent(id)}/members/${encodeURIComponent(mid)}`, {
          method: 'DELETE',
        }),
    },
    messages: {
      // Cursor list on seq: `after` excludes the boundary row; ascending order.
      list: (ws: string, id: string, opts?: { after?: number; limit?: number }) => {
        const params = new URLSearchParams();
        if (opts?.after !== undefined) params.set('after', String(opts.after));
        if (opts?.limit !== undefined) params.set('limit', String(opts.limit));
        const qs = params.toString();
        return request<{ messages: ApiChannelMessage[] }>(
          `/workspaces/${encodeURIComponent(ws)}/channels/${encodeURIComponent(id)}/messages${qs ? `?${qs}` : ''}`,
          { method: 'GET' }
        );
      },
      post: (ws: string, id: string, body: PostChannelMessagePayload) =>
        request<{ message: ApiChannelMessage }>(`/workspaces/${encodeURIComponent(ws)}/channels/${encodeURIComponent(id)}/messages`, {
          method: 'POST',
          body,
        }),
    },
  },
  // Workspace Gateways (multi-bot-gateways): plural accounts per platform,
  // bound agents, per-account CRUD and lifecycle under /workspaces/:slug/gateways/...
  gateways: {
    telegram: {
      list: (ws: string) =>
        request<ApiGatewayConfig[]>(
          `/workspaces/${encodeURIComponent(ws)}/gateways/telegram`,
          { method: 'GET' }
        ),
      create: (ws: string, body: CreateGatewayPayload) =>
        request<ApiGatewayConfig>(
          `/workspaces/${encodeURIComponent(ws)}/gateways/telegram`,
          { method: 'POST', body }
        ),
      get: (ws: string, id: string) =>
        request<ApiGatewayConfig>(
          `/workspaces/${encodeURIComponent(ws)}/gateways/telegram/${encodeURIComponent(id)}`,
          { method: 'GET' }
        ),
      update: (ws: string, id: string, body: UpdateGatewayPayload) =>
        request<ApiGatewayConfig>(
          `/workspaces/${encodeURIComponent(ws)}/gateways/telegram/${encodeURIComponent(id)}`,
          { method: 'PUT', body }
        ),
      enable: (ws: string, id: string) =>
        request<void>(
          `/workspaces/${encodeURIComponent(ws)}/gateways/telegram/${encodeURIComponent(id)}/enable`,
          { method: 'POST', body: {} }
        ),
      disable: (ws: string, id: string) =>
        request<void>(
          `/workspaces/${encodeURIComponent(ws)}/gateways/telegram/${encodeURIComponent(id)}/disable`,
          { method: 'POST', body: {} }
        ),
      test: (ws: string, id: string) =>
        request<{ ok: boolean; bot_username?: string; error?: string }>(
          `/workspaces/${encodeURIComponent(ws)}/gateways/telegram/${encodeURIComponent(id)}/test`,
          { method: 'POST', body: {} }
        ),
      delete: (ws: string, id: string) =>
        request<void>(
          `/workspaces/${encodeURIComponent(ws)}/gateways/telegram/${encodeURIComponent(id)}`,
          { method: 'DELETE' }
        ),
      bindings: {
        list: (ws: string) =>
          request<ApiGatewayBinding[]>(
            `/workspaces/${encodeURIComponent(ws)}/gateways/telegram/bindings`,
            { method: 'GET' }
          ),
        create: (ws: string, body: CreateGatewayBindingPayload) =>
          request<ApiGatewayBinding>(
            `/workspaces/${encodeURIComponent(ws)}/gateways/telegram/bindings`,
            { method: 'POST', body }
          ),
        remove: (ws: string, id: string) =>
          request<void>(
            `/workspaces/${encodeURIComponent(ws)}/gateways/telegram/bindings/${encodeURIComponent(id)}`,
            { method: 'DELETE' }
          ),
      },
      // One-time pairing token for the signed-in member; revoke cancels a
      // minted-but-unused token.
      pairing: {
        create: (ws: string) =>
          request<{ token: ApiPairingToken }>(
            `/workspaces/${encodeURIComponent(ws)}/gateways/telegram/pairing-tokens`,
            { method: 'POST', body: {} }
          ),
        revoke: (ws: string, token: string) =>
          request<void>(
            `/workspaces/${encodeURIComponent(ws)}/gateways/telegram/pairing-tokens/${encodeURIComponent(token)}`,
            { method: 'DELETE' }
          ),
      },
      links: {
        // The signed-in member's current Telegram link, or `{ link: null }`.
        getMine: (ws: string) =>
          request<{ link: ApiGatewayLink | null }>(
            `/workspaces/${encodeURIComponent(ws)}/gateways/telegram/links/me`,
            { method: 'GET' }
          ),
        // Member-gated self unpair.
        removeMine: (ws: string) =>
          request<void>(
            `/workspaces/${encodeURIComponent(ws)}/gateways/telegram/links/me`,
            { method: 'DELETE' }
          ),
        // Admin per-member unpair.
        remove: (ws: string, userId: string) =>
          request<void>(
            `/workspaces/${encodeURIComponent(ws)}/gateways/telegram/links/${encodeURIComponent(userId)}`,
            { method: 'DELETE' }
          ),
      },
    },
    // WhatsApp gateway (multi-bot-gateways): config CRUD + enable/disable +
    // health are gateways.write server-side; pairing start/status/regenerate/
    // logout drive the multi-device QR flow; pairing tokens and the self link
    // are member-gated — the same split as Telegram.
    whatsapp: {
      list: (ws: string) =>
        request<ApiWhatsAppGatewayConfig[]>(
          `/workspaces/${encodeURIComponent(ws)}/gateways/whatsapp`,
          { method: 'GET' }
        ),
      create: (ws: string, body: CreateWhatsAppGatewayPayload) =>
        request<ApiWhatsAppGatewayConfig>(
          `/workspaces/${encodeURIComponent(ws)}/gateways/whatsapp`,
          { method: 'POST', body }
        ),
      get: (ws: string, id: string) =>
        request<ApiWhatsAppGatewayConfig>(
          `/workspaces/${encodeURIComponent(ws)}/gateways/whatsapp/${encodeURIComponent(id)}`,
          { method: 'GET' }
        ),
      update: (ws: string, id: string, body: UpdateWhatsAppGatewayPayload) =>
        request<ApiWhatsAppGatewayConfig>(
          `/workspaces/${encodeURIComponent(ws)}/gateways/whatsapp/${encodeURIComponent(id)}`,
          { method: 'PUT', body }
        ),
      enable: (ws: string, id: string) =>
        request<void>(
          `/workspaces/${encodeURIComponent(ws)}/gateways/whatsapp/${encodeURIComponent(id)}/enable`,
          { method: 'POST', body: {} }
        ),
      disable: (ws: string, id: string) =>
        request<void>(
          `/workspaces/${encodeURIComponent(ws)}/gateways/whatsapp/${encodeURIComponent(id)}/disable`,
          { method: 'POST', body: {} }
        ),
      delete: (ws: string, id: string) =>
        request<void>(
          `/workspaces/${encodeURIComponent(ws)}/gateways/whatsapp/${encodeURIComponent(id)}`,
          { method: 'DELETE' }
        ),
      // Cloud: Meta phone-number probe; multi-device: device connection state.
      health: (ws: string, id: string) =>
        request<ApiWhatsAppHealth>(
          `/workspaces/${encodeURIComponent(ws)}/gateways/whatsapp/${encodeURIComponent(id)}/health`,
          { method: 'GET' }
        ),
      pairing: {
        // start runs the md pairing flow — QR-only by default; with the
        // account's phone digits the server answers an 8-digit pair_code
        // (design D12: "Pair code: 4821-9376" beside the QR).
        start: (ws: string, id: string, phone?: string) =>
          request<ApiWhatsAppPairing>(
            `/workspaces/${encodeURIComponent(ws)}/gateways/whatsapp/${encodeURIComponent(id)}/pairing/start`,
            { method: 'POST', body: phone ? { phone } : {} }
          ),
        status: (ws: string, id: string) =>
          request<ApiWhatsAppPairing>(
            `/workspaces/${encodeURIComponent(ws)}/gateways/whatsapp/${encodeURIComponent(id)}/pairing/status`,
            { method: 'GET' }
          ),
        regenerate: (ws: string, id: string) =>
          request<ApiWhatsAppPairing>(
            `/workspaces/${encodeURIComponent(ws)}/gateways/whatsapp/${encodeURIComponent(id)}/pairing/regenerate`,
            { method: 'POST', body: {} }
          ),
        logout: (ws: string, id: string) =>
          request<{ status: string }>(
            `/workspaces/${encodeURIComponent(ws)}/gateways/whatsapp/${encodeURIComponent(id)}/pairing/logout`,
            { method: 'POST', body: {} }
          ),
      },
      pairingTokens: {
        create: (ws: string) =>
          request<{ token: ApiPairingToken }>(
            `/workspaces/${encodeURIComponent(ws)}/gateways/whatsapp/pairing-tokens`,
            { method: 'POST', body: {} }
          ),
        revoke: (ws: string, token: string) =>
          request<void>(
            `/workspaces/${encodeURIComponent(ws)}/gateways/whatsapp/pairing-tokens/${encodeURIComponent(token)}`,
            { method: 'DELETE' }
          ),
      },
      links: {
        // The signed-in member's current WhatsApp link, or `{ link: null }`.
        getMine: (ws: string) =>
          request<{ link: ApiGatewayLink | null }>(
            `/workspaces/${encodeURIComponent(ws)}/gateways/whatsapp/links/me`,
            { method: 'GET' }
          ),
        // Member-gated self unpair.
        removeMine: (ws: string) =>
          request<void>(
            `/workspaces/${encodeURIComponent(ws)}/gateways/whatsapp/links/me`,
            { method: 'DELETE' }
          ),
      },
    },
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
    // Agent-private lifecycle hooks (agent config modal, D13). The list
    // response carries all three sections the modal shows: read-only
    // instance + workspace visibility, plus the agent's own hooks.
    listHooks: (ws: string, agent: string) =>
      request<{ instance: ApiHook[]; workspace: ApiHook[]; agent: ApiHook[] }>(
        `/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}/hooks`,
        { method: 'GET' }
      ),
    createHook: (ws: string, agent: string, body: ApiHookPayload) =>
      request<ApiHookSaveResult>(`/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}/hooks`, {
        method: 'POST',
        body,
      }),
    updateHook: (ws: string, agent: string, id: string, body: PatchHookPayload) =>
      request<ApiHookSaveResult>(
        `/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}/hooks/${encodeURIComponent(id)}`,
        { method: 'PATCH', body }
      ),
    deleteHook: (ws: string, agent: string, id: string) =>
      request<void>(
        `/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}/hooks/${encodeURIComponent(id)}`,
        { method: 'DELETE' }
      ),
    reorderHooks: (ws: string, agent: string, ids: string[]) =>
      request<void>(`/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}/hooks/reorder`, {
        method: 'POST',
        body: { ids },
      }),
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
    // Extracted memory (integrate-agent-zero-memory tasks 5.1). Reads ride
    // membership; promotion, delete, consolidate, and the settings writes
    // are workspace.write server-side (Members get 403).
    notes: (ws: string, query: ApiMemoryNotesQuery = {}) => {
      const params = new URLSearchParams();
      if (query.q) params.set('q', query.q);
      if (query.visibility) params.set('visibility', query.visibility);
      if (query.topic) params.set('topic', query.topic);
      if (query.from) params.set('from', query.from);
      if (query.until) params.set('until', query.until);
      if (query.include_superseded) params.set('include_superseded', 'true');
      const qs = params.toString();
      return request<ApiMemoryNoteList>(
        `/workspaces/${encodeURIComponent(ws)}/memory/notes${qs ? `?${qs}` : ''}`,
        { method: 'GET' }
      );
    },
    note: (ws: string, id: string) =>
      request<ApiMemoryNoteDetail>(
        `/workspaces/${encodeURIComponent(ws)}/memory/notes/${encodeURIComponent(id)}`,
        { method: 'GET' }
      ),
    promoteNote: (ws: string, id: string, note?: string) =>
      request<{ note: ApiMemoryNote }>(
        `/workspaces/${encodeURIComponent(ws)}/memory/notes/${encodeURIComponent(id)}/promote`,
        { method: 'POST', body: note ? { note } : {} }
      ),
    deleteNote: (ws: string, id: string) =>
      request<void>(
        `/workspaces/${encodeURIComponent(ws)}/memory/notes/${encodeURIComponent(id)}`,
        { method: 'DELETE' }
      ),
    events: (ws: string, query: ApiMemoryEventsQuery = {}) => {
      const params = new URLSearchParams();
      if (query.session_id) params.set('session_id', query.session_id);
      if (query.visibility) params.set('visibility', query.visibility);
      if (query.from) params.set('from', query.from);
      if (query.until) params.set('until', query.until);
      const qs = params.toString();
      return request<{ events: ApiMemoryEvent[] }>(
        `/workspaces/${encodeURIComponent(ws)}/memory/events${qs ? `?${qs}` : ''}`,
        { method: 'GET' }
      );
    },
    consolidate: (ws: string) =>
      request<{ report: ApiMorningReport }>(
        `/workspaces/${encodeURIComponent(ws)}/memory/consolidate`,
        { method: 'POST' }
      ),
    report: (ws: string) =>
      request<{ report: ApiMorningReport }>(
        `/workspaces/${encodeURIComponent(ws)}/memory/report`,
        { method: 'GET' }
      ),
    getSettings: (ws: string) =>
      request<{ settings: ApiMemorySettings }>(
        `/workspaces/${encodeURIComponent(ws)}/memory/settings`,
        { method: 'GET' }
      ),
    updateSettings: (ws: string, body: ApiMemorySettingsUpdate) =>
      request<{ settings: ApiMemorySettings }>(
        `/workspaces/${encodeURIComponent(ws)}/memory/settings`,
        { method: 'PUT', body }
      ),
    testSettings: (ws: string, body: ApiMemorySettingsTest) =>
      request<ApiMemorySettingsTestResult>(
        `/workspaces/${encodeURIComponent(ws)}/memory/settings/test`,
        { method: 'POST', body }
      ),
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

// ---------------------------------------------------------------------------
// Agent session index (change agent-session-index, D3): per-user session rows
// for one agent, delivered newest-activity-first; `running` marks a live run
// on that session right now (run-manager intersection, server-side).
// ---------------------------------------------------------------------------

export interface ApiAgentSession {
  id: string;
  session_id: string;
  title: string;
  created_at: string;
  last_active_at: string;
  running: boolean;
}

export function listAgentSessions(slug: string, agentSlug: string): Promise<{ sessions: ApiAgentSession[] }> {
  return request(
    `/workspaces/${encodeURIComponent(slug)}/agents/${encodeURIComponent(agentSlug)}/sessions`,
    { method: 'GET' }
  );
}

/** Soft-deletes the caller's own session row; foreign-owned or absent rows
 * answer 404 (indistinguishable by design — no existence leak). */
export function deleteAgentSession(slug: string, agentSlug: string, sessionId: string): Promise<void> {
  return request<void>(
    `/workspaces/${encodeURIComponent(slug)}/agents/${encodeURIComponent(agentSlug)}/sessions/${encodeURIComponent(sessionId)}`,
    { method: 'DELETE' }
  );
}

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

