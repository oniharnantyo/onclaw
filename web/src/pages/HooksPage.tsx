import Hooks from '../components/Hooks';
import type { Agent } from '../components/Agents';

interface HooksPageProps {
  agents: Agent[];
  showToast: (message: string, type?: 'success' | 'error') => void;
}

export default function HooksPage({ agents, showToast }: HooksPageProps) {
  return <Hooks agents={agents} showToast={showToast} />;
}
