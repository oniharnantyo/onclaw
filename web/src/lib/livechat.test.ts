/**
 * @vitest-environment jsdom
 */
import { describe, it, expect, vi } from 'vitest';
import { fetchSessionTranscript } from './livechat';
import { api } from './api';

vi.mock('../lib/api', () => ({
  api: {
    onUnauthorized: () => () => {},
    agents: {
      sessionEvents: vi.fn().mockResolvedValue({
        events: [
          { id: 'e1', kind: 'message_completed', occurred_at: 't0', turn_id: 'turn-1',
            message: { role: 'user', content: 'hello' } },
          { id: 'e2', kind: 'message_completed', occurred_at: 't1', turn_id: 'turn-1',
            message: { role: 'assistant', content: 'first reply' } },
          { id: 'e3', kind: 'tool_call_started', occurred_at: 't2', turn_id: 'turn-2',
            tool_call: { call_id: 'c1', name: 'files.write', arguments: 'x' } },
          { id: 'e4', kind: 'tool_call_finished', occurred_at: 't3', turn_id: 'turn-2',
            tool_result: { call_id: 'c1', result: 'ok', latency: 1500000 } },
          { id: 'e5', kind: 'message_completed', occurred_at: 't4', turn_id: 'turn-2',
            message: { role: 'assistant', content: 'second reply' } },
        ],
      }),
    },
  },
}));

// This environment's jsdom exposes no localStorage; the store module touches
// it at boot (thread persistence) — install a minimal stub.
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

describe('fetchSessionTranscript', () => {
  it('folds events per turn and rebuilds the turn response id for chaining', async () => {
    const { messages } = await fetchSessionTranscript('ws1', 'atlas', 'sess_h-1');

    // user + agent per turn, two turns
    expect(messages.map((m: any) => m.author)).toEqual(['you', 'agent', 'agent']);
    expect(messages[0].text).toBe('hello');
    expect(messages[1].text).toBe('first reply');
    expect(messages[2].text).toBe('second reply');
    expect(messages[2].tools).toHaveLength(1);

    // The chain link: each agent message carries resp_<session>_<turn> so the
    // next turn after a reload chains instead of birthing a fresh session.
    expect(messages[1].resp).toBe('resp_sess_h-1_turn-1');
    expect(messages[2].resp).toBe('resp_sess_h-1_turn-2');
  });

  it('hydrates reasoning as ordered parts interleaved with tool cards', async () => {
    vi.mocked(api.agents.sessionEvents).mockResolvedValueOnce({
      next: '',
      events: [
        { id: 'e1', kind: 'message_completed', occurred_at: 't0', turn_id: 'turn-1',
          message: { role: 'user', content: 'fetch the blog' } },
        { id: 'e2', kind: 'message_completed', occurred_at: 't1', turn_id: 'turn-1',
          message: { role: 'assistant', content: "I'll fetch it.", reasoning_content: 'need the page.' } },
        { id: 'e3', kind: 'tool_call_started', occurred_at: 't2', turn_id: 'turn-1',
          tool_call: { call_id: 'c1', name: 'web.fetch', arguments: '{"url":"https://x"}' } },
        { id: 'e4', kind: 'tool_call_finished', occurred_at: 't3', turn_id: 'turn-1',
          tool_result: { call_id: 'c1', result: '<html>', latency: 410000000 } },
        { id: 'e5', kind: 'message_completed', occurred_at: 't4', turn_id: 'turn-1',
          message: { role: 'assistant', content: 'Here is the summary.', reasoning_content: 'got it, summarizing.' } },
      ],
    });
    const { messages } = await fetchSessionTranscript('ws1', 'atlas', 'sess_h-2');

    const agent = messages[1];
    expect(agent.tools).toHaveLength(1);
    expect(agent.parts).toEqual([
      { k: 'reasoning', text: 'need the page.' },
      { k: 'tool', i: 0 },
      { k: 'reasoning', text: 'got it, summarizing.' },
    ]);
  });
});

describe('fetchSessionTranscript — context meter restore (chat-context-meter)', () => {
  it('surfaces the LAST turn_completed usage.final_input_tokens as finalInputTokens', async () => {
    vi.mocked(api.agents.sessionEvents).mockResolvedValueOnce({
      next: '',
      events: [
        { id: 'e1', kind: 'turn_started', occurred_at: 't0', turn_id: 'turn-1' },
        { id: 'e2', kind: 'turn_completed', occurred_at: 't1', turn_id: 'turn-1',
          usage: { input_tokens: 1000, output_tokens: 100, total_tokens: 1100, final_input_tokens: 1000 } },
        { id: 'e3', kind: 'message_completed', occurred_at: 't2', turn_id: 'turn-2',
          message: { role: 'user', content: 'again' } },
        { id: 'e4', kind: 'turn_completed', occurred_at: 't3', turn_id: 'turn-2',
          usage: { input_tokens: 2000, output_tokens: 50, total_tokens: 2050, final_input_tokens: 2000 } },
      ],
    });

    const hydrated = await fetchSessionTranscript('ws1', 'atlas', 'sess_h-usage');
    expect(hydrated.finalInputTokens).toBe(2000);
  });

  it('leaves finalInputTokens undefined when no turn_completed carries usage', async () => {
    vi.mocked(api.agents.sessionEvents).mockResolvedValueOnce({
      next: '',
      events: [
        { id: 'e1', kind: 'message_completed', occurred_at: 't0', turn_id: 'turn-1',
          message: { role: 'user', content: 'hello' } },
        { id: 'e2', kind: 'turn_completed', occurred_at: 't1', turn_id: 'turn-1' },
      ],
    });

    const hydrated = await fetchSessionTranscript('ws1', 'atlas', 'sess_h-nousage');
    expect(hydrated.finalInputTokens).toBeUndefined();
  });
});
