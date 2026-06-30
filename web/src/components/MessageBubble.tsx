import { Wrench, CheckSquare, User, Robot } from '@phosphor-icons/react';

export interface ContentBlock {
  type: string;
  text?: string;
  assistant_gen_text?: { text: string };
  tool_call?: { id: string; name: string; arguments: string };
  tool_result?: { id: string; name: string; content: string };
}

export interface Message {
  id?: number;
  seq?: number;
  role: string;
  content_blocks?: ContentBlock[];
  created_at?: string;
}

interface MessageBubbleProps {
  message: Message;
  isStreaming?: boolean;
}

function ToolCallBlock({ block }: { block: ContentBlock['tool_call'] }) {
  if (!block) return null;
  return (
    <div
      className="block-tool-call"
      role="region"
      aria-label={`Tool call: ${block.name}`}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: '6px', marginBottom: '4px' }}>
        <Wrench size={12} weight="fill" aria-hidden style={{ color: 'var(--accent)' }} />
        <strong>Tool Call</strong>
        <span style={{ color: 'var(--text-muted)', fontSize: '11px' }}>{block.name}</span>
      </div>
      <div style={{ color: 'var(--text-muted)', wordBreak: 'break-all', fontSize: '11.5px' }}>
        {block.arguments}
      </div>
    </div>
  );
}

function ToolResultBlock({ block }: { block: ContentBlock['tool_result'] }) {
  if (!block) return null;
  return (
    <div
      className="block-tool-result"
      role="region"
      aria-label={`Tool result: ${block.name}`}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: '6px', marginBottom: '4px' }}>
        <CheckSquare size={12} weight="fill" aria-hidden style={{ color: 'var(--success)' }} />
        <strong>Tool Output</strong>
        <span style={{ color: 'var(--text-muted)', fontSize: '11px' }}>{block.name}</span>
      </div>
      <div style={{ color: 'var(--text-muted)', wordBreak: 'break-all', fontSize: '11.5px' }}>
        {block.content}
      </div>
    </div>
  );
}

export default function MessageBubble({ message, isStreaming }: MessageBubbleProps) {
  const isUser = message.role === 'user';

  const renderContentBlocks = (blocks: ContentBlock[] | undefined) => {
    if (!blocks) return null;
    return blocks.map((b, idx) => {
      if (b.assistant_gen_text?.text) {
        return <div key={idx} className="block-text">{b.assistant_gen_text.text}</div>;
      }
      if (b.type === 'text' && b.text) {
        return <div key={idx} className="block-text">{b.text}</div>;
      }
      if (b.tool_call) {
        return <ToolCallBlock key={idx} block={b.tool_call} />;
      }
      if (b.tool_result) {
        return <ToolResultBlock key={idx} block={b.tool_result} />;
      }
      return null;
    });
  };

  return (
    <div
      className={`message-bubble ${message.role}`}
      role="article"
      aria-label={`${isUser ? 'You' : 'AI Assistant'} message`}
    >
      <span className="message-sender" aria-hidden="true">
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: '4px' }}>
          {isUser
            ? <><User size={10} weight="fill" /> You</>
            : <><Robot size={10} weight="fill" /> Assistant</>
          }
        </span>
      </span>
      <div className="message-content">
        {renderContentBlocks(message.content_blocks)}
        {isStreaming && (
          <span className="streaming-indicator" aria-label="Agent is typing" aria-live="polite">
            <span />
            <span />
            <span />
          </span>
        )}
      </div>
    </div>
  );
}
