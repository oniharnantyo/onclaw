import Memory from '../components/Memory';

interface MemoryPageProps {
  showToast: (message: string, type?: 'success' | 'error') => void;
}

export default function MemoryPage({ showToast }: MemoryPageProps) {
  return <Memory showToast={showToast} />;
}
