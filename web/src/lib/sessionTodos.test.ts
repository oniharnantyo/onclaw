// Session todos data lane (add-session-todos-surface tasks 1.1/1.2): the
// newest todo_write wins across the session's loaded messages, a cleared list
// clears the surface (no leak-through from older calls), and the store hook
// live-updates on the runtime's patchTools-style deep-clone writes.
import { describe, it, expect, beforeEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import {
  latestTodoCall,
  selectSessionTodos,
  hasOpenItems,
  agentExposesTodoWrite,
  useSessionTodoPlan,
} from './sessionTodos';
import { useStore } from '../store';
import { seedDb } from '../data/seed';

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

// Card/massaging helpers — the ground-truth shapes from lib/toolDisplay.ts:
// `args`/`res` are JSON TEXT on agent-turn tool cards.
const todoCall = (callId: string, items: any[], revision?: number) => ({
  callId,
  name: 'todo_write',
  args: JSON.stringify(revision !== undefined ? { items, revision } : { items }),
  res: JSON.stringify({ ok: true, items }),
});

const lsCall = { name: 'ls', args: JSON.stringify({ path: '.' }), res: JSON.stringify([]) };

const agentMsg = (id: string, tools: any[]) =>
  ({ id, author: 'agent', agentId: 'a-atlas', ts: '9:00 AM', text: '', tools });

const item = (key: string, status: string) => ({ key, text: 'Do ' + key, status });

describe('latestTodoCall / selectSessionTodos (task 1.1)', () => {
  it('parses a single todo_write call into items + revision', () => {
    const messages = [
      { id: 'm1', author: 'you', ts: '9:00 AM', text: 'plan my work' },
      agentMsg('m2', [todoCall('call-1', [item('k1', 'done'), item('k2', 'pending')], 3)]),
    ];
    const call = latestTodoCall(messages);
    expect(call).not.toBeNull();
    expect(call!.callId).toBe('call-1');
    expect(call!.plan).not.toBeNull();
    expect(call!.plan!.revision).toBe(3);
    expect(call!.plan!.items.map((i) => [i.key, i.status])).toEqual([['k1', 'done'], ['k2', 'pending']]);
    expect(selectSessionTodos(messages)?.revision).toBe(3);
  });

  it('cross-turn rewrite: the LAST agent message carrying todo_write wins', () => {
    const messages = [
      agentMsg('m1', [todoCall('call-1', [item('k1', 'pending')], 1)]),
      { id: 'm2', author: 'you', ts: '9:01 AM', text: 'go on' },
      agentMsg('m3', [todoCall('call-2', [item('k1', 'done'), item('k2', 'active')], 2)]),
    ];
    const call = latestTodoCall(messages);
    expect(call!.callId).toBe('call-2');
    expect(call!.plan!.revision).toBe(2);
    expect(selectSessionTodos(messages)!.items.map((i) => i.status)).toEqual(['done', 'active']);
  });

  it('same-turn rewrite: the LAST todo_write card within one message wins', () => {
    const messages = [
      agentMsg('m1', [
        todoCall('call-1', [item('k1', 'pending')], 1),
        lsCall,
        todoCall('call-2', [item('k1', 'active')], 2),
      ]),
    ];
    const call = latestTodoCall(messages);
    expect(call!.callId).toBe('call-2');
    expect(call!.plan!.revision).toBe(2);
    expect(call!.plan!.items[0].status).toBe('active');
  });

  it('a cleared list (empty items) yields plan null — an older plan never leaks', () => {
    const messages = [
      agentMsg('m1', [todoCall('call-1', [item('k1', 'pending')], 1)]),
      agentMsg('m2', [todoCall('call-2', [], 2)]),
    ];
    const call = latestTodoCall(messages);
    expect(call!.callId).toBe('call-2');
    expect(call!.plan).toBeNull();
    expect(selectSessionTodos(messages)).toBeNull();
  });

  it('an unparsable newest call (mid-stream empty args) keeps the callId but plan null', () => {
    const messages = [
      agentMsg('m1', [todoCall('call-1', [item('k1', 'pending')], 1)]),
      agentMsg('m2', [{ callId: 'call-2', name: 'todo_write', args: '', res: '' }]),
    ];
    const call = latestTodoCall(messages);
    expect(call!.callId).toBe('call-2');
    expect(call!.plan).toBeNull();
    expect(selectSessionTodos(messages)).toBeNull();
  });

  it('no todo_write anywhere, undefined/null messages, or tool-less messages → null', () => {
    expect(latestTodoCall(undefined)).toBeNull();
    expect(latestTodoCall(null)).toBeNull();
    expect(latestTodoCall([])).toBeNull();
    expect(selectSessionTodos(undefined)).toBeNull();

    // Non-agent messages without tools are skipped, not a crash.
    const toolless = [
      { id: 'm1', author: 'you', ts: '9:00 AM', text: 'hi' },
      agentMsg('m2', [lsCall]),
      { id: 'm3', author: 'agent', agentId: 'a-atlas', ts: '9:01 AM', text: 'done, no cards' },
    ];
    expect(latestTodoCall(toolless)).toBeNull();
  });
});

describe('hasOpenItems (auto-collapse predicate)', () => {
  it('truth table: pending/active are open; done/failed are not', () => {
    expect(hasOpenItems([item('k1', 'pending') as any])).toBe(true);
    expect(hasOpenItems([item('k1', 'active') as any])).toBe(true);
    expect(hasOpenItems([item('k1', 'done') as any])).toBe(false);
    expect(hasOpenItems([item('k1', 'failed') as any])).toBe(false);
    expect(hasOpenItems([item('k1', 'done') as any, item('k2', 'pending') as any])).toBe(true);
    expect(hasOpenItems([])).toBe(false);
  });
});

describe('agentExposesTodoWrite (present-only gate)', () => {
  it('true unless the agent denylist names todo_write', () => {
    expect(agentExposesTodoWrite({ disabled_tools: ['ls', 'grep'] })).toBe(true);
    expect(agentExposesTodoWrite({ disabled_tools: [] })).toBe(true);
    expect(agentExposesTodoWrite({})).toBe(true);
    expect(agentExposesTodoWrite({ disabled_tools: ['todo_write'] })).toBe(false);
    expect(agentExposesTodoWrite(null)).toBe(false);
    expect(agentExposesTodoWrite(undefined)).toBe(false);
  });
});

describe('useSessionTodoPlan — live store updates (task 1.2)', () => {
  // Store seeding idiom from store/sessions.test.ts.
  const seedThread = (list: any[], active?: string) => {
    useStore.setState({
      db: {
        acme: {
          ...seedDb().acme,
          threads: { 'a-atlas': { active: active ?? list[0]?.id ?? null, list } },
        },
      },
      pos: { tenantId: 'acme', view: 'chats', chatId: 'a-atlas', showContext: false },
    });
  };

  beforeEach(() => {
    localStorage.clear();
  });

  it('live-updates when the runtime pushes a new todo_write card — no reseed, no remount', async () => {
    seedThread([{
      id: 's1',
      title: 'Chat',
      updated: '',
      messages: [
        { id: 'm1', author: 'you', ts: '9:00 AM', text: 'plan my work' },
        agentMsg('m2', [todoCall('call-1', [item('k1', 'active'), item('k2', 'pending')], 1)]),
      ],
    }]);
    const { result } = renderHook(() => useSessionTodoPlan());
    expect(result.current.callId).toBe('call-1');
    expect(result.current.plan!.revision).toBe(1);

    // Exactly what the live runtime's patchTools does (chat/runtime.tsx): a
    // deep-cloning updateTenant write pushing a new card onto the LAST agent
    // message's tools array.
    await act(async () => {
      useStore.getState().updateTenant('acme', (t: any) => {
        const th = t.threads['a-atlas'];
        const sess = th.list.find((x: any) => x.id === th.active);
        const last = sess.messages[sess.messages.length - 1];
        if (!last.tools) last.tools = [];
        last.tools.push(todoCall('call-2', [item('k1', 'done'), item('k2', 'active')], 2));
        return t;
      });
    });
    expect(result.current.callId).toBe('call-2');
    expect(result.current.plan!.revision).toBe(2);
    expect(result.current.plan!.items.map((i) => i.status)).toEqual(['done', 'active']);
  });

  it('returns the stable no-call constant and keeps it across unrelated store writes', async () => {
    seedThread([{ id: 's1', title: 'Chat', updated: '', messages: [{ id: 'm1', author: 'you', ts: '9:00 AM', text: 'hello' }] }]);
    const { result } = renderHook(() => useSessionTodoPlan());
    expect(result.current).toEqual({ plan: null, callId: null });
    const first = result.current;
    // zustand v5 referential-stability contract: a notification that does not
    // change the derivation must not mint a fresh object (EMPTY_QUEUE rule).
    await act(async () => { useStore.setState({ search: 'x' }); });
    expect(result.current).toBe(first);
  });

  it('reads the ACTIVE session of the current chat, not the first in the list', () => {
    seedThread(
      [
        { id: 's1', title: 'One', updated: '', messages: [agentMsg('m1', [todoCall('call-old', [item('k1', 'done')], 1)])] },
        { id: 's2', title: 'Two', updated: '', messages: [agentMsg('m2', [todoCall('call-new', [item('k1', 'pending')], 2)])] },
      ],
      's2'
    );
    const { result } = renderHook(() => useSessionTodoPlan());
    expect(result.current.callId).toBe('call-new');
  });
});
