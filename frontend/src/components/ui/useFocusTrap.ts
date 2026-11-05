/**
 * useFocusTrap: the kit's Tab-cycle trap, extracted from Modal (task 7.1,
 * design D10a) so Modal, the Lightbox, and the vendored zoom dialogs share
 * one implementation. While `enabled`, Tab and Shift+Tab cycle keyboard
 * focus inside the container ref instead of escaping to the page behind the
 * overlay; when focus sits outside the container (or on the container's
 * focus-parking shell itself), either direction pulls it back inside.
 */
import { useEffect, type RefObject } from "react";

/** Elements the Tab trap may cycle through (onclaw-v2 selector, verbatim). */
const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export function useFocusTrap(
  containerRef: RefObject<HTMLElement | null>,
  enabled = true
): void {
  useEffect((): (() => void) | undefined => {
    if (!enabled) return;

    const onKeyDown = (e: KeyboardEvent): void => {
      if (e.key !== "Tab") return;
      const box = containerRef.current;

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
    };

    window.addEventListener("keydown", onKeyDown);

    return (): void => {
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [containerRef, enabled]);
}
