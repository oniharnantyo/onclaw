declare global {
  interface Agent {
    id: string;
    workspace_id?: string;
    slug?: string;
    name: string;
    model: string;
    temp: number;
    temperature?: number;
    max_tokens?: number | null;
    effort?: string | null;
    autonomy: string;
    channelPost?: boolean;
    role: string;
    description?: string;
    brief?: string;
    identity?: string;
    soul?: string;
    status: string;
    tools: string[];
    skills?: string[];
    mcp?: string[];
    avatar?: Record<string, any>;
    prompts_status?: 'generating' | 'ready' | 'failed' | string;
    prompts_error?: string | null;
    lastActive: string;
    prompt?: string;
    provider?: string;
    provider_id?: string;
    created_by?: string | null;
    updated_by?: string | null;
    created_at?: string;
    updated_at?: string;
  }

  interface ChatMessage {
    id: string;
    author: string;
    agentId?: string;
    ts: string;
    text: string;
    cron?: string;
    tools?: any[];
    branch?: number;
    branches?: any[];
  }

  interface ThreadSession {
    id: string;
    title: string;
    updated: string;
    messages: ChatMessage[];
  }

  interface ThreadState {
    active: string | null;
    list: ThreadSession[];
  }

  interface CronJob {
    id: string;
    name: string;
    agentId: string;
    expr: string;
    human: string;
    next?: string;
    enabled: boolean;
    last?: {
      status: string;
      when: string;
      dur: string;
    };
  }

  interface Run {
    id: string;
    agentId: string;
    trigger: string;
    when: string;
    dur: string;
    tokens: string;
    status: string;
  }

  interface Channel {
    id: string;
    name: string;
    purpose: string;
    agentId: string;
    unread: number;
    members: string[];
  }

  interface Person {
    id: string;
    name: string;
    email: string;
    role: string;
  }

  interface Integration {
    id: string;
    name: string;
    detail: string;
    connected: boolean;
  }

  interface McpServer {
    id: string;
    name: string;
    transport: string;
    auth: string;
    tools: number;
    status: string;
    sample?: string[];
  }

  interface Skill {
    id: string;
    name: string;
    version: string;
    uses: number;
    enabled: boolean;
    source: string;
    desc?: string;
  }

  interface ApiKey {
    id: string;
    name: string;
    masked: string;
    full: string;
    created: string;
  }

  interface Workspace {
    id: string;
    name: string;
    sub: string;
    tz: string;
    defaultModel: string;
    retention: string;
    agents: Agent[];
    channels: Channel[];
    people: Person[];
    threads: Record<string, ThreadState>;
    cron: CronJob[];
    runs: Run[];
    members: Person[];
    integrations: Integration[];
    mcpServers: McpServer[];
    skillLib: Skill[];
    keys: ApiKey[];
  }

  interface AppState {
    db: Record<string, Workspace>;
    pos: {
      tenantId: string;
      view: string;
      chatId: string;
      showContext: boolean;
    };
    ui: any;
    [key: string]: any; // fallback for zustand actions
  }
}

export {};
