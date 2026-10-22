/**
 * Tooltip: hover/focus tooltip with a 150ms delay (onclaw-v2 parity, design
 * D3 — deliberately Base-UI-free: a cloned trigger plus an absolutely
 * positioned bubble covers the kit's needs). Dismisses on Escape (on the
 * trigger and globally while open), preserves the child's own handlers, and
 * wires aria-describedby only while open.
 */
import {
  Children,
  cloneElement,
  useEffect,
  useId,
  useRef,
  useState,
} from "react";
import type {
  FocusEvent as ReactFocusEvent,
  HTMLAttributes,
  KeyboardEvent as ReactKeyboardEvent,
  MouseEvent as ReactMouseEvent,
  ReactElement,
  ReactNode,
} from "react";

import { cn } from "../../lib/cn";

export type TooltipPlacement = "right" | "left" | "top" | "bottom";

export type TooltipProps = {
  content: ReactNode;
  children: ReactElement;
  placement?: TooltipPlacement;
  delayMs?: number;
  disabled?: boolean;
  className?: string;
  id?: string;
};

const PLACEMENT_CLASSES: Record<TooltipPlacement, string> = {
  right: "left-[calc(100%+8px)] top-1/2 -translate-y-1/2",
  left: "right-[calc(100%+8px)] top-1/2 -translate-y-1/2",
  top: "bottom-[calc(100%+8px)] left-1/2 -translate-x-1/2",
  bottom: "top-[calc(100%+8px)] left-1/2 -translate-x-1/2",
};

export function Tooltip({
  content,
  children,
  placement = "right",
  delayMs = 150,
  disabled = false,
  className = "",
  id: customId,
}: TooltipProps): ReactElement {
  const [isOpen, setIsOpen] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const generatedId = useId();
  const tooltipId = customId || `tooltip-${generatedId}`;

  const clearTimer = (): void => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  };

  const showTooltip = (): void => {
    if (disabled || !content) return;

    clearTimer();
    timerRef.current = setTimeout(() => {
      setIsOpen(true);
    }, delayMs);
  };

  const hideTooltip = (): void => {
    clearTimer();
    setIsOpen(false);
  };

  // Dismiss on global Escape
  useEffect(() => {
    if (!isOpen) return;

    const handleKeyDown = (e: KeyboardEvent): void => {
      if (e.key === "Escape") {
        hideTooltip();
      }
    };

    window.addEventListener("keydown", handleKeyDown);

    return (): void => {
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [isOpen]);

  // Clean up timer on unmount
  useEffect(() => {
    return (): void => {
      clearTimer();
    };
  }, []);

  if (disabled || !content) {
    return children;
  }

  // SAFETY: Tooltip's contract is a single trigger element; its props are
  // the HTML attribute set the handlers below compose with.
  const child = Children.only(children) as ReactElement<
    HTMLAttributes<HTMLElement>
  >;

  const handleMouseEnter = (e: ReactMouseEvent<HTMLElement>): void => {
    child.props.onMouseEnter?.(e);
    showTooltip();
  };

  const handleMouseLeave = (e: ReactMouseEvent<HTMLElement>): void => {
    child.props.onMouseLeave?.(e);
    hideTooltip();
  };

  const handleFocus = (e: ReactFocusEvent<HTMLElement>): void => {
    child.props.onFocus?.(e);
    showTooltip();
  };

  const handleBlur = (e: ReactFocusEvent<HTMLElement>): void => {
    child.props.onBlur?.(e);
    hideTooltip();
  };

  const handleKeyDown = (e: ReactKeyboardEvent<HTMLElement>): void => {
    child.props.onKeyDown?.(e);

    if (e.key === "Escape") {
      hideTooltip();
    }
  };

  const existingDescribedBy = child.props["aria-describedby"];

  const describedBy: string | undefined =
    [existingDescribedBy, isOpen ? tooltipId : undefined]
      .filter(Boolean)
      .join(" ") || undefined;

  const trigger = cloneElement(child, {
    "aria-describedby": describedBy,
    onMouseEnter: handleMouseEnter,
    onMouseLeave: handleMouseLeave,
    onFocus: handleFocus,
    onBlur: handleBlur,
    onKeyDown: handleKeyDown,
  });

  return (
    <div className={cn("relative inline-flex items-center", className)}>
      {trigger}
      {isOpen && (
        <div
          id={tooltipId}
          role="tooltip"
          className={cn(
            "od-fade pointer-events-none absolute z-50 whitespace-nowrap rounded-md bg-[var(--fg)] px-2.5 py-1 text-[12px] font-medium text-[var(--bg)] shadow-[var(--elev-raised)]",
            PLACEMENT_CLASSES[placement]
          )}
        >
          {content}
        </div>
      )}
    </div>
  );
}
