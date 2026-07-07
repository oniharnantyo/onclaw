import { type ReactNode } from 'react';
import type { ContentBlock } from '../../types/chat';

export interface ChainOfThoughtProps {
  reasoning: ContentBlock;
  tools: ContentBlock[];
  renderReasoning: (reasoning: ContentBlock) => ReactNode;
  renderTool: (tool: ContentBlock, idx: number) => ReactNode;
}

export default function ChainOfThought({
  reasoning,
  tools,
  renderReasoning,
  renderTool,
}: ChainOfThoughtProps) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', margin: '8px 0' }}>
      <div className="cot-reasoning">
        {renderReasoning(reasoning)}
      </div>
      {tools.length > 0 && (
        <div className="cot-tools" role="none" style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
          {tools.map((tool, idx) => renderTool(tool, idx))}
        </div>
      )}
    </div>
  );
}
