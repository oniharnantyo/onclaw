// Shared stagger-reveal primitive (generative-ui spec: streaming honesty).
// The wire delivers tool arguments and results complete, so reveal-style
// elements (search sources, timeline events…) animate their reveal
// client-side AFTER arrival — a short stagger that completes without user
// interaction and never claims live mid-stream parsing. The live decision is
// latched at first mount: hydrated history renders fully revealed
// immediately, and a mid-animation turn completion cannot restart the
// animation or flip rows between states.

import { useState, type CSSProperties, type ReactNode } from "react";
import { cx } from "../helpers";

/** Per-item stagger step; the whole reveal stays under ~1.2s even for long lists. */
export const REVEAL_STEP_MS = 60;
const MAX_STEPS = 20;

/** Latched at first mount: stagger only when the content arrived live. */
export function useStaggerReveal(live: boolean): boolean {
  const [stagger] = useState(() => live);
  return stagger;
}

/** Clamped per-item delay so long lists still finish promptly. */
export function revealDelay(index: number): number {
  return Math.max(0, Math.min(index, MAX_STEPS)) * REVEAL_STEP_MS;
}

interface RevealProps {
  /** Position in the reveal order. */
  index: number;
  /** Latched stagger decision (useStaggerReveal) — false renders settled. */
  stagger: boolean;
  children: ReactNode;
  className?: string;
  style?: CSSProperties;
  /** Element tag: rows are usually `li`. */
  as?: 'div' | 'li';
  /** data-od-id test anchor. */
  odId?: string;
}

/** One staggered row. With `stagger` false this is a plain wrapper — fully
 * revealed immediately, no animation class, no delay (hydrated renders). */
export function Reveal({ index, stagger, children, className, style, as = 'div', odId }: RevealProps) {
  const Tag = as;
  if (!stagger) {
    return (
      <Tag className={className} style={style} data-od-id={odId}>
        {children}
      </Tag>
    );
  }
  return (
    <Tag
      className={cx('od-genui-reveal', className)}
      style={{ ...style, animationDelay: `${revealDelay(index)}ms` }}
      data-od-id={odId}
    >
      {children}
    </Tag>
  );
}
