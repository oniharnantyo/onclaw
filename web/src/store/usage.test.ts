import { describe, it, expect, beforeEach } from 'vitest';
import { useStore } from './index';
import { seedDb } from '../data/seed';

// This environment's jsdom exposes no localStorage (same mode behind the
// ~66 pre-existing failures); install a minimal stub so this suite runs.
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

const posFor = (tenantId: string) => ({ tenantId, view: 'chats', chatId: 'a-atlas', showContext: false });

const seedThreads = (threads: Record<string, { active: string | null; list: any[] }>) => {
  useStore.setState({
    db: {
      acme: {
        ...seedDb().acme,
        threads,
      },
    },
    pos: posFor('acme'),
  });
};

const activeSession = (chatId: string) => {
  const s: any = useStore.getState();
  const th = s.db.acme.threads[chatId];
  return th.list.find((x: any) => x.id === th.active);
};

describe('recordThreadUsage (chat-context-meter)', () => {
  beforeEach(() => {
    localStorage.clear();
    seedThreads({
      'a-atlas': { active: 's1', list: [{ id: 's1', title: 'One', updated: '', messages: [] }] },
      'a-beacon': { active: 's2', list: [{ id: 's2', title: 'Two', updated: '', messages: [] }] },
    });
  });

  it('stores { finalInput, at } on the thread\'s ACTIVE session for a number', () => {
    useStore.getState().recordThreadUsage('acme', 'a-atlas', 68452);

    const sess = activeSession('a-atlas');
    expect(sess.usage).toEqual({ finalInput: 68452, at: expect.any(String) });
    expect(typeof sess.usage.at).toBe('string');
    expect(sess.usage.at.length).toBeGreaterThan(0);
  });

  it('clears the field for null so the meter hides instead of showing a stale value', () => {
    useStore.getState().recordThreadUsage('acme', 'a-atlas', 68452);
    useStore.getState().recordThreadUsage('acme', 'a-atlas', null);

    expect(activeSession('a-atlas').usage).toBeUndefined();
  });

  it('is isolated per thread — recording on one thread leaves another untouched', () => {
    useStore.getState().recordThreadUsage('acme', 'a-atlas', 1000);

    expect(activeSession('a-atlas').usage).toEqual({ finalInput: 1000, at: expect.any(String) });
    expect(activeSession('a-beacon').usage).toBeUndefined();
  });

  it('targets only the addressed thread even when both carry values', () => {
    useStore.getState().recordThreadUsage('acme', 'a-atlas', 1000);
    useStore.getState().recordThreadUsage('acme', 'a-beacon', 2000);
    useStore.getState().recordThreadUsage('acme', 'a-atlas', null);

    expect(activeSession('a-atlas').usage).toBeUndefined();
    expect(activeSession('a-beacon').usage.finalInput).toBe(2000);
  });
});

describe('recordThreadUsage turn block (assistant-ui context-display adoption)', () => {
  beforeEach(() => {
    localStorage.clear();
    seedThreads({
      'a-atlas': { active: 's1', list: [{ id: 's1', title: 'One', updated: '', messages: [] }] },
    });
  });

  it('keeps the last turn input/output alongside finalInput when reported', () => {
    useStore.getState().recordThreadUsage('acme', 'a-atlas', 68_000, { input: 68_000, output: 1_200 });

    expect(activeSession('a-atlas').usage).toEqual({
      finalInput: 68_000,
      input: 68_000,
      output: 1_200,
      at: expect.any(String),
    });
  });

  it('omits the turn rows when the wire did not report them (present-only)', () => {
    useStore.getState().recordThreadUsage('acme', 'a-atlas', 68_000);
    expect(activeSession('a-atlas').usage).toEqual({ finalInput: 68_000, at: expect.any(String) });

    useStore.getState().recordThreadUsage('acme', 'a-atlas', 68_000, {});
    expect(activeSession('a-atlas').usage).toEqual({ finalInput: 68_000, at: expect.any(String) });
  });

  it('keeps a server-provided context breakdown when the wire carries one (D7)', () => {
    const breakdown = { instructions: 1_200, tools: 3_400, conversation: 12_000 };
    useStore.getState().recordThreadUsage('acme', 'a-atlas', 50_000, {
      input: 50_000, output: 900, contextBreakdown: breakdown,
    });

    const usage = activeSession('a-atlas').usage;
    expect(usage.contextBreakdown).toEqual(breakdown);
    // finalInput semantics unchanged — still the meter's numerator.
    expect(usage.finalInput).toBe(50_000);
  });

  it('a later terminal event without turn rows drops them from the record', () => {
    useStore.getState().recordThreadUsage('acme', 'a-atlas', 68_000, { input: 68_000, output: 1_200 });
    useStore.getState().recordThreadUsage('acme', 'a-atlas', 71_000);

    expect(activeSession('a-atlas').usage).toEqual({ finalInput: 71_000, at: expect.any(String) });
  });
});
