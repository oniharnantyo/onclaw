import Agents from '../components/Agents';
import type { Agent } from '../components/Agents';
import type { Provider } from '../components/Providers';

interface AgentsPageProps {
  agents: Agent[];
  providers: Provider[];
  loadAgents: () => Promise<void>;
  showToast: (message: string, type?: 'success' | 'error') => void;
}

export default function AgentsPage({ agents, providers, loadAgents, showToast }: AgentsPageProps) {
  return <Agents agents={agents} providers={providers} loadAgents={loadAgents} showToast={showToast} />;
}
