import React, { useState, useRef, useEffect, useId, cloneElement, Children } from 'react';
import { cx } from '../../lib/helpers';

export type TooltipPlacement = 'right' | 'left' | 'top' | 'bottom';

export interface TooltipProps {
  content: React.ReactNode;
  children: React.ReactElement;
  placement?: TooltipPlacement;
  delayMs?: number;
  disabled?: boolean;
  className?: string;
  id?: string;
}

const PLACEMENT_CLASSES: Record<TooltipPlacement, string> = {
  right: 'left-[calc(100%+8px)] top-1/2 -translate-y-1/2',
  left: 'right-[calc(100%+8px)] top-1/2 -translate-y-1/2',
  top: 'bottom-[calc(100%+8px)] left-1/2 -translate-x-1/2',
  bottom: 'top-[calc(100%+8px)] left-1/2 -translate-x-1/2',
};

export function Tooltip({
  content,
  children,
  placement = 'right',
  delayMs = 150,
  disabled = false,
  className = '',
  id: customId,
}: TooltipProps) {
  const [isOpen, setIsOpen] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const generatedId = useId();
  const tooltipId = customId || `tooltip-${generatedId}`;

  const clearTimer = () => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  };

  const showTooltip = () => {
    if (disabled || !content) return;
    clearTimer();
    timerRef.current = setTimeout(() => {
      setIsOpen(true);
    }, delayMs);
  };

  const hideTooltip = () => {
    clearTimer();
    setIsOpen(false);
  };

  // Dismiss on global Escape
  useEffect(() => {
    if (!isOpen) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        hideTooltip();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen]);

  // Clean up timer on unmount
  useEffect(() => {
    return () => clearTimer();
  }, []);

  if (disabled || !content) {
    return children;
  }

  const child = Children.only(children) as React.ReactElement<any>;

  const handleMouseEnter = (e: React.MouseEvent) => {
    child.props.onMouseEnter?.(e);
    showTooltip();
  };

  const handleMouseLeave = (e: React.MouseEvent) => {
    child.props.onMouseLeave?.(e);
    hideTooltip();
  };

  const handleFocus = (e: React.FocusEvent) => {
    child.props.onFocus?.(e);
    showTooltip();
  };

  const handleBlur = (e: React.FocusEvent) => {
    child.props.onBlur?.(e);
    hideTooltip();
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    child.props.onKeyDown?.(e);
    if (e.key === 'Escape') {
      hideTooltip();
    }
  };

  const existingDescribedBy = child.props['aria-describedby'];
  const describedBy = [existingDescribedBy, isOpen ? tooltipId : undefined]
    .filter(Boolean)
    .join(' ') || undefined;

  const trigger = cloneElement(child, {
    'aria-describedby': describedBy,
    onMouseEnter: handleMouseEnter,
    onMouseLeave: handleMouseLeave,
    onFocus: handleFocus,
    onBlur: handleBlur,
    onKeyDown: handleKeyDown,
  });

  return (
    <div className={cx('relative inline-flex items-center', className)}>
      {trigger}
      {isOpen && (
        <div
          id={tooltipId}
          role="tooltip"
          className={cx(
            'od-fade pointer-events-none absolute z-50 whitespace-nowrap rounded-md bg-[var(--fg)] px-2.5 py-1 text-[12px] font-medium text-[var(--bg)] shadow-[var(--elev-raised)]',
            PLACEMENT_CLASSES[placement]
          )}
        >
          {content}
        </div>
      )}
    </div>
  );
}
