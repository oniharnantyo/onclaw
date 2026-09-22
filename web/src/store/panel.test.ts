/**
 * @vitest-environment jsdom
 */
// Panel store slice tests (add-right-panel 1.1/1.4): one test per slice rule —
// dedup-open focuses, closing the last tab closes the panel, chat switches
// reset, the badge only comes from a panel-able tool finishing while closed,
// opening clears it, and tool-finish mutations NEVER open the panel or touch
// tabs. Candidate matchers are fakes registered through the real registry.
import { describe, it, expect, beforeEach } from 'vitest';
import { useStore } from './index';
import { registerPanelCandidate, resetPanelRegistries } from '../lib/panel/registry';
import { seedDb } from '../data/seed';

/** Points the store at one chat with one active session. */
const setChat = (chatId: string, messages: any[] = []) => {
  useStore.setState({
    db: {
      ...useStore.getState().db,
      t1: {
        ...(useStore.getState().db as any).t1,
        id: 't1',
        threads: { [chatId]: { active: 's1', list: [{ id: 's1', title: 'S', updated: '', messages }] } },
      },
    },
    pos: { tenantId: 't1', view: 'chats', chatId, showContext: false, railExpanded: false },
  } as any);
};

beforeEach(() => {
  resetPanelRegistries();
  useStore.setState({
    db: { ...seedDb(), t1: (seedDb() as any).acme },
    pos: { tenantId: 't1', view: 'chats', chatId: '', showContext: false, railExpanded: false },
    ui: { ...(useStore.getState().ui as any), running: false },
    panel: { open: false, tabs: [], activeId: null, badge: false },
    messageQueue: {},
  } as any);
});

describe('store panel slice — open/dedup (add-right-panel D1)', () => {
  it('openPanelTab opens the panel, appends the tab, and focuses it', () => {
    useStore.getState().openPanelTab({ kind: 'file', title: 'brief.md', payload: { path: 'docs/brief.md' } });
    const p = useStore.getState().panel;
    expect(p.open).toBe(true);
    expect(p.tabs).toHaveLength(1);
    expect(p.tabs[0]).toMatchObject({ kind: 'file', title: 'brief.md', payload: { path: 'docs/brief.md' } });
    expect(p.activeId).toBe(p.tabs[0].id);
  });

  it('opening an artifact whose dedup key already has a tab FOCUSES it — no duplicate', () => {
    useStore.getState().openPanelTab({ kind: 'file', title: 'brief.md', payload: { path: 'docs/brief.md' } });
    useStore.getState().openPanelTab({ kind: 'file', title: 'other.md', payload: { path: 'docs/other.md' } });
    useStore.getState().focusPanelTab(useStore.getState().panel.tabs[1].id);
    // Same artifact again (same kind + payload identity): focuses tab 0.
    useStore.getState().openPanelTab({ kind: 'file', title: 'brief.md — renamed title', payload: { path: 'docs/brief.md' } });
    const p = useStore.getState().panel;
    expect(p.tabs).toHaveLength(2);
    expect(p.activeId).toBe(p.tabs[0].id);
    expect(p.open).toBe(true);
  });

  it('different payload or kind means a second tab', () => {
    useStore.getState().openPanelTab({ kind: 'file', title: 'a', payload: { path: 'a.md' } });
    useStore.getState().openPanelTab({ kind: 'file', title: 'b', payload: { path: 'b.md' } });
    useStore.getState().openPanelTab({ kind: 'browser', title: 'c', payload: { path: 'a.md' } });
    expect(useStore.getState().panel.tabs).toHaveLength(3);
  });
});

describe('store panel slice — closing (add-right-panel D1)', () => {
  it('closing the LAST tab closes the panel', () => {
    useStore.getState().openPanelTab({ kind: 'file', title: 'a', payload: { path: 'a.md' } });
    const id = useStore.getState().panel.tabs[0].id;
    useStore.getState().closePanelTab(id);
    const p = useStore.getState().panel;
    expect(p.open).toBe(false);
    expect(p.tabs).toHaveLength(0);
    expect(p.activeId).toBeNull();
  });

  it('closing one of several keeps the panel open and re-focuses a remaining tab', () => {
    useStore.getState().openPanelTab({ kind: 'file', title: 'a', payload: { path: 'a.md' } });
    useStore.getState().openPanelTab({ kind: 'file', title: 'b', payload: { path: 'b.md' } });
    const [t0, t1] = useStore.getState().panel.tabs;
    useStore.getState().closePanelTab(t1.id); // close the ACTIVE tab
    const p = useStore.getState().panel;
    expect(p.open).toBe(true);
    expect(p.tabs.map((t) => t.id)).toEqual([t0.id]);
    expect(p.activeId).toBe(t0.id);
  });

  it('closing an unknown id is a no-op', () => {
    useStore.getState().openPanelTab({ kind: 'file', title: 'a', payload: { path: 'a.md' } });
    useStore.getState().closePanelTab('nope');
    expect(useStore.getState().panel.tabs).toHaveLength(1);
  });
});

describe('store panel slice — chat switch reset (add-right-panel D1)', () => {
  it('switching chats via goPos resets tabs — nothing carries over', () => {
    setChat('chat-a');
    useStore.getState().openPanelTab({ kind: 'file', title: 'a', payload: { path: 'a.md' } });
    expect(useStore.getState().panel.tabs).toHaveLength(1);
    useStore.getState().goPos({ chatId: 'chat-b' });
    const p = useStore.getState().panel;
    expect(p.open).toBe(false);
    expect(p.tabs).toHaveLength(0);
    expect(p.activeId).toBeNull();
    expect(p.badge).toBe(false);
  });

  it('a goPos without a chat change does NOT reset the panel', () => {
    useStore.getState().openPanelTab({ kind: 'file', title: 'a', payload: { path: 'a.md' } });
    useStore.getState().goPos({ view: 'chats' });
    expect(useStore.getState().panel.tabs).toHaveLength(1);
  });
});

describe('store panel slice — badge rules (add-right-panel 1.4)', () => {
  beforeEach(() => {
    // Payload identity derives from the card's result, so two different files
    // are two different artifacts (the dedup key must distinguish them).
    registerPanelCandidate((card) => {
      if (card.name !== 'document.create' || typeof card.res !== 'string') return null;
      let name = '';
      try { name = String(JSON.parse(card.res || '{}').name || ''); } catch { /* torn result */ }
      return { kind: 'file', title: name, payload: { path: 'docs/' + name } };
    });
    setChat('chat-a');
  });

  const finishTool = (card: any) => {
    // One store write carrying the newly finished card — exactly what the
    // livechat fold does through updateTenant.
    setChat('chat-a', [{ id: 'm1', author: 'agent', tools: [card] }]);
  };

  it('a panel-able tool finishing while closed sets the dot badge', () => {
    useStore.setState({ ui: { ...(useStore.getState().ui as any), running: true } } as any);
    finishTool({ callId: 'c1', name: 'document.create', args: '{}', res: '{"name":"brief.md"}', ms: 5 });
    expect(useStore.getState().panel.badge).toBe(true);
  });

  it('the panel NEVER opens itself from a tool event — open stays false, tabs stay empty', () => {
    useStore.setState({ ui: { ...(useStore.getState().ui as any), running: true } } as any);
    finishTool({ callId: 'c1', name: 'document.create', args: '{}', res: '{"name":"brief.md"}', ms: 5 });
    finishTool({ callId: 'c2', name: 'document.create', args: '{}', res: '{"name":"other.md"}', ms: 5 });
    const p = useStore.getState().panel;
    expect(p.open).toBe(false);
    expect(p.tabs).toHaveLength(0);
    expect(p.activeId).toBeNull();
  });

  it('opening the panel clears the badge', () => {
    useStore.setState({ ui: { ...(useStore.getState().ui as any), running: true } } as any);
    finishTool({ callId: 'c1', name: 'document.create', args: '{}', res: '{"name":"brief.md"}', ms: 5 });
    expect(useStore.getState().panel.badge).toBe(true);
    useStore.getState().setPanelOpen(true);
    expect(useStore.getState().panel.badge).toBe(false);
    expect(useStore.getState().panel.open).toBe(true);
  });

  it('no badge when the panel is already open, and none from a tool with no candidate', () => {
    useStore.setState({ ui: { ...(useStore.getState().ui as any), running: true } } as any);
    useStore.getState().setPanelOpen(true);
    finishTool({ callId: 'c1', name: 'document.create', args: '{}', res: '{"name":"brief.md"}', ms: 5 });
    expect(useStore.getState().panel.badge).toBe(false);

    useStore.getState().setPanelOpen(false);
    finishTool({ callId: 'c2', name: 'execute', args: '{}', res: 'ok', ms: 1 });
    expect(useStore.getState().panel.badge).toBe(false);
  });

  it('a mid-stream card (no result yet) does not badge', () => {
    useStore.setState({ ui: { ...(useStore.getState().ui as any), running: true } } as any);
    finishTool({ callId: 'c1', name: 'document.create', args: '{}', ms: 0 });
    expect(useStore.getState().panel.badge).toBe(false);
  });

  it('history already in the transcript does not badge — only NEW finishes do', () => {
    // Arrive at a chat that already holds a panel-able finished card (idle —
    // hydration path): re-baseline, no badge.
    setChat('chat-a', [{ id: 'm1', author: 'agent', tools: [{ callId: 'c1', name: 'document.create', args: '{}', res: '{"name":"brief.md"}', ms: 5 }] }]);
    expect(useStore.getState().panel.badge).toBe(false);
    // A later genuinely new finish while closed and running badges.
    useStore.setState({ ui: { ...(useStore.getState().ui as any), running: true } } as any);
    setChat('chat-a', [
      { id: 'm1', author: 'agent', tools: [{ callId: 'c1', name: 'document.create', args: '{}', res: '{"name":"brief.md"}', ms: 5 }] },
      { id: 'm2', author: 'agent', tools: [{ callId: 'c2', name: 'document.create', args: '{}', res: '{"name":"next.md"}', ms: 5 }] },
    ]);
    expect(useStore.getState().panel.badge).toBe(true);
  });
});

describe('store panel slice — members tab on channel entry (add-right-panel 1.5)', () => {
  // Channels are server-only (never seeded) — inject one row, the same shape
  // loadChannels stores.
  const dbWithChannel = () => {
    const db: any = seedDb();
    db.acme.channels = [{ id: 'ch-ops', slug: 'ops', name: 'ops', purpose: 'Ops', members: [], unread: 0 }];
    return db;
  };

  it('selectChat on a channel opens the members tab; agent chats get none', () => {
    useStore.setState({
      db: dbWithChannel(),
      pos: { tenantId: 'acme', view: 'chats', chatId: '', showContext: false, railExpanded: false },
    } as any);
    useStore.getState().selectChat('ch-ops');
    let p = useStore.getState().panel;
    expect(p.open).toBe(true);
    expect(p.tabs).toHaveLength(1);
    expect(p.tabs[0].kind).toBe('members');

    useStore.getState().selectChat('a-atlas');
    p = useStore.getState().panel;
    // Chat switch reset the channel's tabs and opened no members tab.
    expect(p.open).toBe(false);
    expect(p.tabs).toHaveLength(0);
  });

  it('re-selecting the same channel re-focuses the members tab without duplicating it', () => {
    useStore.setState({
      db: dbWithChannel(),
      pos: { tenantId: 'acme', view: 'chats', chatId: '', showContext: false, railExpanded: false },
    } as any);
    useStore.getState().selectChat('ch-ops');
    useStore.getState().closePanelTab(useStore.getState().panel.tabs[0].id);
    useStore.getState().selectChat('ch-ops');
    const p = useStore.getState().panel;
    expect(p.open).toBe(true);
    expect(p.tabs.filter((t) => t.kind === 'members')).toHaveLength(1);
  });
});
