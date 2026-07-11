import { useRef, useEffect, type ReactNode, useImperativeHandle, forwardRef } from 'react';
import { useThread } from '../ChatProvider';
import { isMessageVisible } from '../chat/groupBlocks';
import type { ChatMessage } from '../../types/chat';

export interface ThreadRootProps {
  children: ReactNode;
}

export function ThreadRoot({ children }: ThreadRootProps) {
  return <div className="thread-root" role="none">{children}</div>;
}

export interface ThreadViewportProps {
  children: ReactNode;
  className?: string;
}

export const ThreadViewport = forwardRef<HTMLDivElement, ThreadViewportProps>(
  ({ children, className = '' }, ref) => {
    const { messages, isStreaming } = useThread();
    const viewportRef = useRef<HTMLDivElement>(null);
    useImperativeHandle(ref, () => viewportRef.current!);

    const isAtBottomRef = useRef(true);

    const handleScroll = () => {
      const el = viewportRef.current;
      if (!el) return;
      const offset = 50;
      isAtBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight <= offset;
    };

    useEffect(() => {
      const el = viewportRef.current;
      if (!el) return;
      if (isStreaming && isAtBottomRef.current) {
        el.scrollTop = el.scrollHeight;
      }
    }, [messages, isStreaming]);

    useEffect(() => {
      const el = viewportRef.current;
      if (el) {
        el.scrollTop = el.scrollHeight;
      }
    }, [messages.length]); // Scroll when length changes (e.g. initial load or new message sent)

    return (
      <div
        ref={viewportRef}
        onScroll={handleScroll}
        className={`thread-viewport ${className}`}
        role="log"
        aria-label="Conversation messages"
        aria-live="polite"
      >
        {children}
      </div>
    );
  }
);

ThreadViewport.displayName = 'Thread.Viewport';

export interface ThreadEmptyProps {
  children: ReactNode;
}

export function ThreadEmpty({ children }: ThreadEmptyProps) {
  const { messages } = useThread();
  const visibleMessages = messages.filter((m) => m.role !== 'system');
  if (visibleMessages.length > 0) return null;
  return <div className="thread-empty" role="status">{children}</div>;
}

export interface ThreadMessagesProps {
  children: (msg: ChatMessage, idx: number) => ReactNode;
}

export function ThreadMessages({ children }: ThreadMessagesProps) {
  const { messages } = useThread();
  const visibleMessages = messages.filter((m) => isMessageVisible(m, messages));
  return (
    <div className="thread-messages" role="none">
      {visibleMessages.map((msg, idx) => children(msg, idx))}
    </div>
  );
}

export interface ThreadScrollToBottomProps {
  children: ReactNode;
  viewportRef: React.RefObject<HTMLDivElement | null>;
  className?: string;
}

export function ThreadScrollToBottom({ children, viewportRef, className = '' }: ThreadScrollToBottomProps) {
  const handleClick = () => {
    const el = viewportRef.current;
    if (el) {
      el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' });
    }
  };
  return (
    <button onClick={handleClick} className={`thread-scroll-to-bottom ${className}`} type="button" aria-label="Scroll to bottom">
      {children}
    </button>
  );
}

export const Thread = Object.assign(ThreadRoot, {
  Root: ThreadRoot,
  Viewport: ThreadViewport,
  Empty: ThreadEmpty,
  Messages: ThreadMessages,
  ScrollToBottom: ThreadScrollToBottom,
});
