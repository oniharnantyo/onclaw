import { type ReactNode } from 'react';
import ChatProvider from './ChatProvider';
import type { Conversation } from '../types/chat';

export interface ThreadRuntimeProps {
  children: ReactNode;
  initialAgents?: { name: string; is_default: boolean }[];
  initialSkills?: { name: string; description: string }[];
  initialConversations?: Conversation[];
  defaultAgent?: string;
  showToast: (msg: string, type?: 'success' | 'error') => void;
}

export default function ThreadRuntime({
  children,
  initialAgents,
  initialSkills,
  initialConversations,
  defaultAgent,
  showToast,
}: ThreadRuntimeProps) {
  return (
    <ChatProvider
      initialAgents={initialAgents}
      initialSkills={initialSkills}
      initialConversations={initialConversations}
      defaultAgent={defaultAgent}
      showToast={showToast}
    >
      <div className="thread-container">
        {children}
      </div>
    </ChatProvider>
  );
}
