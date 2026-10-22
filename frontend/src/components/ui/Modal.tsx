/**
 * Modal: the kit's dialog primitive (onclaw-v2 parity, design D3), ported
 * from onclaw-v2's components/ui/Modal.tsx with explicit types. State-driven
 * visibility — callers conditionally render it; when mounted it traps Tab
 * inside the box, closes on Escape (swallowing the key so the webview never
 * treats it as a shortcut), parks focus on the dialog shell on open and
 * restores it to the previous element on close.
 *
 * No ring/outline classes here beyond `outline-none` on the box: the global
 * `:focus-visible` contract styles keyboard focus on the controls INSIDE the
 * dialog, and the box itself is a focus parking spot, not a control (D9).
 */
import { useEffect, useRef } from "react";
import type { ReactElement, ReactNode } from "react";

import { cn } from "../../lib/cn";
import { Icon } from "./Icon";

/** Elements the Tab trap may cycle through (onclaw-v2 selector, verbatim). */
const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export type ModalProps = {
  /** Heading of the dialog; also the accessible name. */
  title: string;
  onClose: () => void;
  /** Body content, rendered in the scrollable area under the header. */
  children?: ReactNode;
  /** Footer row (action buttons), rendered above the bottom border. */
  footer?: ReactNode;
  /** Widens the box (max-w-4xl) for form-heavy dialogs. */
  wide?: boolean;
  /** Extra classes on the dialog surface (e.g. the `glass-overlay` hook). */
  boxClassName?: string;
  /** Identifies the dialog in tests. */
  testId?: string;
};

export function Modal({
  title,
  onClose,
  children,
  footer,
  wide = false,
  boxClassName,
  testId,
}: ModalProps): ReactElement {
  const boxRef = useRef<HTMLDivElement>(null);

  // Latest-callback ref: buddies call sites pass inline arrows, so keying the
  // listener effect on onClose would tear down and re-run focus management on
  // every keystroke of a controlled input inside the dialog.
  const onCloseRef = useRef(onClose);
  useEffect((): void => {
    onCloseRef.current = onClose;
  });

  // Mount/unmount-only: park focus, trap Tab, listen for Escape, restore.
  useEffect((): (() => void) => {
    // SAFETY: focus only ever lands on HTMLElements here (the trigger
    // control or the tabIndex box); the restore call is null-safe.
    const prev = document.activeElement as HTMLElement | null;
    // Park focus on the dialog shell — UNLESS the content already grabbed
    // focus this commit (an autoFocus input mounts before this passive effect
    // runs); the parked shell is only the fallback landing spot.
    const box = boxRef.current;

    if (box && !box.contains(document.activeElement)) box.focus();

    const onKeyDown = (e: KeyboardEvent): void => {
      if (e.key === "Escape") {
        e.preventDefault();
        // Topmost dialog wins: a stacked dialog's listener was added last, so
        // stopping here keeps one press from closing the whole stack.
        e.stopImmediatePropagation();
        onCloseRef.current();

        return;
      }

      if (e.key === "Tab") {
        const box = boxRef.current;

        if (!box) return;
        const items = Array.from(box.querySelectorAll<HTMLElement>(FOCUSABLE));

        if (!items.length) return;
        const first = items[0];
        const last = items[items.length - 1];
        const active = document.activeElement;
        const inside = active instanceof Node && box.contains(active);

        if (e.shiftKey && (!inside || active === first || active === box)) {
          e.preventDefault();
          last.focus();
        } else if (!e.shiftKey && (!inside || active === last)) {
          e.preventDefault();
          first.focus();
        }
      }
    };

    window.addEventListener("keydown", onKeyDown);

    return (): void => {
      window.removeEventListener("keydown", onKeyDown);
      prev?.focus?.();
    };
    // SAFETY: the effect intentionally runs once per mount; onClose is read
    // through onCloseRef so the listener never goes stale.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-0 md:p-6"
      role="dialog"
      aria-modal="true"
      aria-label={title}
      data-testid={testId}
    >
      <div
        aria-hidden="true"
        className="od-fade absolute inset-0 bg-[color-mix(in_oklab,var(--fg)_32%,transparent)]"
        onClick={onClose}
      />
      <div
        ref={boxRef}
        tabIndex={-1}
        className={cn(
          "od-pop relative flex h-[100dvh] max-h-[100dvh] w-full flex-col overflow-hidden rounded-none border border-line bg-surface shadow-[var(--elev-raised)] outline-none md:h-auto md:max-h-[86vh] md:rounded-lg",
          wide ? "max-w-4xl" : "max-w-lg",
          boxClassName
        )}
      >
        <div className="flex h-14 shrink-0 items-center justify-between border-b border-linesoft px-5">
          <h2 className="text-[17px] font-semibold text-fg">{title}</h2>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close dialog"
            className="flex h-8 w-8 items-center justify-center rounded-md text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg"
          >
            <Icon name="x" size={16} />
          </button>
        </div>
        <div className="od-scroll flex-1 overflow-y-auto overflow-x-hidden">
          {children}
        </div>
        {footer && (
          <div className="flex shrink-0 items-center justify-end gap-2.5 border-t border-linesoft bg-[color-mix(in_oklab,var(--fg)_3%,transparent)] px-5 py-3.5">
            {footer}
          </div>
        )}
      </div>
    </div>
  );
}
