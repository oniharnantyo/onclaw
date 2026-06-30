import { useState, useEffect, useRef } from 'react';
import { PaperPlaneTilt, Cpu } from '@phosphor-icons/react';
import MessageBubble from './MessageBubble';
import type { Message, ContentBlock } from './MessageBubble';
import type { Agent } from './Agents';

interface ChatProps {
  agents: Agent[];
  chatAgent: string;
  setChatAgent: (name: string) => void;
  activeConvID: number | null;
  setActiveConvID: (id: number | null) => void;
  messages: Message[];
  loadMessages: (id: number) => void;
  loadConversations: () => void;
  showToast: (msg: string, type?: 'success' | 'error') => void;
}

export default function Chat({
  agents,
  chatAgent,
  setChatAgent,
  activeConvID,
  setActiveConvID,
  messages,
  loadMessages,
  loadConversations,
  showToast,
}: ChatProps) {
  const [chatPrompt, setChatPrompt] = useState('');
  const [isStreaming, setIsStreaming] = useState(false);
  const [streamingMessages, setStreamingMessages] = useState<Message[]>([]);
  const chatEndRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    chatEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages, streamingMessages]);

  const runChat = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!chatPrompt.trim() || isStreaming) return;

    const prompt = chatPrompt;
    setChatPrompt('');
    setIsStreaming(true);

    const userMsg: Message = {
      role: 'user',
      content_blocks: [{ type: 'text', assistant_gen_text: { text: prompt } }]
    };
    setStreamingMessages([userMsg]);

    try {
      const res = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          prompt,
          agent: chatAgent,
          conversation_id: activeConvID || 0
        })
      });

      if (!res.ok) {
        const errData = await res.json();
        showToast(errData.error || 'Chat stream failed to start', 'error');
        setIsStreaming(false);
        return;
      }

      const reader = res.body?.getReader();
      if (!reader) {
        showToast('ReadableStream not supported', 'error');
        setIsStreaming(false);
        return;
      }

      const decoder = new TextDecoder('utf-8');
      let buffer = '';
      let activeBlocks: ContentBlock[] = [];
      let tempConvID = activeConvID;

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
                tempConvID = data.conversation_id;
                setActiveConvID(tempConvID);
                loadConversations();
              } else if (event === 'message') {
                const newBlocks: ContentBlock[] = data.content_blocks || [];
                activeBlocks = [...activeBlocks, ...newBlocks];

                setStreamingMessages([
                  userMsg,
                  {
                    role: 'assistant',
                    content_blocks: activeBlocks
                  }
                ]);
              } else if (event === 'error') {
                showToast(data.error || 'Stream error occurred', 'error');
              }
            } catch (e) {
              console.error('Error parsing SSE block', e);
            }
          }
        }
      }

      setIsStreaming(false);
      setStreamingMessages([]);
      if (tempConvID) {
        loadMessages(tempConvID);
      }
      // Refocus input after stream completes
      setTimeout(() => inputRef.current?.focus(), 100);
    } catch {
      showToast('Stream interrupted due to connection error', 'error');
      setIsStreaming(false);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      if (chatPrompt.trim() && !isStreaming) {
        runChat(e as unknown as React.FormEvent);
      }
    }
  };

  const allMessages = messages.length > 0 ? messages : streamingMessages;
  const showEmpty = allMessages.length === 0;

  return (
    <div className="chat-area">
      <div
        className="message-list"
        role="log"
        aria-label="Conversation messages"
        aria-live="polite"
      >
        {showEmpty && (
          <div className="chat-empty-state" aria-label="No messages yet">
            <div className="empty-icon" aria-hidden="true">
              <Cpu size={26} weight="duotone" />
            </div>
            <h3>Agent Session Idle</h3>
            <p>
              Type a prompt below to start the ReAct execution loop.
              The agent will reason, act, and respond in real time.
            </p>
          </div>
        )}

        {/* Persisted messages */}
        {messages.map((m, idx) => (
          <MessageBubble key={m.id ?? idx} message={m} />
        ))}

        {/* Streaming messages */}
        {streamingMessages.map((m, idx) => (
          <MessageBubble
            key={`stream-${idx}`}
            message={m}
            isStreaming={isStreaming && m.role === 'assistant' && idx === streamingMessages.length - 1}
          />
        ))}

        <div ref={chatEndRef} aria-hidden="true" />
      </div>

      {/* Input area */}
      <div className="chat-input-area">
        <form
          className="chat-input-container"
          onSubmit={runChat}
          aria-label="Send a message"
        >
          {/* Agent selector */}
          <div className="agent-select-wrapper" style={{ position: 'relative' }}>
            <select
              id="chat-agent-select"
              className="form-select"
              value={chatAgent}
              onChange={(e) => setChatAgent(e.target.value)}
              disabled={isStreaming}
              aria-label="Select AI agent"
              style={{ width: '160px' }}
            >
              {agents.map((a) => (
                <option key={a.name} value={a.name}>
                  {a.name}{a.is_default ? ' ★' : ''}
                </option>
              ))}
            </select>
          </div>

          {/* Text input */}
          <input
            ref={inputRef}
            id="chat-prompt-input"
            type="text"
            className="form-input"
            placeholder={isStreaming ? 'Agent is responding…' : 'Ask agent to perform a task…'}
            value={chatPrompt}
            onChange={(e) => setChatPrompt(e.target.value)}
            onKeyDown={handleKeyDown}
            disabled={isStreaming}
            required
            autoComplete="off"
            aria-label="Message input"
            style={{ flex: 1 }}
          />

          {/* Send button */}
          <button
            id="chat-send-btn"
            type="submit"
            className="btn btn-primary btn-icon"
            disabled={isStreaming || !chatPrompt.trim()}
            aria-label={isStreaming ? 'Sending…' : 'Send message'}
            style={{ padding: '9px 14px', flexShrink: 0 }}
          >
            {isStreaming ? (
              <span
                style={{
                  width: '16px',
                  height: '16px',
                  border: '2px solid rgba(10,15,30,0.3)',
                  borderTopColor: '#0a0f1e',
                  borderRadius: '50%',
                  animation: 'spin 0.75s linear infinite',
                  display: 'inline-block',
                }}
                aria-hidden
              />
            ) : (
              <PaperPlaneTilt size={16} weight="fill" aria-hidden />
            )}
          </button>
        </form>

        {/* AI disclosure — UX guideline: label AI-generated content */}
        <p style={{
          textAlign: 'center',
          marginTop: '10px',
          fontSize: '11px',
          color: 'var(--text-subtle)',
          letterSpacing: '0.03em',
        }}>
          Responses are AI-generated. Review before acting on them.
        </p>
      </div>
    </div>
  );
}
