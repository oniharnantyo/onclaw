/**
 * @vitest-environment jsdom
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useChatRuntime } from './runtime';
import { useStore } from '../store';
import { runTurn } from '../lib/openresponses';
import { getLiveChatStatus, handleV1AuthFailure } from '../lib/livechat';
import { api } from '../lib/api';

vi.mock('@assistant-ui/react', () => ({
  useExternalStoreRuntime: vi.fn((opts) => opts),
}));
vi.mock('../lib/openresponses', () => ({
  runTurn: vi.fn(),
  // Real codec (tiny pure fn) — the conflict-queue test asserts the retried
  // turn chains via the decoded session id.
  sessionIdFromResponseId: (rid: string | null | undefined) => {
    if (!rid || !rid.startsWith('resp_')) return undefined;
    const payload = rid.slice('resp_'.length);
    const cut = payload.lastIndexOf('_');
    if (cut <= 0 || cut >= payload.length - 1) return undefined;
    return payload.slice(0, cut);
  },
}));
// Real livechat module (connect-status store, hydration) except the network
// exchange, which the auth-failure tests stub.
vi.mock('../lib/livechat', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/livechat')>();
  return { ...actual, handleV1AuthFailure: vi.fn() };
});
vi.mock('../lib/api', () => ({
  api: {
    onUnauthorized: vi.fn(),
    agents: { cancelRun: vi.fn().mockResolvedValue({ cancelled: true }) },
  },
}));

// This environment's jsdom exposes no localStorage (same mode behind the ~66
// pre-existing failures); install a minimal stub so these suites run.
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

describe('useChatRuntime', () => {
  beforeEach(() => {
    localStorage.clear();
    act(() => {
      const mkAgent = (id: string, name: string, tools: string[]) => ({
        id, name, model: 'claude-sonnet-5', temp: 0.4, autonomy: 'approval', channelPost: false,
        role: 'Test agent', status: 'idle', tools, skills: ['research'], lastActive: 'now', prompt: ''
      });
      useStore.setState({
        pos: { tenantId: 't1', view: 'chats', chatId: 'a1', showContext: false },
        ui: { configAgent: null, scheduleEdit: null, wsOpen: false, running: false, toasts: [] },
        db: {
          t1: {
            id: 't1',
            name: 'T1', sub: 't1', tz: 'America/Los_Angeles',
            defaultModel: 'claude-sonnet-5', retention: '90 days',
            people: [], schedules: [], runs: [], members: [], integrations: [], skillLib: [], keys: [],
            agents: [mkAgent('a1', 'Alice', ['search']), mkAgent('a2', 'Bob', [])],
            // agentId is '' (not 'a1') so no agent auto-responds to plain
            // channel messages — the no-turn and DM-only tests rely on it.
            channels: [{ id: 'c1', name: 'general', purpose: 'Team chat', agentId: '', unread: 0, members: ['a1', 'a2'] }],
            threads: {
              a1: { active: 's1', list: [{ id: 's1', title: 'Chat', updated: '', messages: [{
                id: 'm1', text: 'hello', author: 'agent', ts: '2026-08-27T00:00:00Z',
                agentId: 'a1', scheduler: 'sched1', name: 'Agent 1',
                tools: [{ name: 'test.tool', args: 'foo', ms: 100 }]
              }] }] },
              c1: { active: 's2', list: [{ id: 's2', title: 'Chat', updated: '', messages: [{
                id: 'm2', text: 'teammate msg', author: 'you', ts: '2026-08-27T00:00:00Z',
              }] }] }
            }
          }
        }
      });
    });
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
  });

  it('conversion round-trip maintains identity/scheduler-origin/tools', () => {
    const { result } = renderHook(() => useChatRuntime('a1'));
    const converted: any = result.current.convertMessage(useStore.getState().db.t1.threads.a1.list[0].messages[0]);
    
    expect(converted.id).toBe('m1');
    expect(converted.role).toBe('assistant');
    expect(converted.metadata.custom.onclaw.agentId).toBe('a1');
    expect(converted.metadata.custom.onclaw.scheduler).toBe('sched1');
    expect(converted.content[1].type).toBe('tool-call');
    expect(converted.content[1].toolName).toBe('test.tool');
  });

  it('teammate no-turn', async () => {
    const { result } = renderHook(() => useChatRuntime('c1'));
    // Simulate user sending message without mention
    await act(async () => {
      await result.current.onNew({ role: 'user', content: [{ type: 'text', text: 'hello team' }] } as any);
    });
    // Check if an agent responded (should not)
    vi.advanceTimersByTime(10000);
    const msgs = useStore.getState().db.t1.threads.c1.list[0].messages;
    expect(msgs.length).toBe(2); // Only the new message is added, no agent reply
    expect(msgs[1].author).toBe('you');
  });

  it('mention fan-out with no chat key runs no canned replies and surfaces the connect state', async () => {
    const { result } = renderHook(() => useChatRuntime('c1'));
    await act(async () => {
      await result.current.onNew({ role: 'user', content: [{ type: 'text', text: '@Alice @Bob hello' }] } as any);
    });
    vi.advanceTimersByTime(10000);
    const msgs = useStore.getState().db.t1.threads.c1.list[0].messages;
    // Mock retirement (task 5.3): only the user message lands — no canned
    // agent replies — and the connect state flips to disconnected.
    expect(msgs.length).toBe(2);
    expect(msgs[1].author).toBe('you');
    expect(runTurn).not.toHaveBeenCalled();
    expect(getLiveChatStatus().state).toBe('disconnected');
    expect(useStore.getState().ui.running).toBe(false);
  });

  it('DM-only affordance gating', async () => {
    // try to edit in channel
    const { result } = renderHook(() => useChatRuntime('c1'));
    await act(async () => {
      await result.current.onEdit({ sourceId: 'm2', role: 'user', content: [{ type: 'text', text: 'edited' }] } as any);
    });
    // Should be ignored
    const msgs = useStore.getState().db.t1.threads.c1.list[0].messages;
    expect(msgs[0].text).toBe('teammate msg');
  });

  it('stop-cancel keeps partial text', async () => {
    const { result } = renderHook(() => useChatRuntime('a1'));
    
    act(() => {
      useStore.getState().patchUi({ running: true });
    });
    
    await act(async () => {
      await result.current.onCancel();
    });
    
    expect(useStore.getState().ui.running).toBe(false);
  });
});

describe('useChatRuntime — live session binding (birth → chain → reset-forks)', () => {
  const activeSession = (): any => {
    const th = useStore.getState().db.t1.threads.a1;
    return th.list.find((x: any) => x.id === th.active);
  };

  const send = (result: any, text: string) =>
    act(async () => {
      await result.current.onNew({ role: 'user', content: [{ type: 'text', text }] } as any);
    });

  beforeEach(() => {
    localStorage.clear();
    vi.mocked(runTurn).mockReset();
    vi.mocked(handleV1AuthFailure).mockReset();
    vi.mocked(api.agents.cancelRun).mockClear();
    act(() => {
      const mkAgent = (id: string, name: string, tools: string[]) => ({
        id, name, model: 'claude-sonnet-5', temp: 0.4, autonomy: 'approval', channelPost: false,
        role: 'Test agent', status: 'idle', tools, skills: ['research'], lastActive: 'now', prompt: ''
      });
      useStore.setState({
        pos: { tenantId: 't1', view: 'chats', chatId: 'a1', showContext: false },
        ui: { configAgent: null, scheduleEdit: null, wsOpen: false, running: false, toasts: [] },
        db: {
          t1: {
            id: 't1',
            name: 'T1', sub: 't1', tz: 'America/Los_Angeles',
            defaultModel: 'claude-sonnet-5', retention: '90 days',
            people: [], schedules: [], runs: [], members: [], integrations: [], skillLib: [], keys: [],
            agents: [mkAgent('a1', 'Alice', ['search'])],
            channels: [],
            threads: {
              // Legacy counter-shaped session id (pre-binding) — must migrate
              // lazily on its first live turn.
              a1: { active: 's1', list: [{ id: 's1', title: 'Chat', updated: '', messages: [] }] },
            },
          },
        },
      });
    });
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
  });

  it('birth turn mints/binds a sess_ id and sends it as metadata, then records the response id', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, 'first turn');

    expect(runTurn).toHaveBeenCalledTimes(1);
    const [key, params, cb]: any[] = vi.mocked(runTurn).mock.calls[0];
    expect(key).toBe('k-live');
    // No recorded resp chain → BIRTH: sessionId metadata, no chaining.
    expect(params.sessionId).toMatch(/^sess_/);
    expect(params.previousResponseId).toBeUndefined();
    // Lazy migration: the session records and adopts the minted id.
    expect(activeSession().sess).toBe(params.sessionId);

    // Stream lifecycle: early response id (cancel window), deltas, done.
    await act(async () => { cb.onResponseId('resp_sess-turn-one_1'); });
    act(() => { cb.onDelta('Hi'); });
    await act(async () => { cb.onDone('resp_sess-turn-one_1'); });

    const msgs = activeSession().messages;
    const last = msgs[msgs.length - 1];
    expect(last.author).toBe('agent');
    expect(last.text).toBe('Hi');
    expect(last.resp).toBe('resp_sess-turn-one_1');
    expect(useStore.getState().ui.running).toBe(false);
  });

  it('subsequent turns chain via previous_response_id from the last recorded resp', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    // A bound session with a recorded response chain.
    act(() => {
      useStore.getState().updateTenant('t1', (t: any) => {
        const s = t.threads.a1.list[0];
        s.sess = 'sess_bound-1';
        s.messages = [{ id: 'm1', author: 'agent', ts: '', text: 'prior', resp: 'resp_prior_9' }];
        return t;
      });
    });
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, 'second turn');

    expect(runTurn).toHaveBeenCalledTimes(1);
    const [, params]: any[] = vi.mocked(runTurn).mock.calls[0];
    // Chain: previous_response_id only — no session metadata on later turns.
    expect(params.previousResponseId).toBe('resp_prior_9');
    expect(params.sessionId).toBeUndefined();
  });

  it('/reset passes through as ordinary text (commands are /compact-only, chat-compact-command D8)', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    act(() => {
      useStore.getState().updateTenant('t1', (t: any) => {
        const s = t.threads.a1.list[0];
        s.sess = 'sess_old-1';
        s.messages = [
          { id: 'u1', author: 'you', ts: '', text: 'hi' },
          { id: 'm1', author: 'agent', ts: '', text: 'prior', resp: 'resp_old_1' },
        ];
        return t;
      });
    });
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, '/reset');

    // The old local /reset interception is gone: /reset is an unknown command
    // and rides the send path as an ordinary chained turn.
    expect(runTurn).toHaveBeenCalledTimes(1);
    const [, params]: any[] = vi.mocked(runTurn).mock.calls[0];
    expect(params.input).toBe('/reset');
    expect(params.previousResponseId).toBe('resp_old_1');
    // The thread was NOT cleared locally — the message lands and the turn
    // runs (with its optimistic agent row, like any ordinary message).
    const after = activeSession();
    expect(after.messages).toHaveLength(4);
    expect(after.messages[2].author).toBe('you');
    expect(after.messages[2].text).toBe('/reset');
    expect(after.messages[3].author).toBe('agent');
  });

  it('stop cancels the in-flight server run by decoding resp_<session>_<turn> at the last underscore', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, 'long turn');

    expect(useStore.getState().ui.running).toBe(true);
    const [, , cb]: any[] = vi.mocked(runTurn).mock.calls[0];
    // Minted id arrives at the FIRST stream event — mid-flight cancel window.
    await act(async () => { cb.onResponseId('resp_sess-live-1_turn-uuid-42'); });
    act(() => { cb.onDelta('partial '); });

    await act(async () => { await result.current.onCancel(); });

    // Codec (internal/openresponses/codec.go): strip resp_, split at the LAST
    // underscore → session / turn segments.
    expect(api.agents.cancelRun).toHaveBeenCalledWith('t1', 'a1', 'sess-live-1', 'turn-uuid-42');
    expect(useStore.getState().ui.running).toBe(false);
    // Partial text persists in the transcript.
    const msgs = activeSession().messages;
    expect(msgs[msgs.length - 1].text).toBe('partial ');
  });

  it('stop before the first stream event still cancels the server run by session (placeholder turn)', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, 'slow first token');

    // Nothing streamed yet: no minted response id exists, but the run is
    // already live server-side. The stop must still reach it — addressed by
    // the bound session (the cancel endpoint is session-scoped; the turn
    // segment is a placeholder). Skipping the cancel here left the run
    // streaming to completion after "stop", and a reload re-attached to it.
    const [, params]: any[] = vi.mocked(runTurn).mock.calls[0];
    expect(params.sessionId).toMatch(/^sess_/);

    await act(async () => { await result.current.onCancel(); });

    expect(api.agents.cancelRun).toHaveBeenCalledWith('t1', 'a1', params.sessionId, 'pending');
    expect(useStore.getState().ui.running).toBe(false);
  });

  it('/v1 auth failure re-exchanges once and retries the turn with the fresh key', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-stale');
    vi.mocked(runTurn).mockImplementation(async () => {});
    vi.mocked(handleV1AuthFailure).mockResolvedValue('k-fresh');
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, 'turn one');

    // First attempt runs on the stale key, then the stream fails auth.
    const [k1, , cb1]: any[] = vi.mocked(runTurn).mock.calls[0];
    expect(k1).toBe('k-stale');
    await act(async () => { cb1.onError('invalid api key', { unauthorized: true }); });

    // Design D4: clear slot → re-exchange once → retry the same turn once.
    expect(handleV1AuthFailure).toHaveBeenCalledWith('t1');
    expect(runTurn).toHaveBeenCalledTimes(2);
    const [k2, params2]: any[] = vi.mocked(runTurn).mock.calls[1];
    expect(k2).toBe('k-fresh');
    // The retry carries the same binding (same birth/chained identity).
    expect(params2.input).toBe('turn one');
    expect(useStore.getState().ui.running).toBe(true);
  });

  it('/v1 auth failure with a failed re-exchange surfaces the connect state and does not retry', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-stale');
    vi.mocked(runTurn).mockImplementation(async () => {});
    vi.mocked(handleV1AuthFailure).mockResolvedValue(null);
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, 'turn one');

    const [, , cb1]: any[] = vi.mocked(runTurn).mock.calls[0];
    await act(async () => { cb1.onError('invalid api key', { unauthorized: true }); });

    expect(handleV1AuthFailure).toHaveBeenCalledTimes(1);
    expect(runTurn).toHaveBeenCalledTimes(1);
    expect(useStore.getState().ui.running).toBe(false);
    expect(getLiveChatStatus().state).toBe('disconnected');
  });

  it('terminal failure (provider 429) retracts the empty optimistic row', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, 'turn one');
    // The optimistic empty agent message exists while the turn runs.
    expect(activeSession().messages.some((m: any) => m.id && m.author === 'agent' && m.text === '')).toBe(true);

    const [, , cb]: any[] = vi.mocked(runTurn).mock.calls[0];
    await act(async () => { cb.onError('Error from provider: Rate limit exceeded', { unauthorized: false }); });

    expect(useStore.getState().ui.running).toBe(false);
    // Nothing streamed → the empty row is retracted, not left loading forever.
    expect(activeSession().messages.some((m: any) => m.author === 'agent')).toBe(false);
    // The user's message stays.
    expect(activeSession().messages.some((m: any) => m.author === 'you' && m.text === 'turn one')).toBe(true);
  });

  it('terminal failure after partial text keeps the partial transcript', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, 'long turn');
    const [, , cb]: any[] = vi.mocked(runTurn).mock.calls[0];
    await act(async () => { cb.onResponseId('resp_sess-live-1_turn-1'); });
    act(() => { cb.onDelta('partial '); });
    await act(async () => { cb.onError('stream interrupted', { unauthorized: false }); });

    const msgs = activeSession().messages;
    const last = msgs[msgs.length - 1];
    // Design D8: a terminal non-auth failure appends an in-thread error entry
    // after the partial transcript.
    expect(last.author).toBe('error');
    expect(last.error).toBe('stream interrupted');
    const agentMsg = msgs[msgs.length - 2];
    expect(agentMsg.author).toBe('agent');
    expect(agentMsg.text).toBe('partial ');
    expect(useStore.getState().ui.running).toBe(false);
  });

  it('non-auth failure appends an in-thread error entry after retracting the empty row', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, 'turn one');
    const [, , cb]: any[] = vi.mocked(runTurn).mock.calls[0];
    await act(async () => { cb.onError('Error from provider: Rate limit exceeded', { unauthorized: false }); });

    const msgs = activeSession().messages;
    const last = msgs[msgs.length - 1];
    expect(last.author).toBe('error');
    expect(last.error).toBe('Error from provider: Rate limit exceeded');
    // The empty optimistic agent row is gone; the error entry replaced it.
    expect(msgs.some((m: any) => m.author === 'agent')).toBe(false);
  });

  it('auth failure never appends an error entry (connect-state path only)', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-stale');
    vi.mocked(runTurn).mockImplementation(async () => {});
    vi.mocked(handleV1AuthFailure).mockResolvedValue(null);
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, 'turn one');
    const [, , cb]: any[] = vi.mocked(runTurn).mock.calls[0];
    await act(async () => { cb.onError('invalid api key', { unauthorized: true }); });

    expect(activeSession().messages.some((m: any) => m.author === 'error')).toBe(false);
    expect(getLiveChatStatus().state).toBe('disconnected');
  });

  it('reasoning deltas accumulate as an ordered reasoning part, distinct from text', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, 'think hard');
    const [, , cb]: any[] = vi.mocked(runTurn).mock.calls[0];
    act(() => { cb.onReasoningDelta?.('step one. '); });
    act(() => { cb.onReasoningDelta?.('step two.'); });
    act(() => { cb.onDelta('Answer'); });
    // Deltas coalesce into one store write per macrotask (nested-update guard).
    act(() => { vi.advanceTimersByTime(1); });

    const last = activeSession().messages[activeSession().messages.length - 1];
    expect(last.author).toBe('agent');
    expect(last.parts).toEqual([{ k: 'reasoning', text: 'step one. step two.' }]);
    expect(last.text).toBe('Answer');
  });

  it('turn body keeps stream order: reasoning → tool call → reasoning → text', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, 'fetch then answer');
    const [, , cb]: any[] = vi.mocked(runTurn).mock.calls[0];
    act(() => { cb.onReasoningDelta?.('need the page. '); });
    act(() => { cb.onToolCall?.('web.fetch', 'call_1'); });
    act(() => { cb.onToolCall?.('web.fetch', 'call_1', '{"url":"https://x"}'); });
    act(() => { cb.onToolOutput?.('call_1', 'web.fetch', '<html>', 410, false); });
    act(() => { cb.onReasoningDelta?.('got it, summarizing.'); });
    act(() => { cb.onDelta('Here is the summary.'); });
    // Deltas coalesce into one store write per macrotask (nested-update guard).
    act(() => { vi.advanceTimersByTime(1); });

    const last = activeSession().messages[activeSession().messages.length - 1];
    expect(last.parts).toEqual([
      { k: 'reasoning', text: 'need the page. ' },
      { k: 'tool', i: 0 },
      { k: 'reasoning', text: 'got it, summarizing.' },
    ]);
    expect(last.tools).toHaveLength(1);
    expect(last.text).toBe('Here is the summary.');
  });

  it('whitespace-only reasoning deltas do not mint empty bubbles', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, 'blank thoughts');
    const [, , cb]: any[] = vi.mocked(runTurn).mock.calls[0];
    act(() => { cb.onReasoningDelta?.('   '); });
    act(() => { cb.onDelta('Answer'); });
    // Deltas coalesce into one store write per macrotask (nested-update guard).
    act(() => { vi.advanceTimersByTime(1); });

    const last = activeSession().messages[activeSession().messages.length - 1];
    expect(last.parts ?? []).toEqual([]);
    expect(last.text).toBe('Answer');
  });

  it('tool call args land on the card: added pushes argless, done updates by call id (no duplicates)', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, 'use a tool');
    const [, , cb]: any[] = vi.mocked(runTurn).mock.calls[0];
    act(() => { cb.onToolCall?.('files.list', 'call_1'); });
    act(() => { cb.onToolCall?.('files.list', 'call_1', '{"path":"."}'); });
    act(() => { cb.onToolOutput?.('call_1', 'files.list', '["a"]', 42, false); });

    const last = activeSession().messages[activeSession().messages.length - 1];
    expect(last.tools).toHaveLength(1);
    expect(last.tools[0]).toMatchObject({ name: 'files.list', callId: 'call_1', args: '{"path":"."}', res: '["a"]', ms: 42 });
  });

  it('stop with nothing streamed retracts the empty optimistic row', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, 'slow turn');
    // No response id captured yet (nothing streamed), then the user stops.
    await act(async () => { await result.current.onCancel(); });

    expect(useStore.getState().ui.running).toBe(false);
    expect(activeSession().messages.some((m: any) => m.author === 'agent')).toBe(false);
  });

  it('onUsage with a usable final input records { finalInput, at } on the active session', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, 'metered turn');
    const [, , cb]: any[] = vi.mocked(runTurn).mock.calls[0];
    await act(async () => {
      await cb.onUsage({ inputTokens: 50000, outputTokens: 1200, totalTokens: 51200, finalInputTokens: 50000 });
    });

    const sess = activeSession();
    expect(sess.usage).toEqual({ finalInput: 50000, at: expect.any(String) });
  });

  it('onUsage(null) clears a previously stored meter value', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, 'turn one');
    const [, , cb]: any[] = vi.mocked(runTurn).mock.calls[0];
    await act(async () => {
      await cb.onUsage({ inputTokens: 10, outputTokens: 5, totalTokens: 15, finalInputTokens: 10 });
    });
    await send(result, 'turn two');
    const [, , cb2]: any[] = vi.mocked(runTurn).mock.calls[1];
    await act(async () => { await cb2.onUsage(null); });

    expect(activeSession().usage).toBeUndefined();
  });

  it('onUsage with usage lacking finalInputTokens clears the meter (no stale/zero value)', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, 'turn one');
    const [, , cb]: any[] = vi.mocked(runTurn).mock.calls[0];
    await act(async () => {
      await cb.onUsage({ inputTokens: 10, outputTokens: 5, totalTokens: 15 });
    });

    expect(activeSession().usage).toBeUndefined();
  });

  it('meter state is isolated per thread — recording leaves other threads untouched', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    act(() => {
      useStore.getState().updateTenant('t1', (t: any) => {
        t.threads.c1 = { active: 's9', list: [{ id: 's9', title: 'C', updated: '', messages: [] }] };
        return t;
      });
    });
    useStore.getState().recordThreadUsage('t1', 'c1', 9999);
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, 'turn one');
    const [, , cb]: any[] = vi.mocked(runTurn).mock.calls[0];
    await act(async () => {
      await cb.onUsage({ inputTokens: 10, outputTokens: 5, totalTokens: 15, finalInputTokens: 10 });
    });

    expect(activeSession().usage.finalInput).toBe(10);
    const c1Th: any = useStore.getState().db.t1.threads.c1;
    const c1Sess = c1Th.list.find((x: any) => x.id === c1Th.active);
    expect(c1Sess.usage).toEqual({ finalInput: 9999, at: expect.any(String) });
  });
});

describe('useChatRuntime — 409 conflict queue (live-run-reattach fix)', () => {
  const encoder = new TextEncoder();

  const activeSession = (): any => {
    const th = useStore.getState().db.t1.threads.a1;
    return th.list.find((x: any) => x.id === th.active);
  };

  const send = (result: any, text: string) =>
    act(async () => {
      await result.current.onNew({ role: 'user', content: [{ type: 'text', text }] } as any);
    });

  beforeEach(() => {
    localStorage.clear();
    vi.mocked(runTurn).mockReset();
    vi.mocked(handleV1AuthFailure).mockReset();
    act(() => {
      const mkAgent = (id: string, name: string, tools: string[]) => ({
        id, name, model: 'claude-sonnet-5', temp: 0.4, autonomy: 'approval', channelPost: false,
        role: 'Test agent', status: 'idle', tools, skills: ['research'], lastActive: 'now', prompt: ''
      });
      useStore.setState({
        pos: { tenantId: 't1', view: 'chats', chatId: 'a1', showContext: false },
        ui: { configAgent: null, scheduleEdit: null, wsOpen: false, running: false, toasts: [] },
        db: {
          t1: {
            id: 't1', name: 'T1', sub: 't1', tz: 'America/Los_Angeles',
            defaultModel: 'claude-sonnet-5', retention: '90 days',
            people: [], schedules: [], runs: [], members: [], integrations: [], skillLib: [], keys: [],
            agents: [mkAgent('a1', 'Alice', ['search'])],
            channels: [],
            threads: {
              // Bound session mid-run: the tail agent message carries the
              // response chain the queue's re-dispatch must extend.
              a1: {
                active: 'sess_live-9',
                list: [{
                  id: 'sess_live-9', title: 'Chat', updated: '',
                  messages: [{ id: 'm0', author: 'agent', ts: '', text: 'partial', resp: 'resp_sess_live-9_turn-1' }],
                }],
              },
            },
          },
        },
      });
    });
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('queues the send behind the active run, streams it, and re-dispatches chained', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');

    // The catch-up stream: run_active, a delta of the still-running turn,
    // terminal, [DONE] — after which the queued turn re-dispatches.
    const encoder = new TextEncoder();
    vi.stubGlobal('fetch', vi.fn(async () =>
      ({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'text/event-stream' }),
        body: new ReadableStream<Uint8Array>({
          start(controller) {
            controller.enqueue(encoder.encode(`data: ${JSON.stringify({ kind: 'run_active', occurred_at: 'x' })}\n\n`));
            controller.enqueue(encoder.encode(`data: ${JSON.stringify({ id: 'e9', kind: 'text_delta', occurred_at: 'x', turn_id: 'turn-1', text_delta: ' finished' })}\n\n`));
            controller.enqueue(encoder.encode('data: [DONE]\n\n'));
            controller.close();
          },
        }),
      }) as unknown as Response
    ));

    vi.mocked(runTurn)
      .mockImplementationOnce(async (_k: any, _p: any, cb: any) => {
        cb.onError('agent.Run: conflict: a run is already active for session "sess_live-9"', { conflict: true });
      })
      .mockImplementationOnce(async () => {});

    const { result } = renderHook(() => useChatRuntime('a1'));
    await send(result, 'any update?');

    // Drain the queue chain (hydrate → attach → [DONE] → re-dispatch).
    await act(async () => { await Promise.resolve(); });
    await act(async () => { await Promise.resolve(); });

    expect(runTurn).toHaveBeenCalledTimes(2);
    // The re-dispatch streams the SAME text, chained to the drained run.
    const [, retryParams]: any[] = vi.mocked(runTurn).mock.calls[1];
    expect(retryParams.input).toBe('any update?');
    expect(retryParams.previousResponseId).toBe('resp_sess_live-9_turn-1');
    expect(useStore.getState().ui.running).toBe(true);

    // The active run's tail streamed into the seeded message before the
    // queued turn took over.
    const msgs = activeSession().messages;
    expect(msgs[0].text).toBe('partial finished');
    // The conflict left no error card behind.
    expect(msgs.some((m: any) => m.author === 'error')).toBe(false);
  });

  it('a queued retry that conflicts again surfaces the error instead of looping', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.stubGlobal('fetch', vi.fn(async () =>
      ({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'text/event-stream' }),
        body: new ReadableStream<Uint8Array>({
          start(controller) {
            controller.enqueue(encoder.encode('data: [DONE]\n\n'));
            controller.close();
          },
        }),
      }) as unknown as Response
    ));

    vi.mocked(runTurn)
      .mockImplementationOnce(async (_k: any, _p: any, cb: any) => {
        cb.onError('agent.Run: conflict: a run is already active for session "sess_live-9"', { conflict: true });
      })
      .mockImplementationOnce(async (_k: any, _p: any, cb: any) => {
        cb.onError('agent.Run: conflict: a run is already active for session "sess_live-9"', { conflict: true });
      });

    const { result } = renderHook(() => useChatRuntime('a1'));
    await send(result, 'any update?');
    await act(async () => { await Promise.resolve(); });
    await act(async () => { await Promise.resolve(); });

    // Exactly one queue + one guarded retry — the second conflict is terminal.
    expect(runTurn).toHaveBeenCalledTimes(2);
    const msgs = activeSession().messages;
    expect(msgs.some((m: any) => m.author === 'error')).toBe(true);
    expect(useStore.getState().ui.running).toBe(false);
  });
});

describe('useChatRuntime — /compact command (chat-compact-command)', () => {
  const activeSession = (): any => {
    const th = useStore.getState().db.t1.threads.a1;
    return th.list.find((x: any) => x.id === th.active);
  };

  const send = (result: any, text: string) =>
    act(async () => {
      await result.current.onNew({ role: 'user', content: [{ type: 'text', text }] } as any);
    });

  beforeEach(() => {
    localStorage.clear();
    vi.mocked(runTurn).mockReset();
    vi.mocked(handleV1AuthFailure).mockReset();
    act(() => {
      const mkAgent = (id: string, name: string, tools: string[]) => ({
        id, name, model: 'claude-sonnet-5', temp: 0.4, autonomy: 'approval', channelPost: false,
        role: 'Test agent', status: 'idle', tools, skills: ['research'], lastActive: 'now', prompt: ''
      });
      useStore.setState({
        pos: { tenantId: 't1', view: 'chats', chatId: 'a1', showContext: false },
        ui: { configAgent: null, scheduleEdit: null, wsOpen: false, running: false, toasts: [] },
        db: {
          t1: {
            id: 't1', name: 'T1', sub: 't1', tz: 'America/Los_Angeles',
            defaultModel: 'claude-sonnet-5', retention: '90 days',
            people: [], schedules: [], runs: [], members: [], integrations: [], skillLib: [], keys: [],
            agents: [mkAgent('a1', 'Alice', ['search'])],
            channels: [{ id: 'c1', name: 'general', purpose: 'Team chat', agentId: '', unread: 0, members: ['a1'] }],
            threads: {
              // Bound session with a recorded response chain — a compact turn
              // binds via previous_response_id.
              a1: {
                active: 'sess_live-1',
                list: [{
                  id: 'sess_live-1', title: 'Chat', updated: '',
                  messages: [{ id: 'm0', author: 'agent', ts: '', text: 'prior', resp: 'resp_sess_live-1_t1' }],
                }],
              },
              c1: { active: 's2', list: [{ id: 's2', title: 'Chat', updated: '', messages: [] }] },
            },
          },
        },
      });
    });
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
  });

  it('intercepts /compact <focus>: command turn, focus as input, no user message, status flag on', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, '/compact keep the decisions');

    expect(runTurn).toHaveBeenCalledTimes(1);
    const [, params]: any[] = vi.mocked(runTurn).mock.calls[0];
    expect(params.input).toBe('keep the decisions');
    expect(params.command).toBe('compact');
    // Bind-only: chains to the existing response — never births a session.
    expect(params.previousResponseId).toBe('resp_sess_live-1_t1');
    expect(params.sessionId).toBeUndefined();
    // The command text never entered the message list and no optimistic
    // agent row was minted — only the prior message remains.
    const msgs = activeSession().messages;
    expect(msgs).toHaveLength(1);
    expect(msgs[0].id).toBe('m0');
    expect(useStore.getState().ui.running).toBe(true);
    expect(useStore.getState().ui.compacting).toBe(true);
  });

  it('bare /compact sends an empty (absent) focus as input', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, '/compact');

    const [, params]: any[] = vi.mocked(runTurn).mock.calls[0];
    expect(params.input).toBe('');
    expect(params.command).toBe('compact');
  });

  it('an unbound thread never births a session for a compact turn (bind-only, design D2)', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    act(() => {
      useStore.getState().updateTenant('t1', (t: any) => {
        t.threads.a1 = { active: 's1', list: [{ id: 's1', title: 'Chat', updated: '', messages: [] }] };
        return t;
      });
    });
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, '/compact');

    const [, params]: any[] = vi.mocked(runTurn).mock.calls[0];
    expect(params.sessionId).toBeUndefined();
    expect(params.previousResponseId).toBeUndefined();
    // No lazy migration happened — the session stays unbound.
    expect(activeSession().id).toBe('s1');
  });

  it('compacted event swaps the status for the divider; onDone records the chain on it', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, '/compact');
    const [, , cb]: any[] = vi.mocked(runTurn).mock.calls[0];

    await act(async () => { cb.onContextCompacted({ tokensBefore: 154000, tokensAfter: 9200 }); });

    // The status row retracts at the compacted event; the divider entry is
    // the only thing the turn leaves behind (no user pill, no agent row).
    expect(useStore.getState().ui.compacting).toBe(false);
    expect(useStore.getState().ui.running).toBe(true);
    let msgs = activeSession().messages;
    expect(msgs).toHaveLength(2);
    expect(msgs[1].author).toBe('compaction');
    expect(msgs[1].compaction).toEqual({ tokensBefore: 154000, tokensAfter: 9200 });
    expect(msgs[1].summarySaved).toBe(true);

    await act(async () => { cb.onDone('resp_sess_live-1_t9'); });

    expect(useStore.getState().ui.running).toBe(false);
    expect(useStore.getState().ui.compacting).toBe(false);
    // The compact turn is the new chain head: later turns chain to it.
    msgs = activeSession().messages;
    expect(msgs[1].resp).toBe('resp_sess_live-1_t9');
    expect(useStore.getState().getLastResponse('a1')).toBe('resp_sess_live-1_t9');
  });

  it('quiet completion (no compacted event) leaves nothing in the thread', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, '/compact');
    const [, , cb]: any[] = vi.mocked(runTurn).mock.calls[0];
    await act(async () => { cb.onDone('resp_sess_live-1_t9'); });

    expect(useStore.getState().ui.running).toBe(false);
    expect(useStore.getState().ui.compacting).toBe(false);
    // No divider (no compacted event), no agent row, no error entry.
    expect(activeSession().messages).toHaveLength(1);
  });

  it('failure retracts the status row and leaves nothing — no error entry', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, '/compact');
    const [, , cb]: any[] = vi.mocked(runTurn).mock.calls[0];
    await act(async () => { cb.onError('Error from provider: Rate limit exceeded', {}); });

    expect(useStore.getState().ui.running).toBe(false);
    expect(useStore.getState().ui.compacting).toBe(false);
    const msgs = activeSession().messages;
    expect(msgs).toHaveLength(1);
    expect(msgs.some((m: any) => m.author === 'error')).toBe(false);
  });

  it('unknown /foo sends as ordinary text in an agent chat (design D8)', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, '/foo list tools');

    expect(runTurn).toHaveBeenCalledTimes(1);
    const [, params]: any[] = vi.mocked(runTurn).mock.calls[0];
    expect(params.input).toBe('/foo list tools');
    expect(params.command).toBeUndefined();
    expect(activeSession().messages.some((m: any) => m.author === 'you' && m.text === '/foo list tools')).toBe(true);
  });

  it('channels are unaffected: /compact passes through as plain text with no command turn', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('c1'));

    await send(result, '/compact');

    // c1 has no bound agent — nothing runs; the text lands verbatim.
    expect(runTurn).not.toHaveBeenCalled();
    const msgs = useStore.getState().db.t1.threads.c1.list[0].messages;
    expect(msgs.some((m: any) => m.author === 'you' && m.text === '/compact')).toBe(true);
    expect(useStore.getState().ui.compacting).toBeFalsy();
  });
});

describe('useChatRuntime — attachment sends (add-chat-attachments D11/D12)', () => {
  const activeSession = (): any => {
    const th = useStore.getState().db.t1.threads.a1;
    return th.list.find((x: any) => x.id === th.active);
  };

  const send = (result: any, text: string, chips?: any[]) =>
    act(async () => {
      await result.current.onNew({ role: 'user', content: [{ type: 'text', text }] } as any, chips);
    });

  const readyChip = (over: Partial<any> = {}) => ({
    key: 'c1', id: 'att-1', name: 'shot.png', mime: 'image/png', size: 12,
    url: '/api/v1/files/k1', state: 'ready', progress: 100, ...over,
  });

  beforeEach(() => {
    localStorage.clear();
    vi.mocked(runTurn).mockReset();
    vi.mocked(handleV1AuthFailure).mockReset();
    act(() => {
      const mkAgent = (id: string, name: string, tools: string[]) => ({
        id, name, model: 'claude-sonnet-5', temp: 0.4, autonomy: 'approval', channelPost: false,
        role: 'Test agent', status: 'idle', tools, skills: ['research'], lastActive: 'now', prompt: ''
      });
      useStore.setState({
        pos: { tenantId: 't1', view: 'chats', chatId: 'a1', showContext: false },
        ui: { configAgent: null, scheduleEdit: null, wsOpen: false, running: false, toasts: [] },
        db: {
          t1: {
            id: 't1', name: 'T1', sub: 't1', tz: 'America/Los_Angeles',
            defaultModel: 'claude-sonnet-5', retention: '90 days',
            people: [], schedules: [], runs: [], members: [], integrations: [], skillLib: [], keys: [],
            agents: [mkAgent('a1', 'Alice', ['search'])],
            channels: [],
            threads: {
              a1: { active: 'sess_live-5', list: [{ id: 'sess_live-5', title: 'Chat', updated: '', messages: [] }] },
            },
          },
        },
      });
    });
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('onNew with ready chips pushes the entry carrying attachments and calls runTurn with them', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, 'what is this?', [readyChip()]);

    const user = activeSession().messages.find((m: any) => m.author === 'you');
    expect(user.attachments).toEqual([
      { id: 'att-1', name: 'shot.png', mime: 'image/png', size: 12, url: '/api/v1/files/k1' },
    ]);
    expect(runTurn).toHaveBeenCalledTimes(1);
    const [, params]: any[] = vi.mocked(runTurn).mock.calls[0];
    expect(params.attachments).toEqual(user.attachments);
    expect(params.input).toBe('what is this?');
  });

  it('only ready chips ride the entry and the turn (uploading/rejected are filtered)', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, 'mixed tray', [
      readyChip(),
      { key: 'c2', name: 'mid.png', mime: 'image/png', size: 5, state: 'uploading', progress: 40 },
      { key: 'c3', name: 'bad.docx', mime: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', size: 9, state: 'rejected', progress: 0, reason: 'Not supported — export as PDF' },
    ]);

    const user = activeSession().messages.find((m: any) => m.author === 'you');
    expect(user.attachments).toHaveLength(1);
    expect(user.attachments[0].name).toBe('shot.png');
    const [, params]: any[] = vi.mocked(runTurn).mock.calls[0];
    expect(params.attachments).toEqual(user.attachments);
  });

  it('attachment-only send (empty text, ≥1 ready chip) proceeds', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const { result } = renderHook(() => useChatRuntime('a1'));

    await send(result, '', [readyChip({ key: 'c9', id: 'att-9', name: 'report.pdf', mime: 'application/pdf', size: 5033164, url: '/api/v1/files/k2' })]);

    // The optimistic user entry lands WITH its chips — the visible content is
    // the attachment alone.
    const user = activeSession().messages.find((m: any) => m.author === 'you');
    expect(user.text).toBe('');
    expect(user.attachments).toEqual([
      { id: 'att-9', name: 'report.pdf', mime: 'application/pdf', size: 5033164, url: '/api/v1/files/k2' },
    ]);
    expect(runTurn).toHaveBeenCalledTimes(1);
    const [, params]: any[] = vi.mocked(runTurn).mock.calls[0];
    expect(params.input).toBe('');
    expect(params.attachments).toEqual(user.attachments);
  });

  it('the 409-conflict queued re-dispatch carries the same attachments', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    const encoder = new TextEncoder();
    vi.stubGlobal('fetch', vi.fn(async () =>
      ({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'text/event-stream' }),
        body: new ReadableStream<Uint8Array>({
          start(controller) {
            controller.enqueue(encoder.encode('data: [DONE]\n\n'));
            controller.close();
          },
        }),
      }) as unknown as Response
    ));

    vi.mocked(runTurn)
      .mockImplementationOnce(async (_k: any, _p: any, cb: any) => {
        cb.onError('agent.Run: conflict: a run is already active for session "sess_live-5"', { conflict: true });
      })
      .mockImplementationOnce(async () => {});

    const { result } = renderHook(() => useChatRuntime('a1'));
    await send(result, 'look at this', [readyChip()]);

    // Drain the queue chain (hydrate → attach → [DONE] → re-dispatch).
    await act(async () => { await Promise.resolve(); });
    await act(async () => { await Promise.resolve(); });

    expect(runTurn).toHaveBeenCalledTimes(2);
    const [, retryParams]: any[] = vi.mocked(runTurn).mock.calls[1];
    expect(retryParams.input).toBe('look at this');
    expect(retryParams.attachments).toEqual([
      { id: 'att-1', name: 'shot.png', mime: 'image/png', size: 12, url: '/api/v1/files/k1' },
    ]);
  });

  it('regenerate re-sends the stored user entry attachment references (never re-uploads)', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const stored = [{ id: 'att-1', name: 'shot.png', mime: 'image/png', size: 12, url: '/api/v1/files/k1' }];
    act(() => {
      useStore.getState().updateTenant('t1', (t: any) => {
        const s = t.threads.a1.list[0];
        s.messages = [
          { id: 'u1', author: 'you', ts: '', text: 'see this', attachments: stored },
          { id: 'g1', author: 'agent', ts: '', text: 'old reply', resp: 'resp_sess_live-5_t1' },
        ];
        return t;
      });
    });
    const { result } = renderHook(() => useChatRuntime('a1'));

    await act(async () => {
      await result.current.onReload('g1');
    });

    expect(runTurn).toHaveBeenCalledTimes(1);
    const [, params]: any[] = vi.mocked(runTurn).mock.calls[0];
    expect(params.input).toBe('see this');
    // The references come straight from the stored entry — same urls/ids.
    expect(params.attachments).toEqual(stored);
  });

  it('regenerate works for an attachment-only user turn (empty text)', async () => {
    localStorage.setItem('onclaw.api_key.t1', 'k-live');
    vi.mocked(runTurn).mockImplementation(async () => {});
    const stored = [{ id: 'att-9', name: 'report.pdf', mime: 'application/pdf', size: 5033164, url: '/api/v1/files/k2' }];
    act(() => {
      useStore.getState().updateTenant('t1', (t: any) => {
        const s = t.threads.a1.list[0];
        s.messages = [
          { id: 'u1', author: 'you', ts: '', text: '', attachments: stored },
          { id: 'g1', author: 'agent', ts: '', text: 'old reply', resp: 'resp_sess_live-5_t1' },
        ];
        return t;
      });
    });
    const { result } = renderHook(() => useChatRuntime('a1'));

    await act(async () => {
      await result.current.onReload('g1');
    });

    expect(runTurn).toHaveBeenCalledTimes(1);
    const [, params]: any[] = vi.mocked(runTurn).mock.calls[0];
    expect(params.input).toBe('');
    expect(params.attachments).toEqual(stored);
  });
});
