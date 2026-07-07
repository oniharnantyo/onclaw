import { type ReactNode } from 'react';
import { useThreadList } from '../ChatProvider';
import type { Conversation } from '../../types/chat';

export interface ThreadListRootProps {
  children: ReactNode;
  className?: string;
}

export function ThreadListRoot({ children, className = '' }: ThreadListRootProps) {
  return (
    <div className={`thread-list-root ${className}`} role="navigation" aria-label="Conversation history">
      {children}
    </div>
  );
}

export interface ThreadListItemsProps {
  children: (conv: Conversation, active: boolean) => ReactNode;
}

export function ThreadListItems({ children }: ThreadListItemsProps) {
  const { conversations, activeConvID } = useThreadList();
  return (
    <div className="thread-list-items" role="list">
      {(conversations || []).map((c) => children(c, activeConvID === c.id))}
    </div>
  );
}

export interface ThreadListNewProps {
  children: ReactNode;
  onClick: () => void;
  className?: string;
  style?: React.CSSProperties;
}

export function ThreadListNew({ children, onClick, className = '', style }: ThreadListNewProps) {
  return (
    <button
      onClick={onClick}
      className={`thread-list-new ${className}`}
      type="button"
      style={style}
      aria-label="New conversation"
      title="New conversation"
    >
      {children}
    </button>
  );
}

export const ThreadList = Object.assign(ThreadListRoot, {
  Root: ThreadListRoot,
  Items: ThreadListItems,
  New: ThreadListNew,
});
