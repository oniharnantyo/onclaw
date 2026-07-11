import MCP from '../components/MCP';

interface McpPageProps {
  showToast: (message: string, type?: 'success' | 'error') => void;
}

export default function McpPage({ showToast }: McpPageProps) {
  return <MCP showToast={showToast} />;
}
