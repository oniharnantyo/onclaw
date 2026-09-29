/**
 * @vitest-environment jsdom
 */
// Session todos surface suites (add-session-todos-surface tasks 2.1/2.2/2.3
// and 3.1): present-only chip, anchored popover, sub-640px bottom sheet, and
// the D4 auto-surface state machine (arm → first-write open → run/completion
// collapse → sticky dismissal → re-arm).
import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { render, fireEvent, cleanup, act } from '@testing-library/react';
import { SessionTodosSurface } from './SessionTodosSurface';
import { useStore } from '../../../store';
import { seedDb } from '../../../data/seed';

// This environment's jsdom exposes no localStorage (same mode behind the ~66
// pre-existing failures); install a minimal stub so this suite runs.
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

// jsdom has no matchMedia either; the surface guards on its absence, but the
// sheet gate needs a real stub. `wide=false` answers false to every query —
// only the component's (min-width: 640px) question matters here.
const realMatchMedia = window.matchMedia;
const WIDE = '(min-width: 640px)';
const mediaStub = (wide: boolean) =>
  ((query: string) => ({
    matches: wide && query === WIDE,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as any;

// Card/massaging helpers — same ground-truth shapes as lib/sessionTodos.test.ts.
const item = (key: string, status: string, reason?: string) => ({
  key, text: 'Do ' + key, status, ...(reason ? { reason } : {}),
});

const todoCall = (callId: string, items: any[], revision?: number) => ({
  callId,
  name: 'todo_write',
  args: JSON.stringify(revision !== undefined ? { items, revision } : { items }),
  res: JSON.stringify({ ok: true, items }),
});

const agentMsg = (id: string, tools: any[]) =>
  ({ id, author: 'agent', agentId: 'a-atlas', ts: '9:00 AM', text: '', tools });

const exposedAgent = { id: 'a-atlas', name: 'Atlas', disabled_tools: [] };
const plainAgent = { id: 'a-atlas', name: 'Atlas', disabled_tools: ['todo_write'] };

// Store seeding idiom from lib/sessionTodos.test.ts.
const seedThread = (list: any[], active?: string) => {
  useStore.setState({
    db: {
      acme: {
        ...seedDb().acme,
        threads: { 'a-atlas': { active: active ?? list[0]?.id ?? null, list } },
      },
    },
    pos: { tenantId: 'acme', view: 'chats', chatId: 'a-atlas', showContext: false },
  } as any);
};

const setRunning = (v: boolean) => {
  act(() => {
    useStore.setState({ ui: { ...useStore.getState().ui, running: v } });
  });
};

// Exactly what the live runtime's patchTools does: a deep-cloning updateTenant
// write pushing a new card onto the LAST agent message's tools array.
const pushTodoCall = (call: any) => {
  act(() => {
    useStore.getState().updateTenant('acme', (t: any) => {
      const th = t.threads['a-atlas'];
      const sess = th.list.find((x: any) => x.id === th.active);
      const last = sess.messages[sess.messages.length - 1];
      if (!last.tools) last.tools = [];
      last.tools.push(call);
      return t;
    });
  });
};

const renderSurface = (agent: any = exposedAgent) => {
  const utils = render(<SessionTodosSurface agent={agent}/>);
  return {
    chip: () => utils.container.querySelector('[data-od-id="todos-chip"]') as HTMLButtonElement | null,
    popover: () => utils.container.querySelector('[data-od-id="todos-popover"]'),
    sheet: () => utils.container.querySelector('[data-od-id="todos-sheet"]'),
    dismissBtn: () => utils.container.querySelector('[data-od-id="todos-dismiss"]'),
    row: (key: string) => utils.container.querySelector(`[data-od-id="todo-row-${key}"]`),
    rowText: (key: string) =>
      utils.container.querySelector(`[data-od-id="todo-row-${key}"] span.min-w-0`),
    ...utils,
  };
};

beforeEach(() => {
  localStorage.clear();
  window.matchMedia = mediaStub(true);
  setRunning(false);
});

afterEach(() => {
  cleanup();
  window.matchMedia = realMatchMedia;
});

describe('SessionTodosSurface — presence branches (task 2.1, present-only)', () => {
  it('no plan → renders nothing', () => {
    seedThread([{ id: 's1', title: 'Chat', updated: '', messages: [{ id: 'm1', author: 'you', ts: '9:00 AM', text: 'hello' }] }]);
    const h = renderSurface();
    expect(h.chip()).toBeNull();
    expect(h.container.textContent).toBe('');
  });

  it('plan but the agent does not expose todo_write → renders nothing', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [todoCall('call-1', [item('k1', 'pending')], 1)])],
    }]);
    const h = renderSurface(plainAgent);
    expect(h.chip()).toBeNull();
    expect(h.container.textContent).toBe('');
  });

  it('plan + an agent that exposes todo_write → the chip renders', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [todoCall('call-1', [item('k1', 'done'), item('k2', 'pending')], 1)])],
    }]);
    const h = renderSurface();
    expect(h.chip()).not.toBeNull();
  });
});

describe('SessionTodosSurface — chip states (D2)', () => {
  it('an active item shows the spinner span and the active item text', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [todoCall('call-1', [item('k1', 'done'), item('k2', 'active')], 2)])],
    }]);
    const h = renderSurface();
    const chip = h.chip()!;
    expect(chip.querySelector('.od-genui-spin')).not.toBeNull();
    expect(chip.textContent).toContain('Do k2');
    expect(chip.getAttribute('title')).toContain('Do k2');
  });

  it('no active item shows the done/total count in mono', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [todoCall('call-1', [
        item('k1', 'done'), item('k2', 'done'), item('k3', 'pending'),
        item('k4', 'failed', 'oom'), item('k5', 'pending'),
      ], 1)])],
    }]);
    const h = renderSurface();
    const chip = h.chip()!;
    expect(chip.textContent).toContain('2/5');
    expect(chip.querySelector('.od-genui-spin')).toBeNull();
    expect(chip.getAttribute('title')).toBe('Todos · 2 of 5 done');
  });

  it('a store rewrite flips the chip live — no remount, no auto-open while idle', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [todoCall('call-1', [item('k1', 'pending'), item('k2', 'pending')], 1)])],
    }]);
    const h = renderSurface();
    expect(h.chip()!.textContent).toContain('0/2');

    pushTodoCall(todoCall('call-2', [item('k1', 'done'), item('k2', 'active')], 2));
    const chip = h.chip()!;
    expect(chip).not.toBeNull();
    expect(chip.querySelector('.od-genui-spin')).not.toBeNull();
    expect(chip.textContent).toContain('Do k2');
    expect(chip.textContent).not.toContain('0/2');
    // Idle store: a rewrite updates the chip but never opens the popover.
    expect(h.popover()).toBeNull();
  });
});

describe('SessionTodosSurface — popover (task 2.2, D3)', () => {
  const seedFour = () => seedThread([{
    id: 's1', title: 'Chat', updated: '',
    messages: [agentMsg('m1', [todoCall('call-1', [
      item('k1', 'done'),
      item('k2', 'active'),
      item('k3', 'failed', 'Permission denied by policy'),
      item('k4', 'pending'),
    ], 4)])],
  }]);

  it('activating the chip shows every item with the transcript row states, incl. the failed reason', () => {
    seedFour();
    const h = renderSurface();
    fireEvent.click(h.chip()!);
    const panel = h.popover();
    expect(panel).not.toBeNull();
    expect(h.sheet()).toBeNull();
    expect(h.chip()!.getAttribute('aria-expanded')).toBe('true');
    // Header register: Todos · agent · rev.
    expect(panel!.textContent).toContain('Todos');
    expect(panel!.textContent).toContain('Atlas');
    expect(panel!.textContent).toContain('rev 4');
    // Every item + the failed reason.
    expect(panel!.textContent).toContain('Do k1');
    expect(panel!.textContent).toContain('Do k2');
    expect(panel!.textContent).toContain('Do k3');
    expect(panel!.textContent).toContain('Do k4');
    expect(panel!.textContent).toContain('Permission denied by policy');
    // Row styling matches the transcript states (classes live on the row's
    // text span, not the li).
    expect(h.rowText('k1')!.className).toContain('line-through');
    expect(h.rowText('k3')!.className).toContain('text-danger');
    expect(h.rowText('k4')!.className).toContain('text-muted');
    expect(h.rowText('k4')!.className).not.toContain('line-through');
    // The chip remains alongside the panel.
    expect(h.chip()).not.toBeNull();
  });

  it('the dismiss control closes the popover; the chip remains', () => {
    seedFour();
    const h = renderSurface();
    fireEvent.click(h.chip()!);
    expect(h.popover()).not.toBeNull();
    fireEvent.click(h.dismissBtn()!);
    expect(h.popover()).toBeNull();
    expect(h.chip()).not.toBeNull();
  });

  it('an outside pointerdown closes the popover', () => {
    seedFour();
    const h = renderSurface();
    fireEvent.click(h.chip()!);
    expect(h.popover()).not.toBeNull();
    fireEvent.pointerDown(document.body);
    expect(h.popover()).toBeNull();
    expect(h.chip()).not.toBeNull();
  });

  it('Escape closes the popover', () => {
    seedFour();
    const h = renderSurface();
    fireEvent.click(h.chip()!);
    expect(h.popover()).not.toBeNull();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(h.popover()).toBeNull();
  });
});

describe('SessionTodosSurface — auto-surface state machine (task 3.1, D4)', () => {
  it('(1) mid-run write lands a new callId → the popover opens with no user action', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [todoCall('call-1', [item('k1', 'pending')], 1)])],
    }]);
    const h = renderSurface();
    expect(h.popover()).toBeNull();

    setRunning(true); // ARM: snapshot, never opens by itself
    expect(h.popover()).toBeNull();

    pushTodoCall(todoCall('call-2', [item('k1', 'active'), item('k2', 'pending')], 2));
    expect(h.popover()).not.toBeNull();
    expect(h.popover()!.textContent).toContain('Do k2');
  });

  it('(2a) running flips false → the popover collapses back to the chip', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [todoCall('call-1', [item('k1', 'pending')], 1)])],
    }]);
    const h = renderSurface();
    setRunning(true);
    pushTodoCall(todoCall('call-2', [item('k1', 'active'), item('k2', 'pending')], 2));
    expect(h.popover()).not.toBeNull();

    setRunning(false);
    expect(h.popover()).toBeNull();
    expect(h.chip()).not.toBeNull();
    expect(h.chip()!.textContent).toContain('Do k1'); // chip keeps tracking (k1 is the active item)
  });

  it('(2b) mount mid-run opens nothing; the plan reaching all-done collapses the auto-open', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [todoCall('call-1', [item('k1', 'pending'), item('k2', 'active')], 1)])],
    }]);
    setRunning(true); // reload mid-run: hydrated history is not a live event
    const h = renderSurface();
    expect(h.popover()).toBeNull();

    pushTodoCall(todoCall('call-2', [item('k1', 'done'), item('k2', 'done')], 2));
    expect(h.popover()).toBeNull(); // auto-opened, then completion-collapse
    expect(h.chip()).not.toBeNull();
    expect(h.chip()!.textContent).toContain('2/2');
  });

  it('(3) user dismissal sticks: a same-run rewrite updates the chip but never reopens', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [todoCall('call-1', [item('k1', 'pending')], 1)])],
    }]);
    const h = renderSurface();
    setRunning(true);
    pushTodoCall(todoCall('call-2', [item('k1', 'active'), item('k2', 'pending')], 2));
    expect(h.popover()).not.toBeNull();

    fireEvent.click(h.dismissBtn()!);
    expect(h.popover()).toBeNull();

    pushTodoCall(todoCall('call-3', [item('k1', 'done'), item('k2', 'done'), item('k3', 'pending')], 3));
    expect(h.popover()).toBeNull();
    expect(h.chip()!.textContent).toContain('2/3');
  });

  it('(4) the next run re-arms: dismissal is cleared and a new write auto-opens again', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [todoCall('call-1', [item('k1', 'pending')], 1)])],
    }]);
    const h = renderSurface();
    setRunning(true);
    pushTodoCall(todoCall('call-2', [item('k1', 'active')], 2));
    expect(h.popover()).not.toBeNull();
    fireEvent.click(h.dismissBtn()!);
    expect(h.popover()).toBeNull();

    setRunning(false); // run 1 ends
    setRunning(true);  // run 2 arms: dismissedRef cleared
    expect(h.popover()).toBeNull();

    pushTodoCall(todoCall('call-3', [item('k3', 'active'), item('k4', 'pending')], 3));
    expect(h.popover()).not.toBeNull();
    expect(h.popover()!.textContent).toContain('Do k3');
  });
});

describe('SessionTodosSurface — bottom sheet below the floor (task 2.3, D3)', () => {
  it('with matchMedia below 640px, the chip opens the fixed bottom sheet, not the popover', () => {
    window.matchMedia = mediaStub(false);
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [todoCall('call-1', [item('k1', 'active'), item('k2', 'pending')], 1)])],
    }]);
    const h = renderSurface();
    fireEvent.click(h.chip()!);
    expect(h.sheet()).not.toBeNull();
    expect(h.popover()).toBeNull();
    // Same content contract as the popover.
    expect(h.sheet()!.textContent).toContain('Do k1');
    expect(h.sheet()!.textContent).toContain('Do k2');
    // Dismiss closes the sheet; the chip is unchanged.
    fireEvent.click(h.dismissBtn()!);
    expect(h.sheet()).toBeNull();
    expect(h.chip()).not.toBeNull();
  });

  it('with matchMedia at/above 640px, the chip opens the anchored popover, not the sheet', () => {
    window.matchMedia = mediaStub(true);
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [todoCall('call-1', [item('k1', 'pending')], 1)])],
    }]);
    const h = renderSurface();
    fireEvent.click(h.chip()!);
    expect(h.popover()).not.toBeNull();
    expect(h.sheet()).toBeNull();
  });
});
