/**
 * @vitest-environment jsdom
 */
// Browser mirror source tests (add-right-panel 4.1–4.3): the candidate
// matcher, the four mirror states (idle / mirroring / frozen / closed), and
// the staleness caption math. Registration is a module-load side effect of
// importing ./BrowserMirror — this suite never resets the registries.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, cleanup } from '@testing-library/react';
import { matchPanelCandidate, renderPanelSource } from '../../../../../lib/panel/registry';
import { useStore } from '../../../../../store';
import './BrowserMirror';
import { actionsSinceScreenshot, browserCards, formatCaptureTime, stalenessCaption } from './mirror';

vi.mock('../../../../../lib/panel/filesApi', () => ({
  agentFileUrl: (ws: string, agent: string, path: string) => `files://${ws}/${agent}/${path}`,
  fetchAgentFile: vi.fn(async () => ({ bytes: new Uint8Array([1]).buffer, contentType: 'image/png' })),
  listAgentDir: vi.fn(async () => []),
}));

const card = (over: Record<string, unknown> = {}) => ({ callId: 'c1', name: 'browser.navigate', args: '{"url":"https://example.com"}', res: '{"loaded":true,"title":"Example"}', ms: 42, ts: '2026-09-22T09:04:00Z', ...over });

const shotCard = (over: Record<string, unknown> = {}) =>
  card({ name: 'browser.screenshot', args: '{}', res: '{"path":"/jail/agents/atlas/browser/screenshot-17.png"}', ms: 900, ...over });

const message = (tools: any[]) => ({ role: 'assistant', content: [], tools });

let chatSeq = 0;
function seed(opts: { running?: boolean; messages?: any[]; agentName?: string } = {}) {
  const chatId = `chat-${++chatSeq}`;
  useStore.setState((s: any) => ({
    pos: { ...s.pos, tenantId: 't1', chatId },
    ui: { ...s.ui, running: opts.running ?? false },
    db: {
      ...s.db,
      t1: {
        ...s.db.t1,
        sub: 't1',
        agents: [{ id: 'ag1', slug: 'atlas', name: opts.agentName ?? 'Atlas' }],
        channels: [],
        threads: { ...(s.db.t1?.threads || {}), [chatId]: { active: 'sess1', list: [{ id: 'sess1', title: 'Chat', updated: '', messages: opts.messages ?? [] }] } },
      },
    },
  }));
  return chatId;
}

const renderMirror = (ctx: any = {}) =>
  render(<div>{renderPanelSource('browser', { tab: { id: 'tb', kind: 'browser', title: 'Browser', payload: {}, dedupKey: 'browser::{}' }, ctx })}</div>);

beforeEach(() => {
  useStore.setState({ panel: { open: true, tabs: [], activeId: null, badge: false } });
});

afterEach(() => {
  cleanup();
  useStore.setState({ ui: { ...(useStore.getState() as any).ui, running: false } });
});

describe('panel sources/browser — candidate matcher (4.1)', () => {
  it('maps any browser.* card to a browser candidate', () => {
    expect(matchPanelCandidate(card())).toEqual({ kind: 'browser', title: 'Browser', payload: {} });
    expect(matchPanelCandidate(card({ name: 'browser.click', args: '{"ref":"#go"}' }))).toEqual({ kind: 'browser', title: 'Browser', payload: {} });
  });

  it('ignores non-browser cards and mid-stream non-cards', () => {
    expect(matchPanelCandidate(card({ name: 'files.write' }))).toBeNull();
    expect(matchPanelCandidate({})).toBeNull();
    expect(matchPanelCandidate({ name: 'browserx.act' })).toBeNull();
  });
});

describe('panel sources/browser — staleness caption math (4.3)', () => {
  it('counts 0 actions since when the screenshot is the last card', () => {
    const cards = browserCards([message([card({ name: 'browser.read', args: '{}' }), shotCard()])]);
    expect(actionsSinceScreenshot(cards)).toBe(0);
    expect(stalenessCaption(cards)).toBe(`captured ${formatCaptureTime('2026-09-22T09:04:00Z')} · up to date`);
  });

  it('counts 3 actions since when three browser cards follow the screenshot', () => {
    const cards = browserCards([message([
      shotCard({ ts: '2026-09-22T10:00:00Z' }),
      card({ name: 'browser.click', args: '{"ref":"#go"}' }),
      card({ name: 'browser.type', args: '{"ref":"#q","text":"onclaw"}' }),
      card({ name: 'browser.navigate', args: '{"url":"https://example.com/next"}' }),
    ])]);
    expect(actionsSinceScreenshot(cards)).toBe(3);
    expect(stalenessCaption(cards)).toBe(`captured ${formatCaptureTime('2026-09-22T10:00:00Z')} · 3 actions since`);
  });
});

describe('panel sources/browser — mirror states (4.2)', () => {
  it('idle: no browser cards yet explains the mirror fills when the agent browses', () => {
    seed({ messages: [] });
    const { container } = renderMirror();
    expect(container.querySelector('[data-od-id="panel-browser-idle"]')).not.toBeNull();
    expect(container.textContent).toContain('The mirror fills in here when the agent browses.');
  });

  it('mirroring: run active shows address bar, live chip, screenshot and feed', async () => {
    seed({ running: true, messages: [message([card(), shotCard(), card({ name: 'browser.click', args: '{"ref":"#go"}', res: '{"ok":true}' })])] });
    const { container } = renderMirror();
    expect(container.querySelector('[data-od-id="panel-browser-mirroring"]')).not.toBeNull();
    expect(container.querySelector('[data-od-id="panel-browser-live"]')).not.toBeNull();
    expect(container.querySelector('[data-od-id="panel-browser-address"]')?.textContent).toContain('https://example.com');
    await vi.waitFor(() => expect(container.querySelector('[data-od-id="panel-browser-image"]')).not.toBeNull());
    // The click AFTER the screenshot makes the mirror one action stale.
    expect(container.querySelector('[data-od-id="panel-browser-staleness"]')?.textContent).toBe(`captured ${formatCaptureTime('2026-09-22T09:04:00Z')} · 1 action since`);
    const feed = container.querySelector('[data-od-id="panel-browser-feed"]')!;
    expect(feed.textContent).toContain('Navigate');
    expect(feed.textContent).toContain('42ms');
  });

  it('frozen: finished run keeps the screenshot but is visibly distinguished', async () => {
    seed({ running: false, messages: [message([card(), shotCard()])] });
    const { container } = renderMirror();
    expect(container.querySelector('[data-od-id="panel-browser-frozen"]')).not.toBeNull();
    expect(container.querySelector('[data-od-id="panel-browser-frozen-note"]')?.textContent).toContain('Frozen — run finished');
    expect(container.querySelector('[data-od-id="panel-browser-live"]')).toBeNull();
    await vi.waitFor(() => expect(container.querySelector('[data-od-id="panel-browser-image"]')).not.toBeNull());
  });

  it('mid-stream card without a result still mirrors, not frozen', () => {
    seed({ running: false, messages: [message([card(), card({ name: 'browser.read', args: '{}', res: undefined })])] });
    const { container } = renderMirror();
    expect(container.querySelector('[data-od-id="panel-browser-mirroring"]')).not.toBeNull();
  });

  it('closed: a chat whose browser activity predates the active session', () => {
    const chatId = seed({ messages: [message([shotCard()])] });
    const first = renderMirror();
    expect(first.container.querySelector('[data-od-id="panel-browser-frozen"]')).not.toBeNull();
    first.unmount();
    // The active session switches to one with no browser cards — the tab is
    // now a mirror of a torn-down / superseded session.
    useStore.setState((s: any) => ({
      db: { ...s.db, t1: { ...s.db.t1, threads: { ...s.db.t1.threads, [chatId]: { active: 'sess2', list: [{ id: 'sess2', title: 'Chat', updated: '', messages: [] }, s.db.t1.threads[chatId].list[0]] } } } },
    }));
    const second = renderMirror();
    expect(second.container.querySelector('[data-od-id="panel-browser-closed"]')).not.toBeNull();
    expect(second.container.textContent).toContain('superseded');
  });

  it('header names the owning agent', () => {
    seed({ messages: [message([shotCard()])], agentName: 'Beacon' });
    const { container } = renderMirror({ primaryAgentId: 'ag1' });
    expect(container.querySelector('[data-od-id="panel-browser-header"]')?.textContent).toContain('Beacon browser');
  });

  it('missing screenshot file renders a distinct missing state, never a broken img', async () => {
    const { fetchAgentFile } = await import('../../../../../lib/panel/filesApi');
    (fetchAgentFile as any).mockResolvedValueOnce(null);
    seed({ messages: [message([shotCard()])] });
    const { container } = renderMirror();
    await vi.waitFor(() => expect(container.querySelector('[data-od-id="panel-browser-missing"]')).not.toBeNull());
    expect(container.querySelector('[data-od-id="panel-browser-image"]')).toBeNull();
  });
});
