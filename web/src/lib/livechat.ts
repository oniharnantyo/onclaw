// Live chat session support: per-workspace key provisioning (JWT → key
// exchange), connect-state tracking, and server-authoritative transcript
// hydration from the native session-events endpoint.
//
// Key storage follows the store contract: `onclaw.api_key.<workspaceId>` in
// localStorage via the store's getWorkspaceKey/setWorkspaceKey/clearWorkspaceKey.
// This module reads/writes the same slot directly as a fallback so the web
// build stays green while the store lands its half in parallel.
import { api, apiKeys, formatApiError } from './api';
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

export function isBoundSessionId(sessionId: string | null | undefined): boolean {
  return typeof sessionId === 'string' && sessionId.startsWith('sess_');
}

export interface HydratedTranscript {
  messages: any[];
  /** interrupt ids with an approval card still pending (no later turn activity). */
  pendingInterruptIds: string[];
  /** The last `turn_completed` event's `usage.final_input_tokens` — reloads
   * restore the context meter from it. Undefined when no turn carries usage. */
  finalInputTokens?: number;
}

function toolCardFor(turn: any): any[] {
  if (!turn.tools) turn.tools = [];
  return turn.tools;
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

  const messages: any[] = [];
  const pendingInterruptIds: string[] = [];
  let activityAfterInterrupt = false;
  const openInterrupts = new Set<string>();
  // Last turn_completed's final-call input (events are chronological — a
  // later turn_completed carrying usage overwrites an earlier one).
  let finalInputTokens: number | undefined;

  // Per-turn accumulator so user/assistant/tool events fold into one agent
  // message per turn, matching how live streaming builds the thread.
  let turnUser: any = null;
  let turnAgent: any = null;
  let turnId = '';
  const flushTurn = () => {
    // Rebuild the turn's minted response id (codec: resp_<session>_<turn>) so
    // a hydrated thread chains via previous_response_id instead of birthing
    // a fresh session on its next turn.
    if (turnAgent && turnId) turnAgent.resp = 'resp_' + sessionId + '_' + turnId;
    if (turnUser) messages.push(turnUser);
    if (turnAgent) messages.push(turnAgent);
    turnUser = null;
    turnAgent = null;
    turnId = '';
  };

  for (const ev of events || []) {
    // A new turn id flushes the accumulated turn even without a user message
    // (tool-only or cron turns) so each turn becomes its own agent message.
    if (ev.turn_id && turnId && ev.turn_id !== turnId) flushTurn();
    if (ev.turn_id) turnId = ev.turn_id;
    switch (ev.kind) {
      case 'message_completed': {
        const role = ev.message?.role;
        if (role === 'user') {
          if (turnUser || turnAgent) flushTurn();
          turnUser = {
            id: ev.id || `h-u-${messages.length}`,
            author: 'you',
            ts: ev.occurred_at || '',
            text: ev.message?.content || '',
          };
        } else if (role === 'assistant') {
          if (!turnAgent) {
            turnAgent = {
              id: ev.id || `h-a-${messages.length}`,
              author: 'agent',
              ts: ev.occurred_at || '',
              text: '',
              tools: [],
            };
          }
          turnAgent.text = (turnAgent.text || '') + (ev.message?.content || '');
          // Reasoning hydrates as ordered bubbles (same parts model as live
          // streaming): a message's reasoning lands where the event sits in
          // the turn — before its tool calls, or between them and the text.
          if (ev.message?.reasoning_content) appendReasoningPart(turnAgent, ev.message.reasoning_content);
        }
        if (openInterrupts.size > 0) {
          activityAfterInterrupt = true;
          openInterrupts.clear();
        }
        break;
      }
      case 'tool_call_started': {
        if (!turnAgent) {
          turnAgent = {
            id: ev.id || `h-a-${messages.length}`,
            author: 'agent',
            ts: ev.occurred_at || '',
            text: '',
            tools: [],
          };
        }
        toolCardFor(turnAgent).push({
          callId: ev.tool_call?.call_id,
          name: ev.tool_call?.name,
          args: ev.tool_call?.arguments || '',
          ms: 0,
        });
        appendToolPart(turnAgent, turnAgent.tools.length - 1);
        if (openInterrupts.size > 0) {
          activityAfterInterrupt = true;
          openInterrupts.clear();
        }
        break;
      }
      case 'tool_call_finished': {
        if (!turnAgent) {
          turnAgent = {
            id: ev.id || `h-a-${messages.length}`,
            author: 'agent',
            ts: ev.occurred_at || '',
            text: '',
            tools: [],
          };
        }
        const card = toolCardFor(turnAgent).find((t: any) => t.callId && t.callId === ev.tool_result?.call_id);
        if (card) {
          card.res = ev.tool_result?.result || '';
          card.ms = ev.tool_result?.latency ? Math.round(ev.tool_result.latency / 1e6) : 0;
          if (ev.tool_result?.is_error) card.error = card.res;
        }
        if (openInterrupts.size > 0) {
          activityAfterInterrupt = true;
          openInterrupts.clear();
        }
        break;
      }
      case 'approval_required': {
        if (!turnAgent) {
          turnAgent = {
            id: ev.id || `h-a-${messages.length}`,
            author: 'agent',
            ts: ev.occurred_at || '',
            text: '',
            tools: [],
          };
        }
        const interruptId = ev.approval?.interrupt_id || '';
        toolCardFor(turnAgent).push({
          args: '',
          ms: 0,
          approval: {
            interruptId,
            command: ev.approval?.command || '',
            sessionId,
            resolved: false,
          },
        });
        appendToolPart(turnAgent, turnAgent.tools.length - 1);
        openInterrupts.add(interruptId);
        activityAfterInterrupt = false;
        break;
      }
      case 'turn_started':
      case 'text_delta':
      case 'cancelled':
      case 'error':
      default:
        break;
      case 'turn_completed': {
        if (typeof ev.usage?.final_input_tokens === 'number') {
          finalInputTokens = ev.usage.final_input_tokens;
        }
        break;
      }
    }
  }
  flushTurn();

  // An approval is still pending when the latest interrupt was never followed
  // by resumed turn activity (mirrors the server's PendingApproval rule).
  if (!activityAfterInterrupt && openInterrupts.size > 0) {
    openInterrupts.forEach((id) => id && pendingInterruptIds.push(id));
  }

  return finalInputTokens === undefined
    ? { messages, pendingInterruptIds }
    : { messages, pendingInterruptIds, finalInputTokens };
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
}): Promise<HydratedTranscript | null> {
  const { workspaceId, agentSlug, chatId, sessionId } = opts;
  if (!isBoundSessionId(sessionId)) return null;
  let hydrated: HydratedTranscript;
  try {
    hydrated = await fetchSessionTranscript(workspaceId, agentSlug, sessionId);
  } catch {
    // Old backend / transient failure: keep the local thread untouched.
    return null;
  }
  if (hydrated.messages.length > 0) {
    applyServerTranscript(workspaceId, chatId, sessionId, hydrated.messages);
  }
  return hydrated;
}
