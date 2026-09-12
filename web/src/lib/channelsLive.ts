// Channel live events (change integrate-agent-channels, design D13): the SSE
// consumer for `/api/v1/workspaces/:ws/channels/:id/events?stream=true`.
//
// Connectivity follows the session-events precedent (livechat.streamSession
// Events): fetch + a ReadableStream reader — not native EventSource, which
// cannot send the JWT auth header. The subscriber contract:
//
//   1. The room REST-loads the feed first (cursor list) and passes the last
//      seen seq as `lastSeq` — events at or below it are dropped (seq-keyed
//      dedup against the REST load).
//   2. Every `data:` frame is one JSON event `{type, seq, payload}` with
//      snake_case payloads; message_posted's `{message: …}` envelope is
//      unwrapped here so consumers see the persisted feed row directly.
//   3. On a dropped/failed stream the consumer reconnects with gap recovery:
//      it refetches the feed after the last seen seq and replays the rows as
//      message_posted events before reopening the stream, so nothing posted
//      while offline is lost.
//
// This module stays UI-free: the room view wiring is gated behind the ASCII
// gallery approval (task 8.0); the store's applyChannelEvent is the only
// consumer today.
import { api } from './api';
// API_ORIGIN/getToken are read through the module namespace instead of named
// imports: suites that mock './api' narrowly keep working — a missing export
// degrades to undefined rather than breaking the import binding (the same
// pattern livechat.ts uses).
import * as apiModule from './api';
import type { ApiChannelMessage } from './api';

// ---------------------------------------------------------------------------
// Event vocabulary (design D11 SSE surface)
// ---------------------------------------------------------------------------

export type ChannelLiveEventType =
  | 'message_posted'
  | 'summon_considering'
  | 'summon_decided'
  | 'run_started'
  | 'run_finished';

/** One SSE frame. message_posted's payload is the persisted feed row (the
 * reader unwraps the wire's `{message: …}` envelope); the summon/run lifecycle
 * payloads are snake_case blobs (agent_id, session_id, reason, …) the room UI
 * renders (gated, task 8.1). */
export interface ChannelLiveEvent {
  type: ChannelLiveEventType;
  /** Monotonic per-channel feed cursor on message_posted frames (0 otherwise)
   * — the dedup key against the REST load. */
  seq: number;
  channel_id?: string;
  payload: ApiChannelMessage | Record<string, unknown>;
}

export type ChannelLiveStatus = 'connecting' | 'live' | 'reconnecting';

// ---------------------------------------------------------------------------
// Low-level stream reader (same mechanics as livechat.streamSessionEvents)
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

export interface StreamChannelEventsOptions {
  workspaceId: string;
  channelId: string;
  signal?: AbortSignal;
  /** Fired once the HTTP response is established — the reconnect loop resets
   * its backoff here. */
  onOpen?: () => void;
  onEvent: (ev: ChannelLiveEvent) => void;
  onError?: (err: unknown) => void;
}

/**
 * Streams one SSE session for a channel: opens the connection, parses
 * frames, and fires onEvent per well-formed event. The promise resolves when
 * the stream ends (proxies close idle connections); network/HTTP failures
 * call onError. Aborting the signal tears the stream down silently. The
 * reconnect loop lives in subscribeChannel — this function is single-shot.
 */
export async function streamChannelEvents(opts: StreamChannelEventsOptions): Promise<void> {
  const { workspaceId, channelId, signal, onOpen, onEvent, onError } = opts;
  const url = `${apiOrigin()}/api/v1/workspaces/${encodeURIComponent(
    workspaceId
  )}/channels/${encodeURIComponent(channelId)}/events?stream=true`;

  // Same auth as every other API call (request()): the workspace member's JWT.
  const headers: Record<string, string> = { Accept: 'text/event-stream' };
  const token = bearerToken();
  if (token) headers.Authorization = `Bearer ${token}`;

  try {
    const res = await fetch(url, { headers, signal });
    if (!res.ok || !res.body) {
      throw new Error(`channel event stream failed (HTTP ${res.status})`);
    }
    const contentType = res.headers.get('Content-Type') || '';
    if (contentType && !contentType.includes('text/event-stream')) {
      throw new Error(`channel event stream returned unexpected content type ${contentType}`);
    }
    onOpen?.();

    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';
    const handleFrame = (frame: string) => {
      const data = frame
        .split('\n')
        .filter((line) => line.startsWith('data:'))
        .map((line) => line.slice('data:'.length).replace(/^ /, ''))
        .join('\n');
      if (!data) return;
      // A malformed frame (or a keepalive comment slipping through) must not
      // tear down a healthy stream — skip it and keep reading.
      try {
        const ev = JSON.parse(data) as ChannelLiveEvent;
        if (!ev || typeof ev.type !== 'string') return;
        // message_posted rides a {message: …} envelope on the wire (hub.go) —
        // unwrap it so every consumer sees the persisted feed row; the dedup
        // seq stays on the frame.
        const payload = ev.payload as { message?: unknown } | undefined;
        if (ev.type === 'message_posted' && payload && typeof payload === 'object' &&
            payload.message && typeof payload.message === 'object') {
          ev.payload = payload.message as ApiChannelMessage;
        }
        onEvent(ev);
      } catch {
        // unparseable frame — ignored
      }
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
          }
        }
        if (done) break;
      }
      // A server that closes without the trailing blank line still gets its
      // final frame processed.
      buffer += decoder.decode();
      if (buffer.trim()) handleFrame(buffer);
    } finally {
      signal?.removeEventListener('abort', teardown);
      teardown();
    }
  } catch (err) {
    if (signal?.aborted) return;
    onError?.(err);
  }
}

// ---------------------------------------------------------------------------
// Subscribe API: dedup + reconnect with gap recovery
// ---------------------------------------------------------------------------

export interface SubscribeChannelOptions {
  workspaceId: string;
  channelId: string;
  /** Seq of the last message already folded in from the REST feed load —
   * events at or below it are dropped (seq dedup, D13). */
  lastSeq?: number;
  /** Aborting tears the subscription down silently (chat switch / unmount).
   * An internal controller is chained to it for the unsubscribe path. */
  signal?: AbortSignal;
  onEvent: (ev: ChannelLiveEvent) => void;
  /** Connection-state transitions — the room UI's live indicator (gated).
   * 'live' fires once the stream is established; 'reconnecting' on each
   * gap-recovery pass. Never 'reconnecting' before the first connect. */
  onStatus?: (status: ChannelLiveStatus) => void;
}

/** Backoff between reconnect attempts: 1s, 2s, 4s … capped at 15s; reset the
 * moment a stream is established. Code constant per design D16. */
const RECONNECT_BASE_MS = 1000;
const RECONNECT_MAX_MS = 15000;

/**
 * Subscribes to a channel's live event stream (design D13): connect AFTER the
 * REST feed load, dedup by seq, and on stream loss reconnect with gap
 * recovery — the feed is refetched after the last seen seq and the rows are
 * replayed as message_posted events before the stream reopens.
 *
 * Returns an unsubscribe function (idempotent, safe on double-invoke from
 * StrictMode effects).
 */
export function subscribeChannel(opts: SubscribeChannelOptions): () => void {
  const { workspaceId, channelId, lastSeq, signal, onEvent, onStatus } = opts;
  let lastSeenSeq = typeof lastSeq === 'number' ? lastSeq : 0;
  let closed = false;
  const owned = new AbortController();
  const abort = () => owned.abort();
  if (signal) {
    if (signal.aborted) abort();
    else signal.addEventListener('abort', abort, { once: true });
  }

  /** Seq-keyed dedup against the REST load and previously seen events. Only
   * message_posted frames carry a feed seq — lifecycle frames send 0 (hub.go:
   * "0 otherwise") and pass through untouched, nothing to dedup on. */
  const emit = (ev: ChannelLiveEvent) => {
    if (typeof ev.seq === 'number' && ev.seq > 0) {
      if (ev.seq <= lastSeenSeq) return;
      lastSeenSeq = ev.seq;
    }
    onEvent(ev);
  };

  /** Gap recovery: refetch the feed after the last seen seq and replay the
   * rows as message_posted events. A failed refetch is tolerated — the next
   * reconnect retries it; seq dedup makes replays idempotent. */
  const recoverGap = async () => {
    try {
      const res = await api.channels.messages.list(workspaceId, channelId, { after: lastSeenSeq });
      for (const msg of res?.messages || []) {
        emit({ type: 'message_posted', seq: msg.seq, channel_id: msg.channel_id, payload: msg });
      }
    } catch {
      // offline / old backend — the reconnect loop retries
    }
  };

  const delay = (attempt: number) =>
    Math.min(RECONNECT_BASE_MS * Math.pow(2, attempt), RECONNECT_MAX_MS);

  const loop = async () => {
    let attempt = 0;
    while (!closed && !owned.signal.aborted) {
      onStatus?.(attempt === 0 ? 'connecting' : 'reconnecting');
      await new Promise((r) => setTimeout(r, attempt === 0 ? 0 : delay(attempt)));
      if (closed || owned.signal.aborted) return;
      // Every pass after the first recovers the offline gap first so the
      // stream only carries events posted from now on.
      if (attempt > 0) await recoverGap();
      await streamChannelEvents({
        workspaceId,
        channelId,
        signal: owned.signal,
        onOpen: () => {
          attempt = 0;
          onStatus?.('live');
        },
        onEvent: emit,
        // onError is implicit: the promise resolving after a failure lands
        // back here, and the loop reconnects.
      });
      attempt++;
    }
  };

  void loop();

  return () => {
    if (closed) return;
    closed = true;
    owned.abort();
    signal?.removeEventListener('abort', abort);
  };
}
