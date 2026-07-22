import { computeContextUsed, computeCompactionAnnotated, isContextOverLimit, chatReducer } from './ChatProvider';
import type { ChatState } from './ChatProvider';
import { shouldRenderCompactionMarker, getSummaryText } from './chat/Renderers';
import type { ChatMessage, RawTurn } from '../types/chat';

function summaryMessage(summary: string): ChatMessage {
  return {
    id: 1,
    seq: 1,
    role: 'assistant',
    is_summary: true,
    content_blocks: [
      { type: 'assistant_gen_text', assistant_gen_text: { text: summary } },
    ],
  };
}

function normalMessage(): ChatMessage {
  return {
    id: 2,
    seq: 2,
    role: 'assistant',
    content_blocks: [
      { type: 'assistant_gen_text', assistant_gen_text: { text: 'normal reply' } },
    ],
  };
}

export function runCompactionTests() {
  /* S5: dispatch decision — summary turn renders as a compaction marker */
  if (shouldRenderCompactionMarker(summaryMessage('The earlier chat was about X')) !== true) {
    throw new Error('S5 Failed: a summary ChatMessage must render as a compaction marker');
  }

  /* S5: a normal message (is_summary falsy/undefined) does NOT render as a marker */
  if (shouldRenderCompactionMarker(normalMessage()) !== false) {
    throw new Error('S5 Failed: a normal message must NOT render as a compaction marker');
  }
  if (shouldRenderCompactionMarker({ role: 'assistant', content_blocks: [] }) !== false) {
    throw new Error('S5 Failed: a message with undefined is_summary must NOT render as a marker');
  }

  /* S5: summary text is correctly extracted from the assistant_gen_text block */
  const text = getSummaryText(summaryMessage('Summary body here'));
  if (text !== 'Summary body here') {
    throw new Error(`S5 Failed: getSummaryText returned "${text}", expected "Summary body here"`);
  }

  /* S6: computeContextUsed skips a trailing summary row and returns the prior turn's total_tokens */
  const turnsWithTrailingSummary: RawTurn[] = [
    { is_summary: false, prompt_tokens: 1200, total_tokens: 1300 },
    { is_summary: false, prompt_tokens: 800, total_tokens: 900 },
    { is_summary: true, prompt_tokens: 0, total_tokens: 0 },
  ];
  if (computeContextUsed(turnsWithTrailingSummary) !== 900) {
    throw new Error(
      `S6 Failed: computeContextUsed should skip trailing summary and return 900, got ${computeContextUsed(turnsWithTrailingSummary)}`
    );
  }

  /* S6: when the last turn is a normal turn, its total_tokens are used */
  const turnsNoSummary: RawTurn[] = [
    { is_summary: false, prompt_tokens: 1200, total_tokens: 1300 },
    { is_summary: false, prompt_tokens: 800, total_tokens: 900 },
  ];
  if (computeContextUsed(turnsNoSummary) !== 900) {
    throw new Error(`S6 Failed: computeContextUsed should return 900, got ${computeContextUsed(turnsNoSummary)}`);
  }

  /* S6: empty list returns 0 */
  if (computeContextUsed([]) !== 0) {
    throw new Error('S6 Failed: computeContextUsed must return 0 for an empty list');
  }

  /* S6: fallback to prompt_tokens when total_tokens is 0 on the last non-summary turn */
  const turnsFallback: RawTurn[] = [
    { is_summary: false, prompt_tokens: 500, total_tokens: 600 },
    { is_summary: true, prompt_tokens: 0, total_tokens: 0 },
  ];
  if (computeContextUsed(turnsFallback) !== 600) {
    throw new Error(`S6 Failed: computeContextUsed should return total_tokens 600, got ${computeContextUsed(turnsFallback)}`);
  }

  /* S6: one-time annotation — increased count while baseline established => true */
  if (computeCompactionAnnotated(1, 2, true) !== true) {
    throw new Error('S6 Failed: increased compaction_count (baseline established) must annotate true');
  }

  /* S6: one-time annotation — equal count => false */
  if (computeCompactionAnnotated(2, 2, true) !== false) {
    throw new Error('S6 Failed: equal compaction_count must annotate false');
  }

  /* S6: switching conversations (no baseline yet) => false even if count is higher */
  if (computeCompactionAnnotated(0, 2, false) !== false) {
    throw new Error('S6 Failed: first load of a conversation must NOT annotate (no baseline)');
  }

  /* Over-limit guard: used tokens exceeding the window disables input + flags meter */
  if (isContextOverLimit(5000, 5001) !== true) {
    throw new Error('Over-limit Failed: used > window must report over limit');
  }
  if (isContextOverLimit(5000, 5000) !== false) {
    throw new Error('Over-limit Failed: used == window must NOT report over limit (strict >)');
  }
  if (isContextOverLimit(5000, 3000) !== false) {
    throw new Error('Over-limit Failed: used < window must NOT report over limit');
  }
  /* Unknown window (0) keeps the guard inactive so input is never disabled on missing data */
  if (isContextOverLimit(0, 99999) !== false) {
    throw new Error('Over-limit Failed: window 0 must keep guard inactive');
  }

  /* S6: Live compaction state updates - SET_COMPACTING reducer transitions */
  const makeBaseState = (overrides?: Partial<ChatState>): ChatState => ({
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
  });

  const startCompactionState = chatReducer(makeBaseState(), { type: 'SET_COMPACTING', compacting: true });
  if (startCompactionState.isCompacting !== true) {
    throw new Error('S6 Live Failed: SET_COMPACTING started must set isCompacting to true');
  }

  const endCompactionState = chatReducer(startCompactionState, { type: 'SET_COMPACTING', compacting: false });
  if (endCompactionState.isCompacting !== false) {
    throw new Error('S6 Live Failed: SET_COMPACTING completed must set isCompacting to false');
  }

  /* S6: Live usage updates - SET_CONTEXT_USED reducer transitions */
  const usageState = chatReducer(makeBaseState(), { type: 'SET_CONTEXT_USED', usedSize: 450 });
  if (usageState.contextUsed !== 450) {
    throw new Error(`S6 Live Failed: SET_CONTEXT_USED must set contextUsed, expected 450, got ${usageState.contextUsed}`);
  }

  console.log('compaction tests passed');
}
