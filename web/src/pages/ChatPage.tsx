import Chat from '../components/Chat';
import ThreadRuntime from '../components/ThreadRuntime';
import type { Agent } from '../components/Agents';
import type { Skill } from '../components/Skills';
import type { Conversation } from '../types/chat';

interface ChatPageProps {
  showToast: (message: string, type?: 'success' | 'error') => void;
  agents: Agent[];
  skills: Skill[];
  conversations: Conversation[];
  chatAgent: string;
  threadKey: number;
  onNewConversation: () => void;
}

export default function ChatPage({
  showToast,
  agents,
  skills,
  conversations,
  chatAgent,
  threadKey,
  onNewConversation,
}: ChatPageProps) {
  return (
    <ThreadRuntime
      key={threadKey}
      showToast={showToast}
      initialAgents={agents}
      initialSkills={skills}
      initialConversations={conversations}
      defaultAgent={chatAgent}
    >
      <Chat onNewConversation={onNewConversation} />
    </ThreadRuntime>
  );
}
