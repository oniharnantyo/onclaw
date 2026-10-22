/**
 * Reveal counter for the elements' staggered landing (task 4.2, design D7):
 * the vendored elements size their visible slice from a counter prop
 * (`visibleCount` / `visibleResults`) and re-key their rows through `cycle`,
 * so a whole result landing at once can still arrive one row at a time.
 * Under reduced motion the counter jumps straight to the full count — all
 * rows visible immediately (spec: staggered arrival MUST be disabled under
 * reduced motion).
 */
import { useEffect, useState } from "react";

/** Tick interval of the reveal replay. */
const REVEAL_INTERVAL_MS = 140;

/**
 * Counts up to `total` once the caller flips `arrived`, one step per tick;
 * with `animate` false (reduced motion) the count is always `total`. The
 * counter restarts when the total grows, so a longer re-report replays.
 */
export function useRevealCount(
  total: number,
  arrived: boolean,
  animate: boolean
): number {
  const [count, setCount] = useState<number>(0);

  useEffect((): void | (() => void) => {
    if (!arrived) {
      setCount(0);

      return undefined;
    }

    if (!animate) {
      setCount(total);

      return undefined;
    }

    let current = 0;

    setCount(0);

    const timer = setInterval((): void => {
      current += 1;
      setCount(Math.min(current, total));

      if (current >= total) clearInterval(timer);
    }, REVEAL_INTERVAL_MS);

    return (): void => clearInterval(timer);
  }, [arrived, animate, total]);

  return count;
}
