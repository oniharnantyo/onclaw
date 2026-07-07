import { useState, type ReactNode } from 'react';
import type { ContentBlock } from '../../types/chat';

export interface ToolGroupProps {
  tools: ContentBlock[];
  children: (tool: ContentBlock, idx: number) => ReactNode;
}

export default function ToolGroup({ tools, children }: ToolGroupProps) {
  const [isOpen, setIsOpen] = useState(false);

  return (
    <div className="tool-group" role="region" aria-label="Tool execution group">
      <button
        className="tool-group-trigger"
        onClick={() => setIsOpen(!isOpen)}
        type="button"
        aria-expanded={isOpen}
      >
        <span className="tool-group-title">
          {tools.length} tool calls
        </span>
        <span className="tool-group-icon" aria-hidden="true">
          {isOpen ? '▼' : '▶'}
        </span>
      </button>
      {isOpen && (
        <div className="tool-group-content">
          {tools.map((tool, idx) => children(tool, idx))}
        </div>
      )}
    </div>
  );
}
