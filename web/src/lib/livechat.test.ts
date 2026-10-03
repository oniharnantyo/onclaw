/**
 * @vitest-environment jsdom
 */
import { describe, it, expect, vi, afterEach } from 'vitest';
import { fetchSessionTranscript, streamSessionEvents, attachCatchUpStream, abortCatchUpStream,
  isBoundSessionId, hydrateSession, applyServerTranscript,
} from './livechat';
import { useStore } from '../store';
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
  // streamSessionEvents authenticates exactly like request(): JWT bearer.
  API_ORIGIN: '',
  getToken: () => 'jwt-test-token',
  // Named exports ../store (imported below for the attach tests) needs.
  pollAgentPromptsStatus: async () => ({}),
  formatApiError: (_e: unknown, fallback: string) => fallback,
  setToken: () => {},
  clearToken: () => {},
  ApiError: class ApiError extends Error {},
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

describe('isBoundSessionId (run sessions hydrate like any session, integrate-scheduler D7)', () => {
  it('accepts interactive sess_ ids and scheduler-run sched_ ids', () => {
    expect(isBoundSessionId('sess_abc-123')).toBe(true);
    expect(isBoundSessionId('sched_sch-1_1725996000')).toBe(true);
    expect(isBoundSessionId(null)).toBe(false);
    expect(isBoundSessionId(undefined)).toBe(false);
    expect(isBoundSessionId('s1')).toBe(false);
    expect(isBoundSessionId('random')).toBe(false);
  });
});

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

  it('skips empty user/assistant echoes so hydrated turns never mint blank bubbles', async () => {
    vi.mocked(api.agents.sessionEvents).mockResolvedValueOnce({
      next: '',
      events: [
        { id: 'e1', kind: 'message_completed', occurred_at: 't0', turn_id: 'turn-1',
          message: { role: 'user', content: 'give me full article' } },
        // Tool loop: text-less assistant request + tool result persisted under
        // role user — neither may hydrate as a message (they render as blank
        // bubbles and split the turn).
        { id: 'e2', kind: 'tool_call_started', occurred_at: 't1', turn_id: 'turn-1',
          tool_call: { call_id: 'c1', name: 'browser.read', arguments: '{}' } },
        { id: 'e3', kind: 'message_completed', occurred_at: 't2', turn_id: 'turn-1',
          message: { role: 'user', content: '' } },
        { id: 'e4', kind: 'message_completed', occurred_at: 't3', turn_id: 'turn-1',
          message: { role: 'assistant', content: '' } },
        { id: 'e5', kind: 'tool_call_finished', occurred_at: 't4', turn_id: 'turn-1',
          tool_result: { call_id: 'c1', result: 'page body', latency: 1000000 } },
        { id: 'e6', kind: 'message_completed', occurred_at: 't5', turn_id: 'turn-1',
          message: { role: 'assistant', content: 'Here it is.' } },
        // A turn made solely of empty echoes contributes nothing at all.
        { id: 'f1', kind: 'message_completed', occurred_at: 't6', turn_id: 'turn-2',
          message: { role: 'user', content: '' } },
        { id: 'f2', kind: 'message_completed', occurred_at: 't7', turn_id: 'turn-2',
          message: { role: 'assistant', content: '' } },
      ],
    });
    const { messages } = await fetchSessionTranscript('ws1', 'atlas', 'sess_h-skip');

    expect(messages.map((m: any) => m.author)).toEqual(['you', 'agent']);
    expect(messages[0].text).toBe('give me full article');
    expect(messages[1].text).toBe('Here it is.');
    expect(messages[1].tools).toHaveLength(1);
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

  it('hydrates the last turn input/output tokens and server breakdown from turn_completed usage', async () => {
    vi.mocked(api.agents.sessionEvents).mockResolvedValueOnce({
      next: '',
      events: [
        { id: 'e1', kind: 'turn_completed', occurred_at: 't0', turn_id: 'turn-1',
          usage: { input_tokens: 1000, output_tokens: 100, total_tokens: 1100, final_input_tokens: 1000 } },
        { id: 'e2', kind: 'turn_completed', occurred_at: 't1', turn_id: 'turn-2',
          usage: {
            input_tokens: 68_000, output_tokens: 1_200, total_tokens: 69_200, final_input_tokens: 68_000,
            context_breakdown: { instructions: 1_200, tools: 3_400, conversation: 12_000 },
          } },
      ],
    });

    const hydrated = await fetchSessionTranscript('ws1', 'atlas', 'sess_h-turndetail');
    // LAST terminal event wins, matching the finalInputTokens rule.
    expect(hydrated.turnInputTokens).toBe(68_000);
    expect(hydrated.turnOutputTokens).toBe(1_200);
    expect(hydrated.contextBreakdown).toEqual({ instructions: 1_200, tools: 3_400, conversation: 12_000 });
  });

  it('leaves turn input/output and breakdown undefined when the wire did not report them', async () => {
    vi.mocked(api.agents.sessionEvents).mockResolvedValueOnce({
      next: '',
      events: [
        { id: 'e1', kind: 'turn_completed', occurred_at: 't0', turn_id: 'turn-1',
          usage: { final_input_tokens: 900 } },
      ],
    });

    const hydrated = await fetchSessionTranscript('ws1', 'atlas', 'sess_h-nodetail');
    expect(hydrated.finalInputTokens).toBe(900);
    expect(hydrated.turnInputTokens).toBeUndefined();
    expect(hydrated.turnOutputTokens).toBeUndefined();
    expect(hydrated.contextBreakdown).toBeUndefined();
  });
});

describe('fetchSessionTranscript — live delta vocabulary (catch-up stream)', () => {
  it('folds live text/reasoning deltas into the turn body like the runtime does', async () => {
    vi.mocked(api.agents.sessionEvents).mockResolvedValueOnce({
      next: '',
      events: [
        { id: 'e1', kind: 'message_completed', occurred_at: 't0', turn_id: 'turn-1',
          message: { role: 'user', content: 'catch me up' } },
        { id: 'e2', kind: 'text_delta', occurred_at: 't1', turn_id: 'turn-1', text_delta: 'Hel' },
        { id: 'e3', kind: 'text_delta', occurred_at: 't2', turn_id: 'turn-1', text_delta: 'lo there' },
        { id: 'e4', kind: 'reasoning_delta', occurred_at: 't3', turn_id: 'turn-1', reasoning_delta: 'thinking…' },
        { id: 'e5', kind: 'tool_call_started', occurred_at: 't4', turn_id: 'turn-1',
          tool_call: { call_id: 'c1', name: 'files.write', arguments: '{}' } },
        { id: 'e6', kind: 'turn_completed', occurred_at: 't5', turn_id: 'turn-1',
          usage: { final_input_tokens: 900 } },
      ],
    });

    const { messages, finalInputTokens, lastEventId } = await fetchSessionTranscript('ws1', 'atlas', 'sess_h-delta');

    // Deltas fold into ONE agent message; the reasoning delta becomes an
    // ordered part, exactly like live streaming renders it.
    expect(messages.map((m: any) => m.author)).toEqual(['you', 'agent']);
    expect(messages[1].text).toBe('Hello there');
    expect(messages[1].parts).toEqual([{ k: 'reasoning', text: 'thinking…' }, { k: 'tool', i: 0 }]);
    expect(messages[1].tools).toHaveLength(1);
    expect(finalInputTokens).toBe(900);
    // The last event seen — any kind — is the catch-up cursor (D4).
    expect(lastEventId).toBe('e6');
  });
});

// ---------------------------------------------------------------------------
// prompt_blocked — hook enforcement transcript entries (integrate-agent-hooks)
// ---------------------------------------------------------------------------

describe('fetchSessionTranscript — prompt_blocked notices', () => {
  it('hydrates a blocked prompt as a standalone notice between the user message and the turn terminal', async () => {
    vi.mocked(api.agents.sessionEvents).mockResolvedValueOnce({
      next: '',
      events: [
        { id: 'e1', kind: 'message_completed', occurred_at: 't0', turn_id: 'turn-1',
          message: { role: 'user', content: 'wipe the production database' } },
        // The hook block: the model never ran; a well-formed terminal follows.
        { id: 'e2', kind: 'prompt_blocked', occurred_at: 't1', turn_id: 'turn-1',
          prompt_blocked: { hook: 'Compliance Gate', reason: 'destructive prompts require approval' } },
        { id: 'e3', kind: 'turn_completed', occurred_at: 't2', turn_id: 'turn-1' },
      ],
    });

    const { messages } = await fetchSessionTranscript('ws1', 'atlas', 'sess_h-blocked');

    expect(messages.map((m: any) => m.author)).toEqual(['you', 'notice']);
    expect(messages[1].notice).toEqual({ hook: 'Compliance Gate', reason: 'destructive prompts require approval' });
    expect(messages[1].text).toBe('');
  });

  it('hydrates a service-run write escalation with the tool payload intact (add-integration-authority)', async () => {
    vi.mocked(api.agents.sessionEvents).mockResolvedValueOnce({
      next: '',
      events: [
        { id: 'e1', kind: 'message_completed', occurred_at: 't0', turn_id: 'turn-1',
          message: { role: 'user', content: 'PR 42 opened' } },
        // The webhook run paused at a write-tier connection tool: the approval
        // event carries the escalation instead of a shell command.
        { id: 'e2', kind: 'approval_required', occurred_at: 't1', turn_id: 'turn-1',
          approval: {
            interrupt_id: 'int-gh-1',
            tool: {
              name: 'github.merge_pull_request',
              service: 'github',
              service_name: 'GitHub',
              connection_id: 'conn-gh',
              tier: 'write',
            },
          } },
      ],
    });

    const { messages, pendingInterruptIds } = await fetchSessionTranscript('ws1', 'atlas', 'sess_h-esc');

    const agent = messages.find((m: any) => m.author === 'agent');
    expect(agent).toBeTruthy();
    const card = agent.tools.find((t: any) => t.approval);
    expect(card).toBeTruthy();
    // The escalation rides the card untouched — the transcript card branches
    // on its presence and renders server-declared truth.
    expect(card.approval.interruptId).toBe('int-gh-1');
    expect(card.approval.tool).toEqual({
      name: 'github.merge_pull_request',
      service: 'github',
      service_name: 'GitHub',
      connection_id: 'conn-gh',
      tier: 'write',
    });
    // Never followed by turn activity → the interrupt is still pending.
    expect(pendingInterruptIds).toContain('int-gh-1');
  });

  it('mints the same notice shape from the live catch-up stream (identical rendering after reload)', async () => {
    const seed = () => {
      const db: any = {
        ws1: {
          id: 'ws1', name: 'WS', sub: 'ws1', tz: 'UTC',
          agents: [], channels: [], people: [], schedules: [], runs: [], members: [], integrations: [], skillLib: [], keys: [],
          threads: { 'chat-1': { active: 'sess_cu2', list: [{ id: 'sess_cu2', title: 'Live', updated: '', messages: [] }] } },
        },
      };
      useStore.setState({
        db,
        ui: { configAgent: null, scheduleEdit: null, wsOpen: false, running: false, runningChatId: null, toasts: [] },
      });
    };
    seed();
    const encoder2 = new TextEncoder();
    const frame = (ev: any) => `data: ${JSON.stringify(ev)}\n\n`;
    vi.stubGlobal('fetch', vi.fn(async () =>
      ({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'text/event-stream' }),
        body: new ReadableStream<Uint8Array>({
          start(controller) {
            controller.enqueue(encoder2.encode(frame({ id: 'x1', kind: 'prompt_blocked', occurred_at: 't1', turn_id: 'turn-9', prompt_blocked: { hook: 'Gate', reason: 'nope' } })));
            controller.enqueue(encoder2.encode('data: [DONE]\n\n'));
            controller.close();
          },
        }),
      } as unknown as Response)
    ));

    attachCatchUpStream({
      workspaceId: 'ws1', agentSlug: 'atlas', chatId: 'chat-1', sessionId: 'sess_cu2',
    });

    await vi.waitFor(() => {
      const sess = useStore.getState().db.ws1.threads['chat-1'].list.find((x: any) => x.id === 'sess_cu2');
      expect(sess.messages).toHaveLength(1);
    });
    const sess = useStore.getState().db.ws1.threads['chat-1'].list.find((x: any) => x.id === 'sess_cu2');
    const noticeMsg = sess.messages[0] as any;
    expect(noticeMsg.author).toBe('notice');
    expect(noticeMsg.notice).toEqual({ hook: 'Gate', reason: 'nope' });
    expect(useStore.getState().ui.running).toBe(false);
    vi.unstubAllGlobals();
  });
});

describe('fetchSessionTranscript — memory_ingested chip (integrate-agent-zero-memory D11)', () => {
  it('hydrates the chip as a standalone post-turn entry carrying counts, never content', async () => {
    vi.mocked(api.agents.sessionEvents).mockResolvedValueOnce({
      next: '',
      events: [
        { id: 'e1', kind: 'message_completed', occurred_at: 't0', turn_id: 'turn-1',
          message: { role: 'user', content: 'remember the Stripe migration' } },
        { id: 'e2', kind: 'memory_ingested', occurred_at: 't1', turn_id: 'turn-1',
          memory_ingested: { note_ids: ['n-1', 'n-2'], event_ids: ['g-1'], counts: { shared: 2, user: 1, agent: 0 } } },
        { id: 'e3', kind: 'turn_completed', occurred_at: 't2', turn_id: 'turn-1' },
      ],
    });

    const { messages } = await fetchSessionTranscript('ws1', 'atlas', 'sess_h-mem');

    // The chip is its own post-turn entry after the user message.
    expect(messages.map((m: any) => m.author)).toEqual(['you', 'memory']);
    expect(messages[1].memory).toEqual({
      noteIds: ['n-1', 'n-2'],
      eventIds: ['g-1'],
      counts: { shared: 2, user: 1, agent: 0 },
    });
    expect(messages[1].text).toBe('');
  });

  it('mints the same chip shape from the live catch-up stream (identical rendering after reload)', async () => {
    const seed = () => {
      const db: any = {
        ws1: {
          id: 'ws1', name: 'WS', sub: 'ws1', tz: 'UTC',
          agents: [], channels: [], people: [], schedules: [], runs: [], members: [], integrations: [], skillLib: [], keys: [],
          threads: { 'chat-1': { active: 'sess_cu3', list: [{ id: 'sess_cu3', title: 'Live', updated: '', messages: [] }] } },
        },
      };
      useStore.setState({
        db,
        ui: { configAgent: null, scheduleEdit: null, wsOpen: false, running: false, runningChatId: null, toasts: [] },
      });
    };
    seed();
    const enc = new TextEncoder();
    const fr = (ev: any) => `data: ${JSON.stringify(ev)}\n\n`;
    vi.stubGlobal('fetch', vi.fn(async () =>
      ({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'text/event-stream' }),
        body: new ReadableStream<Uint8Array>({
          start(controller) {
            controller.enqueue(enc.encode(fr({ id: 'm1', kind: 'memory_ingested', occurred_at: 't1', turn_id: 'turn-9',
              memory_ingested: { note_ids: ['n-9'], event_ids: [], counts: { shared: 0, user: 1, agent: 0 } } })));
            controller.enqueue(enc.encode('data: [DONE]\n\n'));
            controller.close();
          },
        }),
      } as unknown as Response)
    ));

    attachCatchUpStream({
      workspaceId: 'ws1', agentSlug: 'atlas', chatId: 'chat-1', sessionId: 'sess_cu3',
    });

    await vi.waitFor(() => {
      const sess = useStore.getState().db.ws1.threads['chat-1'].list.find((x: any) => x.id === 'sess_cu3');
      expect(sess.messages).toHaveLength(1);
    });
    const sess = useStore.getState().db.ws1.threads['chat-1'].list.find((x: any) => x.id === 'sess_cu3');
    const chip = sess.messages[0] as any;
    expect(chip.author).toBe('memory');
    expect(chip.memory.counts).toEqual({ shared: 0, user: 1, agent: 0 });
    expect(useStore.getState().ui.running).toBe(false);
    vi.unstubAllGlobals();
  });
});

describe('fetchSessionTranscript — skill_candidate chip (add-skill-curation-from-traces)', () => {
  it('hydrates the chip as its own post-turn entry carrying the run coordinates', async () => {
    vi.mocked(api.agents.sessionEvents).mockResolvedValueOnce({
      next: '',
      events: [
        { id: 'e1', kind: 'message_completed', occurred_at: 't0', turn_id: 'turn-1',
          message: { role: 'user', content: 'run the deploy' } },
        { id: 'e2', kind: 'skill_candidate', occurred_at: 't1', turn_id: 'turn-1',
          skill_candidate: { workspace_id: 'ws1', agent_id: 'ag-1', session_id: 'sess_h-skc', run_id: 'turn-1',
            cluster_id: 'cl-deploy', skill_name: 'deploy-rollout', candidate_id: 'cand-1' } },
        { id: 'e3', kind: 'turn_completed', occurred_at: 't2', turn_id: 'turn-1' },
      ],
    });

    const { messages } = await fetchSessionTranscript('ws1', 'atlas', 'sess_h-skc');

    // The chip is its own post-turn entry after the user message, mapped to
    // the camelCase shape the chip component reads.
    expect(messages.map((m: any) => m.author)).toEqual(['you', 'skill_candidate']);
    expect(messages[1].skillCandidate).toEqual({
      workspaceId: 'ws1',
      agentId: 'ag-1',
      sessionId: 'sess_h-skc',
      runId: 'turn-1',
      clusterId: 'cl-deploy',
      skillName: 'deploy-rollout',
      candidateId: 'cand-1',
    });
    expect(messages[1].text).toBe('');
  });

  it('mints the same chip shape from the live catch-up stream (identical rendering after reload)', async () => {
    const seed = () => {
      const db: any = {
        ws1: {
          id: 'ws1', name: 'WS', sub: 'ws1', tz: 'UTC',
          agents: [], channels: [], people: [], schedules: [], runs: [], members: [], integrations: [], skillLib: [], keys: [],
          threads: { 'chat-1': { active: 'sess_cu4', list: [{ id: 'sess_cu4', title: 'Live', updated: '', messages: [] }] } },
        },
      };
      useStore.setState({
        db,
        ui: { configAgent: null, scheduleEdit: null, wsOpen: false, running: false, runningChatId: null, toasts: [] },
      });
    };
    seed();
    const enc = new TextEncoder();
    const fr = (ev: any) => `data: ${JSON.stringify(ev)}\n\n`;
    vi.stubGlobal('fetch', vi.fn(async () =>
      ({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'text/event-stream' }),
        body: new ReadableStream<Uint8Array>({
          start(controller) {
            controller.enqueue(enc.encode(fr({ id: 'k1', kind: 'skill_candidate', occurred_at: 't1', turn_id: 'turn-9',
              skill_candidate: { workspace_id: 'ws1', agent_id: 'ag-1', session_id: 'sess_cu4', run_id: 'turn-9',
                cluster_id: 'cl-deploy', skill_name: '', candidate_id: '' } })));
            controller.enqueue(enc.encode('data: [DONE]\n\n'));
            controller.close();
          },
        }),
      } as unknown as Response)
    ));

    attachCatchUpStream({
      workspaceId: 'ws1', agentSlug: 'atlas', chatId: 'chat-1', sessionId: 'sess_cu4',
    });

    await vi.waitFor(() => {
      const sess = useStore.getState().db.ws1.threads['chat-1'].list.find((x: any) => x.id === 'sess_cu4');
      expect(sess.messages).toHaveLength(1);
    });
    const sess = useStore.getState().db.ws1.threads['chat-1'].list.find((x: any) => x.id === 'sess_cu4');
    const chip = sess.messages[0] as any;
    expect(chip.author).toBe('skill_candidate');
    expect(chip.skillCandidate).toEqual({
      workspaceId: 'ws1', agentId: 'ag-1', sessionId: 'sess_cu4', runId: 'turn-9',
      clusterId: 'cl-deploy', skillName: '', candidateId: '',
    });
    expect(useStore.getState().ui.running).toBe(false);
    vi.unstubAllGlobals();
  });
});

// ---------------------------------------------------------------------------
// streamSessionEvents — resilient SSE consumer for the catch-up stream (D4)
// ---------------------------------------------------------------------------

const encoder = new TextEncoder();

/** Minimal SSE Response: fetch is mocked, so only what the consumer reads is
 * provided — ok/status/headers/body. `open` keeps the stream unclosed (only
 * an abort can unblock the reader). */
function sseResponse(
  chunks: string[],
  init?: { status?: number; contentType?: string | null; open?: boolean }
): Response {
  const status = init?.status ?? 200;
  const headers = new Headers();
  if (init?.contentType !== null) headers.set('Content-Type', init?.contentType ?? 'text/event-stream');
  return {
    ok: status >= 200 && status < 300,
    status,
    headers,
    body: new ReadableStream<Uint8Array>({
      start(controller) {
        for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
        if (!init?.open) controller.close();
      },
    }),
  } as unknown as Response;
}

const frame = (ev: any) => `data: ${JSON.stringify(ev)}\n\n`;

describe('streamSessionEvents', () => {
  it('parses chunked SSE — including lines split across chunks — firing onEvent per event and onDone on [DONE]', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      sseResponse([
        // First event's JSON is split mid-line across three chunks.
        'data: {"id":"s1","kind":"message_completed","turn_id":"t1","message":{"role":"assistant","content":"Hel',
        'lo"}}\n\ndata: {"id":"s2","kind":"tu',
        'rn_completed","turn_id":"t1","usage":{"final_input_tokens":42}}\n\n',
        frame({ id: 's3', kind: 'text_delta', turn_id: 't1', text_delta: '!'}),
        'data: [DONE]\n\n',
      ])
    );
    vi.stubGlobal('fetch', fetchMock);

    const events: any[] = [];
    const onDone = vi.fn();
    const onError = vi.fn();
    await streamSessionEvents({
      workspaceId: 'ws1',
      agentSlug: 'atlas',
      sessionId: 'sess_sse-1',
      after: 'e0',
      onEvent: (ev) => events.push(ev),
      onDone,
      onError,
    });

    expect(events.map((e) => e.id)).toEqual(['s1', 's2', 's3']);
    expect(events[0].message.content).toBe('Hello');
    expect(onDone).toHaveBeenCalledTimes(1);
    expect(onError).not.toHaveBeenCalled();

    // Auth + cursor match the existing call conventions (JWT bearer, after=).
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toBe(
      '/api/v1/workspaces/ws1/agents/atlas/sessions/sess_sse-1/events?stream=true&after=e0'
    );
    expect(init.headers.Authorization).toBe('Bearer jwt-test-token');
    vi.unstubAllGlobals();
  });

  it('treats a plain stream end (no [DONE]) as completion', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(sseResponse([frame({ id: 's1', kind: 'turn_started', turn_id: 't1' })])));

    const onDone = vi.fn();
    const onError = vi.fn();
    await streamSessionEvents({
      workspaceId: 'ws1',
      agentSlug: 'atlas',
      sessionId: 'sess_sse-2',
      onEvent: () => {},
      onDone,
      onError,
    });

    expect(onDone).toHaveBeenCalledTimes(1);
    expect(onError).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });

  it('honors abort: clean teardown without onDone or onError', async () => {
    // A stream that stays open after its first frame — only the abort can
    // unblock the pending read.
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        sseResponse([frame({ id: 's1', kind: 'turn_started', turn_id: 't1' })], { open: true })
      )
    );
    const controller = new AbortController();
    const onEvent = vi.fn();
    const onDone = vi.fn();
    const onError = vi.fn();

    const settled = streamSessionEvents({
      workspaceId: 'ws1',
      agentSlug: 'atlas',
      sessionId: 'sess_sse-3',
      signal: controller.signal,
      onEvent,
      onDone,
      onError,
    });
    // Wait until the first event landed and the reader is parked on read().
    await vi.waitFor(() => expect(onEvent).toHaveBeenCalledTimes(1));
    controller.abort();
    await settled;

    expect(onDone).not.toHaveBeenCalled();
    expect(onError).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });

  it('reports network failure via onError', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('fetch failed')));

    const onDone = vi.fn();
    const onError = vi.fn();
    await streamSessionEvents({
      workspaceId: 'ws1',
      agentSlug: 'atlas',
      sessionId: 'sess_sse-4',
      onEvent: () => {},
      onDone,
      onError,
    });

    expect(onError).toHaveBeenCalledTimes(1);
    expect(onDone).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });

  it('reports a malformed data payload via onError (parse failure)', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(sseResponse(['data: {not-json\n\n'])));

    const onError = vi.fn();
    await streamSessionEvents({
      workspaceId: 'ws1',
      agentSlug: 'atlas',
      sessionId: 'sess_sse-5',
      onEvent: () => {},
      onDone: () => {},
      onError,
    });

    expect(onError).toHaveBeenCalledTimes(1);
    vi.unstubAllGlobals();
  });
});

// ---------------------------------------------------------------------------
// attachCatchUpStream — reattach runtime (live-run-reattach-and-catchup)
// ---------------------------------------------------------------------------

describe('attachCatchUpStream', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  /** Binds ws1/chat-1 to session sess_cu with an empty thread. */
  const seed = () => {
    const db: any = {
      ws1: {
        id: 'ws1', name: 'WS', sub: 'ws1', tz: 'UTC',
        agents: [], channels: [], people: [], schedules: [], runs: [], members: [], integrations: [], skillLib: [], keys: [],
        threads: { 'chat-1': { active: 'sess_cu', list: [{ id: 'sess_cu', title: 'Live', updated: '', messages: [] }] } },
      },
    };
    useStore.setState({
      db,
      ui: { configAgent: null, scheduleEdit: null, wsOpen: false, running: false, runningChatId: null, toasts: [] },
    });
  };

  const session = (): any =>
    useStore.getState().db.ws1.threads['chat-1'].list.find((x: any) => x.id === 'sess_cu');

  it('run_active flips the spinner before the first real event and never renders as a bubble', async () => {
    seed();
    // Hold the stream open after the status frame: the spinner must be on
    // with NO run event delivered yet (the live run is mid tool call).
    const abort = new AbortController();
    vi.stubGlobal('fetch', vi.fn(async () =>
      sseResponse([frame({ kind: 'run_active', occurred_at: '2026-09-08T07:13:00Z' })], { open: true })
    ));

    attachCatchUpStream({
      workspaceId: 'ws1', agentSlug: 'atlas', chatId: 'chat-1', sessionId: 'sess_cu',
      after: 'e1', signal: abort.signal,
    });

    await vi.waitFor(() => {
      expect(useStore.getState().ui.running).toBe(true);
    });
    // The synthetic frame rendered nothing — no split bubble, no cursor move.
    expect(session().messages).toEqual([]);

    // Abandoning the stream (chat switch) clears the spinner.
    abort.abort();
    expect(useStore.getState().ui.running).toBe(false);
  });

  it('duplicate tool_call_started (replay + tap overlap) never duplicates the card', async () => {
    seed();
    vi.stubGlobal('fetch', vi.fn(async () =>
      sseResponse([
        frame({ id: 'e2', kind: 'tool_call_started', occurred_at: 'x', turn_id: 't1',
          tool_call: { call_id: 'c1', name: 'web.search', arguments: '{"q":"x"}' } }),
        // The subscribe-boundary window can deliver the same call again from
        // the live tap after the history replay already carded it.
        frame({ kind: 'tool_call_started', occurred_at: 'x', turn_id: 't1',
          tool_call: { call_id: 'c1', name: 'web.search', arguments: '{"q":"x"}' } }),
        frame({ id: 'e4', kind: 'tool_call_finished', occurred_at: 'x', turn_id: 't1',
          tool_result: { call_id: 'c1', result: 'results', latency: 420000000 } }),
        'data: [DONE]\n\n',
      ])
    ));

    attachCatchUpStream({
      workspaceId: 'ws1', agentSlug: 'atlas', chatId: 'chat-1', sessionId: 'sess_cu',
    });

    await vi.waitFor(() => {
      expect(session().messages.length).toBeGreaterThan(0);
    });
    const agent = session().messages[0];
    expect(agent.tools).toHaveLength(1);
    expect(agent.tools[0].res).toBe('results');
    expect(agent.tools[0].ms).toBe(420);
    expect(useStore.getState().ui.running).toBe(false);
  });

  it('fires onError (and clears the spinner) when the stream fails', async () => {
    seed();
    vi.stubGlobal('fetch', vi.fn(async () => {
      throw new Error('boom');
    }));

    const onDone = vi.fn();
    const onError = vi.fn();
    attachCatchUpStream({
      workspaceId: 'ws1', agentSlug: 'atlas', chatId: 'chat-1', sessionId: 'sess_cu',
      onDone, onError,
    });

    await vi.waitFor(() => {
      expect(onError).toHaveBeenCalledTimes(1);
    });
    expect(useStore.getState().ui.running).toBe(false);
    expect(onDone).not.toHaveBeenCalled();
  });

  it('registers the abort handle: a second attach for the chat aborts the first stream silently', async () => {
    seed();
    // Open streams: only an abort can end the first attach's read loop.
    const fetchMock = vi.fn(async (_url: unknown, _init?: unknown) => sseResponse([], { open: true }));
    vi.stubGlobal('fetch', fetchMock);

    const onDone1 = vi.fn();
    const onError1 = vi.fn();
    attachCatchUpStream({
      workspaceId: 'ws1', agentSlug: 'atlas', chatId: 'chat-1', sessionId: 'sess_cu',
      onDone: onDone1, onError: onError1,
    });
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect((fetchMock.mock.calls[0][1] as any).signal.aborted).toBe(false);

    // Last-writer-wins (fix-chat-stop-on-reattached-run D1): attaching again
    // for the same chat aborts the previous stream's owned handle.
    attachCatchUpStream({
      workspaceId: 'ws1', agentSlug: 'atlas', chatId: 'chat-1', sessionId: 'sess_cu',
    });
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));

    expect((fetchMock.mock.calls[0][1] as any).signal.aborted).toBe(true);
    expect((fetchMock.mock.calls[1][1] as any).signal.aborted).toBe(false);
    // The superseded stream detached silently — stop-style aborts are not
    // failures, so its terminal hooks never fire.
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(onDone1).not.toHaveBeenCalled();
    expect(onError1).not.toHaveBeenCalled();
  });

  it('caller-signal abort (chat switch) still suppresses onDone and onError', async () => {
    seed();
    const caller = new AbortController();
    vi.stubGlobal('fetch', vi.fn(async () =>
      sseResponse([frame({ id: 'e1', kind: 'turn_started', occurred_at: 'x', turn_id: 't1' })], { open: true })
    ));

    const onDone = vi.fn();
    const onError = vi.fn();
    attachCatchUpStream({
      workspaceId: 'ws1', agentSlug: 'atlas', chatId: 'chat-1', sessionId: 'sess_cu',
      signal: caller.signal, onDone, onError,
    });
    // The stream is live (the first event flipped the spinner).
    await vi.waitFor(() => expect(useStore.getState().ui.running).toBe(true));

    caller.abort();
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(useStore.getState().ui.running).toBe(false);
    expect(onDone).not.toHaveBeenCalled();
    expect(onError).not.toHaveBeenCalled();
  });

  it('a stale event delivered after the abort does not re-assert the spinner or write the transcript', async () => {
    seed();
    // The stream carries a trailing INCOMPLETE frame (no blank line) and stays
    // open: the frame parks in the consumer's buffer until the read loop ends.
    // Aborting via the stop handle ends the loop, and the consumer's
    // final-buffer flush delivers the stale frame to onEvent AFTER the abort —
    // the attach must not flip the spinner back on or write the store
    // (fix-chat-stop-on-reattached-run D2).
    const fetchMock = vi.fn(async () =>
      sseResponse([
        `data: ${JSON.stringify({ id: 'e9', kind: 'text_delta', occurred_at: 'x', turn_id: 't1', text_delta: 'late' })}`,
      ], { open: true })
    );
    vi.stubGlobal('fetch', fetchMock);

    attachCatchUpStream({
      workspaceId: 'ws1', agentSlug: 'atlas', chatId: 'chat-1', sessionId: 'sess_cu',
    });
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    // Let the first read consume the chunk — the partial frame parks in the
    // consumer's buffer with the reader waiting on the open stream.
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(useStore.getState().ui.running).toBe(false);

    abortCatchUpStream('chat-1');
    await new Promise((resolve) => setTimeout(resolve, 0));

    // The stale event fired post-abort and was dropped: spinner stays off,
    // nothing was written to the transcript.
    expect(useStore.getState().ui.running).toBe(false);
    expect(session().messages).toEqual([]);
  });
});

describe('fetchSessionTranscript — context_compacted divider (chat-compact-command)', () => {
  it('hydrates a compaction event as a divider entry carrying the token estimates', async () => {
    vi.mocked(api.agents.sessionEvents).mockResolvedValueOnce({
      next: '',
      events: [
        { id: 'e1', kind: 'message_completed', occurred_at: 't0', turn_id: 'turn-1',
          message: { role: 'user', content: 'long conversation' } },
        { id: 'e2', kind: 'message_completed', occurred_at: 't1', turn_id: 'turn-1',
          message: { role: 'assistant', content: 'long reply' } },
        { id: 'e3', kind: 'context_compacted', occurred_at: 't2', turn_id: 'turn-2',
          compaction: { tokens_before: 154000, tokens_after: 9200 } },
        { id: 'e4', kind: 'turn_completed', occurred_at: 't3', turn_id: 'turn-2' },
      ],
    });

    const { messages } = await fetchSessionTranscript('ws1', 'atlas', 'sess_h-compact');

    expect(messages.map((m: any) => m.author)).toEqual(['you', 'agent', 'compaction']);
    const divider = messages[2];
    expect(divider.text).toBe('');
    expect(divider.compaction).toEqual({ tokensBefore: 154000, tokensAfter: 9200 });
    // Hydrated entries carry no summarySaved flag — the divider shows the
    // token counts only (mockup D); no agent ack message is fabricated.
    expect(divider.summarySaved).toBeUndefined();
  });

  it('mints the same divider shape from the live catch-up stream', async () => {
    const db: any = {
      ws1: {
        id: 'ws1', name: 'WS', sub: 'ws1', tz: 'UTC',
        agents: [], channels: [], people: [], schedules: [], runs: [], members: [], integrations: [], skillLib: [], keys: [],
        threads: { 'chat-9': { active: 'sess_cu-c', list: [{ id: 'sess_cu-c', title: 'Live', updated: '', messages: [] }] } },
      },
    };
    useStore.setState({
      db,
      ui: { configAgent: null, scheduleEdit: null, wsOpen: false, running: false, runningChatId: null, toasts: [] },
    });
    const encoder = new TextEncoder();
    const frame = (ev: any) => `data: ${JSON.stringify(ev)}\n\n`;
    vi.stubGlobal('fetch', vi.fn(async () =>
      ({
        ok: true,
        status: 200,
        headers: new Headers({ 'Content-Type': 'text/event-stream' }),
        body: new ReadableStream<Uint8Array>({
          start(controller) {
            controller.enqueue(encoder.encode(frame({ id: 'x1', kind: 'context_compacted', occurred_at: 't1', turn_id: 'turn-1', compaction: { tokens_before: 154000, tokens_after: 9200 } })));
            controller.enqueue(encoder.encode('data: [DONE]\n\n'));
            controller.close();
          },
        }),
      } as unknown as Response)
    ));

    attachCatchUpStream({ workspaceId: 'ws1', agentSlug: 'atlas', chatId: 'chat-9', sessionId: 'sess_cu-c' });

    await vi.waitFor(() => {
      const sess = useStore.getState().db.ws1.threads['chat-9'].list.find((x: any) => x.id === 'sess_cu-c');
      expect(sess.messages).toHaveLength(1);
      const entry = sess.messages[0] as any;
      expect(entry.author).toBe('compaction');
      expect(entry.compaction).toEqual({ tokensBefore: 154000, tokensAfter: 9200 });
    });
  });
});

// ---------------------------------------------------------------------------
// Attachment metadata hydration (add-chat-attachments D10 + tasks 10.3)
// ---------------------------------------------------------------------------

describe('fetchSessionTranscript — attachment metadata on user messages', () => {
  const image = { id: 'att-1', name: 'shot.png', mime: 'image/png', size: 12, url: '/api/v1/files/k1' };
  const pdf = { name: 'report.pdf', mime: 'application/pdf', size: 5033164, url: '/api/v1/files/k2' };

  it('hydrates message.attachments onto the user entry unchanged', async () => {
    vi.mocked(api.agents.sessionEvents).mockResolvedValueOnce({
      next: '',
      events: [
        { id: 'e1', kind: 'message_completed', occurred_at: 't0', turn_id: 'turn-1',
          message: { role: 'user', content: "Here's the shot", attachments: [image, pdf] } },
        { id: 'e2', kind: 'message_completed', occurred_at: 't1', turn_id: 'turn-1',
          message: { role: 'assistant', content: 'nice' } },
      ],
    });

    const { messages } = await fetchSessionTranscript('ws1', 'atlas', 'sess_h-att-1');

    expect(messages[0].author).toBe('you');
    expect(messages[0].attachments).toEqual([image, pdf]);
    // Assistant entries carry no attachment field.
    expect(messages[1].attachments).toBeUndefined();
  });

  it('keeps an attachment-only user message (empty content, non-empty attachments)', async () => {
    vi.mocked(api.agents.sessionEvents).mockResolvedValueOnce({
      next: '',
      events: [
        { id: 'e1', kind: 'message_completed', occurred_at: 't0', turn_id: 'turn-1',
          message: { role: 'user', content: '', attachments: [image] } },
        { id: 'e2', kind: 'message_completed', occurred_at: 't1', turn_id: 'turn-1',
          message: { role: 'assistant', content: 'I see a screenshot.' } },
      ],
    });

    const { messages } = await fetchSessionTranscript('ws1', 'atlas', 'sess_h-att-2');

    expect(messages.map((m: any) => m.author)).toEqual(['you', 'agent']);
    expect(messages[0].text).toBe('');
    expect(messages[0].attachments).toEqual([image]);
  });

  it('still drops text-less attachment-less user messages (tool-result echoes)', async () => {
    vi.mocked(api.agents.sessionEvents).mockResolvedValueOnce({
      next: '',
      events: [
        { id: 'e1', kind: 'message_completed', occurred_at: 't0', turn_id: 'turn-1',
          message: { role: 'user', content: 'real prompt' } },
        { id: 'e2', kind: 'message_completed', occurred_at: 't1', turn_id: 'turn-1',
          message: { role: 'user', content: '' } },
      ],
    });

    const { messages } = await fetchSessionTranscript('ws1', 'atlas', 'sess_h-att-3');

    expect(messages.map((m: any) => m.author)).toEqual(['you']);
    expect(messages[0].text).toBe('real prompt');
  });

  it('survives a persist/restore round-trip (messages are stored whole)', async () => {
    const { persistAllThreads, loadPersistedThreads } = await import('../store/threadPersistence');
    const attached = { id: 'u1', author: 'you', ts: 't0', text: 'see this', attachments: [image, pdf] };
    const db: any = {
      t1: {
        id: 't1', name: 'T1', sub: 't1', tz: 'UTC',
        agents: [], channels: [], people: [], schedules: [], runs: [], members: [], integrations: [], skillLib: [], keys: [],
        threads: { 'chat-1': { active: 's1', list: [{ id: 's1', title: 'Chat', updated: '', messages: [attached] }] } },
      },
    };

    persistAllThreads(db);
    const restored = loadPersistedThreads('t1');
    const msg = restored['chat-1'].list[0].messages[0];

    expect(msg.attachments).toEqual([image, pdf]);
  });
});

// ---------------------------------------------------------------------------
// Followed run tail survives stop (fix-chat-stop-on-reattached-run D5)
// ---------------------------------------------------------------------------

describe('followed run tail survives stop (fix-chat-stop-on-reattached-run D5)', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  /** Binds ws1/chat-1 to session sess_tail with an empty thread. */
  const seed = () => {
    const db: any = {
      ws1: {
        id: 'ws1', name: 'WS', sub: 'ws1', tz: 'UTC',
        agents: [], channels: [], people: [], schedules: [], runs: [], members: [], integrations: [], skillLib: [], keys: [],
        threads: { 'chat-1': { active: 'sess_tail', list: [{ id: 'sess_tail', title: 'Live', updated: '', messages: [] }] } },
      },
    };
    useStore.setState({
      db,
      pos: { tenantId: 'ws1', view: 'chats', chatId: 'chat-1', showContext: false, railExpanded: false },
      ui: { configAgent: null, scheduleEdit: null, wsOpen: false, running: false, runningChatId: null, toasts: [] },
    });
  };

  const session = (): any =>
    useStore.getState().db.ws1.threads['chat-1'].list.find((x: any) => x.id === 'sess_tail');

  // The pre-persist snapshot a hydrate sees while the run is still streaming
  // (or after a stop cancelled it): the turn's user message and tool calls are
  // committed, but the streamed answer is NOT — text/reasoning deltas are
  // never persisted until the message completes.
  const PRE_PERSIST_EVENTS = [
    { id: 'e1', kind: 'message_completed', occurred_at: 't0', turn_id: 'T1',
      message: { role: 'user', content: 'Run diagnostics' } },
    { id: 'e2', kind: 'tool_call_started', occurred_at: 't1', turn_id: 'T1',
      tool_call: { call_id: 'c1', name: 'grafana.query', arguments: 'q' } },
    { id: 'e3', kind: 'tool_call_finished', occurred_at: 't2', turn_id: 'T1',
      tool_result: { call_id: 'c1', result: '99.9%', latency: 1000000 } },
  ];
  const COMPLETED_EVENTS = [
    ...PRE_PERSIST_EVENTS,
    { id: 'e4', kind: 'message_completed', occurred_at: 't3', turn_id: 'T1',
      message: { role: 'assistant', content: 'The answer is 42' } },
    { id: 'e5', kind: 'turn_completed', occurred_at: 't4', turn_id: 'T1',
      usage: { final_input_tokens: 4321 } },
  ];

  /** Open SSE Response with a push handle — the test plays the live server. */
  const openStream = () => {
    let controller: ReadableStreamDefaultController<Uint8Array> | null = null;
    const response = {
      ok: true,
      status: 200,
      headers: new Headers({ 'Content-Type': 'text/event-stream' }),
      body: new ReadableStream<Uint8Array>({
        start(c) { controller = c; },
      }),
    } as unknown as Response;
    return {
      response,
      push: (ev: any) => controller!.enqueue(encoder.encode(frame(ev))),
      close: () => controller!.close(),
    };
  };

  /** Mount-flow follow: hydrate → attach → stream the tail. Returns the
   * stream handle so tests can deliver further frames. */
  const followRun = async () => {
    seed();
    vi.mocked(api.agents.sessionEvents).mockResolvedValueOnce({
      next: '',
      events: PRE_PERSIST_EVENTS,
    });
    const hydrated = await hydrateSession({ workspaceId: 'ws1', agentSlug: 'atlas', chatId: 'chat-1', sessionId: 'sess_tail' });
    expect(hydrated!.messages.map((m: any) => m.author)).toEqual(['you', 'agent']);
    // The streamed answer is not persisted yet — the agent row has no text.
    expect(hydrated!.messages[1].text).toBe('');

    const stream = openStream();
    vi.stubGlobal('fetch', vi.fn(async () => stream.response));
    attachCatchUpStream({
      workspaceId: 'ws1', agentSlug: 'atlas', chatId: 'chat-1', sessionId: 'sess_tail',
      after: hydrated!.lastEventId,
    });
    await new Promise((r) => setTimeout(r, 0));

    // The run streams its answer live: text, reasoning, a second tool card.
    stream.push({ id: '', kind: 'text_delta', occurred_at: 'x', turn_id: 'T1', text_delta: 'The answer is ' });
    stream.push({ id: '', kind: 'text_delta', occurred_at: 'x', turn_id: 'T1', text_delta: '42' });
    stream.push({ id: '', kind: 'reasoning_delta', occurred_at: 'x', turn_id: 'T1', reasoning_delta: 'computing' });
    stream.push({ id: '', kind: 'tool_call_started', occurred_at: 'x', turn_id: 'T1',
      tool_call: { call_id: 'c2', name: 'web.search', arguments: '{}' } });
    await new Promise((r) => setTimeout(r, 0));

    const tail = session().messages.find((m: any) => m.author === 'agent');
    expect(tail.text).toBe('The answer is 42');
    expect(tail.parts).toEqual([{ k: 'tool', i: 0 }, { k: 'reasoning', text: 'computing' }, { k: 'tool', i: 1 }]);
    expect(session().messages.map((m: any) => m.author)).toEqual(['you', 'agent']);
    return { stream, tail };
  };

  /** The stop control (runtime onCancel ordering): detach the followed stream,
   * then clear the spinner. */
  const stop = () => {
    abortCatchUpStream('chat-1');
    useStore.getState().patchUi({ running: false });
  };

  it('stop mid-catch-up + terminal re-hydrate keeps every streamed part (racing/cancelled snapshot)', async () => {
    await followRun();
    stop();
    expect(useStore.getState().ui.running).toBe(false);

    // The run reaches its terminal state; a re-hydrate lands whose snapshot
    // predates the streamed answer (the fetch raced the terminal, or the
    // cancelled run never persisted the partial message). The replace must
    // not vanish what the turn streamed.
    vi.mocked(api.agents.sessionEvents).mockResolvedValueOnce({
      next: '',
      events: PRE_PERSIST_EVENTS,
    });
    await hydrateSession({ workspaceId: 'ws1', agentSlug: 'atlas', chatId: 'chat-1', sessionId: 'sess_tail' });

    const msgs = session().messages;
    expect(msgs.map((m: any) => m.author)).toEqual(['you', 'agent']);
    expect(msgs[1].text).toBe('The answer is 42');
    expect(msgs[1].tools.map((t: any) => t.callId)).toEqual(['c1', 'c2']);
    expect(msgs[1].parts).toEqual([{ k: 'tool', i: 0 }, { k: 'reasoning', text: 'computing' }, { k: 'tool', i: 1 }]);
  });

  it('terminal re-hydrate with the completed message converges — no duplicate turn, no stale body', async () => {
    await followRun();
    stop();

    // History caught up: the completed message is persisted with the full
    // streamed text. Server truth wins — the transcript converges without
    // duplicating the turn or regressing to a stale client body.
    vi.mocked(api.agents.sessionEvents).mockResolvedValueOnce({
      next: '',
      events: COMPLETED_EVENTS,
    });
    const hydrated = await hydrateSession({ workspaceId: 'ws1', agentSlug: 'atlas', chatId: 'chat-1', sessionId: 'sess_tail' });

    const msgs = session().messages;
    expect(msgs.map((m: any) => m.author)).toEqual(['you', 'agent']);
    expect(msgs[1].text).toBe('The answer is 42');
    expect(msgs[1].resp).toBe('resp_sess_tail_T1');
    // The usage from the terminal event still drives the context meter.
    expect(hydrated!.finalInputTokens).toBe(4321);
  });

  it('a transcript replace racing the live stream keeps the tail and the stream keeps folding (poller race)', async () => {
    const { stream } = await followRun();

    // A replace lands mid-stream with a pre-persist snapshot (approval-poll
    // tick, second attach's hydration): the streamed prefix must survive and
    // the stream must keep folding onto the same row.
    vi.mocked(api.agents.sessionEvents).mockResolvedValueOnce({
      next: '',
      events: PRE_PERSIST_EVENTS,
    });
    const snapshot = await fetchSessionTranscript('ws1', 'atlas', 'sess_tail');
    applyServerTranscript('ws1', 'chat-1', 'sess_tail', snapshot.messages);
    expect(session().messages.map((m: any) => m.author)).toEqual(['you', 'agent']);
    expect(session().messages[1].text).toBe('The answer is 42');

    stream.push({ id: '', kind: 'text_delta', occurred_at: 'x', turn_id: 'T1', text_delta: '!' });
    await new Promise((r) => setTimeout(r, 0));
    expect(session().messages.map((m: any) => m.author)).toEqual(['you', 'agent']);
    expect(session().messages[1].text).toBe('The answer is 42!');
  });
});
