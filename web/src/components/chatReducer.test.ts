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

  console.log('chatReducer tests passed');
}
