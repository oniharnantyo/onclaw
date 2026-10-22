/**
 * Reduced-motion hook (tasks 3.6, D10): one live check of the
 * `prefers-reduced-motion` media query for the card chrome — the running
 * label's shimmer swaps to static muted text under it (design D10: a
 * spinner held through high-frequency streaming re-renders is the costliest
 * animation on the page; the shimmer is the same trade, and motion must
 * disable when the user prefers reduced motion).
 *
 * jsdom (and any host without matchMedia) reads as no-preference; the
 * styles.css global reduced-motion guard remains the CSS-level backstop.
 */
import { useEffect, useState } from "react";

const QUERY = "(prefers-reduced-motion: reduce)";

/** Reads the live media query; a host without matchMedia reads as
 * no-preference (the styles.css global guard remains the CSS backstop). */
function readPreference(): boolean {
  return window.matchMedia?.(QUERY).matches === true;
}

export function useReducedMotion(): boolean {
  const [reduced, setReduced] = useState<boolean>(readPreference);

  useEffect((): void | (() => void) => {
    const mq = window.matchMedia?.(QUERY);

    if (mq === undefined) return undefined;

    const onChange = (event: MediaQueryListEvent): void => {
      setReduced(event.matches);
    };

    setReduced(mq.matches);
    mq.addEventListener("change", onChange);

    return (): void => {
      mq.removeEventListener("change", onChange);
    };
  }, []);

  return reduced;
}
