/**
 * @vitest-environment jsdom
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent } from '@testing-library/react';
import { ChatView } from './ChatView';
import { useStore } from '../../store';

vi.mock('../../lib/api', () => ({
  api: {
    onUnauthorized: vi.fn(),
    skills: { list: vi.fn().mockResolvedValue({ skills: [] }) },
    tools: { list: vi.fn().mockResolvedValue({ tools: [] }) },
    agents: { listSkills: vi.fn().mockResolvedValue({ skills: [] }) },
  },
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
  onToggleMembers: vi.fn(), onConfigure: vi.fn(), onSend: vi.fn(),
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
