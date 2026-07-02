import { useId } from 'react';
import { Info } from '@phosphor-icons/react';

interface TooltipProps {
  content: string;
  position?: 'top' | 'bottom';
  align?: 'center' | 'left' | 'right';
}

export default function Tooltip({ content, position = 'top', align = 'center' }: TooltipProps) {
  const tooltipId = useId();

  const alignmentClass = align === 'left' ? 'tooltip-align-left' : align === 'right' ? 'tooltip-align-right' : '';

  return (
    <div className={`tooltip ${position === 'bottom' ? 'tooltip-bottom' : ''} ${alignmentClass}`}>
      <button
        type="button"
        className="tooltip-trigger"
        aria-describedby={tooltipId}
        tabIndex={0}
      >
        <Info size={14} weight="bold" />
      </button>
      <div
        id={tooltipId}
        role="tooltip"
        className="tooltip-content"
      >
        {content}
      </div>
    </div>
  );
}
