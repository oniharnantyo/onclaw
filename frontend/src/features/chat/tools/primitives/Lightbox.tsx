/**
 * Lightbox (task 4.8, design D6): a modal overlay that shows one image
 * enlarged over the transcript. Closes on Escape and on a backdrop click;
 * focus moves into the overlay while open and returns to the trigger (the
 * element focused at open time) on close — the disclosure contract the
 * spec's "Screenshot thumbnail enlarges" scenario states.
 */
import { useEffect, useRef } from "react";
import type { MouseEvent as ReactMouseEvent, ReactElement } from "react";
import { X } from "lucide-react";

export interface LightboxProps {
  src: string;
  alt: string;
  onClose: () => void;
  testidPrefix?: string;
}

export function Lightbox({
  src,
  alt,
  onClose,
  testidPrefix = "lightbox",
}: LightboxProps): ReactElement {
  const closeButtonRef = useRef<HTMLButtonElement | null>(null);

  // The trigger to restore focus to, captured once at open.
  const restoreRef = useRef<HTMLElement | null>(
    typeof document !== "undefined" &&
      document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null
  );

  useEffect((): (() => void) => {
    closeButtonRef.current?.focus();

    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key === "Escape") onClose();
    };

    document.addEventListener("keydown", onKeyDown);

    return (): void => {
      document.removeEventListener("keydown", onKeyDown);
      restoreRef.current?.focus();
    };
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-[color-mix(in_oklab,var(--fg)_55%,transparent)] p-6"
      data-testid={testidPrefix}
      role="dialog"
      aria-modal="true"
      aria-label={alt}
      onClick={onClose}
    >
      <figure
        className="max-h-full max-w-full rounded-md border border-line bg-surface p-2 shadow-[var(--elev-raised)]"
        onClick={(event: ReactMouseEvent<HTMLElement>): void =>
          event.stopPropagation()
        }
      >
        <img
          src={src}
          alt={alt}
          className="max-h-[75vh] max-w-[75vw] object-contain"
          data-testid={`${testidPrefix}-image`}
        />
        <figcaption className="flex items-center gap-2 px-1 pt-1">
          <span className="min-w-0 flex-1 truncate text-[11px] text-muted">
            {alt}
          </span>
          <button
            type="button"
            ref={closeButtonRef}
            title="Close"
            className="rounded p-1 text-muted hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg"
            data-testid={`${testidPrefix}-close`}
            onClick={onClose}
          >
            <X aria-hidden="true" className="size-4" />
          </button>
        </figcaption>
      </figure>
    </div>
  );
}
