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
    /** Tool denylist (refactor-agent-tools-denylist): catalog keys the agent
     * must NOT expose — empty means every catalog tool is enabled. */
    disabled_tools: string[];
    skills?: string[];
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

  /** A chat attachment reference (add-chat-attachments D10/D11): mirrors the
   * server's {name, mime, size, url} on CompletedMessage.attachments — the
   * capability `url` is the render and wire token. `id` is the upload row id
   * when known (regenerate re-sends references, never re-uploads).
   *
   * Reference-document mention chips (add-reference-documents 10.4) ride the
   * SAME array with `kind: "document"` — identity only ({documentId, name,
   * path}, no url/mime/size), which is why every file field stays optional
   * for them. */
  interface ChatAttachment {
    id?: string;
    name: string;
    mime?: string;
    size?: number;
    url?: string;
    kind?: string;
    documentId?: string;
    path?: string;
  }

  interface ChatMessage {
    id: string;
    author: string;
    agentId?: string;
    ts: string;
    /** Parseable ISO wall-clock instant alongside the display `ts` — the
     * transcript's day separators read this (adopt-assistant-ui-elements D11). */
    at?: string;
    text: string;
    /** Scheduler-origin marker (integrate-scheduler D1): schedule name when known. */
    scheduler?: string;
    tools?: any[];
    branch?: number;
    branches?: any[];
    resp?: string;
    reasoning?: string;
    error?: string;
    parts?: any[];
    attachments?: ChatAttachment[];
  }

  interface ThreadSession {
    id: string;
    title: string;
    updated: string;
    messages: ChatMessage[];
    sess?: string;
  }

  interface ThreadState {
    active: string | null;
    list: ThreadSession[];
  }

  interface Channel {
    id: string;
    workspace_id?: string;
    name: string;
    slug?: string;
    purpose: string;
    conventions?: string;
    created_at?: string;
    updated_at?: string;
    /** Legacy primary-agent pointer (design D15 dropped it) — optional now. */
    agentId?: string;
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

  interface Skill {
    id: string;
    name: string;
    version: string;
    enabled: boolean;
    tier: string;
    source: string;
    locked?: boolean;
    desc?: string;
    dependencies?: { tools?: string[]; binaries?: string[]; python?: string[] };
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
    /** Server-only scheduler rows (lib/schedulers Scheduler); typed loosely
     * here to keep the global surface free of imports. */
    schedules: any[];
    runs: any[];
    members: Person[];
    integrations: Integration[];
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
