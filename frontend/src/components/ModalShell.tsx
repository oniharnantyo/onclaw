import { useEffect } from "react";
import type { ReactElement } from "react";

import { Modal } from "./ui/Modal";
import type { ModalShellProps } from "./types";

/**
 * Reference count over every open dialog shell. Stacked dialogs exist
 * (a ConfirmDialog can render over an open ModalShell), so the body scroll
 * lock is acquired per mount and only restored when the LAST dialog closes —
 * task 3.6, no new deps.
 */
let scrollLockCount = 0;

/** Hides the document scrollbars; returns the release function. */
function acquireScrollLock(): () => void {
  if (scrollLockCount === 0) {
    document.documentElement.style.overflow = "hidden";
    document.body.style.overflow = "hidden";
  }

  scrollLockCount += 1;

  let released = false;

  return (): void => {
    if (released) return;
    released = true;
    scrollLockCount -= 1;

    if (scrollLockCount === 0) {
      document.documentElement.style.overflow = "";
      document.body.style.overflow = "";
    }
  };
}

/**
 * The one dialog shell every modal in the app renders through: the ported
 * kit `Modal` (backdrop, Escape-to-close, focus park/trap/restore) with the
 * title, optional description, body, and action row in fixed slots. Callers
 * supply the content; the chrome lives here once.
 *
 * The dialog surface keeps the `glass-overlay` class — the Liquid Glass
 * grammar's hook for "blur what's behind this" (`html[data-glass]
 * .glass-overlay` in styles.css) — alongside the token surface treatment.
 * (The daisyUI-era `.modal-box` selector half of that rule is dead now and
 * retires with the cutover wave.)
 */
export function ModalShell({
  title,
  description,
  testId,
  wide,
  onClose,
  children,
  actions,
}: ModalShellProps): ReactElement {
  // Lock the page behind the dialog while it is open (task 3.6).
  useEffect((): (() => void) => acquireScrollLock(), []);

  return (
    <Modal
      title={title}
      onClose={onClose}
      wide={wide}
      testId={testId}
      footer={actions}
      boxClassName="glass-overlay"
    >
      <div className="p-5">
        {description && (
          <p className="mb-3 text-[13px] text-muted">{description}</p>
        )}
        {children}
      </div>
    </Modal>
  );
}
