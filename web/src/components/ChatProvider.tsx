import { createContext, useContext, useReducer, useCallback, useRef, useEffect, type ReactNode } from 'react';
import type { ChatMessage, ContentBlock, Conversation, RawTurn } from '../types/chat';
import { mergeStreamingDeltas } from './chat/mergeBlockDelta';
import { runChatStream } from './chat/runChatStream';

/* ── Pure helpers (unit-testable without React) ───────────── */

/**
 * computeContextUsed walks backward through the turn rows and returns the
 * prompt_tokens of the most recent NON-summary turn (falling back to
 * total_tokens, then 0). Summary turns anchor the meter on zero prompt tokens,
 * so they must be skipped — otherwise a summary-ending conversation reads 0.
 */
export function computeContextUsed(rawMsgs: RawTurn[]): number {
  if (!rawMsgs || rawMsgs.length === 0) return 0;
  for (let i = rawMsgs.length - 1; i >= 0; i--) {
    const turn = rawMsgs[i];
    if (turn.is_summary) continue;
    return typeof turn.total_tokens === 'number' && turn.total_tokens > 0
      ? turn.total_tokens
      : (typeof turn.prompt_tokens === 'number' ? turn.prompt_tokens : 0);
  }
  return 0;
}

/**
 * isContextOverLimit reports whether the used token count has exceeded the
 * context window. When the window is unknown (0) the guard is inactive so the
 * meter and composer never disable input on missing data.
 */
export function isContextOverLimit(contextWindow: number, contextUsed: number): boolean {
  return contextWindow > 0 && contextUsed > contextWindow;
}

/**
 * computeCompactionAnnotated decides whether to show the one-time
 * "context compacted" annotation. Returns true only when a baseline has
 * already been established for the current conversation AND the new
 * compaction_count has increased. On the first load of a conversation
 * (baselineEstablished=false — e.g. right after switching) it stays false,
 * so reopening a previously-compacted conversation does not re-flash it.
 */
export function computeCompactionAnnotated(
  prevCount: number,
  newCount: number,
  baselineEstablished: boolean
): boolean {
  if (!baselineEstablished) return false;
  return newCount > prevCount;
}

/**
 * lastTurnResponseID returns the response_id of the most recent NON-summary
 * turn, so a resumed session can chain its first following chat onto the
 * prior response. Summary turns are skipped (they don't carry a usable id).
 */
export function lastTurnResponseID(rawMsgs: RawTurn[]): string {
  if (!rawMsgs || rawMsgs.length === 0) return '';
  for (let i = rawMsgs.length - 1; i >= 0; i--) {
    const turn = rawMsgs[i];
    if (turn.is_summary) continue;
    const rid = turn.response_id;
    return typeof rid === 'string' ? rid : '';
  }
  return '';
}

/**
 * isValidConversationId validates that a conversation ID is a positive integer.
 */
export function isValidConversationId(id: number | null | undefined): boolean {
  return typeof id === 'number' && Number.isInteger(id) && id > 0;
}


/* ── State ─────────────────────────────────────────────────── */

export interface ChatState {
  messages: ChatMessage[];
  isStreaming: boolean;
  streamingStart?: number;
  conversations: Conversation[];
  activeConvID: number | null;
  chatAgent: string;
  agents: { name: string; is_default: boolean }[];
  skills: { name: string; description: string }[];
  contextWindow: number;
  contextUsed: number;
  contextCompactionAnnotated: boolean;
  isCompacting: boolean;
  compactionProgress?: number | null;
}

type ChatAction =
  | { type: 'SET_MESSAGES'; messages: ChatMessage[] }
  | { type: 'STREAM_INIT'; userMsg: ChatMessage }
  | { type: 'STREAM_MESSAGE'; conversationID: number; role: 'assistant' | 'user'; blocks: ContentBlock[] }
  | { type: 'STREAM_ERROR'; error: string }
  | { type: 'STREAM_DONE' }
  | { type: 'STREAM_STOPPED' }
  | { type: 'SET_CONVERSATIONS'; conversations: Conversation[] }
  | { type: 'SET_ACTIVE_CONV_ID'; id: number | null }
  | { type: 'SET_AGENTS'; agents: { name: string; is_default: boolean }[] }
  | { type: 'SET_CHAT_AGENT'; name: string }
  | { type: 'SET_SKILLS'; skills: { name: string; description: string }[] }
  | { type: 'SET_CONTEXT_WINDOW'; windowSize: number }
  | { type: 'SET_CONTEXT_USED'; usedSize: number }
  | { type: 'SET_CONTEXT_COMPACTION_ANNOTATED'; annotated: boolean }
  | { type: 'SET_COMPACTING'; compacting: boolean }
  | { type: 'SET_COMPACTION_PROGRESS'; progress: number | null };

export function chatReducer(state: ChatState, action: ChatAction): ChatState {
  switch (action.type) {
    case 'SET_MESSAGES':
      return { ...state, messages: action.messages };
    case 'STREAM_INIT':
      return {
        ...state,
        isStreaming: true,
        streamingStart: Date.now(),
      };
    case 'STREAM_MESSAGE': {
      const { blocks, role } = action;
      // Trailing end-of-message markers carry role but no blocks; ignore them.
      if (blocks.length === 0) return state;
      const lastMsg = state.messages[state.messages.length - 1];

      // Tool results arrive as user-role messages. Keep them in their own
      // message (merging consecutive same-role) so each assistant sub-response
      // is a separate message. streaming_meta.index is unique only within one
      // assistant message; if a tool result is folded into the running
      // assistant message, the next sub-response reuses indices 0,1,... and its
      // text delta collides with the prior tool-call block — hiding the live
      // answer until a refresh re-syncs from the correctly-separated history.
      if (role === 'user') {
        if (lastMsg?.role === 'user') {
          return {
            ...state,
            messages: [
              ...state.messages.slice(0, -1),
              { ...lastMsg, content_blocks: [...(lastMsg.content_blocks || []), ...blocks] },
            ],
          };
        }
        return {
          ...state,
          messages: [...state.messages, { role: 'user', content_blocks: blocks }],
        };
      }

      // Assistant deltas: merge by streaming_meta.index so token-level fragments
      // accumulate into the correct content block instead of appending wholes.
      if (lastMsg?.role === 'assistant') {
        const msgs: ChatMessage[] = [
          ...state.messages.slice(0, -1),
          {
            ...lastMsg,
            content_blocks: mergeStreamingDeltas(lastMsg.content_blocks || [], blocks),
            isStreaming: true,
          },
        ];
        return { ...state, messages: msgs };
      }

      const msgs: ChatMessage[] = [
        ...state.messages,
        {
          role: 'assistant',
          content_blocks: mergeStreamingDeltas([], blocks),
          isStreaming: true,
        },
      ];
      return { ...state, messages: msgs };
    }
    case 'STREAM_ERROR':
      return { ...state, isStreaming: false };
    case 'STREAM_DONE': {
      const msgs = state.messages.map((m) => ({ ...m, isStreaming: false }));
      return { ...state, messages: msgs, isStreaming: false };
    }
    case 'STREAM_STOPPED': {
      let lastAssistant = -1;
      for (let i = state.messages.length - 1; i >= 0; i--) {
        if (state.messages[i].role === 'assistant') {
          lastAssistant = i;
          break;
        }
      }
      const msgs = state.messages.map((m, i) =>
        i === lastAssistant ? { ...m, isStreaming: false, stopped: true } : m
      );
      return { ...state, messages: msgs, isStreaming: false };
    }
    case 'SET_CONVERSATIONS':
      return { ...state, conversations: action.conversations || [] };
    case 'SET_ACTIVE_CONV_ID': {
      if (action.id === state.activeConvID) return state;
      return {
        ...state,
        activeConvID: action.id,
        messages: state.isStreaming ? state.messages : [],
        contextWindow: 0,
        contextUsed: 0,
        contextCompactionAnnotated: false,
        isCompacting: false,
        compactionProgress: null,
      };
    }
    case 'SET_AGENTS':
      return { ...state, agents: action.agents };
    case 'SET_CHAT_AGENT':
      return { ...state, chatAgent: action.name };
    case 'SET_SKILLS':
      return { ...state, skills: action.skills };
    case 'SET_CONTEXT_WINDOW':
      return { ...state, contextWindow: action.windowSize };
    case 'SET_CONTEXT_USED':
      return { ...state, contextUsed: action.usedSize };
    case 'SET_CONTEXT_COMPACTION_ANNOTATED':
      return { ...state, contextCompactionAnnotated: action.annotated };
    case 'SET_COMPACTING':
      return { ...state, isCompacting: action.compacting };
    case 'SET_COMPACTION_PROGRESS':
      return { ...state, compactionProgress: action.progress };
    default:
      return state;
  }
}

/* ── Context ───────────────────────────────────────────────── */

interface ChatContextValue {
  state: ChatState;
  dispatch: React.Dispatch<ChatAction>;
  runChat: (prompt: string, attachments?: ContentBlock[]) => Promise<void>;
  stopChat: () => void;
  loadConversations: () => Promise<void>;
  loadMessages: (convId: number) => Promise<void>;
  loadSkills: () => Promise<void>;
  selectConversation: (id: number) => void;
  showToast: (msg: string, type?: 'success' | 'error') => void;
}

const ChatContext = createContext<ChatContextValue | null>(null);

export function useChat(): ChatContextValue {
  const ctx = useContext(ChatContext);
  if (!ctx) throw new Error('useChat must be used within ChatProvider');
  return ctx;
}

/* ── Provider ──────────────────────────────────────────────── */

interface ChatProviderProps {
  children: ReactNode;
  initialAgents?: { name: string; is_default: boolean }[];
  initialSkills?: { name: string; description: string }[];
  initialConversations?: Conversation[];
  defaultAgent?: string;
  showToast: (msg: string, type?: 'success' | 'error') => void;
}

export default function ChatProvider({
  children,
  initialAgents = [],
  initialSkills = [],
  initialConversations = [],
  defaultAgent = '',
  showToast,
}: ChatProviderProps) {
  const [state, dispatch] = useReducer(chatReducer, {
    messages: [],
    isStreaming: false,
    conversations: initialConversations,
    activeConvID: null,
    chatAgent: defaultAgent,
    agents: initialAgents,
    skills: initialSkills,
    contextWindow: 0,
    contextUsed: 0,
    contextCompactionAnnotated: false,
    isCompacting: false,
    compactionProgress: null,
  });

  // Sync state when props change (fix for race condition)
  useEffect(() => {
    dispatch({ type: 'SET_AGENTS', agents: initialAgents });
  }, [initialAgents]);

  useEffect(() => {
    dispatch({ type: 'SET_SKILLS', skills: initialSkills });
  }, [initialSkills]);

  useEffect(() => {
    dispatch({ type: 'SET_CONVERSATIONS', conversations: initialConversations });
  }, [initialConversations]);

  const activeConvIDRef = useRef(state.activeConvID);

  const abortRef = useRef<AbortController | null>(null);

  // Compaction annotation bookkeeping, keyed per conversation:
  // prevCompactionCountRef holds the last-seen compaction_count for the
  // current conversation; convSeenRef marks whether a baseline has been
  // established (i.e. fetchMessages has run at least once for it). Both are
  // reset to "no baseline" whenever the active conversation changes so that
  // reopening a previously-compacted conversation does not re-flash the
  // annotation. The ref survives the post-turn re-fetch in runChat.onDone
  // because that path does NOT dispatch SET_ACTIVE_CONV_ID.
  const prevCompactionCountRef = useRef(0);
  const convSeenRef = useRef(false);

  // response_id of the most recent committed turn for the active conversation.
  // Sent as previous_response_id on the next (following) chat so the Responses
  // API chains onto the prior turn. Reset on conversation switch; seeded from
  // history on resume.
  const lastResponseIDRef = useRef('');

  const fetchConversations = useCallback(async () => {
    try {
      const res = await fetch('/api/conversations');
      if (res.ok) {
        const data: Conversation[] = await res.json();
        dispatch({ type: 'SET_CONVERSATIONS', conversations: data });
      }
    } catch {
      showToast('Failed to load conversations', 'error');
    }
  }, [showToast]);

  const fetchMessages = useCallback(async (convId: number) => {
    if (!isValidConversationId(convId)) return;
    try {
      const res = await fetch(`/api/conversations/${convId}/messages`);
      if (res.ok) {
        const payload = await res.json();
        const rawMsgs: RawTurn[] = payload.messages || [];
        const seeded = lastTurnResponseID(rawMsgs);
        if (seeded) {
          lastResponseIDRef.current = seeded;
        }
        const contextWindow = payload.context_window || 0;
        dispatch({ type: 'SET_CONTEXT_WINDOW', windowSize: contextWindow });

        // Guard: skip any trailing summary turn(s) so the meter's `used`
        // value is anchored on a real prompt-token count, never on a summary
        // row (which reports zero prompt tokens).
        const contextUsed = computeContextUsed(rawMsgs);
        dispatch({ type: 'SET_CONTEXT_USED', usedSize: contextUsed });

        // One-time compaction annotation: surfaces when compaction_count
        // increases for the current conversation. The first load establishes
        // the baseline without annotating; re-fetches within the same
        // conversation detect the increase.
        const newCount: number = typeof payload.compaction_count === 'number' ? payload.compaction_count : 0;
        const annotated = computeCompactionAnnotated(
          prevCompactionCountRef.current,
          newCount,
          convSeenRef.current
        );
        prevCompactionCountRef.current = newCount;
        convSeenRef.current = true;
        dispatch({ type: 'SET_CONTEXT_COMPACTION_ANNOTATED', annotated });

        const parsedMsgs: ChatMessage[] = [];
        for (const turn of rawMsgs) {
          let messages: any[] = [];
          try {
            messages = JSON.parse((turn.message as string) || '[]');
          } catch {
            messages = [];
          }

          // Handle corrupted/old message formats where role field is missing
          // If messages array is empty but question/answer exist, reconstruct from fallback fields
          if (!Array.isArray(messages) || messages.length === 0) {
            if (typeof turn.question === 'string' && turn.question.trim()) {
              messages.push({
                role: 'user',
                content_blocks: [{
                  type: 'user_input_text',
                  user_input_text: { text: turn.question }
                }]
              });
            }
            if (typeof turn.answer === 'string' && turn.answer.trim()) {
              messages.push({
                role: 'assistant',
                content_blocks: [{
                  type: 'assistant_gen_text',
                  assistant_gen_text: { text: turn.answer }
                }]
              });
            }
          }

          // Track messages properties to implement Q/A fallbacks
          let hasUserMsg = false;
          let assistantMsg: any = null;
          let hasAssistantText = false;

          for (const msg of messages) {
            let role = msg.role as 'user' | 'assistant' | 'system';

            // Handle missing role field - infer from content or fallback
            if (!role || (role !== 'user' && role !== 'assistant' && role !== 'system')) {
              // Try to infer role from content blocks
              const hasAssistantContent = msg.content_blocks?.some((b: any) =>
                b.assistant_gen_text || b.assistant_gen_image || b.function_tool_call
              );
              role = hasAssistantContent ? 'assistant' : 'user';
            }

            let content_blocks = msg.content_blocks || [];

            // Handle flat schema.Message formatting where text is under the 'content' key
            if (content_blocks.length === 0 && typeof msg.content === 'string' && msg.content) {
              if (role === 'user') {
                content_blocks = [{
                  type: 'user_input_text',
                  user_input_text: { text: msg.content }
                }];
              } else if (role === 'assistant') {
                content_blocks = [{
                  type: 'assistant_gen_text',
                  assistant_gen_text: { text: msg.content }
                }];
              }
            }

            if (role === 'user') {
              hasUserMsg = true;
            } else if (role === 'assistant') {
              assistantMsg = msg;
              if (content_blocks.some((b: any) => b.assistant_gen_text?.text?.trim())) {
                hasAssistantText = true;
              }
            }
            msg.content_blocks = content_blocks;
          }

          // Fallback check for missing user message
          if (!hasUserMsg && typeof turn.question === 'string' && turn.question.trim()) {
            messages.unshift({
              role: 'user',
              content_blocks: [{
                type: 'user_input_text',
                user_input_text: { text: turn.question }
              }]
            });
          }

          // Fallback check for missing assistant response text
          if (typeof turn.answer === 'string' && turn.answer.trim()) {
            if (assistantMsg) {
              if (!hasAssistantText) {
                assistantMsg.content_blocks.push({
                  type: 'assistant_gen_text',
                  assistant_gen_text: { text: turn.answer }
                });
              }
            } else {
              messages.push({
                role: 'assistant',
                content_blocks: [{
                  type: 'assistant_gen_text',
                  assistant_gen_text: { text: turn.answer }
                }]
              });
            }
          }

          for (const msg of messages) {
            let role = msg.role as 'user' | 'assistant' | 'system';
            const content_blocks = msg.content_blocks || [];

            // Tool results are stored as 'user' turns/messages in the DB but should render
            // as part of the assistant's message in the UI
            if (role === 'user' && content_blocks?.some((b: any) => b.function_tool_result)) {
              role = 'assistant';
            }
            parsedMsgs.push({
              id: turn.id as number,
              seq: turn.sequence_num as number,
              role,
              content_blocks,
              created_at: turn.created_at as string,
              is_summary: (turn as RawTurn).is_summary ?? false,
            });
          }
        }
        dispatch({ type: 'SET_MESSAGES', messages: parsedMsgs });
      }
    } catch {
      showToast('Failed to load conversation history', 'error');
    }
  }, [showToast]);

  const fetchSkills = useCallback(async () => {
    try {
      const res = await fetch('/api/skills');
      if (res.ok) {
        const data = await res.json();
        dispatch({
          type: 'SET_SKILLS',
          skills: (data || []).map((s: { name: string; description: string }) => ({
            name: s.name,
            description: s.description,
          })),
        });
      }
    } catch {
      // silently fail
    }
  }, []);

  const stopChat = useCallback(() => {
    abortRef.current?.abort();
  }, []);

  const runChat = useCallback(async (prompt: string, attachments?: ContentBlock[]) => {
    if (!prompt.trim() || state.isStreaming) return;

    const userMsg: ChatMessage = {
      role: 'user',
      content_blocks: [
        { type: 'user_input_text', user_input_text: { text: prompt } },
        ...(attachments || []),
      ],
      created_at: new Date().toISOString(),
    };

    dispatch({ type: 'STREAM_INIT', userMsg });
    dispatch({ type: 'SET_COMPACTION_PROGRESS', progress: null });

    // Optimistically show user message
    dispatch({
      type: 'SET_MESSAGES',
      messages: state.activeConvID ? [...state.messages, userMsg] : [userMsg],
    });

    const controller = new AbortController();
    abortRef.current = controller;

    const tempConvID0 = state.activeConvID;
    let tempConvID = tempConvID0;
    let convSet = false;

    const body = JSON.stringify({
      prompt,
      agent: state.chatAgent,
      conversation_id: tempConvID0 || 0,
      previous_response_id: lastResponseIDRef.current,
      content_blocks: attachments || [],
    });

    try {
      await runChatStream(body, controller.signal, {
        onInit: (initData) => {
          tempConvID = initData.conversation_id;
          activeConvIDRef.current = tempConvID;
          // New conversation: clear compaction baseline so it starts fresh.
          prevCompactionCountRef.current = 0;
          convSeenRef.current = false;
          dispatch({ type: 'SET_ACTIVE_CONV_ID', id: tempConvID });
          if (initData.context_window) {
            dispatch({ type: 'SET_CONTEXT_WINDOW', windowSize: initData.context_window });
          }
          if (!tempConvID0 && !convSet) {
            convSet = true;
            fetchConversations();
          }
        },
        onMessage: (msgData) => {
          dispatch({
            type: 'STREAM_MESSAGE',
            conversationID: tempConvID!,
            role: msgData.role === 'user' ? 'user' : 'assistant',
            blocks: msgData.content_blocks || [],
          });
          const usage = msgData.response_meta?.token_usage as { total_tokens?: number; totalTokens?: number; prompt_tokens?: number } | undefined;
          const tokens = typeof usage?.total_tokens === 'number' && usage.total_tokens > 0
            ? usage.total_tokens
            : (typeof usage?.totalTokens === 'number' && usage.totalTokens > 0
              ? usage.totalTokens
              : usage?.prompt_tokens);
          if (typeof tokens === 'number' && tokens > 0) {
            dispatch({ type: 'SET_CONTEXT_USED', usedSize: tokens });
          }
        },
        onTurn: (turnData) => {
          if (turnData.response_id) {
            lastResponseIDRef.current = turnData.response_id;
          }
          const used = typeof turnData.total_tokens === 'number' && turnData.total_tokens > 0
            ? turnData.total_tokens
            : (typeof turnData.tokens === 'number' && turnData.tokens > 0
              ? turnData.tokens
              : turnData.prompt_tokens);
          if (typeof used === 'number' && used > 0) {
            dispatch({ type: 'SET_CONTEXT_USED', usedSize: used });
          }
        },
        onCompaction: (compactionData) => {
          dispatch({ type: 'SET_COMPACTING', compacting: compactionData.status === 'started' });
        },
        onCompactionProgress: (compactionProgressData) => {
          dispatch({ type: 'SET_COMPACTION_PROGRESS', progress: compactionProgressData.progress });
        },
        onUsage: (usageData) => {
          const used = typeof usageData.total_tokens === 'number' && usageData.total_tokens > 0
            ? usageData.total_tokens
            : usageData.prompt_tokens;
          if (typeof used === 'number' && used > 0) {
            dispatch({ type: 'SET_CONTEXT_USED', usedSize: used });
          }
        },
        onStreamError: (err) => {
          // The SSE "error" event is terminal: the backend has already returned
          // (chat.go writes it then exits). Clear compacting + isStreaming so the
          // UI never hangs on "loading" — matches onConnectionError.
          dispatch({ type: 'SET_COMPACTING', compacting: false });
          dispatch({ type: 'SET_COMPACTION_PROGRESS', progress: null });
          showToast(err, 'error');
          dispatch({ type: 'STREAM_ERROR', error: err });
        },
        onDone: () => {
          dispatch({ type: 'SET_COMPACTING', compacting: false });
          dispatch({ type: 'SET_COMPACTION_PROGRESS', progress: null });
          dispatch({ type: 'STREAM_DONE' });
        },
        onStopped: () => {
          dispatch({ type: 'SET_COMPACTING', compacting: false });
          dispatch({ type: 'SET_COMPACTION_PROGRESS', progress: null });
          dispatch({ type: 'STREAM_STOPPED' });
        },
        onConnectionError: (err) => {
          dispatch({ type: 'SET_COMPACTING', compacting: false });
          dispatch({ type: 'SET_COMPACTION_PROGRESS', progress: null });
          showToast(err, 'error');
          dispatch({ type: 'STREAM_ERROR', error: err });
        },
      });
    } finally {
      abortRef.current = null;
    }
  }, [state.isStreaming, state.chatAgent, state.activeConvID, showToast, fetchConversations, fetchMessages]);

  const selectConversation = useCallback((id: number) => {
    // Clear compaction baseline so reopening a conversation never re-flashes
    // the annotation; the first fetchMessages establishes a fresh baseline.
    prevCompactionCountRef.current = 0;
    convSeenRef.current = false;
    lastResponseIDRef.current = '';
    dispatch({ type: 'SET_ACTIVE_CONV_ID', id });
    fetchMessages(id);
  }, [fetchMessages]);

  const value: ChatContextValue = {
    state,
    dispatch,
    runChat,
    stopChat,
    loadConversations: fetchConversations,
    loadMessages: fetchMessages,
    loadSkills: fetchSkills,
    selectConversation,
    showToast,
  };

  return <ChatContext.Provider value={value}>{children}</ChatContext.Provider>;
}

/* ── Selector Hooks ────────────────────────────────────────── */

export function useThread() {
  const chat = useChat();
  return {
    messages: chat.state.messages,
    isStreaming: chat.state.isStreaming,
    isCompacting: chat.state.isCompacting,
    compactionProgress: chat.state.compactionProgress,
    activeConvID: chat.state.activeConvID,
    runChat: chat.runChat,
    selectConversation: chat.selectConversation,
  };
}

export function useComposer() {
  const chat = useChat();
  return {
    chatAgent: chat.state.chatAgent,
    agents: chat.state.agents,
    skills: chat.state.skills,
    isStreaming: chat.state.isStreaming,
    contextOverLimit: isContextOverLimit(chat.state.contextWindow, chat.state.contextUsed),
    dispatch: chat.dispatch,
    runChat: chat.runChat,
    stopChat: chat.stopChat,
  };
}

export function useThreadList() {
  const chat = useChat();
  return {
    conversations: chat.state.conversations,
    activeConvID: chat.state.activeConvID,
    selectConversation: chat.selectConversation,
    loadConversations: chat.loadConversations,
  };
}

export interface MessageContextValue {
  message: ChatMessage;
  index: number;
  isLast: boolean;
}

export const MessageContext = createContext<MessageContextValue | null>(null);

export function useMessage() {
  const ctx = useContext(MessageContext);
  if (!ctx) throw new Error('useMessage must be used within Message.Root');
  return ctx;
}
