import { chatReducer } from './ChatProvider';
import type { ChatState } from './ChatProvider';
import type { ChatMessage, ContentBlock } from '../types/chat';

function makeBaseState(overrides?: Partial<ChatState>): ChatState {
  return {
    messages: [],
    isStreaming: false,
    conversations: [],
    activeConvID: null,
    chatAgent: '',
    agents: [],
    skills: [],
    contextWindow: 0,
    contextUsed: 0,
    contextCompactionAnnotated: false,
    isCompacting: false,
    ...overrides,
  };
}

function assistantPartial(text: string, streaming = true): ChatMessage {
  const block: ContentBlock = {
    type: 'assistant_gen_text',
    assistant_gen_text: { text },
  };
  return {
    role: 'assistant',
    content_blocks: [block],
    isStreaming: streaming,
  };
}

function userMsg(): ChatMessage {
  return {
    role: 'user',
    content_blocks: [{ type: 'user_input_text', user_input_text: { text: 'hi' } }],
  };
}

export function runChatReducerTests(): void {
  /* a. STREAM_STOPPED retains the partial and sets stopped */
  {
    const state = makeBaseState({
      isStreaming: true,
      messages: [userMsg(), assistantPartial('partial answer')],
    });

    const next = chatReducer(state, { type: 'STREAM_STOPPED' });

    if (next.isStreaming !== false) {
      throw new Error(`STREAM_STOPPED: expected isStreaming false, got ${next.isStreaming}`);
    }

    const last = next.messages[next.messages.length - 1];
    if (!last) {
      throw new Error('STREAM_STOPPED: no last message after dispatch');
    }
    if (last.isStreaming !== false) {
      throw new Error(`STREAM_STOPPED: expected last.isStreaming false, got ${last.isStreaming}`);
    }
    if (last.stopped !== true) {
      throw new Error(`STREAM_STOPPED: expected last.stopped true, got ${last.stopped}`);
    }
    const text = last.content_blocks?.[0]?.assistant_gen_text?.text;
    if (text !== 'partial answer') {
      throw new Error(`STREAM_STOPPED: expected partial retained ('partial answer'), got ${JSON.stringify(text)}`);
    }
  }

  /* b. STREAM_DONE contrast: does NOT set stopped */
  {
    const state = makeBaseState({
      isStreaming: true,
      messages: [userMsg(), assistantPartial('full answer')],
    });

    const next = chatReducer(state, { type: 'STREAM_DONE' });

    if (next.isStreaming !== false) {
      throw new Error(`STREAM_DONE: expected isStreaming false, got ${next.isStreaming}`);
    }

    const last = next.messages[next.messages.length - 1];
    if (!last) {
      throw new Error('STREAM_DONE: no last message after dispatch');
    }
    if (last.isStreaming !== false) {
      throw new Error(`STREAM_DONE: expected last.isStreaming false, got ${last.isStreaming}`);
    }
    if (last.stopped === true) {
      throw new Error('STREAM_DONE: last.stopped must NOT be true (only STOPPED sets it)');
    }
  }

  /* c. STREAM_ERROR stops streaming */
  {
    const state = makeBaseState({
      isStreaming: true,
      messages: [userMsg(), assistantPartial('partial')],
    });

    const next = chatReducer(state, { type: 'STREAM_ERROR', error: 'x' });

    if (next.isStreaming !== false) {
      throw new Error(`STREAM_ERROR: expected isStreaming false, got ${next.isStreaming}`);
    }
  }

  /* d. STREAM_STOPPED targets the last ASSISTANT message, not a trailing user turn */
  {
    const state = makeBaseState({
      isStreaming: true,
      messages: [userMsg(), assistantPartial('partial'), userMsg()],
    });

    const next = chatReducer(state, { type: 'STREAM_STOPPED' });

    if (next.messages[2]?.stopped === true) {
      throw new Error('STREAM_STOPPED: must not mark the trailing user message as stopped');
    }
    const assistant = next.messages[1];
    if (assistant?.stopped !== true) {
      throw new Error('STREAM_STOPPED: expected the last assistant message (index 1) to be marked stopped');
    }
    if (assistant?.isStreaming !== false) {
      throw new Error('STREAM_STOPPED: expected assistant.isStreaming false');
    }
  }

  /* e. STREAM_STOPPED with no assistant message marks nothing */
  {
    const state = makeBaseState({
      isStreaming: true,
      messages: [userMsg(), userMsg()],
    });

    const next = chatReducer(state, { type: 'STREAM_STOPPED' });

    if (next.messages.some((m) => m.stopped === true)) {
      throw new Error('STREAM_STOPPED: must not mark any non-assistant message as stopped');
    }
    if (next.isStreaming !== false) {
      throw new Error('STREAM_STOPPED: expected isStreaming false');
    }
  }

  /* f. SET_COMPACTING updates isCompacting */
  {
    const state = makeBaseState({ isCompacting: false });
    const next = chatReducer(state, { type: 'SET_COMPACTING', compacting: true });
    if (next.isCompacting !== true) {
      throw new Error(`SET_COMPACTING: expected isCompacting true, got ${next.isCompacting}`);
    }
  }

  /* h. SET_ACTIVE_CONV_ID preserves messages when isStreaming is true */
  {
    const state = makeBaseState({
      isStreaming: true,
      activeConvID: null,
      messages: [userMsg()],
    });
    const next = chatReducer(state, { type: 'SET_ACTIVE_CONV_ID', id: 42 });
    if (next.activeConvID !== 42) {
      throw new Error(`SET_ACTIVE_CONV_ID: expected activeConvID 42, got ${next.activeConvID}`);
    }
    if (next.messages.length !== 1) {
      throw new Error(`SET_ACTIVE_CONV_ID: expected messages length 1 during streaming, got ${next.messages.length}`);
    }
  }

  /* g. STREAM_MESSAGE separates assistant sub-responses on a tool result so
     streaming_meta.index (unique only within one assistant message) cannot
     collide across them. Reproduces the "final answer hidden until refresh"
     bug: a tool-using turn streams [toolcall idx1] -> tool result -> [text idx1];
     the text must land in its own assistant message, not merge into the
     earlier tool-call block. */
  {
    const toolCall = (idx: number): ContentBlock => ({
      type: 'function_tool_call',
      streaming_meta: { index: idx },
      function_tool_call: { id: 'c1', name: 'read_file', arguments: '{}' },
    });
    const toolResult = (): ContentBlock => ({
      type: 'function_tool_result',
      function_tool_result: { call_id: 'c1', name: 'read_file', content: [] as never[] },
    });
    const textDelta = (idx: number, text: string): ContentBlock => ({
      type: 'assistant_gen_text',
      streaming_meta: { index: idx },
      assistant_gen_text: { text },
    });

    let state = makeBaseState({ isStreaming: true, messages: [userMsg()] });

    state = chatReducer(state, { type: 'STREAM_MESSAGE', conversationID: 1, role: 'assistant', blocks: [toolCall(1)] });
    state = chatReducer(state, { type: 'STREAM_MESSAGE', conversationID: 1, role: 'user', blocks: [toolResult()] });
    state = chatReducer(state, { type: 'STREAM_MESSAGE', conversationID: 1, role: 'assistant', blocks: [textDelta(1, 'final answer')] });

    const msgs = state.messages;
    if (msgs.length !== 4) {
      throw new Error(`STREAM_MESSAGE: expected 4 messages after a tool-using turn, got ${msgs.length}`);
    }
    if (msgs[1].role !== 'assistant' || msgs[1].content_blocks?.[0]?.type !== 'function_tool_call') {
      throw new Error('STREAM_MESSAGE: sub-response 1 must remain a function_tool_call assistant message');
    }
    if (msgs[2].role !== 'user') {
      throw new Error('STREAM_MESSAGE: tool result must be its own user message');
    }
    const finalMsg = msgs[3];
    if (finalMsg.role !== 'assistant') {
      throw new Error('STREAM_MESSAGE: final sub-response must be a separate assistant message');
    }
    const finalText = finalMsg.content_blocks?.find((b) => b.type === 'assistant_gen_text')?.assistant_gen_text?.text;
    if (finalText !== 'final answer') {
      throw new Error(`STREAM_MESSAGE: expected final answer in its own message, got ${JSON.stringify(finalText)}`);
    }
    if (msgs[1].content_blocks?.[0]?.assistant_gen_text) {
      throw new Error('STREAM_MESSAGE: tool-call block was mutated by the later text delta (index collision)');
    }
  }

  console.log('chatReducer tests passed');
}
