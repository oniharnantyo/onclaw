// OpenResponses (/v1) chat client.
//
// The agent is the model: `model` is an agent slug, the workspace API key is
// the credential (the key is the tenant), and `metadata.onclaw_session` binds
// the turn to an OnClaw session. Custom OnClaw events (`onclaw:*`) surface
// server-side tool traces and approval interrupts.
import OpenAI from 'openai';

import { API_ORIGIN } from './api';
// Server-provided per-segment split (wire `usage.context_breakdown`, D7):
// when the terminal event reports it, its numbers replace the client
// estimate in the context popover.
import type { ServerContextBreakdown } from './contextBreakdown';
// add-integration-authority: the service-run write escalation rides the same
// approval event as shell approvals — the shared shape lives in the one
// connections client module.
import type { ConnectionToolEscalation } from './connectionsApi';

export const ONCLAW_SESSION_KEY = 'onclaw_session';

export interface OnclawApproval {
  interrupt_id: string;
  command: string;
  response_id: string;
  session_id: string;
  /** add-integration-authority (task 3.2): present when the paused run is a
   * service-authority run that reached for a write-tier connection tool —
   * the transcript renders the service escalation card instead of the shell
   * approval card. Absent on the ordinary dangerous-command path. */
  tool?: ConnectionToolEscalation;
}

/** Terminal-event usage block, mapped from the wire's snake_case usage. */
export interface TurnUsage {
  inputTokens: number;
  outputTokens: number;
  totalTokens: number;
  /** The last provider call's input tokens — absent when the turn's usage
   * block carries no final-call figure. */
  finalInputTokens?: number;
  /** The server-composed context breakdown (wire `usage.context_breakdown`,
   * omitempty, D7) — segments instructions/tools/files/conversation; the
   * client-estimate fallback applies when the wire omits it. */
  contextBreakdown?: ServerContextBreakdown;
}

export interface TurnCallbacks {
  /** Called on each assistant text chunk. */
  onDelta: (text: string) => void;
  /** Called for each server-side tool call. Fires at output_item.added (card
   * appears, args still streaming — `args` undefined) and again at
   * output_item.done with the complete arguments string; callers key cards
   * by callId so the second call updates rather than duplicates. */
  onToolCall?: (name: string, callId: string, args?: string) => void;
  onToolOutput?: (callId: string, name: string, result: string, latencyMs?: number, isError?: boolean) => void;
  /** Called on each reasoning trace chunk (`onclaw:reasoning_delta`). */
  onReasoningDelta?: (delta: string) => void;
  /** Called when the run compacts the conversation context
   * (`onclaw:context_compacted`) with the display-only token estimates
   * (before → after) from the wire frame. */
  onContextCompacted?: (info: { tokensBefore: number; tokensAfter: number }) => void;
  /** Called when the run pauses for a dangerous-command approval. */
  onApprovalRequired?: (approval: OnclawApproval) => void;
  /**
   * Called once, at the FIRST stream event that carries the minted response
   * id (`response.created`) — early enough that the turn is still cancellable
   * while streaming. `onDone` later receives the id again at completion.
   */
  onResponseId?: (responseId: string) => void;
  /**
   * Called ONCE per terminal event (`response.completed`, `response.incomplete`,
   * `response.failed`) — before onDone/onError — with the mapped usage block,
   * or null when the event carries no usage block. An approval pause ends the
   * stream without a terminal event and never fires this.
   */
  onUsage?: (usage: TurnUsage | null) => void;
  onDone: (responseId?: string) => void;
  /**
   * `meta.unauthorized` marks a /v1 auth failure (401 / invalid_api_key) so
   * the caller can clear the workspace key slot, re-exchange once, and retry
   * (design D4) instead of looping on a stale key.
   * `meta.conflict` marks a 409 from the per-session run lock — another run
   * is still active on this session — so the caller can queue the turn
   * behind the live run instead of surfacing a failure.
   */
  onError: (message: string, meta?: { unauthorized?: boolean; conflict?: boolean }) => void;
}

// cached per (key) client — the OpenAI SDK is cheap but not free
const clients = new Map<string, OpenAI>();

/** Maps a terminal event's wire usage block (snake_case) into TurnUsage —
 * null when the event carries no usage block at all. `context_breakdown`
 * (D7) maps through to `contextBreakdown` when the wire carries it. */
function usageOf(response: any): TurnUsage | null {
  const u = response?.usage;
  if (!u || typeof u !== 'object') return null;
  return {
    inputTokens: u.input_tokens ?? 0,
    outputTokens: u.output_tokens ?? 0,
    totalTokens: u.total_tokens ?? 0,
    ...(typeof u.final_input_tokens === 'number' ? { finalInputTokens: u.final_input_tokens } : {}),
    ...(u.context_breakdown && typeof u.context_breakdown === 'object'
      ? { contextBreakdown: u.context_breakdown as ServerContextBreakdown }
      : {}),
  };
}

export function openResponsesClient(key: string): OpenAI {
  let c = clients.get(key);
  if (!c) {
    // The SDK requires an absolute baseURL; default to the app's own origin
    // (dev: the vite /v1 proxy, prod: same origin as the API).
    const base =
      API_ORIGIN ||
      (typeof window !== 'undefined' ? window.location.origin : '');
    c = new OpenAI({ apiKey: key, baseURL: `${base}/v1`, dangerouslyAllowBrowser: true });
    clients.set(key, c);
  }
  return c;
}

/** Lists the workspace's agents as models (id = agent slug). */
export async function listAgentModels(key: string): Promise<string[]> {
  const client = openResponsesClient(key);
  const page = await client.models.list();
  const ids: string[] = [];
  for await (const m of page) {
    ids.push((m as { id: string }).id);
  }
  return ids;
}

/**
 * Builds the /v1 `input` for a turn. With no attachments this is the plain
 * string — byte-for-byte the pre-attachment request. With attachments it
 * becomes the item-array form: one user message whose content carries an
 * `input_text` part (only when the text is non-empty) plus an
 * `input_image`/`input_file` URL part per attachment, the capability URL as
 * the wire token (add-chat-attachments D2).
 */
function turnInput(text: string, attachments?: ChatAttachment[]): string | OpenAI.Responses.ResponseInput {
  if (!attachments || attachments.length === 0) return text;
  const content: OpenAI.Responses.ResponseInputContent[] = [];
  if (text && text.trim()) content.push({ type: 'input_text', text });
  for (const a of attachments) {
    if (!a || !a.url) continue;
    if ((a.mime || '').toLowerCase().startsWith('image/')) {
      content.push({ type: 'input_image', image_url: a.url, detail: 'auto' });
    } else {
      content.push({ type: 'input_file', file_url: a.url, filename: a.name });
    }
  }
  return [{ role: 'user', content }];
}

/**
 * Runs one agent turn against POST /v1/responses with SSE streaming and
 * translates the event vocabulary into the given callbacks. Aborting
 * `opts.signal` tears the stream down mid-run; an aborted turn fires NO
 * callback — the stop control owns the terminal state (design D5), so the
 * abort must never surface as an error entry.
 */
export async function runTurn(
  key: string,
  params: { agentSlug: string; input: string; sessionId?: string; previousResponseId?: string; command?: string; attachments?: ChatAttachment[] },
  cb: TurnCallbacks,
  opts?: { signal?: AbortSignal },
): Promise<void> {
  const client = openResponsesClient(key);
  let text = '';
  try {
    // `command` rides metadata (where session binding already lives) — e.g.
    // `onclaw_command: "compact"` marks a compact turn whose `input` is the
    // focus text (chat-compact-command design D2).
    const metadata = {
      ...(params.sessionId ? { [ONCLAW_SESSION_KEY]: params.sessionId } : {}),
      ...(params.command ? { onclaw_command: params.command } : {}),
    };
    const stream = await client.responses.create({

      model: params.agentSlug,
      input: turnInput(params.input, params.attachments),
      stream: true,
      ...(Object.keys(metadata).length ? { metadata } : {}),
      ...(params.previousResponseId ? { previous_response_id: params.previousResponseId } : {}),
    }, { signal: opts?.signal });

    let responseId: string | undefined;
    let pendingApproval: OnclawApproval | null = null;

    for await (const ev of stream as unknown as AsyncIterable<{ type: string; [k: string]: any }>) {
      switch (ev.type) {
        case 'response.output_text.delta':
          text += ev.delta ?? '';
          cb.onDelta(ev.delta ?? '');
          break;
        case 'response.output_item.added':
          // Tool results ride the standard output-item lifecycle as
          // `onclaw.function_call_output` items (design D1) — the result is
          // only complete at .done, so outputs fire there, not here.
          if (ev.item?.type === 'function_call') {
            // Args at .added can be partial (eino chunks tool-call arguments
            // across frames), so the card fires argless here and picks up the
            // complete arguments at output_item.done.
            cb.onToolCall?.(ev.item.name, ev.item.call_id);
          }
          break;
        case 'response.output_item.done':
          if (ev.item?.type === 'function_call') {
            cb.onToolCall?.(ev.item.name, ev.item.call_id, ev.item.arguments);
          } else if (ev.item?.type === 'onclaw.function_call_output') {
            cb.onToolOutput?.(ev.item.call_id, ev.item.name, ev.item.result, ev.item.latency_ms, ev.item.is_error);
          }
          break;
        case 'onclaw:reasoning_delta':
          cb.onReasoningDelta?.(ev.delta ?? '');
          break;
        case 'onclaw:context_compacted':
          cb.onContextCompacted?.({
            tokensBefore: ev.tokens_before ?? 0,
            tokensAfter: ev.tokens_after ?? 0,
          });
          break;
        case 'onclaw:approval_required':
          pendingApproval = {
            interrupt_id: ev.interrupt_id,
            command: ev.command,
            response_id: ev.response_id,
            session_id: ev.session_id,
            // Service-run write escalation (add-integration-authority): the
            // backend attaches the connection tool the run paused at. Passed
            // through untouched — the card renders server-declared truth.
            ...(ev.tool ? { tool: ev.tool as ConnectionToolEscalation } : {}),
          };
          cb.onApprovalRequired?.(pendingApproval);
          break;
        case 'response.created':
          // Capture the minted id at the first event so the caller can cancel
          // the run while it is still streaming (design D5).
          responseId = ev.response?.id;
          if (responseId) cb.onResponseId?.(responseId);
          break;
        case 'response.completed':
          responseId = ev.response?.id;
          cb.onUsage?.(usageOf(ev.response));
          break;
        case 'response.incomplete':
          responseId = ev.response?.id;
          cb.onUsage?.(usageOf(ev.response));
          break;
        case 'response.failed':
          cb.onUsage?.(usageOf(ev.response));
          cb.onError(ev.response?.error?.message || 'the model failed to respond');
          return;
      }
    }

    // An approval pause ends the stream without a terminal event.
    if (!pendingApproval) cb.onDone(responseId);
  } catch (err: any) {
    // User-initiated stop: the abort IS the teardown. No onDone/onError —
    // the stop control already settled the composer and the transcript.
    if (opts?.signal?.aborted || err?.name === 'AbortError') return;
    const message = err?.error?.message || err?.message || 'request failed';
    const unauthorized = err?.status === 401 || err?.error?.code === 'invalid_api_key' || err?.code === 'invalid_api_key';
    const conflict = err?.status === 409;
    cb.onError(message, { unauthorized, conflict });
  }
}

/** Decodes the minted response id codec `resp_<session>_<turn>` (see
 * internal/openresponses codec.go: strip the prefix, split at the LAST
 * underscore — turn ids are UUIDs, never underscored) to its session id.
 * Undefined for anything that does not parse. */
export function sessionIdFromResponseId(responseId: string | null | undefined): string | undefined {
  if (!responseId || !responseId.startsWith('resp_')) return undefined;
  const payload = responseId.slice('resp_'.length);
  const cut = payload.lastIndexOf('_');
  if (cut <= 0 || cut >= payload.length - 1) return undefined;
  return payload.slice(0, cut);
}
