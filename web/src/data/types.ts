import type { ApiChannelMember, ApiChannelMessage } from '../lib/api';
import type { Scheduler, SchedulerRun } from '../lib/schedulers';

export interface ToolCall {
  name: string;
  args: string;
  ms: number;
  error?: string;
}

/** One ordered entry of an agent turn body: a reasoning segment or a tool
 * card reference (index into Message.tools) — the stream order they occurred. */
export type MessagePart = { k: 'reasoning'; text: string } | { k: 'tool'; i: number };

export interface Message {
  id: string;
  author: 'you' | 'agent' | 'other' | 'error';
  ts: string;
  text: string;
  /** Accumulated reasoning trace (distinct from the visible text) — live turns only. */
  reasoning?: string;
  /** Ordered turn body: reasoning segments interleaved with tool cards.
   * Absent on legacy/seed messages, which fall back to tools → reasoning → text. */
  parts?: MessagePart[];
  /** Failure text for author:'error' entries (design D8). */
  error?: string;
  agentId?: string;
  /** Scheduler-origin marker (integrate-scheduler D1): the schedule's name
   * when known; the chip falls back to generic copy when it is ''. */
  scheduler?: string;
  name?: string;
  tools?: ToolCall[];
  branches?: Message[]; // Support for branching variants
  resp?: string; // Minted /v1 response id — the previous_response_id chain link
}

export interface ThreadSession {
  id: string;
  title: string;
  updated: string;
  messages: Message[];
  sess?: string; // Bound onclaw_session id (sess_<uuid>) once the thread goes live
  usage?: { finalInput: number; at: string }; // Latest terminal turn's final-call input (context meter, D5)
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
  /** Server-computed effective window — the meter's denominator. */
  effective_context_window?: number;
  /** Token count at which the backend summarizes — the meter's warn threshold. */
  summarization_trigger_tokens?: number;
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

/** Sidebar/room display view of a server channel (wire: ApiChannel in
 * lib/api). Hydrated from the API — channels are never seeded (change
 * integrate-agent-channels). `name` carries the slug so the sidebar/room keep
 * the mock's `#handle` visual; the server's display name rides `slug`'s
 * sibling on the wire, not the UI. The primary-agent concept is gone
 * (design D15) — agentId lingers optional for legacy fixtures. */
export interface Channel {
  id: string;
  workspace_id?: string;
  name: string;
  slug?: string;
  purpose: string;
  conventions?: string;
  created_at?: string;
  updated_at?: string;
  agentId?: string;
  unread: number;
  members: string[];
}

export interface Person {
  id: string;
  name: string;
  presence: 'online' | 'away' | string;
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

export interface Skill {
  id: string;
  name: string;
  version: string;
  enabled: boolean;
  tier: 'system' | 'workspace' | string;
  source: 'authored' | 'upload' | 'git' | 'fork' | 'system' | string;
  locked?: boolean;
  desc: string;
  dependencies?: { tools?: string[]; binaries?: string[]; python?: string[] };
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
  /** Per-channel wire roster (integrate-agent-channels): keyed by channel id. */
  channelRoster?: Record<string, ApiChannelMember[]>;
  /** Per-channel wire feed, ascending seq (integrate-agent-channels). */
  channelMessages?: Record<string, ApiChannelMessage[]>;
  people: Person[];
  threads: Record<string, { active: string | null; list: ThreadSession[] }>;
  /** Server-only (integrate-scheduler): schedules hydrate from the API via
   * loadSchedules and are never seeded — an empty list is the correct state.
   * Wire rows (lib/schedulers Scheduler), not a display projection. */
  schedules: Scheduler[];
  /** Server-only (integrate-scheduler): the workspace-wide scheduler-run
   * history hydrates with the runs screen; never seeded. */
  runs: SchedulerRun[];
  members: Member[];
  integrations: Integration[];
  skillLib: Skill[];
  keys: ApiKey[];
  providers?: ProviderConfig[];
}

