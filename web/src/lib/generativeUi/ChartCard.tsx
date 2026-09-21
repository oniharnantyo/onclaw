// Chart stat card (generative-ui spec, `$type: "chart"`): a label, a headline
// value, and an optional delta (falling tinted red, rising tinted green) over
// a compact hand-rolled SVG sparkline supporting area, line, and bars. The
// series renders even while only some points are designated visible — the
// visible-count clamps, at least one point is always drawn, and the newest
// drawn point is emphasized. Malformed or empty series degrade to the header
// only, never a crash.

import { Icon } from "../../components/ui/Icon";
import { cx } from "../helpers";

export type ChartVariant = 'area' | 'line' | 'bars';

export interface ChartDelta {
  text: string;
  direction: 'up' | 'down' | 'flat';
}

export interface ChartSpec {
  label: string;
  value: string;
  delta: ChartDelta | null;
  points: number[];
  visible: number | null;
  variant: ChartVariant;
}

function finite(v: unknown): number | null {
  const n = typeof v === 'number'
    ? v
    : typeof v === 'string' && v.trim() !== '' ? Number(v) : NaN;
  return Number.isFinite(n) ? n : null;
}

function parseDelta(raw: unknown): ChartDelta | null {
  if (typeof raw === 'number' || typeof raw === 'string') {
    const n = finite(raw);
    return {
      text: typeof raw === 'string' ? raw : String(raw),
      direction: n === null ? 'flat' : n > 0 ? 'up' : n < 0 ? 'down' : 'flat',
    };
  }
  if (raw !== null && typeof raw === 'object') {
    const o = raw as Record<string, unknown>;
    const text = typeof o.value === 'string' ? o.value : typeof o.value === 'number' ? String(o.value) : '';
    if (!text) return null;
    const direction = o.direction === 'up' || o.direction === 'down' ? o.direction : 'flat';
    return { text, direction };
  }
  return null;
}

/** Tolerant envelope parse: unknown keys are ignored, malformed numbers are
 * dropped, an unusable series degrades to an empty list (header-only card). */
export function parseChartSpec(props: Record<string, unknown>): ChartSpec {
  const label = typeof props.label === 'string' ? props.label : '';
  const valueRaw = props.value;
  const value = typeof valueRaw === 'string'
    ? valueRaw
    : typeof valueRaw === 'number' ? String(valueRaw) : '';
  const points = Array.isArray(props.points)
    ? props.points.map(finite).filter((n): n is number => n !== null)
    : [];
  const variant: ChartVariant = props.variant === 'line' || props.variant === 'bars' ? props.variant : 'area';
  return {
    label,
    value,
    delta: parseDelta(props.delta),
    points,
    visible: finite(props.visible),
    variant,
  };
}

/** Visible-count clamp: whole numbers ≥1; junk or zero still draws one point. */
export function clampPoints(points: number[], visible: number | null): number[] {
  if (points.length === 0) return points;
  if (visible === null) return points;
  const n = Math.floor(visible);
  if (!Number.isFinite(n) || n < 1) return points.slice(0, 1);
  return points.slice(0, n);
}

// Normalized viewBox; preserveAspectRatio="none" stretches to the card width,
// so strokes carry vector-effect="non-scaling-stroke".
const W = 100;
const H = 32;
const PAD = 2;

const round2 = (n: number) => Math.round(n * 100) / 100;

function Spark({ points, variant }: { points: number[]; variant: ChartVariant }) {
  const n = points.length;
  const min = Math.min(...points);
  const max = Math.max(...points);
  const span = max - min;
  const yOf = (v: number) => {
    const t = span === 0 ? 0.5 : (v - min) / span;
    return round2(PAD + (1 - t) * (H - 2 * PAD));
  };
  const last = n - 1;
  const xs = n === 1 ? [W / 2] : points.map((_, i) => round2((i * W) / (n - 1)));

  if (variant === 'bars') {
    const band = W / n;
    const barW = round2(Math.max(1.5, band * 0.6));
    return (
      <svg className="mt-1.5 h-12 w-full" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" aria-hidden="true">
        {points.map((v, i) => {
          const y = yOf(v);
          return (
            <rect
              key={i}
              x={round2(i * band + (band - barW) / 2)}
              y={y}
              width={barW}
              height={Math.max(1, round2(H - y))}
              fill="var(--accent)"
              fillOpacity={i === last ? 1 : 0.4}
            />
          );
        })}
      </svg>
    );
  }

  const linePoints = points.map((v, i) => `${xs[i]},${yOf(v)}`).join(' ');
  const areaPath =
    `M ${xs[0]},${yOf(points[0])} ` +
    points.map((v, i) => `L ${xs[i]},${yOf(v)}`).join(' ') +
    ` L ${xs[last]},${H} L ${xs[0]},${H} Z`;
  return (
    <svg className="mt-1.5 h-12 w-full" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" aria-hidden="true">
      {variant === 'area' && <path d={areaPath} fill="var(--accent)" fillOpacity={0.12} stroke="none"/>}
      <polyline
        points={linePoints}
        fill="none"
        stroke="var(--accent)"
        strokeWidth={1.5}
        strokeLinecap="round"
        strokeLinejoin="round"
        vectorEffect="non-scaling-stroke"
      />
      {/* Newest point emphasized (spec): halo + solid dot on the last draw. */}
      <circle cx={xs[last]} cy={yOf(points[last])} r={4} fill="var(--accent)" fillOpacity={0.18}/>
      <circle cx={xs[last]} cy={yOf(points[last])} r={2} fill="var(--accent)"/>
    </svg>
  );
}

function DeltaChip({ delta }: { delta: ChartDelta }) {
  const tone = delta.direction === 'up'
    ? 'text-success bg-[color-mix(in_oklab,var(--success)_12%,transparent)]'
    : delta.direction === 'down'
      ? 'text-danger bg-[color-mix(in_oklab,var(--danger)_12%,transparent)]'
      : 'text-muted bg-[color-mix(in_oklab,var(--fg)_8%,transparent)]';
  return (
    <span className={cx('inline-flex shrink-0 items-center gap-0.5 rounded-full px-1.5 py-0.5 font-mono text-[10px]', tone)}>
      {delta.direction !== 'flat' && <Icon name={delta.direction === 'up' ? 'up' : 'down'} size={10}/>}
      {delta.text}
    </span>
  );
}

interface ChartCardProps {
  spec: ChartSpec;
  odId: string;
}

export function ChartCard({ spec, odId }: ChartCardProps) {
  const shown = clampPoints(spec.points, spec.visible);
  return (
    <div
      data-od-id={odId}
      className="mb-2 rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] px-2.5 py-2"
    >
      <div className="flex min-w-0 items-baseline gap-2">
        {spec.label && <span className="min-w-0 truncate text-[12px] text-muted" title={spec.label}>{spec.label}</span>}
        {spec.value && <span className="shrink-0 text-[15px] font-semibold tabular-nums text-fg">{spec.value}</span>}
        {spec.delta && <DeltaChip delta={spec.delta}/>}
      </div>
      {/* Malformed/empty series → header only (spec: not fatal). */}
      {shown.length > 0 && <Spark points={shown} variant={spec.variant}/>}
    </div>
  );
}
