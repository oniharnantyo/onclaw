import Embeddings from '../components/Embeddings';
import type { Provider } from '../components/Providers';

interface EmbeddingsPageProps {
  providers: Provider[];
  showToast: (message: string, type?: 'success' | 'error') => void;
}

export default function EmbeddingsPage({ providers, showToast }: EmbeddingsPageProps) {
  return <Embeddings providers={providers} showToast={showToast} />;
}
