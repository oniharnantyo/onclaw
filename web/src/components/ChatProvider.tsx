import { createContext, useContext, useReducer, useCallback, useRef, useEffect, type ReactNode } from 'react';
import type { ChatMessage, ContentBlock, Conversation, SSEInitEvent, SSEMessageEvent, SSETurnEvent } from '../types/chat';

/* ── State ─────────────────────────────────────────────────── */

interface ChatState {
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
}

type ChatAction =
  | { type: 'SET_MESSAGES'; messages: ChatMessage[] }
  | { type: 'STREAM_INIT'; userMsg: ChatMessage }
  | { type: 'STREAM_MESSAGE'; conversationID: number; blocks: ContentBlock[] }
  | { type: 'STREAM_ERROR'; error: string }
  | { type: 'STREAM_DONE' }
  | { type: 'SET_CONVERSATIONS'; conversations: Conversation[] }
  | { type: 'SET_ACTIVE_CONV_ID'; id: number | null }
  | { type: 'SET_AGENTS'; agents: { name: string; is_default: boolean }[] }
  | { type: 'SET_CHAT_AGENT'; name: string }
  | { type: 'SET_SKILLS'; skills: { name: string; description: string }[] }
  | { type: 'SET_CONTEXT_WINDOW'; windowSize: number }
  | { type: 'SET_CONTEXT_USED'; usedSize: number };

function chatReducer(state: ChatState, action: ChatAction): ChatState {
  switch (action.type) {
    case 'SET_MESSAGES':
      return { ...state, messages: action.messages };
    case 'STREAM_INIT':
      return {
        ...state,
        isStreaming: true,
        messages: [],
        streamingStart: Date.now(),
      };
    case 'STREAM_MESSAGE': {
      const { blocks } = action;
      const lastMsg = state.messages[state.messages.length - 1];
      const isAssistant = lastMsg?.role === 'assistant';

      let msgs: ChatMessage[];
      if (isAssistant) {
        // Accumulate blocks into the last assistant message
        msgs = [
          ...state.messages.slice(0, -1),
          {
            ...lastMsg,
            content_blocks: [...(lastMsg.content_blocks || []), ...blocks],
            isStreaming: true,
          },
        ];
      } else {
        msgs = [
          ...state.messages,
          {
            role: 'assistant',
            content_blocks: blocks,
            isStreaming: true,
          },
        ];
      }

      return { ...state, messages: msgs };
    }
    case 'STREAM_ERROR':
      return { ...state, isStreaming: false };
    case 'STREAM_DONE': {
      const msgs = state.messages.map((m) => ({ ...m, isStreaming: false }));
      return { ...state, messages: msgs, isStreaming: false };
    }
    case 'SET_CONVERSATIONS':
      return { ...state, conversations: action.conversations || [] };
    case 'SET_ACTIVE_CONV_ID': {
      if (action.id === state.activeConvID) return state;
      return { ...state, activeConvID: action.id, messages: [], contextWindow: 0, contextUsed: 0 };
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
    default:
      return state;
  }
}

/* ── Context ───────────────────────────────────────────────── */

interface ChatContextValue {
  state: ChatState;
  dispatch: React.Dispatch<ChatAction>;
  runChat: (prompt: string, attachments?: ContentBlock[]) => Promise<void>;
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
    try {
      const res = await fetch(`/api/conversations/${convId}/messages`);
      if (res.ok) {
        const payload = await res.json();
        const rawMsgs = payload.messages || [];
        const contextWindow = payload.context_window || 0;
        dispatch({ type: 'SET_CONTEXT_WINDOW', windowSize: contextWindow });

        let contextUsed = 0;
        if (rawMsgs.length > 0) {
          const lastTurn = rawMsgs[rawMsgs.length - 1];
          contextUsed = lastTurn.prompt_tokens ?? lastTurn.total_tokens ?? 0;
        }
        dispatch({ type: 'SET_CONTEXT_USED', usedSize: contextUsed });

        const parsedMsgs: ChatMessage[] = [];
        for (const turn of rawMsgs) {
          let messages: any[] = [];
          try {
            messages = JSON.parse((turn.message as string) || '[]');
          } catch {
            messages = [];
          }
          // Track messages properties to implement Q/A fallbacks
          let hasUserMsg = false;
          let assistantMsg: any = null;
          let hasAssistantText = false;

          for (const msg of messages) {
            let role = msg.role as 'user' | 'assistant' | 'system';
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

    // Optimistically show user message
    dispatch({
      type: 'SET_MESSAGES',
      messages: state.activeConvID ? [...state.messages, userMsg] : [userMsg],
    });

    try {
      const res = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          prompt,
          agent: state.chatAgent,
          conversation_id: state.activeConvID || 0,
          content_blocks: attachments || [],
        }),
      });

      if (!res.ok) {
        const errData = await res.json();
        showToast(errData.error || 'Chat stream failed to start', 'error');
        dispatch({ type: 'STREAM_ERROR', error: errData.error || 'Unknown' });
        return;
      }

      const reader = res.body?.getReader();
      if (!reader) {
        showToast('ReadableStream not supported', 'error');
        dispatch({ type: 'STREAM_ERROR', error: 'ReadableStream not supported' });
        return;
      }

      const decoder = new TextDecoder('utf-8');
      let buffer = '';
      let tempConvID = state.activeConvID;
      let convSet = false;

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const blocks = buffer.split('\n\n');
        buffer = blocks.pop() || '';

        for (const block of blocks) {
          const lines = block.split('\n');
          let event = '';
          let dataStr = '';
          for (const line of lines) {
            if (line.startsWith('event: ')) event = line.slice(7).trim();
            if (line.startsWith('data: ')) dataStr = line.slice(6).trim();
          }

          if (dataStr) {
            try {
              const data = JSON.parse(dataStr);
              if (event === 'init') {
                const initData = data as SSEInitEvent;
                tempConvID = initData.conversation_id;
                activeConvIDRef.current = tempConvID;
                dispatch({ type: 'SET_ACTIVE_CONV_ID', id: tempConvID });
                if (initData.context_window) {
                  dispatch({ type: 'SET_CONTEXT_WINDOW', windowSize: initData.context_window });
                }
                if (!convSet) {
                  convSet = true;
                  fetchConversations();
                }
              } else if (event === 'message') {
                const msgData = data as SSEMessageEvent;
                dispatch({
                  type: 'STREAM_MESSAGE',
                  conversationID: tempConvID!,
                  blocks: msgData.content_blocks || [],
                });
              } else if (event === 'turn') {
                const turnData = data as SSETurnEvent;
                const used = turnData.prompt_tokens ?? turnData.tokens;
                if (typeof used === 'number') {
                  dispatch({ type: 'SET_CONTEXT_USED', usedSize: used });
                }
              } else if (event === 'error') {
                const errData = data as { error: string };
                showToast(errData.error || 'Stream error occurred', 'error');
              }
            } catch {
              // skip malformed data
            }
          }
        }
      }

      dispatch({ type: 'STREAM_DONE' });

      if (tempConvID) {
        // Small delay to let backend persist
        setTimeout(() => fetchMessages(tempConvID!), 200);
      }
    } catch {
      showToast('Stream interrupted due to connection error', 'error');
      dispatch({ type: 'STREAM_ERROR', error: 'Connection error' });
    }
  }, [state.isStreaming, state.chatAgent, state.activeConvID, showToast, fetchConversations, fetchMessages]);

  const selectConversation = useCallback((id: number) => {
    dispatch({ type: 'SET_ACTIVE_CONV_ID', id });
    fetchMessages(id);
  }, [fetchMessages]);

  const value: ChatContextValue = {
    state,
    dispatch,
    runChat,
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
    dispatch: chat.dispatch,
    runChat: chat.runChat,
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
