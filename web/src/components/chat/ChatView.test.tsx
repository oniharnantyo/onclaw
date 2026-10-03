/**
 * @vitest-environment jsdom
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, fireEvent, act } from '@testing-library/react';
import { ChatView } from './ChatView';
import { useStore } from '../../store';
import { fetchOlderTranscriptPage, prependServerTranscript } from '../../lib/livechat';

vi.mock('../../lib/api', () => ({
  api: {
    onUnauthorized: vi.fn(),
    skills: { list: vi.fn().mockResolvedValue({ skills: [] }) },
    tools: { list: vi.fn().mockResolvedValue({ tools: [] }) },
    agents: { listSkills: vi.fn().mockResolvedValue({ skills: [] }) },
  },
}));

// ChatView only touches the livechat connect-state surface; the paging tests
// below mock the older-page fetch/prepend pair and drive the store directly.
const { LIVE_CONNECTED } = vi.hoisted(() => ({ LIVE_CONNECTED: { state: 'connected' as const } }));

vi.mock('../../lib/livechat', () => ({
  getLiveChatStatus: () => LIVE_CONNECTED,
  subscribeLiveChat: () => () => {},
  retryLiveChat: vi.fn(),
  fetchOlderTranscriptPage: vi.fn(),
  prependServerTranscript: vi.fn(),
}));

const tenant = {
  id: 't1',
  sub: 't1',
  name: 'Acme',
  agents: [{ id: 'a1', name: 'Atlas', slug: 'atlas', status: 'idle' }],
};

const target = { kind: 'agent', obj: { id: 'a1', name: 'Atlas' } };
const agent = { id: 'a1', name: 'Atlas', slug: 'atlas', status: 'idle' };

const base = {
  tenant, target, agent, channelMembers: [], thread: [],
  onOpenMembers: vi.fn(), onSend: vi.fn(),
  onCancel: vi.fn(), onAttach: vi.fn(), onCopy: vi.fn(),
  onRefresh: vi.fn(), onBranch: vi.fn(), onEditSubmit: vi.fn(),
  busy: false,
};

describe('ChatView loading states', () => {
  it('shows a single agent avatar while a live turn streams (no thinking row)', () => {
    // Live turns push an optimistic empty agent message and keep running=true
    // for the whole stream — the message row carries the loading dots itself.
    const thread = [
      { id: 'u1', author: 'you', text: 'hello', ts: '' },
      { id: 'm1', author: 'agent', agentId: 'a1', text: '', ts: '' },
    ];
    const { container } = render(<ChatView {...base} thread={thread} typing busy />);
    expect(container.querySelectorAll('[data-od-id="msg-m1"]').length).toBe(1);
    expect(container.querySelectorAll('[data-od-id="msg-thinking"]').length).toBe(0);
  });

  it('shows the thinking row while running with no agent message yet', () => {
    const thread = [{ id: 'u1', author: 'you', text: 'hello', ts: '' }];
    const { container } = render(<ChatView {...base} thread={thread} typing busy />);
    expect(container.querySelectorAll('[data-od-id="msg-thinking"]').length).toBe(1);
  });

  it('renders latency for finished tool cards and pulsing dots only for pending cards during a live turn', () => {
    // Regression: a turn that emits multiple tool calls in sequence must show
    // latency for completed calls (e.g. clickElement that returned an error,
    // navigate that succeeded) and only pulse dots on the call still waiting
    // for output. The whole turn having busy=true must not turn every card into
    // pulsing dots.
    const thread = [
      { id: 'u1', author: 'you', text: 'check lilianweng', ts: '' },
      {
        id: 'm1',
        author: 'agent',
        agentId: 'a1',
        text: '',
        ts: '',
        tools: [
          { name: 'browser.navigate', args: '{"url":"https://lilianweng.github.io/"}', res: '{"loaded":true}', ms: 1420 },
          { name: 'browser.click', args: '{"ref":"182"}', error: 'unknown ref "182"', res: 'unknown ref "182"', ms: 380 },
          { name: 'browser.read', args: '{}', ms: 0 },
        ],
        parts: [
          { k: 'tool', i: 0 },
          { k: 'tool', i: 1 },
          { k: 'tool', i: 2 },
        ],
      },
    ];
    const { container } = render(<ChatView {...base} thread={thread} typing busy />);
    const buttons = container.querySelectorAll('button[data-od-id^="tool-"]');
    expect(buttons.length).toBe(3);

    // Card 0: finished success → shows latency, no dots
    expect(buttons[0].textContent).toContain('1.4 s');
    expect(buttons[0].querySelectorAll('.od-dot').length).toBe(0);

    // Card 1: finished error → shows error latency, no dots
    expect(buttons[1].textContent).toContain('error · 380 ms');
    expect(buttons[1].querySelectorAll('.od-dot').length).toBe(0);

    // Card 2: still pending (no res/error) → pulses dots, no ms label
    expect(buttons[2].querySelectorAll('.od-dot').length).toBe(3);
    expect(buttons[2].textContent).not.toContain('0 ms');
  });

  it('renders a hook-blocked prompt as a notice entry in place of the assistant reply', () => {
    // integrate-agent-hooks: a prompt_blocked transcript entry hydrates (and
    // streams) as an author:'notice' message — a compact notice line, never
    // a spinner or an empty assistant bubble.
    const thread = [
      { id: 'u1', author: 'you', text: 'wipe the database', ts: '' },
      { id: 'n1', author: 'notice', text: '', ts: '9:14 AM', notice: { hook: 'Compliance Gate', reason: 'destructive prompts require approval' } },
    ];
    const { container } = render(<ChatView {...base} thread={thread} />);
    const notice = container.querySelector('[data-testid="prompt-blocked-notice"]');
    expect(notice).not.toBeNull();
    expect(notice?.textContent).toContain('Compliance Gate');
    expect(notice?.textContent).toContain('destructive prompts require approval');
    // No agent message row was minted for the blocked turn.
    expect(container.querySelectorAll('[data-role="assistant"]').length).toBe(0);
  });
});

describe('ChatView compaction surface (chat-compact-command)', () => {
  it('shows the Compacting context… status row while a compact turn runs — no thinking row, no user pill', () => {
    const thread = [{ id: 'u1', author: 'you', text: 'earlier turn', ts: '' }];
    const { container } = render(<ChatView {...base} thread={thread} typing busy compacting />);
    const status = container.querySelector('[data-od-id="compaction-status"]');
    expect(status).not.toBeNull();
    expect(status?.textContent).toContain('Compacting context…');
    expect(container.querySelectorAll('[data-od-id="msg-thinking"]').length).toBe(0);
  });

  it('an ordinary running turn shows the thinking row, never the compact status row', () => {
    const thread = [{ id: 'u1', author: 'you', text: 'hello', ts: '' }];
    const { container } = render(<ChatView {...base} thread={thread} typing busy />);
    expect(container.querySelectorAll('[data-od-id="msg-thinking"]').length).toBe(1);
    expect(container.querySelector('[data-od-id="compaction-status"]')).toBeNull();
  });

  it('renders the compaction divider from a live (summarySaved) thread entry and retracts the status row', () => {
    const thread = [
      { id: 'u1', author: 'you', text: 'earlier turn', ts: '' },
      { id: 'c1', author: 'compaction', text: '', ts: '9:14 AM', summarySaved: true, compaction: { tokensBefore: 154000, tokensAfter: 9200 } },
    ];
    // Real post-compacted-event state: the runtime cleared ui.compacting when
    // the divider landed, but the turn is still running (typing/busy) — the
    // divider has swapped in and NEITHER row renders while it finishes.
    const { container } = render(<ChatView {...base} thread={thread} typing busy />);
    const divider = container.querySelector('[data-od-id="msg-c1"][data-role="compaction"]');
    expect(divider).not.toBeNull();
    expect(divider?.textContent).toContain('Context compacted');
    expect(divider?.textContent).toContain('154k → 9.2k tokens');
    expect(divider?.textContent).toContain('summary saved to transcript');
    expect(container.querySelector('[data-od-id="compaction-status"]')).toBeNull();
    expect(container.querySelectorAll('[data-od-id="msg-thinking"]').length).toBe(0);
  });

  it('renders the divider from a hydrated history entry with token counts only (mockup D)', () => {
    const thread = [
      { id: 'u1', author: 'you', text: 'earlier turn', ts: '' },
      { id: 'h-c1', author: 'compaction', text: '', ts: '', compaction: { tokensBefore: 154000, tokensAfter: 9200 } },
    ];
    const { container } = render(<ChatView {...base} thread={thread} />);
    const divider = container.querySelector('[data-od-id="msg-h-c1"][data-role="compaction"]');
    expect(divider).not.toBeNull();
    expect(divider?.textContent).toContain('154k → 9.2k tokens');
    expect(divider?.textContent).not.toContain('summary saved to transcript');
  });
});

describe('ChatView day separators + hover timestamps (adopt-assistant-ui-elements 9.2)', () => {
  const DAY = 24 * 60 * 60 * 1000;
  const today = new Date();
  const yesterday = new Date(Date.now() - DAY);
  const older = new Date(Date.now() - 3 * DAY);
  // Same formatting calls the component makes — identical environment, so
  // the expected strings match whatever locale the runner resolves.
  const dateLabel = (d: Date) => d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
  const stamp = (d: Date) => d.toLocaleString(undefined, { month: 'short', day: 'numeric', year: 'numeric', hour: 'numeric', minute: '2-digit' });

  const dividersOf = (container: HTMLElement) =>
    Array.from(container.querySelectorAll('[data-od-id="day-divider"]'));
  const titlesInList = (container: HTMLElement) =>
    Array.from(container.querySelector('[data-od-id="message-list"]')!.querySelectorAll('[title]'))
      .map((el) => el.getAttribute('title'));
  // Message rows carry their own UI titles ("Copy", "Edit"); a date+time
  // stamp always contains an h:mm clock component.
  const stampsInList = (container: HTMLElement) =>
    titlesInList(container).filter((t) => t && /\d{1,2}:\d{2}/.test(t));

  it('renders a divider wherever the day changes between consecutive dated entries — Today/Yesterday/date labels', () => {
    const thread = [
      { id: 'u1', author: 'you', text: 'yesterday msg', ts: '', at: yesterday.toISOString() },
      // Undated entry between dated ones: silent, never contributes a divider.
      { id: 'n1', author: 'notice', text: '', ts: '9:14 AM', notice: { hook: 'Compliance Gate', reason: 'no' } },
      { id: 'm1', author: 'agent', agentId: 'a1', text: 'today reply', ts: '', at: today.toISOString() },
      { id: 'm2', author: 'agent', agentId: 'a1', text: 'older reply', ts: '', at: older.toISOString() },
      { id: 'm3', author: 'agent', agentId: 'a1', text: 'same older day', ts: '', at: older.toISOString() },
    ];
    const { container } = render(<ChatView {...base} thread={thread}/>);
    const dividers = dividersOf(container);
    expect(dividers).toHaveLength(2);
    // yesterday → today boundary labels the new day by name; today → older
    // by date; the same-day pair m2/m3 adds nothing.
    expect(dividers[0].textContent).toContain('Today');
    expect(dividers[1].textContent).toContain(dateLabel(older));
    expect(dividers.every((d) => !d.textContent!.includes('Yesterday'))).toBe(true);
    // Each divider sits before the entry of the new day.
    const m1 = container.querySelector('[data-od-id="msg-m1"]')!;
    const m2 = container.querySelector('[data-od-id="msg-m2"]')!;
    expect(dividers[0].compareDocumentPosition(m1) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(dividers[1].compareDocumentPosition(m2) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    // Full-width chrome, house hairline idiom.
    expect(dividers[0].getAttribute('role')).toBe('separator');
    expect(dividers[0].getAttribute('aria-label')).toBe('Today');
  });

  it('undated entries stay silent: no dividers and no hover timestamps anywhere', () => {
    const thread = [
      { id: 'n1', author: 'notice', text: '', ts: '9:14 AM', notice: { hook: 'Compliance Gate', reason: 'no' } },
      { id: 'u1', author: 'you', text: 'legacy display ts', ts: '9:15 AM' },
      { id: 'e1', author: 'error', text: '', ts: 'just now', error: 'boom' },
    ];
    const { container } = render(<ChatView {...base} thread={thread}/>);
    expect(dividersOf(container)).toHaveLength(0);
    expect(stampsInList(container)).toHaveLength(0);
  });

  it('dated entries expose their full date+time on hover (title attr)', () => {
    const thread = [
      { id: 'u1', author: 'you', text: 'yesterday msg', ts: '', at: yesterday.toISOString() },
      { id: 'm1', author: 'agent', agentId: 'a1', text: 'today reply', ts: '', at: today.toISOString() },
    ];
    const { container } = render(<ChatView {...base} thread={thread}/>);
    expect(stampsInList(container)).toEqual([stamp(yesterday), stamp(today)]);
  });

  it('a same-day transcript renders no divider at all (divider needs a day CHANGE)', () => {
    const thread = [
      { id: 'u1', author: 'you', text: 'hello', ts: '', at: today.toISOString() },
      { id: 'm1', author: 'agent', agentId: 'a1', text: 'hi there', ts: '', at: today.toISOString() },
    ];
    const { container } = render(<ChatView {...base} thread={thread}/>);
    expect(dividersOf(container)).toHaveLength(0);
    // Hover timestamps still work without dividers.
    expect(stampsInList(container)).toEqual([stamp(today), stamp(today)]);
  });
});

describe('ChatView message queue stack (adopt-assistant-ui-elements 8.2)', () => {
  beforeEach(() => {
    useStore.setState({
      pos: { tenantId: 't1', view: 'chats', chatId: 'a1', showContext: false },
      messageQueue: {},
    });
  });

  const seedQueue = (texts: string[]) => {
    useStore.setState({
      messageQueue: {
        't1::a1': texts.map((text, i) => ({ id: 'q' + (i + 1), text, enqueuedAt: '' })),
      },
    });
  };

  it('shows no queue chrome when nothing is queued — even while a run is active', () => {
    const thread = [{ id: 'u1', author: 'you', text: 'hello', ts: '' }];
    const { container } = render(<ChatView {...base} thread={thread} typing busy />);
    expect(container.querySelector('[data-od-id="message-queue"]')).toBeNull();
  });

  it('renders a running row naming the in-flight turn above ordered queued rows', () => {
    seedQueue(['first queued', 'second queued']);
    const { container } = render(<ChatView {...base} busy />);
    const stack = container.querySelector('[data-od-id="message-queue"]');
    expect(stack).not.toBeNull();
    // Running row names the in-flight turn (the chat's agent).
    expect(container.querySelector('[data-testid="queue-running-row"]')?.textContent).toContain('Atlas');
    // Ordered cancelable rows under it.
    const items = container.querySelectorAll('[data-od-id="queue-item"]');
    expect(items).toHaveLength(2);
    expect(items[0].textContent).toContain('first queued');
    expect(items[1].textContent).toContain('second queued');
    expect(container.querySelectorAll('[data-testid="queue-remove"]')).toHaveLength(2);
  });

  it('removing a row cancels only that entry through the store', () => {
    seedQueue(['keep me', 'drop me']);
    const { container } = render(<ChatView {...base} busy />);
    const drop = container.querySelector('[data-queue-remove="q2"]');
    expect(drop).not.toBeNull();
    fireEvent.click(drop as Element);

    const q = useStore.getState().messageQueue['t1::a1'] || [];
    expect(q.map((x: any) => x.text)).toEqual(['keep me']);
  });

  it('the running row is gated on the run — rows without an active run carry no running row', () => {
    seedQueue(['still queued']);
    const { container } = render(<ChatView {...base} busy={false} />);
    expect(container.querySelector('[data-od-id="message-queue"]')).not.toBeNull();
    expect(container.querySelector('[data-testid="queue-running-row"]')).toBeNull();
  });
});

// --- Scroll-up paging (add-server-authoritative-transcript D4) --------------
// The sentinel/observer/anchor live in ChatView; the older-page fetch wiring
// lives in ChatRoute. These tests render ChatView against the real store and
// a route-side callback harness that mirrors ChatRoute's onFetchOlder (single
// in-flight ref guard, cursor read from the store's session), with the
// livechat module mocked: prependServerTranscript injects through the store
// so the prepended page renders for real.
describe('ChatView scroll-up paging (add-server-authoritative-transcript)', () => {
  class MockIntersectionObserver {
    static instances: any[] = [];
    callback: any;
    constructor(callback: any, _opts?: any) {
      this.callback = callback;
      MockIntersectionObserver.instances.push(this);
    }
    observe() {}
    unobserve() {}
    disconnect() {}
    fire(isIntersecting: boolean) {
      this.callback([{ isIntersecting }], this);
    }
  }

  const pos = { tenantId: 't1', view: 'chats', chatId: 'a1', showContext: false };

  const seedStoreSession = (messages: any[], paging?: { hasMore: boolean; oldest?: string }) => {
    const sess: any = { id: 'sess_1', title: 'History', updated: '', messages };
    if (paging) {
      sess.eventsHasMore = paging.hasMore;
      sess.eventsOldest = paging.oldest;
    }
    useStore.setState({
      pos,
      db: {
        ...useStore.getState().db,
        t1: {
          ...(useStore.getState().db as any).t1,
          id: 't1', sub: 't1', name: 'Acme', agents: [], channels: [], people: [],
          threads: { a1: { active: 'sess_1', list: [sess] } },
        },
      },
    });
    return sess;
  };

  const storeSession = () =>
    (useStore.getState().db as any).t1.threads.a1.list.find((x: any) => x.id === 'sess_1');

  // Mirrors ChatRoute's onFetchOlder: ref guard, cursor from the store's
  // session, fetch → prepend → clear. Returns whether a fetch started.
  const makeOnFetchOlder = () => {
    const guard = { inFlight: false };
    return vi.fn((sessionId: string): boolean => {
      if (guard.inFlight) return false;
      const sess = (useStore.getState().db as any).t1?.threads?.a1?.list?.find((x: any) => x.id === sessionId);
      const before = sess?.eventsOldest;
      if (!before) return false;
      guard.inFlight = true;
      void (async () => {
        try {
          const page = await fetchOlderTranscriptPage('t1', 'atlas', sessionId, before);
          if (page) prependServerTranscript('t1', 'a1', sessionId, page.messages, { hasMore: page.hasMore, oldest: page.oldest });
        } finally {
          guard.inFlight = false;
        }
      })();
      return true;
    });
  };

  // Mirrors ChatRoute's read path: thread/session derive from the store (the
  // slices useThread selects), so a store injection re-renders the view.
  function PagingHarness(props: any) {
    const th: any = useStore((s: any) => s.db[s.pos.tenantId]?.threads?.a1);
    const list = th && !Array.isArray(th) ? th.list : [];
    const sess = list.find((x: any) => x.id === th?.active) || null;
    return <ChatView {...base} {...props} thread={sess ? sess.messages : []} session={sess} />;
  }

  const fireSentinel = (isIntersecting = true) => {
    const io = MockIntersectionObserver.instances[MockIntersectionObserver.instances.length - 1];
    expect(io).toBeDefined();
    act(() => io.fire(isIntersecting));
  };

  beforeEach(() => {
    MockIntersectionObserver.instances = [];
    vi.stubGlobal('IntersectionObserver', MockIntersectionObserver);
    vi.mocked(fetchOlderTranscriptPage).mockReset();
    vi.mocked(prependServerTranscript).mockReset();
    // The real prependServerTranscript contract, re-implemented over the test
    // store: prepend + stamp the paging fields the sentinel reads back.
    vi.mocked(prependServerTranscript).mockImplementation((workspaceId: any, chatId: any, sessionId: any, messages: any[], paging: any) => {
      useStore.getState().updateTenant(workspaceId, (t: any) => {
        const th = t.threads[chatId];
        const sess = th && th.list.find((x: any) => x.id === sessionId);
        if (!sess) return t;
        sess.messages = [...messages, ...sess.messages];
        sess.eventsHasMore = paging.hasMore;
        sess.eventsOldest = paging.hasMore ? paging.oldest : undefined;
        return t;
      });
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('(i) a visible sentinel fetches the older page once and the prepended page renders', async () => {
    seedStoreSession(
      [
        { id: 'u1', author: 'you', text: 'recent question', ts: '' },
        { id: 'm1', author: 'agent', agentId: 'a1', text: 'recent reply', ts: '' },
      ],
      { hasMore: true, oldest: 'ev_60' }
    );
    let resolvePage: (p: any) => void = () => {};
    vi.mocked(fetchOlderTranscriptPage).mockImplementation(
      () => new Promise((res) => { resolvePage = res; })
    );
    const onFetchOlder = makeOnFetchOlder();
    const { container } = render(<PagingHarness onFetchOlder={onFetchOlder} loadingOlder={false} />);

    expect(container.querySelector('[data-testid="history-sentinel"]')).not.toBeNull();
    fireSentinel(true);

    expect(fetchOlderTranscriptPage).toHaveBeenCalledTimes(1);
    expect(fetchOlderTranscriptPage).toHaveBeenCalledWith('t1', 'atlas', 'sess_1', 'ev_60');

    await act(async () => {
      resolvePage({ messages: [{ id: 'old-1', author: 'you', text: 'the older message', ts: '' }], hasMore: false });
      await Promise.resolve();
      await Promise.resolve();
    });

    // The older page rendered above the recent window through the real store.
    const oldEl = container.querySelector('[data-msg-id="old-1"]') as Element;
    const u1El = container.querySelector('[data-msg-id="u1"]') as Element;
    expect(oldEl).not.toBeNull();
    expect(oldEl.compareDocumentPosition(u1El) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    const sess = storeSession();
    expect(sess.messages.map((m: any) => m.id)).toEqual(['old-1', 'u1', 'm1']);
    // A page that exhausted the log stamps hasMore=false (terminal state).
    expect(sess.eventsHasMore).toBe(false);
    expect(sess.eventsOldest).toBeUndefined();
    expect(onFetchOlder).toHaveBeenCalledTimes(1);
  });

  it('(ii) a visible sentinel expands the local window instead of fetching when messages are hidden locally', () => {
    const msgs = Array.from({ length: 85 }, (_, i) => ({ id: 'm' + i, author: 'you', text: 'msg ' + i, ts: '' }));
    seedStoreSession(msgs, { hasMore: true, oldest: 'ev_1' });
    const onFetchOlder = makeOnFetchOlder();
    const { container } = render(<PagingHarness onFetchOlder={onFetchOlder} loadingOlder={false} />);

    // The 80-message local window hides five before the expansion.
    expect(container.querySelectorAll('[data-msg-id]').length).toBe(80);
    fireSentinel(true);

    expect(fetchOlderTranscriptPage).not.toHaveBeenCalled();
    expect(onFetchOlder).not.toHaveBeenCalled();
    // Every locally held message renders after the window expanded.
    expect(container.querySelectorAll('[data-msg-id]').length).toBe(85);
  });

  it('(iii) a terminal eventsHasMore=false session issues no fetch however often the sentinel fires', () => {
    seedStoreSession(
      [{ id: 'u1', author: 'you', text: 'only page', ts: '' }],
      { hasMore: false, oldest: undefined }
    );
    const onFetchOlder = makeOnFetchOlder();
    render(<PagingHarness onFetchOlder={onFetchOlder} loadingOlder={false} />);

    for (let i = 0; i < 5; i++) fireSentinel(true);

    expect(fetchOlderTranscriptPage).not.toHaveBeenCalled();
    expect(onFetchOlder).not.toHaveBeenCalled();
  });

  it('(iv) double-triggering while a fetch is in flight issues a single call', () => {
    seedStoreSession(
      [{ id: 'u1', author: 'you', text: 'recent question', ts: '' }],
      { hasMore: true, oldest: 'ev_1' }
    );
    // Never resolves — the fetch stays in flight for the whole test.
    vi.mocked(fetchOlderTranscriptPage).mockImplementation(() => new Promise(() => {}));
    const onFetchOlder = makeOnFetchOlder();
    const { container, rerender } = render(<PagingHarness onFetchOlder={onFetchOlder} loadingOlder={false} />);

    fireSentinel(true);
    fireSentinel(true); // synchronous double fire — the route-side ref guard holds
    expect(fetchOlderTranscriptPage).toHaveBeenCalledTimes(1);

    // The view-side gate: while loadingOlder, the observer callback no-ops.
    rerender(<PagingHarness onFetchOlder={onFetchOlder} loadingOlder={true} />);
    expect(container.querySelector('[data-testid="history-loading"]')).not.toBeNull();
    fireSentinel(true);
    expect(fetchOlderTranscriptPage).toHaveBeenCalledTimes(1);
    expect(onFetchOlder).toHaveBeenCalledTimes(2);
  });

  it('(v) the reading position is preserved across the prepend (height-delta anchor)', async () => {
    seedStoreSession(
      [
        { id: 'u1', author: 'you', text: 'recent question', ts: '' },
        { id: 'm1', author: 'agent', agentId: 'a1', text: 'recent reply', ts: '' },
      ],
      { hasMore: true, oldest: 'ev_1' }
    );
    let resolvePage: (p: any) => void = () => {};
    vi.mocked(fetchOlderTranscriptPage).mockImplementation(
      () => new Promise((res) => { resolvePage = res; })
    );
    const onFetchOlder = makeOnFetchOlder();
    const { container } = render(<PagingHarness onFetchOlder={onFetchOlder} loadingOlder={false} />);

    // jsdom reports 0s — stub the scroll geometry on the scroll container.
    const list = container.querySelector('[data-od-id="message-list"]') as HTMLElement;
    let scrollHeight = 1000;
    let scrollTop = 0;
    Object.defineProperty(list, 'scrollHeight', { get: () => scrollHeight, configurable: true });
    Object.defineProperty(list, 'scrollTop', {
      get: () => scrollTop,
      set: (v: number) => { scrollTop = v; },
      configurable: true,
    });
    scrollTop = 400; // the reader is parked 400px into the log

    fireSentinel(true);
    expect(onFetchOlder).toHaveBeenCalledTimes(1);

    // The prepend grows the document by 600px before the store update lands.
    scrollHeight = 1600;
    await act(async () => {
      resolvePage({ messages: [{ id: 'old-1', author: 'you', text: 'older', ts: '' }], hasMore: false });
      await Promise.resolve();
      await Promise.resolve();
    });

    // 1600 - 1000 + 400: the same content sits under the viewport top.
    expect(scrollTop).toBe(1000);
  });

  it('(vi) the bottom auto-scroll does not force the reader down while the anchor restore lands', async () => {
    seedStoreSession(
      [
        { id: 'u1', author: 'you', text: 'recent question', ts: '' },
        { id: 'm1', author: 'agent', agentId: 'a1', text: 'recent reply', ts: '' },
      ],
      { hasMore: true, oldest: 'ev_1' }
    );
    let resolvePage: (p: any) => void = () => {};
    vi.mocked(fetchOlderTranscriptPage).mockImplementation(
      () => new Promise((res) => { resolvePage = res; })
    );
    const onFetchOlder = makeOnFetchOlder();
    const { container } = render(<PagingHarness onFetchOlder={onFetchOlder} loadingOlder={false} />);

    const list = container.querySelector('[data-od-id="message-list"]') as HTMLElement;
    let scrollHeight = 1000;
    let scrollTop = 0;
    Object.defineProperty(list, 'scrollHeight', { get: () => scrollHeight, configurable: true });
    Object.defineProperty(list, 'scrollTop', {
      get: () => scrollTop,
      set: (v: number) => { scrollTop = v; },
      configurable: true,
    });
    scrollTop = 400;

    fireSentinel(true);
    // The prepend grows thread.length — the jump-to-bottom effect runs for
    // that commit and must skip on the pending anchor, leaving the restore to
    // win instead of flinging the reader to scrollTop = scrollHeight.
    scrollHeight = 1600;
    await act(async () => {
      resolvePage({ messages: [{ id: 'old-1', author: 'you', text: 'older', ts: '' }], hasMore: false });
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(scrollTop).not.toBe(1600); // NOT forced to the bottom
    expect(scrollTop).toBe(1000);    // the anchor restore won
  });
});
