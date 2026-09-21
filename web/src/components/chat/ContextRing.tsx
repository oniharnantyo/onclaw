// Context ring + breakdown popover (assistant-ui "context breakdown" element
// adoption, adopt-assistant-ui-elements D1/D2/D7/D11).
//
// The ring lives in the composer's LEFT control rail (it moved out of the
// chat header by design): a small SVG donut that fills clockwise with the
// latest turn's final-call input over the agent's effective context window,
// beside a monospace percentage. Severity ladder ONLY — accent <65%, amber
// 65–85%, danger >85%; the summarization trigger is deliberately not marked
// anywhere (the transcript's compaction divider is the only "it happened"
// signal). No usage or no known window → nothing renders (never a zero or a
// fabricated value). The popover opens upward and breaks the usage down with
// honest-remainder semantics: client estimates at face value ("estimated"),
// the Server context remainder for what the transcript cannot count, real
// Headroom, and the last turn's wire-reported input/output.
import { useEffect, useRef, useState } from 'react';
import { cx, formatTokens } from '../../lib/helpers';
import { computeContextBreakdown } from '../../lib/contextBreakdown';
import { useStore } from '../../store';

// Below 10% the label keeps one decimal: at the 200k default window a short
// turn moves the fill by well under a point, and integer rounding would leave
// the ring reading the same percentage for many turns.
const pctLabel = (pct: number) => (pct > 0 && pct < 10 ? String(Math.round(pct * 10) / 10) : String(Math.round(pct)));

// Pure severity ladder (D1) — no summarization-trigger marking anywhere.
const tierOf = (pct: number): 'accent' | 'warn' | 'danger' => (pct > 85 ? 'danger' : pct >= 65 ? 'warn' : 'accent');
const RING_STROKE = { accent: 'stroke-accent', warn: 'stroke-warn', danger: 'stroke-danger' } as const;
// Amber text keeps the darkened mix the header meter used for contrast on
// light backgrounds; danger uses the token tone.
const RING_TEXT = {
  accent: 'text-accent',
  warn: 'text-[color-mix(in_oklab,var(--warn),black_38%)]',
  danger: 'text-danger',
} as const;
const TINT_SERVER = 'bg-[color-mix(in_oklab,var(--fg)_8%,transparent)]';

const prefersReducedMotion = () =>
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

/** rAF count-up on the popover's used figure (D11 number ticker): an eased
 * ~400ms animation settling exactly on the target. Reduced-motion or a
 * missing rAF renders the target directly. All state writes happen inside the
 * rAF callback — never synchronously in the effect. */
const useCountUp = (target: number, run: boolean): number => {
  const [frameValue, setFrameValue] = useState<number | null>(null);
  const animate = run && !prefersReducedMotion() && typeof requestAnimationFrame === 'function';
  useEffect(() => {
    if (!animate) return;
    let frame = 0;
    // t0 anchors on the rAF clock itself — mixing performance.now() with the
    // frame timestamp skews the eased progress negative on some engines.
    let t0: number | undefined;
    const DURATION = 400;
    const step = (now: number) => {
      if (t0 === undefined) t0 = now;
      const t = Math.min(1, Math.max(0, (now - t0) / DURATION));
      const eased = 1 - Math.pow(1 - t, 3);
      setFrameValue(Math.round(target * eased));
      if (t < 1) frame = requestAnimationFrame(step);
    };
    frame = requestAnimationFrame(step);
    return () => cancelAnimationFrame(frame);
  }, [animate, target]);
  return animate && frameValue !== null ? Math.max(0, frameValue) : target;
};

export function ContextRing({ agent, enabled }: { agent: any; enabled: boolean }) {
  // The meter's data comes from the thread's ACTIVE session (same record the
  // runtime/hydration write): finalInput drives the ring, the transcript
  // entries feed the Conversation/Files estimates, and the turn block carries
  // the popover's detail rows. Addressed by the open chat so the component
  // needs no extra props through ChatView.
  const session = useStore((s: any) => {
    const th = s.db[s.pos.tenantId]?.threads[s.pos.chatId];
    return th && !Array.isArray(th) ? th.list.find((x: any) => x.id === th.active) : undefined;
  });

  const usage = session?.usage;
  const effective = agent?.effective_context_window || 0;
  const visible = Boolean(enabled && usage && usage.finalInput > 0 && effective > 0);

  // The popover must never survive a chat switch: resetting during render
  // (React's sanctioned adjustment pattern, same as ChatView's session-limit
  // reset) closes it the moment the open chat changes — no effect needed.
  const chatKey = useStore((s: any) => s.pos.tenantId + '::' + s.pos.chatId);
  const [open, setOpen] = useState(false);
  const [openChatKey, setOpenChatKey] = useState(chatKey);
  if (openChatKey !== chatKey) {
    setOpenChatKey(chatKey);
    setOpen(false);
  }
  const rootRef = useRef<HTMLSpanElement>(null);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (e: PointerEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);

  // Hooks stay unconditional (the component may return null below): the
  // ticker only animates while the popover is open on a visible ring.
  const used = visible && usage ? usage.finalInput : 0;
  const ticked = useCountUp(used, open && visible);
  if (!visible) return null;

  const pct = Math.min(100, (used / effective) * 100);
  const tier = tierOf(pct);
  const split = computeContextBreakdown(agent, session?.messages, used, effective, usage.contextBreakdown);
  // Bar denominator: the face-value segments plus the server remainder fill
  // exactly the real used envelope (max(sum, used)) — estimates overshooting
  // `used` clamp the bar there while the legend numbers stay at face value.
  const estimateSum = split.segments.reduce((s, seg) => s + seg.tokens, 0);
  const barDenominator = Math.max(1, estimateSum + split.serverContext);
  const widthPct = (tokens: number) => (tokens / barDenominator) * 100 + '%';

  return (
    <span ref={rootRef} className="relative flex items-center">
      <button type="button" data-od-id="context-meter" aria-expanded={open} aria-haspopup="dialog"
        title={formatTokens(used) + ' / ' + formatTokens(effective)}
        onClick={(e) => { e.stopPropagation(); setOpen((v) => !v); }}
        className="flex items-center gap-1.5 rounded-md transition-opacity hover:opacity-80">
        <svg width="18" height="18" viewBox="0 0 18 18" aria-hidden className="-rotate-90">
          <circle cx="9" cy="9" r="7" fill="none" strokeWidth="2.5" className="stroke-linesoft"/>
          <circle cx="9" cy="9" r="7" fill="none" strokeWidth="2.5" strokeLinecap="round"
            strokeDasharray={2 * Math.PI * 7}
            strokeDashoffset={2 * Math.PI * 7 * (1 - Math.min(1, used / effective))}
            className={RING_STROKE[tier]} data-od-id="context-ring-fill"/>
        </svg>
        <span data-od-id="context-meter-pct" className={cx('font-mono text-[10px]', RING_TEXT[tier])}>
          {pctLabel(pct)}%
        </span>
      </button>
      {open && (
        <span role="dialog" aria-label="Context window usage" data-od-id="context-meter-details"
          className="absolute bottom-full left-0 z-30 mb-2 block w-72 rounded-lg border border-linesoft bg-surface p-3 text-left shadow-lg">
          <span className="block text-[10px] font-semibold uppercase tracking-wide text-muted">Context</span>
          <span data-od-id="context-meter-used" className="mt-1.5 block font-mono text-[13px] text-fg">
            {formatTokens(ticked)} <span className="text-muted">/ {formatTokens(effective)}</span>
          </span>
          <span data-od-id="context-bar" className="mt-1.5 flex h-1.5 w-full overflow-hidden rounded-full bg-linesoft">
            {split.segments.map((seg) => (
              <span key={seg.label} className={cx('h-full', seg.tint)} style={{ width: widthPct(seg.tokens) }}/>
            ))}
            {split.serverContext > 0 && (
              <span data-od-id="context-bar-server" className={cx('h-full', TINT_SERVER)} style={{ width: widthPct(split.serverContext) }}/>
            )}
          </span>
          <span className="mt-2 block">
            {split.segments.map((seg) => (
              <span key={seg.label} data-od-id={'context-segment-' + seg.label.toLowerCase().replace(/[^a-z]+/g, '-')}
                className="flex items-center gap-1.5 py-px text-[11px] leading-5 text-fg2">
                <span aria-hidden className={cx('h-1.5 w-1.5 shrink-0 rounded-full', seg.tint)}/>
                <span className="min-w-0 flex-1 truncate">{seg.label}</span>
                <span className="font-mono text-muted">{formatTokens(seg.tokens)}</span>
                {seg.estimated && <span className="text-[10px] text-muted">· estimated</span>}
              </span>
            ))}
            {split.serverContext > 0 && (
              <span data-od-id="context-server-row" title="Composed server-side and invisible to the transcript: this turn's retrieved memory, persona docs, tool schemas, and compaction summaries."
                className="flex items-center gap-1.5 py-px text-[11px] leading-5 text-fg2">
                <span aria-hidden className={cx('h-1.5 w-1.5 shrink-0 rounded-full', TINT_SERVER)}/>
                <span className="min-w-0 flex-1 truncate">Server context</span>
                <span className="font-mono text-muted">{formatTokens(split.serverContext)}</span>
              </span>
            )}
            <span data-od-id="context-headroom-row" className="flex items-center gap-1.5 py-px text-[11px] leading-5 text-fg2">
              <span aria-hidden className="h-1.5 w-1.5 shrink-0 rounded-full border border-linesoft bg-transparent"/>
              <span className="min-w-0 flex-1 truncate">Headroom</span>
              <span className="font-mono text-muted">{formatTokens(split.headroom)}</span>
            </span>
            {typeof usage.input === 'number' && usage.input > 0 && (
              <span data-od-id="context-turn-input" className="flex items-center gap-1.5 py-px text-[11px] leading-5 text-muted">
                <span aria-hidden className="w-1.5 shrink-0"/>
                <span className="min-w-0 flex-1 truncate">Last turn input</span>
                <span className="font-mono">{formatTokens(usage.input)}</span>
              </span>
            )}
            {typeof usage.output === 'number' && usage.output > 0 && (
              <span data-od-id="context-turn-output" className="flex items-center gap-1.5 py-px text-[11px] leading-5 text-muted">
                <span aria-hidden className="w-1.5 shrink-0"/>
                <span className="min-w-0 flex-1 truncate">Last turn output</span>
                <span className="font-mono">{formatTokens(usage.output)}</span>
              </span>
            )}
          </span>
          <span data-od-id="context-caption" className={cx('mt-1.5 block font-mono text-[10px]', RING_TEXT[tier])}>
            {pctLabel(pct)}% of the context window
          </span>
        </span>
      )}
    </span>
  );
}
