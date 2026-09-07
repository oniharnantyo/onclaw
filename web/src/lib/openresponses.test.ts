/**
 * @vitest-environment jsdom
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { runTurn } from './openresponses';

const createMock = vi.hoisted(() => vi.fn());

vi.mock('openai', () => ({
  default: class MockOpenAI {
    responses = { create: createMock };
    constructor(_opts: any) {}
  },
}));

// openResponsesClient caches clients per key — a fresh key per test keeps the
// mock's create implementation isolated.
let keySeq = 0;
const nextKey = () => `k-${++keySeq}`;

beforeEach(() => {
  createMock.mockReset();
});

describe('runTurn — minted response id capture (design D5)', () => {
  it('surfaces the id from the FIRST stream event (response.created), before completion', async () => {
    const events = [
      { type: 'response.created', response: { id: 'resp_sess-a_turn-1' } },
      { type: 'response.output_text.delta', delta: 'hi' },
      { type: 'response.completed', response: { id: 'resp_sess-a_turn-1' } },
    ];
    createMock.mockResolvedValue(events);

    const onResponseId = vi.fn();
    const onDelta = vi.fn();
    const onDone = vi.fn();
    await runTurn(nextKey(), { agentSlug: 'atlas', input: 'hello' }, {
      onDelta, onResponseId, onDone, onError: vi.fn(),
    });

    // Captured at the first event — the cancel window opens mid-stream.
    expect(onResponseId).toHaveBeenCalledTimes(1);
    expect(onResponseId).toHaveBeenCalledWith('resp_sess-a_turn-1');
    expect(onDelta).toHaveBeenCalledWith('hi');
    expect(onDone).toHaveBeenCalledWith('resp_sess-a_turn-1');
  });

  it('keeps onDone(responseId) working when the id only appears at completion', async () => {
    const events = [{ type: 'response.completed', response: { id: 'resp_sess-b_turn-2' } }];
    createMock.mockResolvedValue(events);

    const onResponseId = vi.fn();
    await runTurn(nextKey(), { agentSlug: 'atlas', input: 'hello' }, {
      onDelta: vi.fn(), onResponseId, onDone: vi.fn(), onError: vi.fn(),
    });

    expect(onResponseId).not.toHaveBeenCalled();
  });

  it('does not surface an id when the stream fails before response.created', async () => {
    const events = [{ type: 'response.failed', response: { error: { message: 'boom' } } }];
    createMock.mockResolvedValue(events);

    const onResponseId = vi.fn();
    const onError = vi.fn();
    await runTurn(nextKey(), { agentSlug: 'atlas', input: 'hello' }, {
      onDelta: vi.fn(), onResponseId, onDone: vi.fn(), onError,
    });

    expect(onResponseId).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledWith('boom');
  });
});

describe('runTurn — transcript fidelity dispatch (design D5)', () => {
  it('wires onReasoningDelta to onclaw:reasoning_delta events', async () => {
    const events = [
      { type: 'onclaw:reasoning_delta', delta: 'thinking ' },
      { type: 'onclaw:reasoning_delta', delta: 'hard' },
      { type: 'response.completed', response: { id: 'resp_x_1' } },
    ];
    createMock.mockResolvedValue(events);

    const onReasoningDelta = vi.fn();
    await runTurn(nextKey(), { agentSlug: 'atlas', input: 'hi' }, {
      onDelta: vi.fn(), onReasoningDelta, onDone: vi.fn(), onError: vi.fn(),
    });

    expect(onReasoningDelta).toHaveBeenCalledTimes(2);
    expect(onReasoningDelta).toHaveBeenNthCalledWith(1, 'thinking ');
    expect(onReasoningDelta).toHaveBeenNthCalledWith(2, 'hard');
  });

  it('fires onToolCall argless at output_item.added and with complete args at output_item.done', async () => {
    const events = [
      { type: 'response.output_item.added', item: { type: 'function_call', name: 'files.list', call_id: 'call_1', arguments: '{"pa' } },
      { type: 'response.output_item.done', item: { type: 'function_call', name: 'files.list', call_id: 'call_1', arguments: '{"path":"."}' } },
      { type: 'response.completed', response: { id: 'resp_x_1' } },
    ];
    createMock.mockResolvedValue(events);

    const onToolCall = vi.fn();
    await runTurn(nextKey(), { agentSlug: 'atlas', input: 'hi' }, {
      onDelta: vi.fn(), onToolCall, onDone: vi.fn(), onError: vi.fn(),
    });

    expect(onToolCall).toHaveBeenCalledTimes(2);
    // added: partial args are NOT forwarded (eino chunks args across frames).
    expect(onToolCall).toHaveBeenNthCalledWith(1, 'files.list', 'call_1');
    // done: complete arguments string.
    expect(onToolCall).toHaveBeenNthCalledWith(2, 'files.list', 'call_1', '{"path":"."}');
  });

  it('routes tool outputs from onclaw.function_call_output ITEMS at output_item.done', async () => {
    const events = [
      { type: 'response.output_item.done', item: { type: 'onclaw.function_call_output', call_id: 'call_1', name: 'files.list', result: '["a"]', latency_ms: 42, is_error: false } },
      { type: 'response.completed', response: { id: 'resp_x_1' } },
    ];
    createMock.mockResolvedValue(events);

    const onToolOutput = vi.fn();
    await runTurn(nextKey(), { agentSlug: 'atlas', input: 'hi' }, {
      onDelta: vi.fn(), onToolOutput, onDone: vi.fn(), onError: vi.fn(),
    });

    expect(onToolOutput).toHaveBeenCalledWith('call_1', 'files.list', '["a"]', 42, false);
  });

  it('ignores a top-level onclaw.function_call_output event (the dead case is gone)', async () => {
    const events = [
      { type: 'onclaw.function_call_output', call_id: 'call_1', name: 'files.list', result: 'x', latency_ms: 1, is_error: false },
      { type: 'response.completed', response: { id: 'resp_x_1' } },
    ];
    createMock.mockResolvedValue(events);

    const onToolOutput = vi.fn();
    await runTurn(nextKey(), { agentSlug: 'atlas', input: 'hi' }, {
      onDelta: vi.fn(), onToolOutput, onDone: vi.fn(), onError: vi.fn(),
    });

    expect(onToolOutput).not.toHaveBeenCalled();
  });
});

describe('runTurn — terminal-event usage capture (chat-context-meter)', () => {
  const usageBlock = { input_tokens: 50000, output_tokens: 1200, total_tokens: 51200, final_input_tokens: 50000 };

  it('maps the usage block on response.completed, firing onUsage before onDone', async () => {
    const events = [
      { type: 'response.created', response: { id: 'resp_x_1' } },
      { type: 'response.completed', response: { id: 'resp_x_1', usage: usageBlock } },
    ];
    createMock.mockResolvedValue(events);

    const onUsage = vi.fn();
    const onDone = vi.fn();
    await runTurn(nextKey(), { agentSlug: 'atlas', input: 'hi' }, {
      onDelta: vi.fn(), onUsage, onDone, onError: vi.fn(),
    });

    expect(onUsage).toHaveBeenCalledTimes(1);
    expect(onUsage).toHaveBeenCalledWith({
      inputTokens: 50000, outputTokens: 1200, totalTokens: 51200, finalInputTokens: 50000,
    });
    // Meter lands before the turn is declared done.
    expect(onUsage.mock.invocationCallOrder[0]).toBeLessThan(onDone.mock.invocationCallOrder[0]);
    expect(onDone).toHaveBeenCalledWith('resp_x_1');
  });

  it('maps response.incomplete the same way, including the response id', async () => {
    const events = [
      { type: 'response.created', response: { id: 'resp_x_2' } },
      { type: 'response.incomplete', response: { id: 'resp_x_2', usage: { input_tokens: 10, output_tokens: 5, total_tokens: 15, final_input_tokens: 10 } } },
    ];
    createMock.mockResolvedValue(events);

    const onUsage = vi.fn();
    const onDone = vi.fn();
    await runTurn(nextKey(), { agentSlug: 'atlas', input: 'hi' }, {
      onDelta: vi.fn(), onUsage, onDone, onError: vi.fn(),
    });

    expect(onUsage).toHaveBeenCalledWith({ inputTokens: 10, outputTokens: 5, totalTokens: 15, finalInputTokens: 10 });
    expect(onDone).toHaveBeenCalledWith('resp_x_2');
  });

  it('fires onUsage before onError on response.failed', async () => {
    const events = [{ type: 'response.failed', response: { error: { message: 'boom' }, usage: usageBlock } }];
    createMock.mockResolvedValue(events);

    const onUsage = vi.fn();
    const onError = vi.fn();
    const onDone = vi.fn();
    await runTurn(nextKey(), { agentSlug: 'atlas', input: 'hi' }, {
      onDelta: vi.fn(), onUsage, onDone, onError,
    });

    expect(onUsage).toHaveBeenCalledWith({
      inputTokens: 50000, outputTokens: 1200, totalTokens: 51200, finalInputTokens: 50000,
    });
    expect(onUsage.mock.invocationCallOrder[0]).toBeLessThan(onError.mock.invocationCallOrder[0]);
    expect(onError).toHaveBeenCalledWith('boom');
    expect(onDone).not.toHaveBeenCalled();
  });

  it('leaves finalInputTokens undefined when the usage block omits final_input_tokens', async () => {
    const events = [
      { type: 'response.completed', response: { id: 'resp_x_3', usage: { input_tokens: 7, output_tokens: 3, total_tokens: 10 } } },
    ];
    createMock.mockResolvedValue(events);

    const onUsage = vi.fn();
    await runTurn(nextKey(), { agentSlug: 'atlas', input: 'hi' }, {
      onDelta: vi.fn(), onUsage, onDone: vi.fn(), onError: vi.fn(),
    });

    expect(onUsage).toHaveBeenCalledWith({ inputTokens: 7, outputTokens: 3, totalTokens: 10 });
  });

  it('fires onUsage(null) when a terminal event carries no usage block', async () => {
    const events = [{ type: 'response.completed', response: { id: 'resp_x_4' } }];
    createMock.mockResolvedValue(events);

    const onUsage = vi.fn();
    await runTurn(nextKey(), { agentSlug: 'atlas', input: 'hi' }, {
      onDelta: vi.fn(), onUsage, onDone: vi.fn(), onError: vi.fn(),
    });

    expect(onUsage).toHaveBeenCalledTimes(1);
    expect(onUsage).toHaveBeenCalledWith(null);
  });

  it('never fires onUsage when an approval pause ends the stream without a terminal event', async () => {
    const events = [
      { type: 'onclaw:approval_required', interrupt_id: 'i1', command: 'rm -rf /', response_id: 'resp_x_5', session_id: 'sess_1' },
      // stream ends here — no response.completed/failed
    ];
    createMock.mockResolvedValue(events);

    const onUsage = vi.fn();
    const onDone = vi.fn();
    await runTurn(nextKey(), { agentSlug: 'atlas', input: 'hi' }, {
      onDelta: vi.fn(), onUsage, onDone, onError: vi.fn(),
    });

    expect(onUsage).not.toHaveBeenCalled();
    expect(onDone).not.toHaveBeenCalled();
  });
});
