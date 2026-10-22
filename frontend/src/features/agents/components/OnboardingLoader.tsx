import { useEffect, useState } from "react";
import type { ReactElement } from "react";

import type { OnboardingLoaderProps } from "../types";

/**
 * The informal status lines shown while an agent's prompts generate (design
 * D9). The pool and its order are exact — the loader cycles through it verbatim.
 */
const STATUS_LINES: readonly string[] = [
  "Reading the brief twice…",
  "Sketching a personality…",
  "Warming up its voice…",
  "Deciding what it cares about…",
  "Packing its toolbox…",
  "Learning to say 'I don't know' nicely…",
  "Stretching its context window…",
  "Practicing its first hello…",
  "Making a good first impression…",
  "Almost done — it's on the last page…",
];

/** One status line every 5 seconds (design D9). */
const LINE_INTERVAL_MS = 5000;

/** The elapsed timer appears after 30 seconds (design D9). */
const TIMER_AFTER_MS = 30000;

const TICK_MS = 1000;

/**
 * Reads the OS reduce-motion setting. Environments without the API (jsdom,
 * very old webviews) count as motion allowed — the rotation is the default
 * experience and the query is only ever an enhancement.
 */
function prefersReducedMotion(): boolean {
  try {
    return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  } catch {
    // No matchMedia in this environment.
    return false;
  }
}

/**
 * In-flight state of agent creation (design D9): a busy spinner, the
 * "Onboarding <name>…" label, one rotating informal line, and — once the
 * wait passes 30 seconds — an elapsed timer. Under prefers-reduced-motion
 * the rotation collapses to a static line; the timer still ticks.
 */
export function OnboardingLoader({
  name,
}: OnboardingLoaderProps): ReactElement {
  const [elapsedMs, setElapsedMs] = useState(0);
  // Read once at mount: the OS setting does not change mid-onboarding, and a
  // static line must not start rotating late.
  const [reducedMotion] = useState<boolean>(prefersReducedMotion);

  useEffect((): (() => void) => {
    const tick = window.setInterval((): void => {
      setElapsedMs((prev: number) => prev + TICK_MS);
    }, TICK_MS);

    return (): void => window.clearInterval(tick);
  }, []);

  const line = reducedMotion
    ? STATUS_LINES[0]
    : STATUS_LINES[
        Math.floor(elapsedMs / LINE_INTERVAL_MS) % STATUS_LINES.length
      ];

  const showTimer = elapsedMs >= TIMER_AFTER_MS;

  return (
    <div
      className="mt-6 flex flex-col items-center gap-3 py-8 text-center"
      data-testid="onboarding-loader"
      role="status"
      aria-live="polite"
    >
      {/* onclaw-v2's loader spinner (design D3): track on --border, head on
          --accent. The single in-flight indicator while an agent creates. */}
      <span
        className="h-6 w-6 animate-spin rounded-full border-2 border-line border-t-accent"
        aria-hidden="true"
      />
      <p
        className="text-[13px] font-semibold text-fg"
        data-testid="onboarding-label"
      >
        Onboarding {name}…
      </p>
      <p
        className="min-h-4 font-mono text-[12px] text-muted"
        data-testid="onboarding-line"
      >
        {line}
      </p>
      {showTimer && (
        <p
          className="font-mono text-[12px] text-muted"
          data-testid="onboarding-elapsed"
        >
          {Math.floor(elapsedMs / 1000)}s elapsed
        </p>
      )}
    </div>
  );
}
