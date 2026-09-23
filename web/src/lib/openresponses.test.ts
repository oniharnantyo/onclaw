/**
 * @vitest-environment jsdom
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { runTurn, sessionIdFromResponseId } from './openresponses';

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

  it('maps the server-provided context_breakdown through to contextBreakdown (D7)', async () => {
    const events = [
      {
        type: 'response.completed',
        response: {
          id: 'resp_x_6',
          usage: {
            input_tokens: 50000, output_tokens: 1200, total_tokens: 51200, final_input_tokens: 50000,
            context_breakdown: { instructions: 1200, tools: 3400, files: 800, conversation: 12000 },
          },
        },
      },
    ];
    createMock.mockResolvedValue(events);

    const onUsage = vi.fn();
    await runTurn(nextKey(), { agentSlug: 'atlas', input: 'hi' }, {
      onDelta: vi.fn(), onUsage, onDone: vi.fn(), onError: vi.fn(),
    });

    expect(onUsage).toHaveBeenCalledWith({
      inputTokens: 50000, outputTokens: 1200, totalTokens: 51200, finalInputTokens: 50000,
      contextBreakdown: { instructions: 1200, tools: 3400, files: 800, conversation: 12000 },
    });
  });

  it('omits contextBreakdown when the usage block carries no context_breakdown (estimate stays the fallback)', async () => {
    const events = [
      { type: 'response.completed', response: { id: 'resp_x_7', usage: { input_tokens: 10, output_tokens: 5, total_tokens: 15, final_input_tokens: 10 } } },
    ];
    createMock.mockResolvedValue(events);

    const onUsage = vi.fn();
    await runTurn(nextKey(), { agentSlug: 'atlas', input: 'hi' }, {
      onDelta: vi.fn(), onUsage, onDone: vi.fn(), onError: vi.fn(),
    });

    const [usage] = onUsage.mock.calls[0];
    expect('contextBreakdown' in usage).toBe(false);
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

  it('passes a service-run write escalation through on the approval event untouched (add-integration-authority)', async () => {
    const tool = {
      name: 'github.merge_pull_request',
      service: 'github',
      service_name: 'GitHub',
      connection_id: 'conn-gh',
      tier: 'write',
    };
    const events = [
      { type: 'onclaw:approval_required', interrupt_id: 'i2', command: '', response_id: 'resp_x_6', session_id: 'sess_1', tool },
      // stream ends here — the pause is terminal for this stream
    ];
    createMock.mockResolvedValue(events);

    const onApprovalRequired = vi.fn();
    await runTurn(nextKey(), { agentSlug: 'atlas', input: 'hi' }, {
      onDelta: vi.fn(), onApprovalRequired, onDone: vi.fn(), onError: vi.fn(),
    });

    expect(onApprovalRequired).toHaveBeenCalledTimes(1);
    const approval = onApprovalRequired.mock.calls[0][0];
    expect(approval.interrupt_id).toBe('i2');
    expect(approval.tool).toEqual(tool);
  });

  it('omits the tool field entirely on ordinary shell approval events', async () => {
    const events = [
      { type: 'onclaw:approval_required', interrupt_id: 'i3', command: 'rm -rf /', response_id: 'resp_x_7', session_id: 'sess_1' },
    ];
    createMock.mockResolvedValue(events);

    const onApprovalRequired = vi.fn();
    await runTurn(nextKey(), { agentSlug: 'atlas', input: 'hi' }, {
      onDelta: vi.fn(), onApprovalRequired, onDone: vi.fn(), onError: vi.fn(),
    });

    const approval = onApprovalRequired.mock.calls[0][0];
    expect('tool' in approval).toBe(false);
  });
});

describe('runTurn — 409 conflict dispatch (live-run-reattach fix)', () => {
  it('flags meta.conflict when the per-session run lock rejects the turn', async () => {
    createMock.mockRejectedValue({
      status: 409,
      message: 'agent.Run: conflict: a run is already active for session "sess_1"',
    });

    const onError = vi.fn();
    await runTurn(nextKey(), { agentSlug: 'atlas', input: 'hello' }, {
      onDelta: vi.fn(), onDone: vi.fn(), onError,
    });

    expect(onError).toHaveBeenCalledTimes(1);
    const [, meta] = onError.mock.calls[0];
    expect(meta?.conflict).toBe(true);
    expect(meta?.unauthorized).toBeFalsy();
  });

  it('leaves meta.conflict undefined for non-409 failures', async () => {
    createMock.mockRejectedValue({ status: 429, message: 'rate limited' });

    const onError = vi.fn();
    await runTurn(nextKey(), { agentSlug: 'atlas', input: 'hello' }, {
      onDelta: vi.fn(), onDone: vi.fn(), onError,
    });

    const [, meta] = onError.mock.calls[0];
    expect(meta?.conflict).toBeFalsy();
  });
});

describe('sessionIdFromResponseId — published resp_ codec', () => {
  it.each([
    ['resp_sess_52eca0d5-bccd_turn-9', 'sess_52eca0d5-bccd'],
    ['resp_sess-1_t2', 'sess-1'],
  ])('decodes %s to %s (split at the LAST underscore)', (rid, want) => {
    expect(sessionIdFromResponseId(rid)).toBe(want);
  });

  it.each([
    [''],
    ['resp_'],
    ['resp_nounderscore'],
    ['resp__t'],
    ['m-42'],
    [undefined],
    [null],
  ])('returns undefined for %p', (rid) => {
    expect(sessionIdFromResponseId(rid as any)).toBeUndefined();
  });
});

describe('runTurn — compact command wire (chat-compact-command)', () => {
  it('fires onContextCompacted with tokens_before/tokens_after and keeps the stream terminal contract', async () => {
    const events = [
      { type: 'response.created', response: { id: 'resp_sess-a_turn-9' } },
      { type: 'onclaw:context_compacted', tokens_before: 154000, tokens_after: 9200, sequence_number: 3 },
      { type: 'response.completed', response: { id: 'resp_sess-a_turn-9', usage: { input_tokens: 9200, output_tokens: 210, total_tokens: 9410 } } },
    ];
    createMock.mockResolvedValue(events);

    const onContextCompacted = vi.fn();
    const onDone = vi.fn();
    await runTurn(nextKey(), { agentSlug: 'atlas', input: '', command: 'compact' }, {
      onDelta: vi.fn(), onContextCompacted, onDone, onError: vi.fn(),
    });

    expect(onContextCompacted).toHaveBeenCalledTimes(1);
    expect(onContextCompacted).toHaveBeenCalledWith({ tokensBefore: 154000, tokensAfter: 9200 });
    // The compact stream ends in a normal terminal event (usage, no output).
    expect(onDone).toHaveBeenCalledWith('resp_sess-a_turn-9');
  });

  it('sends metadata.onclaw_command alongside the session binding', async () => {
    createMock.mockResolvedValue([
      { type: 'response.completed', response: { id: 'resp_sess-a_turn-9' } },
    ]);

    await runTurn(nextKey(), { agentSlug: 'atlas', input: 'keep the decisions', command: 'compact', sessionId: 'sess_abc' }, {
      onDelta: vi.fn(), onDone: vi.fn(), onError: vi.fn(),
    });

    const request = createMock.mock.calls[0][0];
    expect(request.metadata).toEqual({ onclaw_session: 'sess_abc', onclaw_command: 'compact' });
    expect(request.input).toBe('keep the decisions');
  });

  it('omits the metadata block entirely when no session or command is present', async () => {
    createMock.mockResolvedValue([
      { type: 'response.completed', response: { id: 'resp_x_1' } },
    ]);

    await runTurn(nextKey(), { agentSlug: 'atlas', input: 'hi' }, {
      onDelta: vi.fn(), onDone: vi.fn(), onError: vi.fn(),
    });

    expect(createMock.mock.calls[0][0].metadata).toBeUndefined();
  });
});

describe('runTurn — attachment turn input (add-chat-attachments D2/D11)', () => {
  const baseCb = () => ({ onDelta: vi.fn(), onDone: vi.fn(), onError: vi.fn() });

  it('builds the exact item-array input for text + image + pdf', async () => {
    createMock.mockResolvedValue([{ type: 'response.completed', response: { id: 'resp_x_1' } }]);

    await runTurn(nextKey(), {
      agentSlug: 'atlas',
      input: 'What do you see here?',
      attachments: [
        { id: 'att-1', name: 'shot.png', mime: 'image/png', size: 12, url: '/api/v1/files/k1' },
        { name: 'report.pdf', mime: 'application/pdf', size: 5033164, url: '/api/v1/files/k2' },
      ],
    }, baseCb());

    const request = createMock.mock.calls[0][0];
    expect(request.input).toEqual([
      {
        role: 'user',
        content: [
          { type: 'input_text', text: 'What do you see here?' },
          { type: 'input_image', image_url: '/api/v1/files/k1', detail: 'auto' },
          { type: 'input_file', file_url: '/api/v1/files/k2', filename: 'report.pdf' },
        ],
      },
    ]);
    // Everything else about the request is unchanged.
    expect(request.model).toBe('atlas');
    expect(request.stream).toBe(true);
  });

  it('sends a plain string input when there are no attachments (byte-for-byte legacy request)', async () => {
    createMock.mockResolvedValue([{ type: 'response.completed', response: { id: 'resp_x_2' } }]);

    await runTurn(nextKey(), { agentSlug: 'atlas', input: 'hello' }, baseCb());

    const request = createMock.mock.calls[0][0];
    expect(request.input).toBe('hello');
    expect(typeof request.input).toBe('string');
  });

  it('omits input_text for attachment-only turns (empty text)', async () => {
    createMock.mockResolvedValue([{ type: 'response.completed', response: { id: 'resp_x_3' } }]);

    await runTurn(nextKey(), {
      agentSlug: 'atlas',
      input: '',
      attachments: [{ id: 'att-9', name: 'dump.sql', mime: 'application/sql', size: 4300000, url: '/api/v1/files/k9' }],
    }, baseCb());

    const request = createMock.mock.calls[0][0];
    expect(request.input).toEqual([
      {
        role: 'user',
        content: [
          { type: 'input_file', file_url: '/api/v1/files/k9', filename: 'dump.sql' },
        ],
      },
    ]);
  });

  it('routes by mime: image parts ride input_image, everything else input_file', async () => {
    createMock.mockResolvedValue([{ type: 'response.completed', response: { id: 'resp_x_4' } }]);

    await runTurn(nextKey(), {
      agentSlug: 'atlas',
      input: 'x',
      attachments: [
        { name: 'a.webp', mime: 'image/webp', size: 1, url: '/api/v1/files/ka' },
        { name: 'notes.txt', mime: 'text/plain', size: 2, url: '/api/v1/files/kb' },
      ],
    }, baseCb());

    const content = createMock.mock.calls[0][0].input[0].content;
    expect(content[1]).toEqual({ type: 'input_image', image_url: '/api/v1/files/ka', detail: 'auto' });
    expect(content[2]).toEqual({ type: 'input_file', file_url: '/api/v1/files/kb', filename: 'notes.txt' });
  });
});
