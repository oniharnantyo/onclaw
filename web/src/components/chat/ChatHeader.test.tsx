/**
 * @vitest-environment jsdom
 */
import { describe, it, expect, vi } from 'vitest';
import { act, fireEvent, render } from '@testing-library/react';
import { ChatHeader } from './ChatHeader';
import { useStore } from '../../store';
import { seedDb } from '../../data/seed';

// This environment's jsdom exposes no localStorage (same mode behind the ~66
// pre-existing failures); install a minimal stub so this suite runs — the
// header now mounts the store-connected session-todos surface.
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
  id: 'a1', name: 'Atlas', slug: 'atlas', status: 'idle', model: 'gpt-5',
  lastActive: '2m ago',
  effective_context_window: 200000,
  summarization_trigger_tokens: 150000,
  // Denylist form (refactor-agent-tools-denylist): the todos-chip presence
  // gate reads it — this default denies todo_write; the chip test overrides.
  disabled_tools: ['todo_write'],
};

const agentTarget = { kind: 'agent', obj: { id: 'a1', name: 'Atlas' } };
const channelTarget = { kind: 'channel', obj: { id: 'ch-ops', name: 'ops', purpose: 'Ops coordination' } };

// The context meter itself moved to the composer's left rail
// (adopt-assistant-ui-elements D1) — its suites live in ContextRing.test.tsx.
// This file pins the header's remaining contract: no meter, no agent-configure
// button (add-right-panel replaced it with the panel toggle), the toggle's
// dot badge, and the members stack as a members-tab opener.

function renderHeader(props: any) {
  return render(<ChatHeader channelMembers={[]} onOpenMembers={() => {}} panelOpen={false} panelBadge={false} onTogglePanel={() => {}} {...props}/>);
}

describe('components/chat/ChatHeader — no context meter (adopt-assistant-ui-elements)', () => {
  it('renders no meter for an agent chat even with usage present', () => {
    const utils = renderHeader({
      target: agentTarget, agent,
      usage: { finalInput: 68000, at: '2:34 PM' },
    });
    expect(utils.container.querySelector('[data-od-id="context-meter"]')).toBeNull();
    expect(utils.container.querySelector('[data-od-id="context-meter-details"]')).toBeNull();
    expect(utils.container.textContent).not.toContain('%');
  });
});

describe('components/chat/ChatHeader — panel toggle replaces configure (add-right-panel 1.4)', () => {
  it('an agent chat header has the panel toggle and NO configure button', () => {
    const onTogglePanel = vi.fn();
    const utils = renderHeader({ target: agentTarget, agent, onTogglePanel });
    expect(utils.container.querySelector('[data-od-id="btn-configure-agent"]')).toBeNull();
    const toggle = utils.container.querySelector('[data-od-id="btn-panel-toggle"]') as HTMLButtonElement;
    expect(toggle).not.toBeNull();
    fireEvent.click(toggle);
    expect(onTogglePanel).toHaveBeenCalledTimes(1);
  });

  it('the toggle shows in every chat kind and reflects the open state', () => {
    const utils = renderHeader({ target: agentTarget, agent, panelOpen: true });
    const toggle = utils.container.querySelector('[data-od-id="btn-panel-toggle"]') as HTMLButtonElement;
    expect(toggle.getAttribute('aria-pressed')).toBe('true');
    expect(toggle.getAttribute('title')).toBe('Hide panel');
  });

  it('the dot badge renders only while a badge is pending and the panel is closed', () => {
    const pending = renderHeader({ target: agentTarget, agent, panelBadge: true, panelOpen: false });
    expect(pending.container.querySelector('[data-od-id="panel-badge"]')).not.toBeNull();
    // Open panel: nothing pending to reveal.
    const open = renderHeader({ target: agentTarget, agent, panelBadge: true, panelOpen: true });
    expect(open.container.querySelector('[data-od-id="panel-badge"]')).toBeNull();
    // No badge pending.
    const clear = renderHeader({ target: agentTarget, agent, panelBadge: false, panelOpen: false });
    expect(clear.container.querySelector('[data-od-id="panel-badge"]')).toBeNull();
  });
});

describe('components/chat/ChatHeader — members stack opens the members tab (add-right-panel 1.5)', () => {
  const members = [
    { id: 'a1', kind: 'agent', name: 'Atlas' },
    { id: 'p1', kind: 'person', name: 'Alice', presence: 'online' },
  ];

  it('clicking the avatar stack opens the members tab (no more onToggleMembers)', () => {
    const onOpenMembers = vi.fn();
    const utils = renderHeader({ target: channelTarget, agent: null, channelMembers: members, onOpenMembers });
    const stack = utils.container.querySelector('[data-od-id="btn-channel-members"]') as HTMLButtonElement;
    expect(stack).not.toBeNull();
    expect(stack.getAttribute('title')).toBe('Open members panel');
    fireEvent.click(stack);
    expect(onOpenMembers).toHaveBeenCalledTimes(1);
  });

  it('no members stack without members (agent chats never offer one)', () => {
    const utils = renderHeader({ target: agentTarget, agent, channelMembers: [] });
    expect(utils.container.querySelector('[data-od-id="btn-channel-members"]')).toBeNull();
  });
});

describe('components/chat/ChatHeader — documents toggle (rework-document-chat-surfaces 2.1, 2026-09-28 user pivot)', () => {
  it('an agent chat with a documents lens renders the toggle beside the panel toggle; click toggles', () => {
    const onToggleDocuments = vi.fn();
    const utils = renderHeader({
      target: agentTarget, agent, documentsAvailable: true, documentsOpen: false, onToggleDocuments,
    });
    const docs = utils.container.querySelector('[data-testid="btn-panel-documents"]') as HTMLButtonElement;
    expect(docs).not.toBeNull();
    // Beside the panel toggle: immediately before it in the action cluster.
    const toggle = utils.container.querySelector('[data-od-id="btn-panel-toggle"]')!;
    expect(toggle.compareDocumentPosition(docs) & Node.DOCUMENT_POSITION_PRECEDING).toBeTruthy();
    expect(docs.getAttribute('aria-pressed')).toBe('false');
    expect(docs.getAttribute('title')).toBe('Show documents');
    fireEvent.click(docs);
    expect(onToggleDocuments).toHaveBeenCalledTimes(1);
  });

  it('pressed state and title reflect an open documents listing', () => {
    const utils = renderHeader({
      target: agentTarget, agent, documentsAvailable: true, documentsOpen: true, onToggleDocuments: () => {},
    });
    const docs = utils.container.querySelector('[data-testid="btn-panel-documents"]') as HTMLButtonElement;
    expect(docs.getAttribute('aria-pressed')).toBe('true');
    expect(docs.getAttribute('title')).toBe('Hide documents');
  });

  it('no documents toggle without a documents lens, and never in channel chats without one', () => {
    const noLens = renderHeader({ target: agentTarget, agent });
    expect(noLens.container.querySelector('[data-testid="btn-panel-documents"]')).toBeNull();
    const channel = renderHeader({ target: channelTarget, agent: null });
    expect(channel.container.querySelector('[data-testid="btn-panel-documents"]')).toBeNull();
  });
});

describe('components/chat/ChatHeader — Langfuse link (integrate-langfuse-tracing 4.1)', () => {
  const lfUrl = (utils: ReturnType<typeof renderHeader>) =>
    utils.container.querySelector('a[data-od-id="btn-open-langfuse"]') as HTMLAnchorElement | null;

  it('offers "Open in Langfuse" when the opened run carries a langfuse_url', () => {
    const utils = renderHeader({ target: agentTarget, agent, langfuseUrl: 'https://langfuse.acme.example.com/trace/tr-123' });
    const link = lfUrl(utils);
    expect(link).not.toBeNull();
    expect(link!.getAttribute('href')).toBe('https://langfuse.acme.example.com/trace/tr-123');
    expect(link!.getAttribute('target')).toBe('_blank');
    expect(link!.getAttribute('rel')).toBe('noopener');
    expect(link!.getAttribute('aria-label')).toBe('Open in Langfuse');
  });

  it('renders no Langfuse action when the run has none (absent / null / empty)', () => {
    for (const langfuseUrl of [undefined, null, '']) {
      const utils = renderHeader({ target: agentTarget, agent, langfuseUrl });
      expect(lfUrl(utils)).toBeNull();
      expect(utils.container.textContent).not.toContain('Langfuse');
    }
  });
});

describe('components/chat/ChatHeader — session todos chip (add-session-todos-surface D2/D6)', () => {
  // Seeding idiom from lib/sessionTodos.test.ts: the active session of the
  // open chat carries a todo_write call the surface's hook derives the plan
  // from. Store state persists across tests in this file, but the surface is
  // present-only — every earlier test's fixtures stay DOM-neutral (see the
  // gate assertions below).
  const seedTodoPlan = () => {
    useStore.setState({
      db: {
        acme: {
          ...seedDb().acme,
          threads: {
            'a-atlas': {
              active: 's1',
              list: [{
                id: 's1', title: 'Chat', updated: '',
                messages: [{
                  id: 'm1', author: 'agent', agentId: 'a-atlas', ts: '9:00 AM', text: '',
                  tools: [{
                    callId: 'call-1', name: 'todo_write',
                    args: JSON.stringify({ items: [
                      { key: 'k1', text: 'Read the logs', status: 'done' },
                      { key: 'k2', text: 'Patch the service', status: 'pending' },
                    ], revision: 1 }),
                    res: JSON.stringify({ ok: true }),
                  }],
                }],
              }],
            },
          },
        },
      },
      pos: { tenantId: 'acme', view: 'chats', chatId: 'a-atlas', showContext: false },
    } as any);
  };

  it('an agent chat with a seeded plan renders the todos chip as the first header action', () => {
    seedTodoPlan();
    const utils = renderHeader({
      target: { kind: 'agent', obj: { id: 'a-atlas', name: 'Atlas' } },
      agent: { ...agent, disabled_tools: [] },
    });
    const chip = utils.container.querySelector('[data-od-id="todos-chip"]') as HTMLElement;
    expect(chip).not.toBeNull();
    expect(chip.textContent).toContain('1/2');
    // Leftmost of the action cluster: before the panel toggle in DOM order.
    const toggle = utils.container.querySelector('[data-od-id="btn-panel-toggle"]')!;
    expect(chip.compareDocumentPosition(toggle) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('an agent chat without todo_write exposure renders no chip even with a plan', () => {
    seedTodoPlan();
    const utils = renderHeader({ target: agentTarget, agent });
    expect(utils.container.querySelector('[data-od-id="todos-chip"]')).toBeNull();
  });

  it('a channel chat never renders the todos chip, even with a seeded plan', () => {
    seedTodoPlan();
    const utils = renderHeader({ target: channelTarget, agent: null });
    expect(utils.container.querySelector('[data-od-id="todos-chip"]')).toBeNull();
    expect(utils.container.querySelector('[data-od-id="todos-popover"]')).toBeNull();
  });
});

describe('components/chat/ChatHeader — running badge (agent status semantics, fix-chat-stop-on-reattached-run 4.1)', () => {
  // The header must follow the same merged signal as the sidebar session
  // indicator (instant store ui.running for the active session, server
  // session-list running flag second) — the agent-level status field flips
  // late and left the header on "Idle" while a run streamed. setState idiom
  // from screens/ChatRoute.test.tsx; every test pins the value it needs so
  // order on the shared store never matters.
  // Same-chat stamp (fix-thinking-leak-on-chat-switch): the header gates on
  // runningChatId === target.obj.id ('a1').
  const setUiRunning = (value: boolean) =>
    useStore.setState({ ui: { ...useStore.getState().ui, running: value, runningChatId: value ? 'a1' : null } });

  it('flips to "Running · last active …" while ui.running is true for the active session, even with an idle agent-level status', () => {
    setUiRunning(false);
    const utils = renderHeader({ target: agentTarget, agent, session: { id: 's1', running: false } });
    expect(utils.container.textContent).toContain('Idle · last active 2m ago');
    act(() => setUiRunning(true));
    expect(utils.container.textContent).toContain('Running · last active 2m ago');
    const dot = utils.container.querySelector('span[title="Running"]') as HTMLElement;
    expect(dot).not.toBeNull();
    expect(dot.className).toContain('od-live');
  });

  it('reads Running from the server session running flag without a local run (foreign run)', () => {
    setUiRunning(false);
    const utils = renderHeader({ target: agentTarget, agent, session: { id: 's1', running: true } });
    expect(utils.container.textContent).toContain('Running · last active 2m ago');
    const dot = utils.container.querySelector('span[title="Running"]') as HTMLElement;
    expect(dot.className).toContain('od-live');
  });

  it('returns to the agent-level Idle label and a still dot on terminal state (ui.running false + session not running)', () => {
    setUiRunning(true);
    const utils = renderHeader({ target: agentTarget, agent, session: { id: 's1', running: false } });
    expect(utils.container.textContent).toContain('Running · last active 2m ago');
    act(() => setUiRunning(false));
    expect(utils.container.textContent).toContain('Idle · last active 2m ago');
    const dot = utils.container.querySelector('span[title="Idle"]') as HTMLElement;
    expect(dot).not.toBeNull();
    expect(dot.className).not.toContain('od-live');
  });
});
