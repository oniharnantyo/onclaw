import Providers from '../components/Providers';
import type { Provider } from '../components/Providers';

interface ProvidersPageProps {
  providers: Provider[];
  loadProviders: () => Promise<void>;
  showToast: (message: string, type?: 'success' | 'error') => void;
}

export default function ProvidersPage({ providers, loadProviders, showToast }: ProvidersPageProps) {
  return <Providers providers={providers} loadProviders={loadProviders} showToast={showToast} />;
}
