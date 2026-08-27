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
  name: string;
  model: string;
  temp: number;
  autonomy: 'approval' | 'suggest' | 'full' | string;
  channelPost: boolean;
  role: string;
  status: 'running' | 'idle' | 'error' | string;
  tools: string[];
  lastActive: string;
  skills: string[];
  mcp: string[];
  prompt: string;
  provider?: string;
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

export interface Workspace {
  id: string;
  name: string;
  plan: string;
  sub: string;
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
}
