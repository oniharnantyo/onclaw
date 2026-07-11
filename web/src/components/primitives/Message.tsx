import { type ReactNode, useState, useEffect } from 'react';
import { Copy, ArrowsCounterClockwise, Check } from '@phosphor-icons/react';
import { useThread, MessageContext, useMessage } from '../ChatProvider';
import type { MessageContextValue } from '../ChatProvider';
import { memoizedGroupBlocks, type ContentGroup } from '../chat/groupBlocks';
import type { ChatMessage } from '../../types/chat';

export interface MessageRootProps {
  children: ReactNode;
  message: ChatMessage;
  index: number;
  isLast: boolean;
  className?: string;
}

export function MessageRoot({ children, message, index, isLast, className = '' }: MessageRootProps) {
  const value: MessageContextValue = {
    message,
    index,
    isLast,
  };

  const roleClass = message.role === 'user' ? 'message-user' : 'message-assistant';

  // Don't render the container if there are no content blocks
  if (!message.content_blocks || message.content_blocks.length === 0) {
    return null;
  }

  return (
    <MessageContext.Provider value={value}>
      <div
        className={`message-root ${roleClass} ${className}`}
        role="presentation"
        tabIndex={0}
      >
        {children}
      </div>
    </MessageContext.Provider>
  );
}

export interface MessageIfUserProps {
  children: ReactNode;
}

export function MessageIfUser({ children }: MessageIfUserProps) {
  const { message } = useMessage();
  if (message.role !== 'user') return null;
  return <>{children}</>;
}

export interface MessageIfAssistantProps {
  children: ReactNode;
}

export function MessageIfAssistant({ children }: MessageIfAssistantProps) {
  const { message } = useMessage();
  if (message.role !== 'assistant') return null;
  return <>{children}</>;
}

export interface MessagePartsProps {
  children: (group: ContentGroup, idx: number) => ReactNode;
}

export function MessageParts({ children }: MessagePartsProps) {
  const { message } = useMessage();
  const groups = memoizedGroupBlocks(message.content_blocks);

  if (!groups || groups.length === 0) {
    return null;
  }

  return (
    <div className="message-parts" role="none">
      {groups.map((group, idx) => children(group, idx))}
    </div>
  );
}

export interface MessageActionBarProps {
  className?: string;
  copyLabel?: string;
  copiedLabel?: string;
  regenerateLabel?: string;
}

export function MessageActionBar({
  className = '',
}: MessageActionBarProps) {
  const { message } = useMessage();
  const { isStreaming, messages, runChat } = useThread();
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    if (!message.content_blocks) return;
    const text = message.content_blocks
      .filter((b) => b.type === 'assistant_gen_text' && b.assistant_gen_text?.text)
      .map((b) => b.assistant_gen_text!.text)
      .join('\n');

    if (!text) return;

    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
    } catch {
      // ignore clipboard error
    }
  };

  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(false), 2000);
    return () => clearTimeout(t);
  }, [copied]);

  const handleRegenerate = () => {
    if (isStreaming) return;
    const userMsgs = messages.filter((m) => m.role === 'user');
    const lastUserMsg = userMsgs[userMsgs.length - 1];
    const textBlock = lastUserMsg?.content_blocks?.find(
      (b) => b.type === 'user_input_text' && b.user_input_text?.text
    );
    const prompt = textBlock?.user_input_text?.text;
    if (prompt) {
      runChat(prompt);
    }
  };

  return (
    <div className={`message-action-bar ${className}`} role="toolbar" aria-label="Message actions">
      <button
        onClick={handleCopy}
        className="action-copy"
        type="button"
        disabled={isStreaming}
        title={copied ? "Copied!" : "Copy response"}
        aria-label={copied ? "Copied!" : "Copy response"}
      >
        {copied ? <Check size={14} weight="bold" /> : <Copy size={14} weight="regular" />}
      </button>
      <button
        onClick={handleRegenerate}
        className="action-regenerate"
        type="button"
        disabled={isStreaming}
        title="Regenerate response"
        aria-label="Regenerate response"
      >
        <ArrowsCounterClockwise size={14} weight="regular" />
      </button>
    </div>
  );
}

export const Message = Object.assign(MessageRoot, {
  Root: MessageRoot,
  IfUser: MessageIfUser,
  IfAssistant: MessageIfAssistant,
  Parts: MessageParts,
  ActionBar: MessageActionBar,
});
