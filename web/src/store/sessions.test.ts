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

const seedThread = (list: any[], active?: string) => {
  useStore.setState({
    db: {
      acme: {
        ...seedDb().acme,
        threads: { 'a-atlas': { active: active ?? list[0]?.id ?? null, list } },
      },
    },
    pos: posFor('acme'),
  });
};

const activeSession = () => {
  const s: any = useStore.getState();
  const th = s.db.acme.threads['a-atlas'];
  return th.list.find((x: any) => x.id === th.active);
};

describe('store session bindings (web-live-chat-sessions)', () => {
  beforeEach(() => {
    localStorage.clear();
    seedThread([{ id: 's1', title: 'Legacy chat', updated: '', messages: [] }]);
  });

  it('ensureSessionBinding lazily migrates a legacy counter id to a sess_<uuid> id', () => {
    const minted = useStore.getState().ensureSessionBinding('a-atlas');

    expect(minted).toMatch(/^sess_[0-9a-f-]{36}$/);
    const sess = activeSession();
    expect(sess.id).toBe(minted);
    expect(sess.sess).toBe(minted);
    // the active pointer follows the migrated id
    expect(useStore.getState().db.acme.threads['a-atlas'].active).toBe(minted);
  });

  it('ensureSessionBinding is stable — a second call returns the same id without re-minting', () => {
    const first = useStore.getState().ensureSessionBinding('a-atlas');
    const second = useStore.getState().ensureSessionBinding('a-atlas');

    expect(second).toBe(first);
    expect(activeSession().sess).toBe(first);
  });

  it('ensureSessionBinding returns null for missing or legacy array-shaped threads', () => {
    useStore.setState({ pos: posFor('acme') });
    expect(useStore.getState().ensureSessionBinding('no-such-thread')).toBeNull();

    useStore.setState({
      db: {
        acme: {
          ...seedDb().acme,
          threads: { 'legacy-agent': [{ id: 'm1', author: 'you', ts: '9:00 AM', text: 'hi' }] },
        },
      },
    });
    expect(useStore.getState().ensureSessionBinding('legacy-agent')).toBeNull();
  });

  it('bindSession sets an explicit sess and getSessionBinding reads it back', () => {
    useStore.getState().bindSession('a-atlas', 'sess_explicit');

    expect(useStore.getState().getSessionBinding('a-atlas')).toBe('sess_explicit');
    expect(activeSession().sess).toBe('sess_explicit');
  });

  it('getSessionBinding returns null when the session has no binding', () => {
    expect(useStore.getState().getSessionBinding('a-atlas')).toBeNull();
  });

  it('bindSession only touches the active session of the thread', () => {
    seedThread([
      { id: 's1', title: 'One', updated: '', messages: [] },
      { id: 's2', title: 'Two', updated: '', messages: [] },
    ], 's2');

    useStore.getState().bindSession('a-atlas', 'sess_two');

    const th: any = useStore.getState().db.acme.threads['a-atlas'];
    expect(th.list.find((x: any) => x.id === 's1').sess).toBeUndefined();
    expect(th.list.find((x: any) => x.id === 's2').sess).toBe('sess_two');
  });

  it('recordResponse stamps resp onto the addressed agent message', () => {
    seedThread([{
      id: 's1',
      title: 'Chat',
      updated: '',
      messages: [
        { id: 'm1', author: 'you', ts: '9:00 AM', text: 'hello' },
        { id: 'm2', author: 'agent', agentId: 'a-atlas', ts: '9:00 AM', text: 'hi back' },
      ],
    }]);

    useStore.getState().recordResponse('a-atlas', 'm2', 'resp_abc123');

    const sess = activeSession();
    expect(sess.messages.find((m: any) => m.id === 'm2').resp).toBe('resp_abc123');
    expect(sess.messages.find((m: any) => m.id === 'm1').resp).toBeUndefined();
  });

  it('getLastResponse returns the most recent resp on the active session', () => {
    seedThread([{
      id: 's1',
      title: 'Chat',
      updated: '',
      messages: [
        { id: 'm1', author: 'you', ts: '9:00 AM', text: 'hello' },
        { id: 'm2', author: 'agent', agentId: 'a-atlas', ts: '9:00 AM', text: 'one', resp: 'resp_one' },
        { id: 'm3', author: 'you', ts: '9:01 AM', text: 'again' },
        { id: 'm4', author: 'agent', agentId: 'a-atlas', ts: '9:01 AM', text: 'two', resp: 'resp_two' },
      ],
    }]);

    expect(useStore.getState().getLastResponse('a-atlas')).toBe('resp_two');
  });

  it('getLastResponse returns null when no message carries a resp', () => {
    expect(useStore.getState().getLastResponse('a-atlas')).toBeNull();
  });
});
