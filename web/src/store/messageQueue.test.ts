/**
 * @vitest-environment jsdom
 */
import { describe, it, expect, beforeEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useStore, useMessageQueue } from './index';
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

// Message queue store state (adopt-assistant-ui-elements 8.1): ordered
// per-chat entries, cancel-one-entry removal, first-in-first-out dequeue.
describe('message queue store state (adopt-assistant-ui-elements 8.1)', () => {
  beforeEach(() => {
    localStorage.clear();
    useStore.setState({
      db: { acme: seedDb().acme },
      pos: { tenantId: 'acme', view: 'chats', chatId: 'a-atlas', showContext: false },
      messageQueue: {},
    });
  });

  const queueOf = (tenantId = 'acme', chatId = 'a-atlas') =>
    useStore.getState().messageQueue[`${tenantId}::${chatId}`] || [];

  it('enqueue appends ordered entries and resolves their ids', () => {
    const id1 = useStore.getState().enqueueChatMessage('acme', 'a-atlas', 'first');
    const id2 = useStore.getState().enqueueChatMessage('acme', 'a-atlas', 'second');
    expect(id1).not.toBe(id2);

    const q = queueOf();
    expect(q).toHaveLength(2);
    expect(q[0].id).toBe(id1);
    expect(q[0].text).toBe('first');
    expect(q[1].id).toBe(id2);
    expect(q[1].text).toBe('second');
  });

  it('enqueue keeps ready attachment references on the entry', () => {
    const atts = [{ id: 'att-1', name: 'shot.png', mime: 'image/png', size: 12, url: '/api/v1/files/k1' }];
    useStore.getState().enqueueChatMessage('acme', 'a-atlas', 'look', atts);

    expect(queueOf()[0].attachments).toEqual(atts);
  });

  it('remove cancels ONLY that entry — neighbors and order stay intact', () => {
    const id1 = useStore.getState().enqueueChatMessage('acme', 'a-atlas', 'first');
    const id2 = useStore.getState().enqueueChatMessage('acme', 'a-atlas', 'second');
    const id3 = useStore.getState().enqueueChatMessage('acme', 'a-atlas', 'third');

    useStore.getState().removeQueuedChatMessage('acme', 'a-atlas', id2);

    const q = queueOf();
    expect(q.map((x) => x.id)).toEqual([id1, id3]);
    expect(q.map((x) => x.text)).toEqual(['first', 'third']);
  });

  it('remove with an unknown id is a no-op (never reorders or clears)', () => {
    const id1 = useStore.getState().enqueueChatMessage('acme', 'a-atlas', 'first');
    useStore.getState().removeQueuedChatMessage('acme', 'a-atlas', 'q-nope');

    expect(queueOf().map((x) => x.id)).toEqual([id1]);
  });

  it('dequeue pops the FIRST entry and drains in order', () => {
    const id1 = useStore.getState().enqueueChatMessage('acme', 'a-atlas', 'first');
    useStore.getState().enqueueChatMessage('acme', 'a-atlas', 'second');

    const head = useStore.getState().dequeueChatMessage('acme', 'a-atlas');
    expect(head?.id).toBe(id1);
    expect(head?.text).toBe('first');
    expect(queueOf().map((x) => x.text)).toEqual(['second']);

    useStore.getState().dequeueChatMessage('acme', 'a-atlas');
    expect(useStore.getState().dequeueChatMessage('acme', 'a-atlas')).toBeNull();
  });

  it('dequeue on an empty queue resolves null', () => {
    expect(useStore.getState().dequeueChatMessage('acme', 'a-atlas')).toBeNull();
  });

  it('queues are isolated per (workspace, chat)', () => {
    useStore.getState().enqueueChatMessage('acme', 'a-atlas', 'atlas msg');
    useStore.getState().enqueueChatMessage('acme', 'a-beacon', 'beacon msg');

    expect(queueOf('acme', 'a-atlas').map((x) => x.text)).toEqual(['atlas msg']);
    expect(queueOf('acme', 'a-beacon').map((x) => x.text)).toEqual(['beacon msg']);

    // Draining one chat leaves the other untouched.
    useStore.getState().dequeueChatMessage('acme', 'a-atlas');
    expect(queueOf('acme', 'a-atlas')).toHaveLength(0);
    expect(queueOf('acme', 'a-beacon')).toHaveLength(1);
  });

  it('the useMessageQueue selector returns a stable reference while nothing changes', async () => {
    const { result } = renderHook(() => useMessageQueue('acme', 'a-atlas'));
    await act(async () => { useStore.getState().enqueueChatMessage('acme', 'a-atlas', 'one'); });
    const afterEnqueue = result.current;
    // Re-render without a queue change for THIS chat must not mint a fresh
    // array (zustand v5 referential-stability contract).
    await act(async () => { useStore.getState().enqueueChatMessage('acme', 'a-beacon', 'other chat'); });
    expect(result.current).toBe(afterEnqueue);
    expect(result.current.map((x) => x.text)).toEqual(['one']);
  });
});
