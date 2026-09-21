/**
 * @vitest-environment jsdom
 */
// Composer-side context meter suites (adopt-assistant-ui-elements, tasks
// 3.3–3.5). The meter moved out of the chat header: the ring + breakdown
// popover live in the composer's LEFT control rail. Migrated from
// ChatHeader.test.tsx and extended for the honest-remainder popover (D2/D7).
import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { render, fireEvent, waitFor, cleanup } from '@testing-library/react';
import { ContextRing } from './ContextRing';
import { Composer } from './Composer';
import { useStore } from '../../store';
import { formatTokens } from '../../lib/helpers';

// This environment's jsdom exposes no localStorage (same mode behind the
// ~66 pre-existing failures); install a minimal stub so these suites run.
if (typeof (globalThis as any).localStorage === 'undefined') {
  const backing = new Map<string, string>();
  (globalThis as any).localStorage = {
    getItem: (k: string) => (backing.has(k) ? backing.get(k)! : null),
    setItem: (k: string, v: string) => void backing.set(k, String(v)),
    removeItem: (k: string) => void backing.delete(k),
    clear: () => void backing.clear(),
    key: (i: number) => Array.from(backing.keys())[i] ?? null,
    get length() { return backing.size; },
  };
}

const agent = {
  id: 'a1', name: 'Atlas', slug: 'atlas',
  effective_context_window: 200_000,
  summarization_trigger_tokens: 150_000,
  prompt: 'x'.repeat(400), // 100 tokens estimated
  role: '',
  tools: ['a', 'b', 'c'], // 360
  skills: ['s1'], // 40
};

// 8 + 100 per entry → conversation estimate 216 (attachments are the Files
// segment, never double-counted into Conversation)
const messages = [
  { id: 'm1', author: 'you', ts: '', text: 'q'.repeat(400) },
  { id: 'm2', author: 'agent', ts: '', text: 'a'.repeat(400), attachments: [{ mime: 'application/pdf', size: 400 }] },
];
// Face-value estimate sum: 100 + 360 + 40 + 216 + 100 (400-byte file) = 816
const ESTIMATE_SUM = 816;

const seed = (opts: {
  usage?: any;
  messages?: any[];
  agent?: any;
  chatId?: string;
}) => {
  const chatId = opts.chatId || 'a1';
  useStore.setState({
    pos: { tenantId: 't1', view: 'chats', chatId, showContext: false },
    db: {
      t1: {
        id: 't1',
        threads: {
          [chatId]: {
            active: 's1',
            list: [{ id: 's1', title: 'One', updated: '', messages: opts.messages || [], ...(opts.usage ? { usage: opts.usage } : {}) }],
          },
        },
      },
    },
  } as any);
};

// This environment's jsdom also exposes no window.matchMedia, so the popover's
// used figure would run its 400ms rAF count-up and assertions would race the
// animation under parallel CPU load (the intermittent openPopover flake).
// Report prefers-reduced-motion: reduce so ContextRing renders the settled
// target directly — the live animation path in the browser is untouched, and
// nothing in this file asserts on the ticker itself.
const realMatchMedia = window.matchMedia;
const mediaStub = (query: string) => ({
  matches: query === '(prefers-reduced-motion: reduce)',
  media: query,
  onchange: null,
  addListener: () => {},
  removeListener: () => {},
  addEventListener: () => {},
  removeEventListener: () => {},
  dispatchEvent: () => false,
});

const renderRing = (over: any = {}) => {
  const utils = render(<ContextRing agent={over.agent ?? agent} enabled={over.enabled ?? true}/>);
  return {
    ring: () => utils.container.querySelector('[data-od-id="context-meter"]'),
    ringFill: () => utils.container.querySelector('[data-od-id="context-ring-fill"]'),
    ringPct: () => utils.container.querySelector('[data-od-id="context-meter-pct"]'),
    details: () => utils.container.querySelector('[data-od-id="context-meter-details"]'),
    detailsUsed: () => utils.container.querySelector('[data-od-id="context-meter-used"]'),
    caption: () => utils.container.querySelector('[data-od-id="context-caption"]'),
    serverRow: () => utils.container.querySelector('[data-od-id="context-server-row"]') as HTMLElement | null,
    headroomRow: () => utils.container.querySelector('[data-od-id="context-headroom-row"]'),
    turnInput: () => utils.container.querySelector('[data-od-id="context-turn-input"]'),
    turnOutput: () => utils.container.querySelector('[data-od-id="context-turn-output"]'),
    triggerTick: () => utils.container.querySelector('[data-od-id="context-meter-trigger-tick"]'),
    ...utils,
  };
};

const openPopover = async (h: ReturnType<typeof renderRing>, expectedUsed = '68k') => {
  fireEvent.click(h.ring() as Element);
  await waitFor(() => expect(h.details()).not.toBeNull());
  // With the reduced-motion stub above the used figure renders the target
  // directly; the wait stays as a no-cost guard for the settled total.
  await waitFor(() => expect(h.detailsUsed()?.textContent).toBe(expectedUsed + ' / 200k'), { timeout: 5_000 });
  return h;
};

beforeEach(() => {
  localStorage.clear();
  window.matchMedia = mediaStub as any;
});

afterEach(() => {
  cleanup();
  window.matchMedia = realMatchMedia;
});

describe('ContextRing — fills per turn with severity tiers (migrated from ChatHeader)', () => {
  it('shows 34% in the accent tone at 68,000 of a 200,000 window', () => {
    seed({ usage: { finalInput: 68_000, at: '2:34 PM' }, messages });
    const h = renderRing();
    expect(h.ringPct()?.textContent).toBe('34%');
    expect(h.ringFill()?.getAttribute('class')).toContain('stroke-accent');
    expect(h.ringPct()?.getAttribute('class')).toContain('text-accent');
    expect(h.ring()?.getAttribute('title')).toBe('68k / 200k');
  });

  it('shows 70% in amber at 140,000', () => {
    seed({ usage: { finalInput: 140_000, at: '' }, messages });
    const h = renderRing();
    expect(h.ringPct()?.textContent).toBe('70%');
    expect(h.ringFill()?.getAttribute('class')).toContain('stroke-warn');
    expect(h.ringPct()?.getAttribute('class')).toContain('text-[color-mix(in_oklab,var(--warn),black_38%)]');
  });

  it('shows 90% in the danger tone at 180,000', () => {
    seed({ usage: { finalInput: 180_000, at: '' }, messages });
    const h = renderRing();
    expect(h.ringPct()?.textContent).toBe('90%');
    expect(h.ringFill()?.getAttribute('class')).toContain('stroke-danger');
    expect(h.ringPct()?.getAttribute('class')).toContain('text-danger');
  });

  it('keeps one decimal below 10% so short turns stay legible at the default window', () => {
    // 8,412 / 200,000 rounds to 4% — the value the user reported as "static".
    seed({ usage: { finalInput: 8_412, at: '' }, messages });
    const h = renderRing();
    expect(h.ringPct()?.textContent).toBe('4.2%');
  });

  it('the ring fills clockwise: the dash offset tracks the used share', () => {
    seed({ usage: { finalInput: 68_000, at: '' }, messages });
    const h = renderRing();
    const c = 2 * Math.PI * 7;
    expect(Number(h.ringFill()?.getAttribute('stroke-dasharray'))).toBeCloseTo(c, 5);
    expect(Number(h.ringFill()?.getAttribute('stroke-dashoffset'))).toBeCloseTo(c * (1 - 0.34), 5);
  });

  it('carries no summarization-trigger tick or marking, even past the trigger (D1)', () => {
    // 160,000 is BOTH past the 150,000 trigger and inside the amber tier —
    // the ring must render the plain ladder, nothing trigger-specific.
    seed({ usage: { finalInput: 160_000, at: '' }, messages });
    const h = renderRing();
    expect(h.ringPct()?.textContent).toBe('80%');
    expect(h.triggerTick()).toBeNull();
    expect(h.container.querySelector('[data-od-id="context-bar"] [data-od-id="context-meter-trigger-tick"]')).toBeNull();
    expect(h.container.textContent).not.toContain('Summarizes');
    expect(h.container.textContent).not.toContain('trigger');
  });
});

describe('ContextRing — hidden outside agent chats and without usage', () => {
  it('is hidden when enabled=false (channel surface), even with usage present', () => {
    seed({ usage: { finalInput: 68_000, at: '' }, messages });
    const h = renderRing({ enabled: false });
    expect(h.ring()).toBeNull();
    expect(h.container.textContent).toBe('');
  });

  it('is hidden without usage — no ring, no 0%', () => {
    seed({ messages });
    const h = renderRing();
    expect(h.ring()).toBeNull();
    expect(h.container.textContent).not.toContain('%');
  });

  it('is hidden for a zero finalInput and for an unknown window — never a fabricated value', () => {
    seed({ usage: { finalInput: 0, at: '' }, messages });
    expect(renderRing().ring()).toBeNull();

    seed({ usage: { finalInput: 68_000, at: '' }, messages });
    expect(renderRing({ agent: { ...agent, effective_context_window: undefined } }).ring()).toBeNull();
  });

  it('renders no ring from the channel composer (mentionOptions set, no slash commands)', () => {
    seed({ usage: { finalInput: 68_000, at: '' }, messages });
    const utils = render(
      <Composer agent={agent} running={false} onSend={() => {}} onCancel={() => {}}
        mentionOptions={[{ id: 'p1', kind: 'person', name: 'Pat' }]}/>
    );
    expect(utils.container.querySelector('[data-od-id="context-meter"]')).toBeNull();
  });

  it('renders no ring from a direct member conversation (no agent)', () => {
    seed({ usage: { finalInput: 68_000, at: '' }, messages });
    const utils = render(
      <Composer agent={null} running={false} onSend={() => {}} onCancel={() => {}}/>
    );
    expect(utils.container.querySelector('[data-od-id="context-meter"]')).toBeNull();
  });
});

describe('ContextRing — restores from the thread on reload', () => {
  it('shows the last turn against the window with no interaction and no new turn', () => {
    seed({ usage: { finalInput: 68_000, at: '2:34 PM' }, messages });
    const h = renderRing();
    expect(h.ringPct()?.textContent).toBe('34%');
    expect(h.ring()?.getAttribute('title')).toBe('68k / 200k');
  });
});

describe('ContextRing — breakdown popover opens upward (migrated + extended)', () => {
  it('opens above the rail on click with used/window counts, bar, legend, Headroom, and caption', async () => {
    seed({ usage: { finalInput: 68_000, input: 68_000, output: 1_200, at: '2:34 PM' }, messages });
    const h = renderRing();
    expect(h.details()).toBeNull();

    await openPopover(h);
    expect(h.ring()?.getAttribute('aria-expanded')).toBe('true');
    // Exact reported counts, 68k / 200k.
    expect(h.detailsUsed()?.textContent).toBe('68k / 200k');
    // Face-value legend at the documented estimate: 816 of the real 68,000.
    const legend = h.details()!.textContent!;
    expect(legend).toContain('Instructions');
    expect(legend).toContain('Tools & skills');
    expect(legend).toContain('Files');
    expect(legend).toContain('Conversation');
    expect(legend).toContain('· estimated');
    // Server context remainder: the real total minus the face-value estimate.
    expect(h.serverRow()?.textContent).toContain(formatTokens(68_000 - ESTIMATE_SUM));
    expect(h.headroomRow()?.textContent).toContain('132k');
    // Turn rows render from real usage.
    expect(h.turnInput()?.textContent).toContain('68k');
    expect(h.turnOutput()?.textContent).toContain('1.2k');
    expect(h.caption()?.textContent).toBe('34% of the context window');
  });

  it('the segmented bar fills the used envelope: estimates + server remainder', async () => {
    seed({ usage: { finalInput: 68_000, at: '' }, messages });
    const h = renderRing();
    await openPopover(h);
    const bar = h.container.querySelector('[data-od-id="context-bar"]')!;
    const slices = Array.from(bar.children) as HTMLElement[];
    // Four estimated segments + the server remainder slice.
    expect(slices).toHaveLength(5);
    const widths = slices.map((s) => parseFloat(s.style.width));
    expect(widths.reduce((a, b) => a + b, 0)).toBeCloseTo(100, 5);
    // Slices are face-value proportions of used (100/68,000 ≈ 0.15% first).
    expect(widths[0]).toBeCloseTo((100 / 68_000) * 100, 3);
    expect(slices[slices.length - 1].getAttribute('data-od-id')).toBe('context-bar-server');
  });

  it('the Server context row explains the unmeasured share on hover', async () => {
    seed({ usage: { finalInput: 68_000, at: '' }, messages });
    const h = renderRing();
    await openPopover(h);
    const title = h.serverRow()?.getAttribute('title') || '';
    expect(title).toContain('retrieved memory');
    expect(title).toContain('persona docs');
    expect(title).toContain('tool schemas');
    expect(title).toContain('compaction summaries');
  });

  it('omits the turn input/output rows when the wire did not report them', async () => {
    seed({ usage: { finalInput: 68_000, at: '' }, messages });
    const h = renderRing();
    await openPopover(h);
    expect(h.turnInput()).toBeNull();
    expect(h.turnOutput()).toBeNull();
    expect(h.details()!.textContent).toContain('34% of the context window');
  });

  it('Escape, outside pointerdown, and a second click close the popover', async () => {
    seed({ usage: { finalInput: 68_000, at: '' }, messages });
    const h = renderRing();

    fireEvent.click(h.ring() as Element);
    await waitFor(() => expect(h.details()).not.toBeNull());
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(h.details()).toBeNull();

    fireEvent.click(h.ring() as Element);
    await waitFor(() => expect(h.details()).not.toBeNull());
    fireEvent.pointerDown(document.body);
    expect(h.details()).toBeNull();

    fireEvent.click(h.ring() as Element);
    await waitFor(() => expect(h.details()).not.toBeNull());
    fireEvent.click(h.ring() as Element);
    expect(h.details()).toBeNull();
  });
});

describe('ContextRing — server-provided breakdown wins when present (D7)', () => {
  const withFiles = [{ id: 'm1', author: 'you', ts: '', text: '', attachments: [{ mime: 'image/png', size: 1 }] }];

  it('replaces the client estimates with the server numbers and drops their estimated caption', async () => {
    seed({
      usage: {
        finalInput: 50_000, input: 50_000, output: 900, at: '',
        contextBreakdown: { instructions: 1_200, tools: 3_400, conversation: 12_000 },
      },
      messages: withFiles,
    });
    const h = renderRing();
    await openPopover(h, '50k');

    const row = (label: string) =>
      Array.from(h.details()!.querySelectorAll('[data-od-id^="context-segment-"]'))
        .find((el) => el.textContent!.startsWith(label))!;

    expect(row('Instructions').textContent).toContain('1.2k');
    expect(row('Instructions').textContent).not.toContain('estimated');
    expect(row('Tools & skills').textContent).toContain('3.4k');
    expect(row('Tools & skills').textContent).not.toContain('estimated');
    expect(row('Conversation').textContent).toContain('12k');
    expect(row('Conversation').textContent).not.toContain('estimated');
    // No server number for files → the client estimate stays, still estimated.
    expect(row('Files').textContent).toContain('1.1k');
    expect(row('Files').textContent).toContain('estimated');
    // Remainder against the server numbers: 50,000 − (1,200 + 3,400 + 12,000 + 1,100).
    // 50,000 − (1,200 + 3,400 + 12,000 + 1,100 files) = 32,300.
    expect(h.serverRow()?.textContent).toContain('32.3k');
  });

  it('falls back to the fully-estimated client breakdown today (no wire field)', async () => {
    seed({ usage: { finalInput: 68_000, at: '' }, messages });
    const h = renderRing();
    await openPopover(h);
    const estimated = h.details()!.textContent!;
    expect(estimated).toContain('· estimated');
    expect(h.serverRow()?.textContent).toContain(formatTokens(68_000 - ESTIMATE_SUM));
  });
});

describe('ContextRing — conversation segment respects compaction', () => {
  it('counts only the transcript entries after the latest compaction divider', async () => {
    seed({
      usage: { finalInput: 40_000, at: '' },
      messages: [
        { id: 'm0', author: 'you', ts: '', text: 'q'.repeat(4_000) }, // pre-compaction bulk
        { id: 'm1', author: 'compaction', ts: '', text: '' },
        { id: 'm2', author: 'agent', ts: '', text: 'a'.repeat(200) }, // 8 + 50 = 58
      ],
    });
    const h = renderRing();
    await openPopover(h, '40k');
    const row = Array.from(h.details()!.querySelectorAll('[data-od-id^="context-segment-"]'))
      .find((el) => el.textContent!.startsWith('Conversation'))!;
    expect(row.textContent).toContain('58');
  });
});
