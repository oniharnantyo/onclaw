import { useRef } from 'react';
import { Cpu, Code, PaperPlaneTilt, FileText, X, Plus } from '@phosphor-icons/react';
import { useChat, useComposer } from './ChatProvider';
import { Thread } from './primitives/Thread';
import { ThreadList } from './primitives/ThreadList';
import { Message } from './primitives/Message';
import { Composer } from './primitives/Composer';

import Sources from './chat/Sources';
import {
  MarkdownBlock,
  ReasoningBlock,
  ToolCallBlock,
  ToolResultBlock,
  MCPCalledBlock,
  ImageBlock,
  FileBlock,
  UnknownBlock,
  pickToolRenderer
} from './chat/Renderers';
import type { ContentBlock, Conversation } from '../types/chat';

function getConversationTitle(c: Conversation) {
  if (c.preview) {
    const msg = c.preview.trim();
    if (msg.startsWith('{') || msg.startsWith('[')) {
      try {
        const parsed = JSON.parse(msg);
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
  if (block.assistant_gen_text?.text) {
    return <MarkdownBlock text={block.assistant_gen_text.text} />;
  }
  if (block.user_input_text?.text) {
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

function formatRelativeTime(dateStr: string): string {
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / 60000);
  const diffHours = Math.floor(diffMins / 60);
  const diffDays = Math.floor(diffHours / 24);

  if (diffMins < 1) return 'just now';
  if (diffMins < 60) return `${diffMins}m ago`;
  if (diffHours < 24) return `${diffHours}h ago`;
  if (diffDays < 7) return `${diffDays}d ago`;
  return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
}

export default function Chat({ onNewConversation }: ChatProps) {
  const { state, selectConversation } = useChat();
  const viewportRef = useRef<HTMLDivElement>(null);

  const visibleMessages = state.messages.filter((m) => {
    if (m.role === 'system') return false;
    if (!m.content_blocks || m.content_blocks.length === 0) return false;

    const allBlocks = state.messages.flatMap((msg) => msg.content_blocks || []);

    const hasVisibleBlock = m.content_blocks.some((b) => {
      if (b.type === 'assistant_gen_text' || b.assistant_gen_text) {
        return !!b.assistant_gen_text?.text?.trim();
      }
      if (b.type === 'user_input_text' || b.user_input_text) {
        return !!b.user_input_text?.text?.trim();
      }
      if (b.type === 'reasoning' || (b as any).reasoning) {
        const text = b.reasoning?.text || (b as any).reasoning?.text;
        return !!text?.trim();
      }
      if (b.type === 'function_tool_call' || b.function_tool_call || b.mcp_tool_call || b.server_tool_call) {
        return true;
      }
      if (b.type === 'function_tool_result' || b.function_tool_result) {
        const tr = b.function_tool_result;
        if (!tr) return false;
        const trId = tr.call_id || (tr as any).id;
        const hasCall = allBlocks.some((otherBlock) => {
          if (otherBlock.type !== 'function_tool_call' || !otherBlock.function_tool_call) return false;
          const tc = otherBlock.function_tool_call;
          const tcId = (tc as any).call_id || tc.id;
          if (tcId && trId) return tcId === trId;
          return tc.name === tr.name;
        });
        return !hasCall;
      }
      if ((b as any).type === 'assistant_gen_image' || b.user_input_image || b.user_input_file || b.mcp_tool_result || b.server_tool_result || (b as any).assistant_gen_image) {
        return true;
      }
      return false;
    });

    return hasVisibleBlock;
  });
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
                    Type a prompt below to start the ReAct execution loop.
                    The agent will reason, act, and respond in real time.
                  </p>
                </div>
              </Thread.Empty>

              {/* Messages list */}
              <Thread.Messages>
                {(msg, idx) => (
                  <Message.Root
                    key={msg.id ?? idx}
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

                      {/* Action Bar */}
                      {msg.role === 'assistant' && (idx === visibleMessagesCount - 1 || visibleMessages[idx + 1]?.role === 'user') && (
                        <Message.ActionBar />
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
                )}
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
                  <Composer.Input placeholder="Ask agent to perform a task…" className="composer-input" />

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
                    <AgentSelector />
                  </div>

                  <Composer.Send className="composer-send-btn">
                    <SendIcon />
                  </Composer.Send>
                </div>
              </div>
            </div>

            <p className="composer-fineprint">
              Responses are AI-generated. Review before acting on them.
            </p>
          </Composer>
        </div>
      </Thread.Root>
    </div>
  );
}
