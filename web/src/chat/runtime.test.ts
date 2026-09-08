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
        ui: { configAgent: null, cronEdit: null, wsOpen: false, running: false, toasts: [] },
        db: {
          t1: {
            id: 't1',
            name: 'T1', sub: 't1', tz: 'America/Los_Angeles',
            defaultModel: 'claude-sonnet-5', retention: '90 days',
            people: [], cron: [], runs: [], members: [], integrations: [], skillLib: [], keys: [],
            agents: [mkAgent('a1', 'Alice', ['search']), mkAgent('a2', 'Bob', [])],
            // agentId is '' (not 'a1') so no agent auto-responds to plain
            // channel messages — the no-turn and DM-only tests rely on it.
            channels: [{ id: 'c1', name: 'general', purpose: 'Team chat', agentId: '', unread: 0, members: ['a1', 'a2'] }],
            threads: {
              a1: { active: 's1', list: [{ id: 's1', title: 'Chat', updated: '', messages: [{
                id: 'm1', text: 'hello', author: 'agent', ts: '2026-08-27T00:00:00Z',
                agentId: 'a1', cron: 'cron1', name: 'Agent 1',
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

  it('conversion round-trip maintains identity/cron/tools', () => {
    const { result } = renderHook(() => useChatRuntime('a1'));
    const converted: any = result.current.convertMessage(useStore.getState().db.t1.threads.a1.list[0].messages[0]);
    
    expect(converted.id).toBe('m1');
    expect(converted.role).toBe('assistant');
    expect(converted.metadata.custom.onclaw.agentId).toBe('a1');
    expect(converted.metadata.custom.onclaw.cron).toBe('cron1');
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
        ui: { configAgent: null, cronEdit: null, wsOpen: false, running: false, toasts: [] },
        db: {
          t1: {
            id: 't1',
            name: 'T1', sub: 't1', tz: 'America/Los_Angeles',
            defaultModel: 'claude-sonnet-5', retention: '90 days',
            people: [], cron: [], runs: [], members: [], integrations: [], skillLib: [], keys: [],
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

  it('/reset mints a fresh session id so the next turn births a new session (local clear only)', async () => {
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

    const after = activeSession();
    expect(after.messages).toEqual([]);
    expect(after.sess).toMatch(/^sess_/);
    expect(after.sess).not.toBe('sess_old-1');

    await send(result, 'fresh start');

    expect(runTurn).toHaveBeenCalledTimes(1);
    const [, params]: any[] = vi.mocked(runTurn).mock.calls[0];
    // Post-reset: birth again — fresh session metadata, no chaining across
    // the cleared conversation.
    expect(params.sessionId).toBe(after.sess);
    expect(params.previousResponseId).toBeUndefined();
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
        ui: { configAgent: null, cronEdit: null, wsOpen: false, running: false, toasts: [] },
        db: {
          t1: {
            id: 't1', name: 'T1', sub: 't1', tz: 'America/Los_Angeles',
            defaultModel: 'claude-sonnet-5', retention: '90 days',
            people: [], cron: [], runs: [], members: [], integrations: [], skillLib: [], keys: [],
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
