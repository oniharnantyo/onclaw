/**
 * @vitest-environment node
 */
// SSE consumer for channel events (change integrate-agent-channels, D13):
// frame decoding (incl. the message_posted {message} envelope unwrap), seq
// dedup against the REST load, and reconnect-with-gap-recovery. './api' is
// mocked narrowly — the module reads API_ORIGIN/getToken through the
// namespace, so a missing export degrades instead of breaking the binding.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { streamChannelEvents, subscribeChannel, type ChannelLiveEvent } from './channelsLive';

vi.mock('./api', () => ({
  api: {
    channels: {
      messages: {
        // Gap recovery's cursor refetch; per-test overrides below.
        list: vi.fn().mockResolvedValue({ messages: [] }),
      },
    },
  },
  API_ORIGIN: '',
  getToken: () => 'jwt-test-token',
}));

import { api } from './api';

const encoder = new TextEncoder();

/** Minimal SSE Response: only what the consumer reads is provided. `open`
 * keeps the stream unclosed; `onCancel` flags a reader-side cancel (the
 * unsubscribe path must unblock the pending read). */
function sseResponse(
  chunks: string[],
  init?: { status?: number; contentType?: string | null; open?: boolean; onCancel?: () => void }
): Response {
  const status = init?.status ?? 200;
  const headers = new Headers();
  if (init?.contentType !== null) headers.set('Content-Type', init?.contentType ?? 'text/event-stream');
  return {
    ok: status >= 200 && status < 300,
    status,
    headers,
    body: new ReadableStream<Uint8Array>({
      start(controller) {
        for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
        if (!init?.open) controller.close();
      },
      cancel: () => {
        init?.onCancel?.();
      },
    }),
  } as unknown as Response;
}

const frame = (ev: unknown) => `data: ${JSON.stringify(ev)}\n\n`;

// The wire's message_posted frame: seq top-level, feed row nested (hub.go).
const messagePostedFrame = (seq: number, id: string) =>
  frame({
    type: 'message_posted',
    seq,
    payload: {
      message: {
        id, workspace_id: 'ws1', channel_id: 'ch1', seq,
        author_type: 'agent', author_agent_id: 'a-1', body: 'on it',
        mentions: [], chain_depth: 0, created_at: '2026-09-01T00:00:00Z',
      },
    },
  });

beforeEach(() => {
  vi.mocked(api.channels.messages.list).mockClear().mockResolvedValue({ messages: [] });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('streamChannelEvents', () => {
  it('opens the stream URL with the JWT bearer header and unwraps message_posted payloads', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      sseResponse([messagePostedFrame(7, 'm-7')])
    );
    vi.stubGlobal('fetch', fetchMock);

    const events: ChannelLiveEvent[] = [];
    await streamChannelEvents({
      workspaceId: 'ws1',
      channelId: 'ch1',
      onEvent: (ev) => events.push(ev),
    });

    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toBe('/api/v1/workspaces/ws1/channels/ch1/events?stream=true');
    expect(init.headers.Accept).toBe('text/event-stream');
    expect(init.headers.Authorization).toBe('Bearer jwt-test-token');

    // The {message: …} envelope is gone: the payload IS the feed row.
    expect(events).toHaveLength(1);
    expect(events[0].type).toBe('message_posted');
    expect(events[0].seq).toBe(7);
    const row = events[0].payload as any;
    expect(row.id).toBe('m-7');
    expect(row.body).toBe('on it');
    expect(row.seq).toBe(7);
  });

  it('parses chunked and CRLF frames, ignores keepalive comments and malformed data', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      sseResponse([
        // message_posted split mid-JSON across chunks, CRLF line endings.
        'data: {"type":"message_posted","seq":7,"payload":{"mess',
        'age":{"id":"m-7","seq":7,"body":"on it"}}}\r\n\r\n',
        // Keepalive comment + a summon lifecycle frame.
        ': keepalive\n\n',
        frame({ type: 'summon_considering', seq: 0, payload: { channel_id: 'ch1', agent_id: 'a-1' } }),
        // Malformed frame — skipped, the stream survives.
        'data: {not-json\n\n',
        messagePostedFrame(8, 'm-8'),
      ])
    );
    vi.stubGlobal('fetch', fetchMock);

    const events: ChannelLiveEvent[] = [];
    const onError = vi.fn();
    await streamChannelEvents({ workspaceId: 'ws1', channelId: 'ch1', onEvent: (ev) => events.push(ev), onError });

    expect(events.map((ev) => ev.type)).toEqual(['message_posted', 'summon_considering', 'message_posted']);
    expect((events[0].payload as any).id).toBe('m-7');
    expect(onError).not.toHaveBeenCalled();
  });

  it('still processes the final frame when the server closes without a trailing blank line', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      // No \n\n terminator on the last frame.
      sseResponse([messagePostedFrame(3, 'm-3').trimEnd()])
    ));
    const events: ChannelLiveEvent[] = [];
    await streamChannelEvents({ workspaceId: 'ws1', channelId: 'ch1', onEvent: (ev) => events.push(ev) });
    expect(events).toHaveLength(1);
    expect((events[0].payload as any).id).toBe('m-3');
  });

  it('reports HTTP and network failures via onError', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(sseResponse([], { status: 500 })));
    const onError = vi.fn();
    await streamChannelEvents({ workspaceId: 'ws1', channelId: 'ch1', onEvent: () => {}, onError });
    expect(onError).toHaveBeenCalledTimes(1);

    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('fetch failed')));
    const onError2 = vi.fn();
    await streamChannelEvents({ workspaceId: 'ws1', channelId: 'ch1', onEvent: () => {}, onError: onError2 });
    expect(onError2).toHaveBeenCalledTimes(1);
  });

  it('honors abort: silent teardown, no onError', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      sseResponse([messagePostedFrame(1, 'm-1')], { open: true })
    ));
    const controller = new AbortController();
    const onEvent = vi.fn();
    const onError = vi.fn();
    const settled = streamChannelEvents({
      workspaceId: 'ws1', channelId: 'ch1', signal: controller.signal, onEvent, onError,
    });
    await vi.waitFor(() => expect(onEvent).toHaveBeenCalledTimes(1));
    controller.abort();
    await settled;
    expect(onError).not.toHaveBeenCalled();
  });
});

describe('subscribeChannel — seq dedup', () => {
  it('drops events at or below lastSeq, keeps newer ones, and advances the cursor', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      sseResponse([
        messagePostedFrame(5, 'm-5'), // at the REST cursor — dropped
        messagePostedFrame(6, 'm-6'), // new — kept
        messagePostedFrame(6, 'm-6-dup'), // replay overlap — dropped
        messagePostedFrame(7, 'm-7'), // kept
      ])
    ));
    const seen: string[] = [];
    const unsub = subscribeChannel({
      workspaceId: 'ws1', channelId: 'ch1', lastSeq: 5,
      onEvent: (ev) => seen.push((ev.payload as any).id),
    });
    await vi.waitFor(() => expect(seen).toEqual(['m-6', 'm-7']));
    unsub();
  });

  it('passes seq-0 lifecycle frames through without touching the message cursor', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      sseResponse([
        frame({ type: 'summon_considering', seq: 0, payload: { channel_id: 'ch1', agent_id: 'a-1' } }),
        messagePostedFrame(4, 'm-4'), // must survive: the 0 never advanced the cursor
      ])
    ));
    const events: ChannelLiveEvent[] = [];
    const unsub = subscribeChannel({
      workspaceId: 'ws1', channelId: 'ch1', lastSeq: 3,
      onEvent: (ev) => events.push(ev),
    });
    await vi.waitFor(() => expect(events).toHaveLength(2));
    expect(events[0].type).toBe('summon_considering');
    expect((events[1].payload as any).id).toBe('m-4');
    unsub();
  });
});

describe('subscribeChannel — reconnect with gap recovery', () => {
  it('refetches the feed after the last seen seq, replays rows as message_posted, then reopens', async () => {
    // First connection dies, the second one opens and stays live.
    const fetchMock = vi.fn()
      .mockRejectedValueOnce(new TypeError('fetch failed'))
      .mockResolvedValueOnce(sseResponse([messagePostedFrame(8, 'm-8-live')], { open: true }));
    vi.stubGlobal('fetch', fetchMock);

    // The offline gap: rows 6 and 7 posted while the stream was down.
    vi.mocked(api.channels.messages.list).mockResolvedValueOnce({
      messages: [
        { id: 'm-6', workspace_id: 'ws1', channel_id: 'ch1', seq: 6, author_type: 'user', author_user_id: 'u-1', body: 'gap six', mentions: [], chain_depth: 0, created_at: 'x' },
        { id: 'm-7', workspace_id: 'ws1', channel_id: 'ch1', seq: 7, author_type: 'agent', author_agent_id: 'a-1', body: 'gap seven', mentions: [], chain_depth: 0, created_at: 'x' },
      ] as any,
    });

    const seen: ChannelLiveEvent[] = [];
    const statuses: string[] = [];
    const unsub = subscribeChannel({
      workspaceId: 'ws1', channelId: 'ch1', lastSeq: 5,
      onEvent: (ev) => seen.push(ev),
      onStatus: (s) => statuses.push(s),
    });

    // Reconnect backoff for attempt 1 is 2s — wait it out.
    await vi.waitFor(() => {
      expect(seen.map((ev) => (ev.payload as any).id)).toEqual(['m-6', 'm-7', 'm-8-live']);
    }, { timeout: 8000 });

    // Gap rows replay through the same message_posted path as live frames.
    expect(seen[0].type).toBe('message_posted');
    expect((seen[0].payload as any).body).toBe('gap six');
    // The cursor refetch starts strictly after the last seen seq.
    expect(api.channels.messages.list).toHaveBeenCalledWith('ws1', 'ch1', { after: 5 });
    // Two connections: the failed one and the reopened one.
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(statuses).toEqual(['connecting', 'reconnecting', 'live']);
    unsub();
  });
});

describe('subscribeChannel — teardown', () => {
  it('unsubscribe aborts the open stream and is idempotent', async () => {
    let cancelled = false;
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      sseResponse([messagePostedFrame(9, 'm-9')], {
        open: true,
        onCancel: () => {
          cancelled = true;
        },
      })
    ));
    const onEvent = vi.fn();
    const unsub = subscribeChannel({ workspaceId: 'ws1', channelId: 'ch1', onEvent });
    await vi.waitFor(() => expect(onEvent).toHaveBeenCalledTimes(1));

    unsub();
    unsub(); // double-invoke (StrictMode effects) must be safe
    await vi.waitFor(() => expect(cancelled).toBe(true));
  });
});
