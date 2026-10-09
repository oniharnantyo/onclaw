/**
 * @vitest-environment jsdom
 */
// Floating run card suites (add-subagent-runs-surface tasks 5.1/5.2): the
// present-only card over the transcript (todos lane / tasks lane / neither),
// the Progress section contract (header count, four row states), the Agents
// section (running rows with the D5 display join, Ended collapse, created as
// ended-style), the surface machine (presence-driven auto-surface, sticky
// dismissal, run re-arm, chat-switch reset), the sub-640px sheet, the stream
// subscription rule, and the section→panel openPanelTab payloads.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, fireEvent, cleanup, act } from '@testing-library/react';
import { SessionTodosSurface } from './SessionTodosSurface';
import { useStore } from '../../../store';
import { seedDb } from '../../../data/seed';
import { useTaskStream } from '../../../lib/tasks';

// Partial mock: the store hooks stay real; the stream consumer is a spy so
// the subscription rule (visible-and-open only, released on dismissal) is
// directly assertable.
vi.mock('../../../lib/tasks', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../../lib/tasks')>();
  return { ...actual, useTaskStream: vi.fn() };
});

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

const delegationCall = (description: string, subagentType: string, res?: string) => ({
  callId: 'call-' + description,
  name: 'agent',
  args: JSON.stringify({ subagent_type: subagentType, description, prompt: 'do it' }),
  ...(res ? { res } : {}),
});

const shellCall = (description: string, command: string, res?: string) => ({
  callId: 'call-' + description,
  name: 'background_shell',
  args: JSON.stringify({ description, command }),
  ...(res ? { res } : {}),
});

const exposedAgent = { id: 'a-atlas', name: 'Atlas', disabled_tools: [] };
const plainAgent = { id: 'a-atlas', name: 'Atlas', disabled_tools: ['todo_write'] };

const taskEntry = (over: Record<string, any> = {}) => ({
  taskId: 't1', kind: 'delegation', status: 'running', description: 'research task', ...over,
});

// Store seeding idiom from lib/sessionTodos.test.ts. The tasks slice resets
// with the thread — the store persists across tests in this file, and a stale
// tasks lane would leak presence into every later fixture.
const seedThread = (list: any[], active?: string) => {
  act(() => {
    useStore.setState({
      db: {
        acme: {
          ...seedDb().acme,
          threads: { 'a-atlas': { active: active ?? list[0]?.id ?? null, list } },
        },
      },
      pos: { tenantId: 'acme', view: 'chats', chatId: 'a-atlas', showContext: false },
      tasks: {},
    } as any);
  });
};

const seedTasks = (entries: any[]) => {
  act(() => {
    useStore.setState({
      tasks: { 'a-atlas': { entries, records: {}, sequences: {} } },
    } as any);
  });
};

const setRunning = (v: boolean) => {
  act(() => {
    // Same-chat stamp (fix-thinking-leak-on-chat-switch): the surface's run
    // selector gates on runningChatId === pos.chatId ('a-atlas' here).
    useStore.setState({ ui: { ...useStore.getState().ui, running: v, runningChatId: v ? 'a-atlas' : null } });
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

const plainThread = () =>
  seedThread([{ id: 's1', title: 'Chat', updated: '', messages: [{ id: 'm1', author: 'you', ts: '9:00 AM', text: 'hello' }] }]);

const renderCard = (agent: any = exposedAgent) => {
  const utils = render(<SessionTodosSurface agent={agent}/>);
  return {
    card: () => utils.container.querySelector('[data-od-id="run-card"]'),
    sheet: () => utils.container.querySelector('[data-od-id="run-sheet"]'),
    dismissBtn: () => utils.container.querySelector('[data-od-id="todos-dismiss"]') as HTMLButtonElement | null,
    section: () => utils.container.querySelector('[data-od-id="tasks-section"]') as HTMLButtonElement | null,
    endedRow: () => utils.container.querySelector('[data-od-id="tasks-ended"]') as HTMLButtonElement | null,
    row: (id: string) => utils.container.querySelector(`[data-od-id="task-row-${id}"]`) as HTMLButtonElement | null,
    todoRow: (key: string) => utils.container.querySelector(`[data-od-id="todo-row-${key}"]`),
    todoRowText: (key: string) =>
      utils.container.querySelector(`[data-od-id="todo-row-${key}"] span.min-w-0`),
    ...utils,
  };
};

// The real panel action, captured once — payload tests swap in a spy and
// afterEach restores it so the shared store never keeps a stub.
const realOpenPanelTab = useStore.getState().openPanelTab;

beforeEach(() => {
  vi.mocked(useTaskStream).mockClear();
  localStorage.clear();
  window.matchMedia = mediaStub(true);
  setRunning(false);
});

afterEach(() => {
  cleanup();
  useStore.setState({ openPanelTab: realOpenPanelTab } as any);
  window.matchMedia = realMatchMedia;
});

describe('FloatingRunCard — presence branches (task 5.1, present-only)', () => {
  it('no plan and no task activity → renders nothing anywhere', () => {
    plainThread();
    const h = renderCard();
    expect(h.card()).toBeNull();
    expect(h.sheet()).toBeNull();
    expect(h.container.textContent).toBe('');
  });

  it('todos-only: a plan + an agent exposing todo_write renders the card without the Agents section', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [todoCall('call-1', [item('k1', 'done'), item('k2', 'pending')], 1)])],
    }]);
    const h = renderCard();
    expect(h.card()).not.toBeNull();
    expect(h.section()).toBeNull();
    expect(h.endedRow()).toBeNull();
  });

  it('plan but the agent does not expose todo_write (and no tasks) → renders nothing', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [todoCall('call-1', [item('k1', 'pending')], 1)])],
    }]);
    const h = renderCard(plainAgent);
    expect(h.container.textContent).toBe('');
  });

  it('tasks-only: task entries alone render the card with the Agents section and no todo rows', () => {
    plainThread();
    seedTasks([taskEntry({ taskId: 't1' })]);
    const h = renderCard(plainAgent);
    expect(h.card()).not.toBeNull();
    expect(h.section()).not.toBeNull();
    expect(h.todoRow('k1')).toBeNull();
  });

  it('an unresulted agent call alone (no entries yet) already renders the card', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [delegationCall('research task', 'researcher')])],
    }]);
    const h = renderCard();
    expect(h.card()).not.toBeNull();
    // The section waits for task rows (entries) — presence is call-derived,
    // rows are store-derived, so the subscribe lag renders a header-only card.
    expect(h.section()).toBeNull();
  });

  it('a resulted agent call alone is history, not presence — nothing renders', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [delegationCall('research task', 'researcher', '{"ok":true}')])],
    }]);
    const h = renderCard();
    expect(h.container.textContent).toBe('');
  });
});

describe('FloatingRunCard — Progress section (task 5.1, agent-todos as modified)', () => {
  it('the card header carries the done/total count, the agent name, and the revision', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [todoCall('call-1', [
        item('k1', 'done'), item('k2', 'done'), item('k3', 'pending'),
        item('k4', 'failed', 'oom'), item('k5', 'pending'),
      ], 7)])],
    }]);
    const h = renderCard();
    expect(h.card()!.textContent).toContain('2/5');
    expect(h.card()!.textContent).toContain('Atlas');
    expect(h.card()!.textContent).toContain('rev 7');
  });

  it('the four row states match the transcript cards, incl. the failed reason', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [todoCall('call-1', [
        item('k1', 'done'),
        item('k2', 'active'),
        item('k3', 'failed', 'Permission denied by policy'),
        item('k4', 'pending'),
      ], 4)])],
    }]);
    const h = renderCard();
    expect(h.todoRowText('k1')!.className).toContain('line-through');
    expect(h.card()!.querySelector('.od-genui-spin')).not.toBeNull(); // active row spinner
    expect(h.todoRowText('k3')!.className).toContain('text-danger');
    expect(h.card()!.textContent).toContain('Permission denied by policy');
    expect(h.todoRowText('k4')!.className).toContain('text-muted');
    expect(h.todoRowText('k4')!.className).not.toContain('line-through');
  });

  it('a store rewrite updates rows and count in place — no remount', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [todoCall('call-1', [item('k1', 'pending'), item('k2', 'pending')], 1)])],
    }]);
    const h = renderCard();
    expect(h.card()!.textContent).toContain('0/2');
    const before = h.card();
    pushTodoCall(todoCall('call-2', [item('k1', 'done'), item('k2', 'active')], 2));
    expect(h.card()).toBe(before);
    expect(h.card()!.textContent).toContain('1/2');
    expect(h.card()!.textContent).toContain('Do k2');
  });

  it('the header marker spins only while the chat is running', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [todoCall('call-1', [item('k1', 'pending')], 1)])],
    }]);
    const h = renderCard();
    expect(h.card()!.querySelector('.od-genui-spin')).toBeNull();
    setRunning(true);
    expect(h.card()!.querySelector('.od-genui-spin')).not.toBeNull();
  });
});

describe('FloatingRunCard — Agents section (task 5.1, running rows + Ended collapse)', () => {
  it('running rows carry a spinner, the subagent name from the joined call, and the description', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [
        delegationCall('research task', 'researcher'),
        delegationCall('summary sweep', 'scout'),
        shellCall('run the build', 'npm run build\n--verbose'),
      ])],
    }]);
    seedTasks([
      taskEntry({ taskId: 't1', kind: 'delegation', status: 'running', description: 'research task' }),
      taskEntry({ taskId: 't2', kind: 'delegation', status: 'running', description: 'summary sweep' }),
      taskEntry({ taskId: 't3', kind: 'shell', status: 'running', description: 'run the build' }),
    ]);
    const h = renderCard();
    expect(h.row('t1')!.textContent).toContain('researcher');
    expect(h.row('t1')!.textContent).toContain('research task');
    expect(h.row('t1')!.querySelector('.od-genui-spin')).not.toBeNull();
    expect(h.row('t2')!.textContent).toContain('scout');
    // Shell rows excerpt the joined call's command (first line only).
    expect(h.row('t3')!.textContent).toContain('npm run build');
    expect(h.row('t3')!.textContent).not.toContain('--verbose');
    expect(h.endedRow()).toBeNull();
  });

  it('a delegation without a matching call falls back to the kind label Agent', () => {
    plainThread();
    seedTasks([taskEntry({ taskId: 't1', description: 'unjoinable description' })]);
    const h = renderCard();
    expect(h.row('t1')!.textContent).toContain('Agent');
  });

  it('ended tasks collapse into one closing row: check-circle, Ended, count opposite, chevron — inside the section', () => {
    plainThread();
    seedTasks([
      taskEntry({ taskId: 't1', status: 'running' }),
      taskEntry({ taskId: 't2', status: 'completed', summary: 'done work' }),
      taskEntry({ taskId: 't3', status: 'failed' }),
      taskEntry({ taskId: 't4', status: 'canceled' }),
    ]);
    const h = renderCard();
    const ended = h.endedRow()!;
    expect(ended).not.toBeNull();
    // Closing row beneath the running rows, inside the Agents section.
    expect(ended.compareDocumentPosition(h.row('t1')!) & Node.DOCUMENT_POSITION_PRECEDING).toBeTruthy();
    expect(ended.textContent).toContain('Ended');
    expect(ended.textContent).toContain('3');
    expect(ended.className).toContain('w-full'); // a full-width section row, not a sibling section
    // Ended tasks render no rows of their own.
    expect(h.container.textContent).not.toContain('done work');
  });

  it('created (restart-lost) entries render ended-style — never as running rows', () => {
    plainThread();
    seedTasks([
      taskEntry({ taskId: 't1', status: 'created' }),
      taskEntry({ taskId: 't2', status: 'running' }),
    ]);
    const h = renderCard();
    expect(h.row('t1')).toBeNull();
    expect(h.row('t2')).not.toBeNull();
    expect(h.endedRow()!.textContent).toContain('1');
  });
});

describe('FloatingRunCard — surface machine (task 5.1: auto-surface, sticky dismissal, resets)', () => {
  it('the first todo write of a run auto-surfaces the card with no user action', () => {
    plainThread();
    const h = renderCard();
    expect(h.card()).toBeNull();
    setRunning(true); // arm — never opens by itself
    expect(h.card()).toBeNull();
    pushTodoCall(todoCall('call-2', [item('k1', 'active'), item('k2', 'pending')], 2));
    expect(h.card()).not.toBeNull();
  });

  it('first task activity (task entries landing mid-run) auto-surfaces the card too', () => {
    plainThread();
    const h = renderCard();
    setRunning(true);
    expect(h.card()).toBeNull();
    seedTasks([taskEntry({ taskId: 't1', status: 'running', description: 'research task' })]);
    expect(h.card()).not.toBeNull();
    expect(h.section()).not.toBeNull();
  });

  it('sticky dismissal: dismissing keeps the card closed while the run keeps producing activity', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [todoCall('call-1', [item('k1', 'pending')], 1)])],
    }]);
    seedTasks([taskEntry({ taskId: 't1', status: 'running' })]);
    const h = renderCard();
    expect(h.card()).not.toBeNull();
    fireEvent.click(h.dismissBtn()!);
    expect(h.card()).toBeNull();
    // Later writes and task activity update the lanes but never re-surface.
    pushTodoCall(todoCall('call-3', [item('k1', 'done'), item('k3', 'pending')], 3));
    seedTasks([
      taskEntry({ taskId: 't1', status: 'completed' }),
      taskEntry({ taskId: 't2', status: 'running' }),
    ]);
    expect(h.card()).toBeNull();
    expect(h.container.textContent).toBe('');
  });

  it('the next run re-arms: the card returns after a run boundary', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [todoCall('call-1', [item('k1', 'pending')], 1)])],
    }]);
    const h = renderCard();
    expect(h.card()).not.toBeNull();
    fireEvent.click(h.dismissBtn()!);
    expect(h.card()).toBeNull();

    setRunning(false); // run 1 ends
    setRunning(true);  // run 2 arms: dismissal cleared
    expect(h.card()).not.toBeNull();
  });

  it('a chat switch resets the surface: dismissal does not follow across chats', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [todoCall('call-1', [item('k1', 'pending')], 1)])],
    }]);
    const h = renderCard();
    expect(h.card()).not.toBeNull();
    fireEvent.click(h.dismissBtn()!);
    expect(h.card()).toBeNull();

    act(() => { useStore.getState().goPos({ chatId: 'other' }); });
    expect(h.card()).toBeNull(); // the other chat has no lanes

    act(() => { useStore.getState().goPos({ chatId: 'a-atlas' }); });
    expect(h.card()).not.toBeNull(); // back: reset and present again
  });

  it('Escape dismisses the card', () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [todoCall('call-1', [item('k1', 'pending')], 1)])],
    }]);
    const h = renderCard();
    expect(h.card()).not.toBeNull();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(h.card()).toBeNull();
  });
});

describe('FloatingRunCard — stream subscription rule (chat-runtime spec)', () => {
  it('the stream consumer runs only while the card is visible; dismissal releases it', () => {
    const enabledLog = () =>
      vi.mocked(useTaskStream).mock.calls.map((c) => c[1]);
    plainThread();
    const h = renderCard();
    expect(enabledLog().at(-1)).toBe(false); // absent → not a consumer

    seedTasks([taskEntry({ taskId: 't1', status: 'running' })]);
    expect(enabledLog().at(-1)).toBe(true);  // visible → subscribed

    fireEvent.click(h.dismissBtn()!);
    expect(enabledLog().at(-1)).toBe(false); // sticky-dismissed = closed = released
  });

  it('the consumer is addressed to the open chat', () => {
    plainThread();
    renderCard();
    seedTasks([taskEntry({ taskId: 't1', status: 'running' })]);
    const [chatId, enabled] = vi.mocked(useTaskStream).mock.calls.at(-1)!;
    expect(chatId).toBe('a-atlas');
    expect(enabled).toBe(true);
  });
});

describe('FloatingRunCard — section→panel activation (task 5.2, openPanelTab payloads)', () => {
  const seedTwoRunning = () => {
    seedThread([{
      id: 's1', title: 'Chat', updated: '',
      messages: [agentMsg('m1', [
        delegationCall('research task', 'researcher'),
        delegationCall('summary sweep', 'scout'),
      ])],
    }]);
    seedTasks([
      taskEntry({ taskId: 't1', status: 'running', description: 'research task' }),
      taskEntry({ taskId: 't2', status: 'running', description: 'summary sweep' }),
      taskEntry({ taskId: 't9', status: 'completed' }),
    ]);
  };

  it('activating the Agents section header opens the Subagents list view (no taskId)', () => {
    seedTwoRunning();
    const openSpy = vi.fn();
    useStore.setState({ openPanelTab: openSpy } as any);
    const h = renderCard();
    fireEvent.click(h.section()!);
    expect(openSpy).toHaveBeenCalledTimes(1);
    expect(openSpy).toHaveBeenCalledWith({
      kind: 'subagents', title: 'Subagents', payload: { chatId: 'a-atlas' },
    });
  });

  it('activating a joinable running row passes its taskId for panel pre-selection', () => {
    seedTwoRunning();
    const openSpy = vi.fn();
    useStore.setState({ openPanelTab: openSpy } as any);
    const h = renderCard();
    fireEvent.click(h.row('t1')!);
    expect(openSpy).toHaveBeenCalledWith({
      kind: 'subagents', title: 'Subagents', payload: { chatId: 'a-atlas', taskId: 't1' },
    });
    fireEvent.click(h.row('t2')!);
    expect(openSpy).toHaveBeenLastCalledWith({
      kind: 'subagents', title: 'Subagents', payload: { chatId: 'a-atlas', taskId: 't2' },
    });
  });

  it('the Ended row opens the list view (no taskId)', () => {
    seedTwoRunning();
    const openSpy = vi.fn();
    useStore.setState({ openPanelTab: openSpy } as any);
    const h = renderCard();
    fireEvent.click(h.endedRow()!);
    expect(openSpy).toHaveBeenCalledWith({
      kind: 'subagents', title: 'Subagents', payload: { chatId: 'a-atlas' },
    });
  });

  it('a row without a joinable task id opens the list view', () => {
    plainThread();
    seedTasks([taskEntry({ taskId: '', status: 'running', description: 'orphan' })]);
    const openSpy = vi.fn();
    useStore.setState({ openPanelTab: openSpy } as any);
    const h = renderCard();
    fireEvent.click(h.row('')!);
    expect(openSpy).toHaveBeenCalledWith({
      kind: 'subagents', title: 'Subagents', payload: { chatId: 'a-atlas' },
    });
  });
});

describe('FloatingRunCard — bottom sheet below the floor (task 5.1, D6)', () => {
  const seedPlan = () => seedThread([{
    id: 's1', title: 'Chat', updated: '',
    messages: [agentMsg('m1', [todoCall('call-1', [item('k1', 'active'), item('k2', 'pending')], 2)])],
  }]);

  it('with matchMedia below 640px the card renders as a fixed full-height sheet, not the anchored card', () => {
    window.matchMedia = mediaStub(false);
    seedPlan();
    seedTasks([taskEntry({ taskId: 't1', status: 'running' })]);
    const h = renderCard();
    expect(h.sheet()).not.toBeNull();
    expect(h.card()).toBeNull();
    // Same content contract as the anchored card.
    expect(h.sheet()!.textContent).toContain('Do k1');
    expect(h.sheet()!.textContent).toContain('Do k2');
    expect(h.sheet()!.textContent).toContain('Agents');
    expect(h.row('t1')).not.toBeNull();
    // Dismiss closes the sheet, sticky as ever.
    fireEvent.click(h.dismissBtn()!);
    expect(h.sheet()).toBeNull();
  });

  it('with matchMedia at/above 640px the anchored card renders, not the sheet', () => {
    window.matchMedia = mediaStub(true);
    seedPlan();
    const h = renderCard();
    expect(h.card()).not.toBeNull();
    expect(h.sheet()).toBeNull();
  });
});
