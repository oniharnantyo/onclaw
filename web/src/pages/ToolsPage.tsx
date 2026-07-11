import Tools from '../components/Tools';

interface ToolsPageProps {
  showToast: (message: string, type?: 'success' | 'error') => void;
}

export default function ToolsPage({ showToast }: ToolsPageProps) {
  return <Tools showToast={showToast} />;
}
