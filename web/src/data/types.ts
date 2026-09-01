export interface ToolCall {
  name: string;
  args: string;
  ms: number;
  error?: string;
}

export interface Message {
  id: string;
  author: 'you' | 'agent' | 'other';
  ts: string;
  text: string;
  agentId?: string;
  cron?: string;
  name?: string;
  tools?: ToolCall[];
  branches?: Message[]; // Support for branching variants
}

export interface ThreadSession {
  id: string;
  title: string;
  updated: string;
  messages: Message[];
}

export interface Agent {
  id: string;
  workspace_id?: string;
  slug?: string;
  name: string;
  model: string;
  temp: number;
  temperature?: number;
  max_tokens?: number | null;
  effort?: string | null;
  autonomy: 'approval' | 'suggest' | 'full' | string;
  channelPost?: boolean;
  role: string;
  description?: string;
  brief?: string;
  identity?: string;
  soul?: string;
  bootstrap?: string;
  status: 'running' | 'idle' | 'error' | string;
  tools: string[];
  lastActive: string;
  skills: string[];
  mcp: string[];
  avatar?: Record<string, any>;
  prompts_status?: 'generating' | 'ready' | 'failed';
  prompts_error?: string | null;
  prompt?: string;
  provider?: string;
  provider_id?: string;
  created_by?: string | null;
  updated_by?: string | null;
  created_at?: string;
  updated_at?: string;
}

export interface Channel {
  id: string;
  name: string;
  purpose: string;
  agentId: string;
  unread: number;
  members: string[];
}

export interface Person {
  id: string;
  name: string;
  presence: 'online' | 'away' | string;
}

export interface CronJob {
  id: string;
  name: string;
  agentId: string;
  expr: string;
  human: string;
  next: string;
  enabled: boolean;
  last: {
    status: 'success' | 'failed' | 'skipped' | string;
    when: string;
    dur: string;
  };
}

export interface Run {
  id: string;
  agentId: string;
  trigger: 'cron' | 'chat' | 'api' | string;
  when: string;
  dur: string;
  tokens: string;
  status: 'success' | 'failed' | string;
}

export interface Member {
  id: string;
  name: string;
  email: string;
  role: string;
  invited?: boolean;
}

export interface Integration {
  id: string;
  name: string;
  detail: string;
  connected: boolean;
}

export interface McpServer {
  id: string;
  name: string;
  transport: string;
  auth: string;
  tools: number;
  status: 'connected' | 'error' | 'disabled' | string;
  error?: string;
  sample: string[];
}

export interface Skill {
  id: string;
  name: string;
  version: string;
  uses: number;
  enabled: boolean;
  source: 'registry' | 'workspace' | string;
  desc: string;
}

export interface ApiKey {
  id: string;
  name: string;
  masked: string;
  full: string;
  created: string;
}

export type ProviderType =
  | 'openai'
  | 'anthropic'
  | 'gemini'
  | 'openrouter'
  | 'openai-compatible'
  | 'anthropic-compatible';

export interface ProviderConfig {
  id: string;
  workspace_id: string;
  type: ProviderType | string;
  name: string;
  base_url?: string;
  key_set: boolean;
  key_hint: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface Workspace {
  id: string;
  name: string;
  sub: string;
  slug?: string;
  is_master?: boolean;
  disabled_at?: string | null;
  tz: string;
  defaultModel: string;
  retention: string;
  agents: Agent[];
  channels: Channel[];
  people: Person[];
  threads: Record<string, { active: string | null; list: ThreadSession[] }>;
  cron: CronJob[];
  runs: Run[];
  members: Member[];
  integrations: Integration[];
  mcpServers: McpServer[];
  skillLib: Skill[];
  keys: ApiKey[];
  providers?: ProviderConfig[];
}

