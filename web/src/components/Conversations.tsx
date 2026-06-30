import { ChatCircle, Robot, ChatCenteredDots, ArrowRight } from '@phosphor-icons/react';

export interface Conversation {
  id: number;
  agent_name: string;
  message_count: number;
  created_at: string;
  updated_at: string;
}

interface ConversationsProps {
  conversations: Conversation[];
  selectConversation: (id: number) => void;
}

function formatRelativeTime(dateStr: string): string {
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / 60000);
  const diffHours = Math.floor(diffMins / 60);
  const diffDays = Math.floor(diffHours / 24);

  if (diffMins < 1)   return 'just now';
  if (diffMins < 60)  return `${diffMins}m ago`;
  if (diffHours < 24) return `${diffHours}h ago`;
  if (diffDays < 7)   return `${diffDays}d ago`;
  return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
}

export default function Conversations({ conversations, selectConversation }: ConversationsProps) {
  if (conversations.length === 0) {
    return (
      <div className="page-container" style={{ overflow: 'auto' }}>
        <div className="empty-state" style={{ paddingTop: '80px' }}>
          <div className="empty-state-icon" aria-hidden="true">
            <ChatCenteredDots size={40} weight="duotone" />
          </div>
          <h3>No conversations yet</h3>
          <p>Start a chat session to see your conversation history here.</p>
        </div>
      </div>
    );
  }

  return (
    <div className="page-container">
      <div className="page-toolbar">
        <div className="page-toolbar-left">
          <span
            className="badge badge-inactive"
            aria-label={`${conversations.length} conversations`}
          >
            {conversations.length} sessions
          </span>
        </div>
      </div>

      <div
        className="conversations-container"
        role="list"
        aria-label="Conversation history"
      >
        <div className="conversations-list">
          {conversations.map((c, idx) => (
            <div
              key={c.id}
              className="conversation-item"
              role="listitem"
              onClick={() => selectConversation(c.id)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  selectConversation(c.id);
                }
              }}
              tabIndex={0}
              aria-label={`Conversation ${c.id} with agent ${c.agent_name}`}
              style={{ animationDelay: `${idx * 0.03}s` }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: '14px', flexGrow: 1, minWidth: 0 }}>
                {/* Icon */}
                <div style={{
                  width: '36px',
                  height: '36px',
                  borderRadius: '8px',
                  background: 'var(--accent-muted)',
                  border: '1px solid rgba(34,197,94,0.12)',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  color: 'var(--accent)',
                  flexShrink: 0,
                }} aria-hidden="true">
                  <ChatCircle size={18} weight="duotone" />
                </div>

                {/* Info */}
                <div className="conversation-item-info">
                  <div className="conversation-item-title">
                    Session #{c.id}
                  </div>
                  <div className="conversation-item-meta">
                    <Robot size={11} weight="fill" aria-hidden />
                    {c.agent_name}
                    <span style={{ color: 'var(--border-focus)' }}>·</span>
                    {c.message_count} msg{c.message_count !== 1 ? 's' : ''}
                    <span style={{ color: 'var(--border-focus)' }}>·</span>
                    {formatRelativeTime(c.updated_at)}
                  </div>
                </div>
              </div>

              <ArrowRight
                size={15}
                weight="bold"
                style={{ color: 'var(--text-subtle)', flexShrink: 0 }}
                aria-hidden
              />
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
