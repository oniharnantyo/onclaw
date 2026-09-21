// Live chat session support: per-workspace key provisioning (JWT → key
// exchange), connect-state tracking, and server-authoritative transcript
// hydration from the native session-events endpoint.
//
// Key storage follows the store contract: `onclaw.api_key.<workspaceId>` in
// localStorage via the store's getWorkspaceKey/setWorkspaceKey/clearWorkspaceKey.
// This module reads/writes the same slot directly as a fallback so the web
// build stays green while the store lands its half in parallel.
import { api, apiKeys, formatApiError } from './api';
// API_ORIGIN/getToken are read through the module namespace instead of named
// imports: suites that mock './api' narrowly (e.g. chat/runtime.test.ts) keep
// working — a missing export degrades to undefined rather than breaking the
// import binding. Both exist on the real module.
import * as apiModule from './api';
import { appendReasoningPart, appendToolPart } from './helpers';
import { useStore } from '../store';
import {
  getWorkspaceKey,
  setWorkspaceKey,
  clearWorkspaceKey,
} from '../store/workspaceKeys';

// ---------------------------------------------------------------------------
// Connect state (chat UI surfaces this instead of canned mock replies)
// ---------------------------------------------------------------------------

export interface LiveChatStatus {
  state: 'idle' | 'provisioning' | 'connected' | 'disconnected';
  workspaceId?: string;
  message?: string;
}

let status: LiveChatStatus = { state: 'idle' };
const listeners = new Set<() => void>();

function setStatus(next: LiveChatStatus) {
  status = next;
  listeners.forEach((fn) => fn());
}

export function getLiveChatStatus(): LiveChatStatus {
  return status;
}

export function subscribeLiveChat(fn: () => void): () => void {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}

/** Flips to the disconnected state so the chat shows its connect/retry UI. */
export function markLiveChatDisconnected(workspaceId: string, message?: string) {
  setStatus({
    state: 'disconnected',
    workspaceId,
    message: message ?? 'Live chat is not connected for this workspace. Retry to connect.',
  });
}

/** Exchanges a fresh workspace-scoped key and stores it in the slot. */
export async function exchangeWorkspaceKey(workspaceId: string): Promise<string> {
  const res = await apiKeys.exchange(workspaceId);
  setWorkspaceKey(workspaceId, res.key);
  return res.key;
}

/**
 * Provisions the chat key on workspace entry: returns the stored key when the
 * slot is already filled, otherwise exchanges once. On failure the connect
 * state flips to disconnected — the chat UI shows its retry state, never a
 * canned reply.
 */
export async function ensureWorkspaceKey(workspaceId: string): Promise<string | null> {
  const existing = getWorkspaceKey(workspaceId);
  if (existing) {
    setStatus({ state: 'connected', workspaceId });
    return existing;
  }
  setStatus({ state: 'provisioning', workspaceId });
  try {
    const key = await exchangeWorkspaceKey(workspaceId);
    setStatus({ state: 'connected', workspaceId });
    return key;
  } catch (err) {
    setStatus({
      state: 'disconnected',
      workspaceId,
      message: formatApiError(err, 'Could not connect this browser to live chat.'),
    });
    return null;
  }
}

/**
 * /v1 auth failure recovery: drop the (revoked or stale) slot, re-exchange
 * exactly once, and surface the connect state if the retry fails too.
 * Returns a usable key, or null when the chat must show its connect state.
 */
export async function handleV1AuthFailure(workspaceId: string): Promise<string | null> {
  clearWorkspaceKey(workspaceId);
  try {
    const key = await exchangeWorkspaceKey(workspaceId);
    setStatus({ state: 'connected', workspaceId });
    return key;
  } catch {
    setStatus({
      state: 'disconnected',
      workspaceId,
      message: 'Live chat lost its connection to this workspace. Retry to exchange a new key.',
    });
    return null;
  }
}

/** Retry from the connect state — re-runs provisioning from a clean slot. */
export async function retryLiveChat(workspaceId: string): Promise<string | null> {
  clearWorkspaceKey(workspaceId);
  return ensureWorkspaceKey(workspaceId);
}

// ---------------------------------------------------------------------------
// Transcript hydration (server-authoritative replacement, D7)
// ---------------------------------------------------------------------------

/** Session ids that hydrate from the server transcript: interactive sessions
 * (`sess_<uuid>`) and scheduler-run sessions (`sched_<schedulerID>_<ts>`,
 * integrate-scheduler D7 — run sessions are artifacts that "hydrate like any
 * session"). */
export function isBoundSessionId(sessionId: string | null | undefined): boolean {
  return typeof sessionId === 'string' && (sessionId.startsWith('sess_') || sessionId.startsWith('sched_'));
}

export interface HydratedTranscript {
  messages: any[];
  /** interrupt ids with an approval card still pending (no later turn activity). */
  pendingInterruptIds: string[];
  /** The last `turn_completed` event's `usage.final_input_tokens` — reloads
   * restore the context meter from it. Undefined when no turn carries usage. */
  finalInputTokens?: number;
  /** The last `turn_completed` event's reported turn input/output tokens —
   * the context popover's detail rows after a reload (assistant-ui
   * context-display adoption). Undefined when the wire did not report them. */
  turnInputTokens?: number;
  turnOutputTokens?: number;
  /** The last `turn_completed` event's server-provided context breakdown
   * (`usage.context_breakdown`, omitempty D7) — replaces the client estimate
   * in the popover when present. */
  contextBreakdown?: import('./contextBreakdown').ServerContextBreakdown;
  /** Id of the last event folded into this transcript — the `after` cursor a
   * catch-up stream resumes from (D4). Undefined when the transcript is empty. */
  lastEventId?: string;
}

function toolCardFor(turn: any): any[] {
  if (!turn.tools) turn.tools = [];
  return turn.tools;
}

/** Wire attachment metadata ({name, mime, size, url}) → the entry's
 * ChatAttachment[] (add-chat-attachments D10). Malformed entries (no url or
 * no name) drop rather than render a dead chip. */
function hydrateAttachments(raw: any): ChatAttachment[] {
  if (!Array.isArray(raw)) return [];
  const out: ChatAttachment[] = [];
  for (const a of raw) {
    if (!a || typeof a !== 'object') continue;
    if (typeof a.url !== 'string' || !a.url) continue;
    if (typeof a.name !== 'string' || !a.name) continue;
    out.push({
      ...(typeof a.id === 'string' && a.id ? { id: a.id } : {}),
      name: a.name,
      mime: typeof a.mime === 'string' ? a.mime : '',
      size: typeof a.size === 'number' ? a.size : 0,
      url: a.url,
    });
  }
  return out;
}

/**
 * Folds transcript events into thread messages with the per-turn accumulator
 * shared by transcript hydration and the live catch-up stream (D4): whatever
 * the source, user/assistant/tool events land as one agent message per turn.
 */
class TranscriptTranslator {
  private messages: any[] = [];
  private pendingInterruptIds: string[] = [];
  private activityAfterInterrupt = false;
  private openInterrupts = new Set<string>();
  private finalInputTokens: number | undefined;
  private turnInputTokens: number | undefined;
  private turnOutputTokens: number | undefined;
  private contextBreakdown: import('./contextBreakdown').ServerContextBreakdown | undefined;
  private lastEventId: string | undefined;
  // Per-turn accumulator so user/assistant/tool events fold into one agent
  // message per turn, matching how live streaming builds the thread.
  private turnUser: any = null;
  private turnAgent: any = null;
  private turnId = '';
  // Scheduler-origin marker for the in-flight turn (integrate-scheduler):
  // transcript events from a scheduler run carry origin 'scheduler' and, when
  // the backend includes it, the schedule's name/id.
  private turnOriginTag: string | undefined;

  private sessionId: string;
  private idPrefix = 'h';

  constructor(sessionId: string, idPrefix = 'h') {
    this.sessionId = sessionId;
    this.idPrefix = idPrefix;
  }

  /** Continues an in-flight turn already rendered locally (catch-up, D4):
   * streamed chunks extend THIS message instead of minting a split bubble.
   * The agent message's resp codec (`resp_<session>_<turn>`) names the turn. */
  seedTail(turnId: string, agent: any): void {
    this.turnId = turnId;
    this.turnAgent = agent;
  }

  private ensureAgent(ev: any): any {
    if (!this.turnAgent) {
      this.turnAgent = {
        id: ev.id || `${this.idPrefix}-a-${this.messages.length}`,
        author: 'agent',
        ts: ev.occurred_at || '',
        text: '',
        tools: [],
      };
    }
    return this.turnAgent;
  }

  private flushTurn(): void {
    // Rebuild the turn's minted response id (codec: resp_<session>_<turn>) so
    // a hydrated thread chains via previous_response_id instead of birthing
    // a fresh session on its next turn.
    if (this.turnAgent && this.turnId) this.turnAgent.resp = 'resp_' + this.sessionId + '_' + this.turnId;
    // Stamp the scheduler-origin marker (integrate-scheduler): '' (schedule
    // unknown) still renders the generic chip, so the marker is sticky once set.
    if (this.turnAgent && this.turnOriginTag !== undefined && this.turnAgent.scheduler === undefined) {
      this.turnAgent.scheduler = this.turnOriginTag;
    }
    if (this.turnUser) this.messages.push(this.turnUser);
    if (this.turnAgent) this.messages.push(this.turnAgent);
    this.turnUser = null;
    this.turnAgent = null;
    this.turnId = '';
    this.turnOriginTag = undefined;
  }

  push(ev: any): void {
    if (ev.id) this.lastEventId = ev.id;
    // A new turn id flushes the accumulated turn even without a user message
    // (tool-only or cron turns) so each turn becomes its own agent message.
    if (ev.turn_id && this.turnId && ev.turn_id !== this.turnId) this.flushTurn();
    if (ev.turn_id) this.turnId = ev.turn_id;
    if (ev.origin === 'scheduler' && this.turnOriginTag === undefined) {
      this.turnOriginTag = ev.scheduler_name || ev.scheduler || ev.scheduler_id || '';
    }
    switch (ev.kind) {
      case 'message_completed': {
        const role = ev.message?.role;
        if (role === 'user') {
          const text = ev.message?.content || '';
          // Attachment metadata hydrates into the entry so reloaded history
          // renders the SAME chips the optimistic bubble did
          // (add-chat-attachments D10): {name, mime, size, url} on the wire.
          const atts = hydrateAttachments(ev.message?.attachments);
          // Tool results persist under role user with no extractable text and
          // no attachments — they never render and would split the turn into
          // empty bubbles. Attachment-only user messages (empty content,
          // non-empty attachments) are real messages and must survive.
          if (!text.trim() && atts.length === 0) break;
          if (this.turnUser || this.turnAgent) this.flushTurn();
          this.turnUser = {
            id: ev.id || `${this.idPrefix}-u-${this.messages.length}`,
            author: 'you',
            ts: ev.occurred_at || '',
            text,
            ...(atts.length ? { attachments: atts } : {}),
          };
        } else if (role === 'assistant') {
          const text = ev.message?.content || '';
          const reasoning = ev.message?.reasoning_content || '';
          if (!text.trim() && !reasoning.trim()) break;
          const agent = this.ensureAgent(ev);
          agent.text = (agent.text || '') + text;
          // Reasoning hydrates as ordered bubbles (same parts model as live
          // streaming): a message's reasoning lands where the event sits in
          // the turn — before its tool calls, or between them and the text.
          if (reasoning) appendReasoningPart(agent, reasoning);
        }
        this.noteInterruptActivity();
        break;
      }
      case 'text_delta': {
        // Live-broadcast only (deltas are never persisted): the catch-up
        // stream folds them into the turn body exactly like the live runtime.
        const delta = ev.text_delta || '';
        if (!this.turnAgent && !delta.trim()) break;
        this.ensureAgent(ev).text = (this.turnAgent.text || '') + delta;
        break;
      }
      case 'reasoning_delta': {
        const delta = ev.reasoning_delta || '';
        if (!this.turnAgent && !delta.trim()) break;
        appendReasoningPart(this.ensureAgent(ev), delta);
        break;
      }
      case 'tool_call_started': {
        const agent = this.ensureAgent(ev);
        // A call already carded (the subscribe-boundary window can deliver
        // the same call from BOTH the history replay and the live tap) never
        // duplicates its card — tool_call_finished updates by call id.
        const callId = ev.tool_call?.call_id;
        if (callId && toolCardFor(agent).some((t: any) => t.callId === callId)) break;
        toolCardFor(agent).push({
          callId,
          name: ev.tool_call?.name,
          args: ev.tool_call?.arguments || '',
          ms: 0,
          ts: ev.occurred_at || '', // wall-clock start for the expanded card (design D7)
        });
        appendToolPart(agent, agent.tools.length - 1);
        this.noteInterruptActivity();
        break;
      }
      case 'tool_call_finished': {
        const agent = this.ensureAgent(ev);
        const card = toolCardFor(agent).find((t: any) => t.callId && t.callId === ev.tool_result?.call_id);
        if (card) {
          card.res = ev.tool_result?.result || '';
          card.ms = ev.tool_result?.latency ? Math.round(ev.tool_result.latency / 1e6) : 0;
          if (ev.tool_result?.is_error) card.error = card.res;
        }
        this.noteInterruptActivity();
        break;
      }
      case 'approval_required': {
        const agent = this.ensureAgent(ev);
        const interruptId = ev.approval?.interrupt_id || '';
        toolCardFor(agent).push({
          args: '',
          ms: 0,
          approval: {
            interruptId,
            command: ev.approval?.command || '',
            sessionId: this.sessionId,
            resolved: false,
          },
        });
        appendToolPart(agent, agent.tools.length - 1);
        this.openInterrupts.add(interruptId);
        this.activityAfterInterrupt = false;
        break;
      }
      case 'prompt_blocked': {
        // Hook-blocked prompt (integrate-agent-hooks D6): the model never
        // ran — flush the turn accumulated so far (the user message) and
        // mint a standalone notice entry carrying the enforcing hook and
        // reason. Hydrated history and the live catch-up stream share this
        // case, so the notice renders identically after a reload.
        if (this.turnUser || this.turnAgent) this.flushTurn();
        this.messages.push({
          id: ev.id || `${this.idPrefix}-n-${this.messages.length}`,
          author: 'notice',
          ts: ev.occurred_at || '',
          text: '',
          notice: {
            hook: ev.prompt_blocked?.hook || ev.hook || '',
            reason: ev.prompt_blocked?.reason || ev.reason || '',
          },
        });
        this.noteInterruptActivity();
        break;
      }
      case 'context_compacted': {
        // Compaction divider (chat-compact-command D6): hydrated transcripts
        // and the catch-up replay carry the token estimates in the event's
        // compaction payload. The entry renders through the same
        // CompactionDivider as the live /v1 event — never an agent ack bubble.
        if (this.turnUser || this.turnAgent) this.flushTurn();
        this.messages.push({
          id: ev.id || `${this.idPrefix}-c-${this.messages.length}`,
          author: 'compaction',
          ts: ev.occurred_at || '',
          text: '',
          compaction: {
            tokensBefore: ev.compaction?.tokens_before ?? 0,
            tokensAfter: ev.compaction?.tokens_after ?? 0,
          },
        });
        break;
      }
      case 'memory_ingested': {
        // Post-turn memory chip (integrate-agent-zero-memory D11): the
        // background pipeline's committed counts — never content. The chip
        // is its own post-turn entry: the turn accumulated so far flushes
        // first, then the chip lands exactly where it sat in the event
        // stream. Hydrated history and the catch-up replay share this case,
        // so the chip renders identically after a reload (it is just
        // another session event).
        if (this.turnUser || this.turnAgent) this.flushTurn();
        this.messages.push({
          id: ev.id || `${this.idPrefix}-mem-${this.messages.length}`,
          author: 'memory',
          ts: ev.occurred_at || '',
          text: '',
          memory: {
            noteIds: ev.memory_ingested?.note_ids || [],
            eventIds: ev.memory_ingested?.event_ids || [],
            counts: ev.memory_ingested?.counts || { shared: 0, user: 0, agent: 0 },
          },
        });
        this.noteInterruptActivity();
        break;
      }
      case 'turn_started':
      case 'cancelled':
      case 'error':
      default:
        break;
      case 'turn_completed': {
        if (typeof ev.usage?.final_input_tokens === 'number') {
          this.finalInputTokens = ev.usage.final_input_tokens;
        }
        // Turn detail rows + server breakdown (assistant-ui context-display
        // adoption): each kept only when the wire reported it.
        if (typeof ev.usage?.input_tokens === 'number') this.turnInputTokens = ev.usage.input_tokens;
        if (typeof ev.usage?.output_tokens === 'number') this.turnOutputTokens = ev.usage.output_tokens;
        if (ev.usage?.context_breakdown && typeof ev.usage.context_breakdown === 'object') {
          this.contextBreakdown = ev.usage.context_breakdown;
        }
        break;
      }
    }
  }

  private noteInterruptActivity(): void {
    if (this.openInterrupts.size > 0) {
      this.activityAfterInterrupt = true;
      this.openInterrupts.clear();
    }
  }

  /** Flushed messages plus the in-progress tail, for incremental writers:
   * repeated views stay stable (ids and object identity) so a catch-up writer
   * can re-render the growing tail without duplicating completed turns. */
  view(): {
    messages: any[];
    tail: { turnId: string; user: any; agent: any } | null;
    finalInputTokens?: number;
    turnInputTokens?: number;
    turnOutputTokens?: number;
    contextBreakdown?: import('./contextBreakdown').ServerContextBreakdown;
  } {
    const tail =
      this.turnUser || this.turnAgent
        ? { turnId: this.turnId, user: this.turnUser, agent: this.turnAgent }
        : null;
    return {
      messages: this.messages,
      tail,
      ...(this.finalInputTokens !== undefined ? { finalInputTokens: this.finalInputTokens } : {}),
      ...(this.turnInputTokens !== undefined ? { turnInputTokens: this.turnInputTokens } : {}),
      ...(this.turnOutputTokens !== undefined ? { turnOutputTokens: this.turnOutputTokens } : {}),
      ...(this.contextBreakdown ? { contextBreakdown: this.contextBreakdown } : {}),
    };
  }

  /** Terminal transcript (hydration path): flushes the tail and resolves
   * which approval interrupts were never followed by turn activity. */
  result(): HydratedTranscript {
    this.flushTurn();
    // An approval is still pending when the latest interrupt was never followed
    // by resumed turn activity (mirrors the server's PendingApproval rule).
    if (!this.activityAfterInterrupt && this.openInterrupts.size > 0) {
      this.openInterrupts.forEach((id) => id && this.pendingInterruptIds.push(id));
    }
    return {
      messages: this.messages,
      pendingInterruptIds: this.pendingInterruptIds,
      ...(this.finalInputTokens !== undefined ? { finalInputTokens: this.finalInputTokens } : {}),
      ...(this.turnInputTokens !== undefined ? { turnInputTokens: this.turnInputTokens } : {}),
      ...(this.turnOutputTokens !== undefined ? { turnOutputTokens: this.turnOutputTokens } : {}),
      ...(this.contextBreakdown ? { contextBreakdown: this.contextBreakdown } : {}),
      ...(this.lastEventId !== undefined ? { lastEventId: this.lastEventId } : {}),
    };
  }
}

/**
 * Fetches the session transcript from the native session-events endpoint and
 * translates the event vocabulary into local thread messages. Approvals that
 * the server history shows as unresolved render as pending (actionable) cards.
 */
export async function fetchSessionTranscript(
  workspaceId: string,
  agentSlug: string,
  sessionId: string
): Promise<HydratedTranscript> {
  const res = await api.agents.sessionEvents(workspaceId, agentSlug, sessionId);
  const events = res?.events || [];

  const translator = new TranscriptTranslator(sessionId);
  for (const ev of events || []) translator.push(ev);
  return translator.result();
}

/**
 * Replaces the local thread for a bound session with the server transcript
 * (server-authoritative), preserving only an in-flight optimistic user message.
 */
export function applyServerTranscript(
  workspaceId: string,
  chatId: string,
  sessionId: string,
  messages: any[]
): void {
  const store: any = useStore.getState();
  store.updateTenant(workspaceId, (t: any) => {
    const th = t.threads[chatId];
    const sess = th && th.list.find((x: any) => x.id === sessionId);
    if (!sess) return t;
    const running = store.ui.running;
    const lastLocal = sess.messages[sess.messages.length - 1];
    const inFlight =
      running && lastLocal && lastLocal.author === 'you' ? [lastLocal] : [];
    sess.messages = [...messages, ...inFlight];
    return t;
  });
}

/**
 * Hydrates a bound session into the local store: fetches the server transcript
 * and REPLACES the local thread (server is authoritative), preserving only an
 * in-flight optimistic user message. Legacy unbound sessions hydrate nothing.
 */
export async function hydrateSession(opts: {
  workspaceId: string;
  agentSlug: string;
  chatId: string;
  sessionId: string;
  /** Checked after the fetch resolves and before the transcript replaces the
   * local thread — a stale attempt (StrictMode's aborted first effect run, a
   * chat switch) must not clobber a newer attach's streamed tail. */
  signal?: AbortSignal;
  /** Scheduler-origin tag (integrate-scheduler): the schedule's name when the
   * open path knows it (runs-view context). Stamped onto agent messages that
   * carry no origin tag of their own; '' renders the generic chip. */
  originTag?: string;
}): Promise<HydratedTranscript | null> {
  const { workspaceId, agentSlug, chatId, sessionId, signal, originTag } = opts;
  if (!isBoundSessionId(sessionId)) return null;
  let hydrated: HydratedTranscript;
  try {
    hydrated = await fetchSessionTranscript(workspaceId, agentSlug, sessionId);
  } catch {
    // Old backend / transient failure: keep the local thread untouched.
    return null;
  }
  if (signal?.aborted) return null;
  if (originTag !== undefined) {
    for (const m of hydrated.messages) {
      if (m.author === 'agent' && m.scheduler === undefined) m.scheduler = originTag;
    }
  }
  if (hydrated.messages.length > 0) {
    applyServerTranscript(workspaceId, chatId, sessionId, hydrated.messages);
  }
  return hydrated;
}

// ---------------------------------------------------------------------------
// In-flight re-attachment & catch-up stream (live-run-reattach-and-catchup D4)
// ---------------------------------------------------------------------------

/** Absolute API origin without hard import-binding to api.ts exports: suites
 * that mock './api' narrowly (no API_ORIGIN) resolve to the same-origin
 * default instead of breaking the import. Evaluated lazily — module-scope
 * namespace reads would throw under those mocks. */
function apiOrigin(): string {
  try {
    return (apiModule as any).API_ORIGIN ?? '';
  } catch {
    return '';
  }
}

/** The workspace member's JWT (same credential request() attaches), read
 * through the namespace so narrow api mocks without getToken degrade to an
 * unauthenticated probe rather than throwing. */
function bearerToken(): string | null {
  try {
    return (apiModule as any).getToken?.() ?? null;
  } catch {
    return null;
  }
}

export interface StreamSessionEventsOptions {
  workspaceId: string;
  agentSlug: string;
  sessionId: string;
  /** Only stream events committed after this event id (catch-up cursor). */
  after?: string;
  signal?: AbortSignal;
  onEvent: (ev: any) => void;
  onDone?: () => void;
  onError?: (err: unknown) => void;
}

/**
 * Resilient SSE consumer for the streaming session-events endpoint
 * (`?stream=true&after=<eventId>`). Uses fetch + a ReadableStream reader —
 * not native EventSource, which cannot send the auth header. Each SSE
 * `data:` frame is one transcript event (same vocabulary as the non-stream
 * endpoint); the literal `[DONE]` payload or a plain stream end resolves
 * with onDone, network/parse failures call onError. Aborting the signal
 * tears the stream down silently.
 */
export async function streamSessionEvents(opts: StreamSessionEventsOptions): Promise<void> {
  const { workspaceId, agentSlug, sessionId, after, signal, onEvent, onDone, onError } = opts;
  const params = new URLSearchParams({ stream: 'true' });
  if (after) params.set('after', after);
  const url = `${apiOrigin()}/api/v1/workspaces/${encodeURIComponent(workspaceId)}/agents/${encodeURIComponent(
    agentSlug
  )}/sessions/${encodeURIComponent(sessionId)}/events?${params.toString()}`;

  // Same auth as every other API call (request()): the workspace member's JWT.
  const headers: Record<string, string> = { Accept: 'text/event-stream' };
  const token = bearerToken();
  if (token) headers.Authorization = `Bearer ${token}`;

  let finished = false;
  const finish = () => {
    if (finished) return;
    finished = true;
    onDone?.();
  };

  try {
    const res = await fetch(url, { headers, signal });
    if (!res.ok || !res.body) {
      throw new Error(`session event stream failed (HTTP ${res.status})`);
    }
    const contentType = res.headers.get('Content-Type') || '';
    if (contentType && !contentType.includes('text/event-stream')) {
      throw new Error(`session event stream returned unexpected content type ${contentType}`);
    }

    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';
    let sawDone = false;
    const handleFrame = (frame: string) => {
      const data = frame
        .split('\n')
        .filter((line) => line.startsWith('data:'))
        .map((line) => line.slice('data:'.length).replace(/^ /, ''))
        .join('\n');
      if (!data) return;
      if (data === '[DONE]') {
        sawDone = true;
        return;
      }
      onEvent(JSON.parse(data));
    };
    // The abort path must also unblock a pending read (mock streams and some
    // proxies don't reject in-flight reads on their own).
    const teardown = () => {
      try {
        void reader.cancel();
      } catch {
        // already closed
      }
    };
    if (signal) {
      if (signal.aborted) {
        teardown();
        return;
      }
      signal.addEventListener('abort', teardown, { once: true });
    }

    try {
      for (;;) {
        const { value, done } = await reader.read();
        if (value) {
          // Normalize CRLF so frames split identically however the server
          // ends its lines (a lone trailing \r pairs with the next chunk).
          buffer = (buffer + decoder.decode(value, { stream: true })).replace(/\r\n/g, '\n');
          let cut: number;
          while ((cut = buffer.indexOf('\n\n')) !== -1) {
            const frame = buffer.slice(0, cut);
            buffer = buffer.slice(cut + 2);
            handleFrame(frame);
            if (sawDone) break;
          }
        }
        if (sawDone || done) break;
      }
      // A server that closes without the trailing blank line still gets its
      // final frame processed.
      if (!sawDone) {
        buffer += decoder.decode();
        if (buffer.trim()) handleFrame(buffer);
      }
    } finally {
      signal?.removeEventListener('abort', teardown);
      teardown();
    }
    // Aborted mid-stream: clean teardown — neither onDone nor onError.
    if (signal?.aborted) return;
    finish();
  } catch (err) {
    if (signal?.aborted) return;
    onError?.(err);
    return;
  }
}

/**
 * Attaches the catch-up stream for a bound session (D4): streams every event
 * after the hydrate cursor and folds live deltas, tool cards, and reasoning
 * into the thread via the SAME per-turn translation as transcript hydration.
 *
 * Attachment is unconditional for bound sessions (the page did not itself
 * start a live turn — the caller guards `ui.running`): a transcript heuristic
 * cannot see a run between persisted events, and the server-side design makes
 * the probe idempotent — with no run in flight it replays nothing and answers
 * `[DONE]` immediately (design D2 Phase 3).
 */
export function attachCatchUpStream(opts: {
  workspaceId: string;
  agentSlug: string;
  chatId: string;
  sessionId: string;
  after?: string;
  signal?: AbortSignal;
  /** Completion hooks fired after the internal running-state patch: onDone
   * when the server closes the stream ([DONE] / clean end), onError on a
   * network/HTTP failure. Neither fires when the signal aborts (chat switch). */
  onDone?: () => void;
  onError?: (err: unknown) => void;
}): void {
  const { workspaceId, agentSlug, chatId, sessionId, after, signal, onDone, onError } = opts;
  if (signal?.aborted) return;
  const translator = new TranscriptTranslator(sessionId, 'cu');
  const owned = new Set<string>();

  // Seed the in-flight tail: the newest agent message carrying a response id
  // of this session (e.g. the reload landed between that message and the
  // turn's tool calls; the conflict-queue flow has the queued user message
  // sitting after it) — streamed chunks must continue THAT message instead
  // of minting a split bubble. Its resp (`resp_<session>_<turn>`) names the
  // turn.
  const th = useStore.getState().db[workspaceId]?.threads?.[chatId];
  const sess = th?.list?.find((x: any) => x.id === sessionId);
  const prefix = 'resp_' + sessionId + '_';
  const seedMsg = [...(sess?.messages || [])]
    .reverse()
    .find((m: any) => m.author === 'agent' && typeof m.resp === 'string' && m.resp.startsWith(prefix));
  if (seedMsg) {
    translator.seedTail(seedMsg.resp.slice(prefix.length), JSON.parse(JSON.stringify(seedMsg)));
    owned.add(seedMsg.id);
  }

  const write = () => {
    const view = translator.view();
    const rendered = view.tail
      ? [...view.messages, view.tail.user, view.tail.agent].filter(Boolean)
      : view.messages;
    for (const m of rendered) if (m?.id) owned.add(m.id);
    // Deep-clone on write: the translator keeps mutating these objects across
    // events, while updateTenant stores them by reference.
    const clone = JSON.parse(JSON.stringify(rendered));
    useStore.getState().updateTenant(workspaceId, (t: any) => {
      const th = t.threads[chatId];
      const sess = th && th.list.find((x: any) => x.id === sessionId);
      if (!sess) return t;
      // Rebuild the catch-up tail: messages the stream owns are REPLACED IN
      // PLACE (id-keyed) so an owned turn tail never jumps below messages
      // typed since (the queued-conflict flow); turns the stream minted that
      // the thread doesn't have yet append at the end.
      const byId = new Map(clone.filter((m: any) => m?.id).map((m: any) => [m.id, m]));
      const replaced = new Set<string>();
      sess.messages = sess.messages.map((m: any) => {
        const next = byId.get(m.id);
        if (next === undefined) return m;
        replaced.add(m.id);
        return next;
      });
      for (const m of clone) {
        if (m?.id && !replaced.has(m.id)) sess.messages.push(m);
      }
      return t;
    });
    if (typeof view.finalInputTokens === 'number') {
      useStore.getState().recordThreadUsage(workspaceId, chatId, view.finalInputTokens, {
        input: view.turnInputTokens,
        output: view.turnOutputTokens,
        ...(view.contextBreakdown ? { contextBreakdown: view.contextBreakdown } : {}),
      });
    }
  };

  // Chat switch / unmount mid-catch-up: nothing else will call onDone, so
  // clear the composer spinner here.
  signal?.addEventListener('abort', () => useStore.getState().patchUi({ running: false }), { once: true });

  void streamSessionEvents({
    workspaceId,
    agentSlug,
    sessionId,
    after,
    signal,
    onEvent: (ev) => {
      // Live catch-up renders exactly like a live turn: spinner on until the
      // server closes the stream. The synthetic run_active frame (no run
      // payload) flips the spinner the moment the tap attaches — before the
      // first real event, which can be seconds away mid-tool-call.
      useStore.getState().patchUi({ running: true });
      translator.push(ev);
      write();
    },
    onDone: () => {
      useStore.getState().patchUi({ running: false });
      onDone?.();
    },
    onError: (err) => {
      useStore.getState().patchUi({ running: false });
      onError?.(err);
    },
  });
}
