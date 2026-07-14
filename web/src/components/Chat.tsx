import { useRef, Fragment } from 'react';
import { Cpu, Code, PaperPlaneTilt, FileText, X, Plus, Stop } from '@phosphor-icons/react';
import { useChat, useComposer, isContextOverLimit } from './ChatProvider';
import { Thread } from './primitives/Thread';
import { ThreadList } from './primitives/ThreadList';
import { Message } from './primitives/Message';
import { Composer } from './primitives/Composer';

import Sources from './chat/Sources';
import { isMessageVisible } from './chat/groupBlocks';
import {
  MarkdownBlock,
  ReasoningBlock,
  ToolCallBlock,
  ToolResultBlock,
  MCPCalledBlock,
  ImageBlock,
  FileBlock,
  UnknownBlock,
  pickToolRenderer,
  CompactionMarker,
  shouldRenderCompactionMarker
} from './chat/Renderers';
import type { ContentBlock, Conversation } from '../types/chat';

function getConversationTitle(c: Conversation) {
  if (c.preview) {
    const msg = c.preview.trim();
    if (msg.startsWith('{') || msg.startsWith('[')) {
      try {
        const parsed = JSON.parse(msg);
        if (parsed?.content) {
          return parsed.content;
        }
        const textBlock = parsed?.content_blocks?.find(
          (b: any) => b.user_input_text?.text
        );
        if (textBlock?.user_input_text?.text) {
          return textBlock.user_input_text.text;
        }
      } catch {
        // ignore JSON parse error
      }
    } else {
      return c.preview;
    }
  }
  return `Session #${c.id}`;
}

interface ChatProps {
  onNewConversation: () => void;
}

function renderSingleBlock(block: ContentBlock) {
  if (block.type === 'reasoning' || block.reasoning) {
    return <ReasoningBlock block={block} />;
  }
  if (block.function_tool_call) {
    const Specific = pickToolRenderer(block.function_tool_call.name);
    if (Specific) return <Specific block={block} />;
    return <ToolCallBlock block={block} />;
  }
  if (block.function_tool_result) {
    return <ToolResultBlock block={block} />;
  }
  if (block.mcp_tool_call || block.mcp_tool_result) {
    return <MCPCalledBlock block={block} />;
  }
  if (block.assistant_gen_image || block.user_input_image) {
    return <ImageBlock block={block} />;
  }
  if (block.user_input_file) {
    return <FileBlock block={block} />;
  }
  // Check for text content with trim() to match filtering logic
  if (block.assistant_gen_text?.text?.trim()) {
    return <MarkdownBlock text={block.assistant_gen_text.text} />;
  }
  if (block.user_input_text?.text?.trim()) {
    return <div className="block-text">{block.user_input_text.text}</div>;
  }
  return <UnknownBlock block={block} />;
}

function AgentSelector() {
  const { chatAgent, agents, dispatch, isStreaming } = useComposer();

  return (
    <select
      className="composer-agent-select"
      value={chatAgent}
      onChange={(e) => dispatch({ type: 'SET_CHAT_AGENT', name: e.target.value })}
      disabled={isStreaming}
      aria-label="Select AI agent"
    >
      {agents.map((a) => (
        <option key={a.name} value={a.name}>
          {a.name}{a.is_default ? ' ★' : ''}
        </option>
      ))}
    </select>
  );
}

function SendIcon() {
  const { isStreaming } = useComposer();

  if (isStreaming) {
    return (
      <span
        className="animate-spin"
        style={{
          width: '16px',
          height: '16px',
          border: '2px solid rgba(10,15,30,0.3)',
          borderTopColor: '#0a0f1e',
          borderRadius: '50%',
          display: 'inline-block',
        }}
        aria-hidden
      />
    );
  }
  return <PaperPlaneTilt size={16} weight="fill" aria-hidden />;
}

// Toggle between the stop (cancel) control while streaming and the send
// control while idle. Extracted so the toggle is unit-testable without a DOM.
export function ComposerActions({ isStreaming, stopChat }: { isStreaming: boolean; stopChat: () => void }) {
  if (isStreaming) {
    return (
      <Composer.Cancel className="composer-cancel-btn" onClick={stopChat}>
        <Stop size={16} weight="fill" aria-hidden />
      </Composer.Cancel>
    );
  }
  return (
    <Composer.Send className="composer-send-btn">
      <SendIcon />
    </Composer.Send>
  );
}

function formatRelativeTime(dateStr: string) {
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / 60000);
  if (diffMins < 1) return 'just now';
  if (diffMins < 60) return `${diffMins}m ago`;
  const diffHours = Math.floor(diffMins / 60);
  if (diffHours < 24) return `${diffHours}h ago`;
  return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
}



function formatMessageTimeOnly(dateStr?: string) {
  if (!dateStr) return '';
  const date = new Date(dateStr);
  if (isNaN(date.getTime())) return '';
  
  const options: Intl.DateTimeFormatOptions = {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false
  };
  return date.toLocaleString(undefined, options);
}

function ContextMeter() {
  const { state } = useChat();
  const { contextWindow, contextUsed } = state;

  if (!contextWindow || contextWindow <= 0) {
    return null;
  }

  const contextOverLimit = isContextOverLimit(contextWindow, contextUsed);
  const percentage = Math.min(100, Math.max(0, (contextUsed / contextWindow) * 100));

  const formatTokens = (n: number) => {
    if (n >= 1000) {
      return (n / 1000).toFixed(1).replace(/\.0$/, '') + 'k';
    }
    return n.toString();
  };

  return (
    <div
      className={`context-meter ${contextOverLimit ? 'over' : ''}`}
      title={`Context window usage: ${contextUsed} / ${contextWindow} tokens (${percentage.toFixed(1)}%)${contextOverLimit ? ' — exceeded' : ''}`}
    >
      <span className="context-meter-label">
        {formatTokens(contextUsed)} / {formatTokens(contextWindow)}
      </span>
      <div className="context-meter-track">
        <div
          className="context-meter-fill"
          style={{ width: `${percentage}%` }}
        />
      </div>
      {state.contextCompactionAnnotated && (
        <span
          className="context-meter-annotation"
          title="Context was compacted: earlier messages were summarized to free up the context window."
        >
          Context compacted
        </span>
      )}
      {contextOverLimit && (
        <span
          className="context-meter-warning"
          title="The conversation exceeds the context window. Reduce the conversation or raise max context tokens before sending."
        >
          Context limit exceeded
        </span>
      )}
    </div>
  );
}

function ChatHeaderBar() {
  return (
    <div className="chat-header-bar">
      <div className="chat-header-left">
        <AgentSelector />
      </div>
      <div className="chat-header-right">
        <ContextMeter />
      </div>
    </div>
  );
}

export default function Chat({ onNewConversation }: ChatProps) {
  const { state, selectConversation } = useChat();
  const { isStreaming, stopChat } = useComposer();
  const viewportRef = useRef<HTMLDivElement>(null);

  const visibleMessages = state.messages.filter((m) => isMessageVisible(m, state.messages));
  const visibleMessagesCount = visibleMessages.length;
  // check isLastInTurn inline below

  return (
    <div className="chat-container">
      {/* 1. Sidebar (ThreadList primitive) */}
      <ThreadList className="chat-history">
        <div className="chat-history-header">
          <span className="chat-history-title">History</span>
        </div>

        <div className="chat-sidebar-actions" style={{ padding: '10px 10px 4px' }}>
          <ThreadList.New
            onClick={onNewConversation}
            className="thread-list-new"
          >
            <Plus size={14} weight="bold" />
            <span>New Session</span>
          </ThreadList.New>
        </div>

        <ThreadList.Items>
          {(c, active) => (
            <div
              key={c.id}
              className={`chat-history-item ${active ? 'active' : ''}`}
              role="listitem"
              onClick={() => selectConversation(c.id)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  selectConversation(c.id);
                }
              }}
              tabIndex={0}
              aria-label={`Conversation ${c.id} with ${c.agent_name}`}
            >
              <div className="chat-history-item-title">
                <span style={{ display: 'inline-flex', alignItems: 'center', gap: '4px' }}>
                  <Cpu size={12} weight="duotone" />
                  {' '}{getConversationTitle(c)}
                </span>
              </div>
              <div className="chat-history-item-meta">
                {c.agent_name} · {c.message_count} msg · {formatRelativeTime(c.updated_at)}
              </div>
            </div>
          )}
        </ThreadList.Items>
      </ThreadList>

      {/* 2. Main Thread View (Thread primitive) */}
      <Thread.Root>
        <div className="thread-main">
          <ChatHeaderBar />
          <div style={{ position: 'relative', display: 'flex', flexDirection: 'column', flex: 1, minHeight: 0 }}>
            {/* Viewport for messages */}
            <Thread.Viewport ref={viewportRef} className="message-list">
              {/* Empty state */}
              <Thread.Empty>
                <div className="chat-empty-state" aria-label="No messages yet">
                  <div className="empty-icon" aria-hidden="true">
                    <Cpu size={26} weight="duotone" />
                  </div>
                  <h3>Agent Session Idle</h3>
                  <p>
                    Hi. What can i help you today?
                  </p>
                </div>
              </Thread.Empty>

              {/* Messages list */}
              <Thread.Messages>
                {(msg, idx) => {
                  // A flagged summary turn renders as a compaction boundary
                  // marker, never as a normal assistant bubble.
                  if (shouldRenderCompactionMarker(msg)) {
                    return (
                      <Fragment key={msg.id ?? idx}>
                        <CompactionMarker message={msg} />
                      </Fragment>
                    );
                  }
                  return (
                  <Fragment key={msg.id ?? idx}>
                    <Message.Root
                      message={msg}
                      index={idx}
                      isLast={idx === visibleMessagesCount - 1}
                      className={`message-bubble ${msg.role}`}
                    >
                    <div className="message-content">
                      <Message.Parts>
                        {(group, groupIdx) => (
                          <div key={groupIdx}>{renderSingleBlock(group.block)}</div>
                        )}
                      </Message.Parts>

                      {/* Sources list */}
                      {msg.role === 'assistant' && (
                        <Sources blocks={msg.content_blocks} />
                      )}

                      {/* Action Bar & inline timestamp */}
                      {msg.role === 'assistant' && (idx === visibleMessagesCount - 1 || visibleMessages[idx + 1]?.role === 'user') && (
                        <div className="message-footer" style={{ display: 'flex', alignItems: 'center', gap: '8px', marginTop: '8px' }}>
                          {msg.created_at && (
                            <time className="message-time-inline" dateTime={msg.created_at} style={{ fontSize: '11px', color: 'var(--text-muted, #86868b)', userSelect: 'none' }}>
                              {formatMessageTimeOnly(msg.created_at)}
                            </time>
                          )}
                          {msg.stopped && (
                            <span className="message-stopped-badge">stopped</span>
                          )}
                          <Message.ActionBar className="no-margin-top" />
                        </div>
                      )}

                      {/* Streaming typing indicator */}
                      {state.isStreaming && msg.role === 'assistant' && idx === visibleMessagesCount - 1 && (
                        <span className="streaming-indicator" aria-label="Agent is typing" aria-live="polite">
                          <span />
                          <span />
                          <span />
                        </span>
                      )}
                    </div>
                  </Message.Root>
                </Fragment>
                  );
                }}
              </Thread.Messages>
            </Thread.Viewport>

            {/* Float Scroll to Bottom button */}
            <Thread.ScrollToBottom viewportRef={viewportRef} className="btn-scroll-bottom">
              ↓
            </Thread.ScrollToBottom>
          </div>

          {/* 3. Composer Primitive */}
          <Composer className="chat-input-area">
            <div className="chat-input-container">
              <div className="chat-input-box">
                {/* Text area input */}
                <div style={{ position: 'relative', width: '100%' }}>
                  <Composer.Input placeholder="Ask agent to perform a task… (Shift+Enter for new line)" className="composer-input" />

                  {/* Popover trigger */}
                  <Composer.TriggerPopover className="skill-popover">
                    {(filteredSkills, insertSkill) => (
                      <>
                        {filteredSkills.map((s) => (
                          <button
                            key={s.name}
                            className="skill-popover-item"
                            role="option"
                            onClick={() => insertSkill(s.name)}
                            type="button"
                          >
                            <Code size={14} weight="duotone" aria-hidden />
                            <div style={{ display: 'flex', flexDirection: 'column', gap: '1px', textAlign: 'left' }}>
                              <strong style={{ fontSize: '12.5px' }}>{s.name}</strong>
                              {s.description && (
                                <span style={{ fontSize: '11px', color: 'var(--text-muted)' }}>
                                  {s.description}
                                </span>
                              )}
                            </div>
                          </button>
                        ))}
                      </>
                    )}
                  </Composer.TriggerPopover>
                </div>

                {/* Media attachments preview */}
                <Composer.PastePreview>
                  {(att, remove) => (
                    <div key={att.id} className="paste-preview-chip">
                      {att.url ? (
                        <img src={att.url} alt="Paste preview" className="paste-preview-image" />
                      ) : (
                        <FileText size={16} className="paste-preview-file-icon" />
                      )}
                      <span className="paste-preview-filename">{att.name}</span>
                      <button type="button" onClick={remove} className="paste-preview-remove" aria-label="Remove attachment">
                        <X size={12} />
                      </button>
                    </div>
                  )}
                </Composer.PastePreview>

                {/* Action Toolbar at the bottom */}
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%', marginTop: '6px', paddingTop: '6px', borderTop: '1px solid rgba(255,255,255,0.05)' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                    <Composer.Attach className="composer-attach-btn" />
                  </div>

                  <ComposerActions isStreaming={isStreaming} stopChat={stopChat} />
                </div>
              </div>
            </div>

            <p className="composer-fineprint">
              Responses are AI-generated. Review before acting on them. Enter to send, Shift+Enter for new line.
            </p>
          </Composer>
        </div>
      </Thread.Root>
    </div>
  );
}
