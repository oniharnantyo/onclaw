// Fence transport (markdown-card-elements 2.2–2.3, design D1/D2/D6): the
// model writes tagged code fences in message text; this module is the only
// place that knows each tag's body shape. Validation is render-time and
// fail-closed per fence — a rejected body never mounts, and the caller
// degrades to the ordinary code block — while the message itself fails open
// (D2): one bad fence costs nothing but its source. Fence bodies carry
// semantic data only (D6); the presentational props the elements require
// (visibleCount, cycle, onCancel, React nodes) are derived or dropped here,
// never asked of the model. mermaid/diagram bodies are raw diagram source,
// never JSON; the diagram title rides the fence info string, which
// react-markdown surfaces as the code node's `meta`.

import { useEffect, useMemo, useState } from "react";
import { MathBlock } from "@/components/assistant-ui/elements/math-block";
import { Diagram } from "@/components/assistant-ui/elements/diagram";
import { readMermaidPalette, useMermaidEngine } from "@/lib/assistantUi/mermaidEngine";
import { cx } from "../helpers";

// --- the fence-tag universe --------------------------------------------------

/** The pinned fence universe — the coverage-guard set (GENERATIVE_UI_TYPES in
 * index.tsx is exactly this). shiki is deliberately NOT a tag: it upgrades
 * ordinary code blocks and has no fence of its own. */
export const FENCE_TAGS = [
  'chart', 'timeline', 'preview', 'table', 'ticker', 'activity', 'spec',
  'compare', 'progress', 'score', 'flow', 'math', 'mermaid', 'diagram',
] as const;

export type FenceTag = (typeof FENCE_TAGS)[number];

export function isFenceTag(tag: string): tag is FenceTag {
  return (FENCE_TAGS as readonly string[]).includes(tag);
}

/** Tags whose body is raw diagram source instead of JSON (D6). */
const RAW_SOURCE_TAGS: readonly string[] = ['mermaid', 'diagram'];

// --- strict per-tag shapes (fail-closed; also the mount-path narrowers) ------

export interface FenceChart {
  label: string;
  value: string;
  delta?: string;
  variant?: 'area' | 'line' | 'bars';
  points: number[];
}

export interface FenceTimeline {
  title?: string;
  events: { label: string; at?: string; state?: 'settled' | 'reference'; detail?: string }[];
}

export interface FencePreview {
  url: string;
  html: string;
  title?: string;
}

export interface FenceTable {
  caption?: string;
  columns: { key: string; label: string }[];
  rows: Record<string, unknown>[];
}

export interface FenceTicker {
  value: number;
  label: string;
}

export interface FenceActivity {
  title: string;
  total: number;
  start: string;
  end: string;
  data: { date: string; count: number }[];
}

export interface FenceSpec {
  title: string;
  subtitle?: string;
  rows: { label: string; value: string; emphasis?: boolean }[];
}

export interface FenceCompare {
  traitLabels: string[];
  options: { id: string; name: string; headline: string; traits: (string | false)[] }[];
  recommendedId: string;
  reason: string;
}

export interface FenceProgress {
  title: string;
  stages: { name: string; weight: number }[];
  stageIndex: number;
  stageProgress: number;
  eta: string;
}

export interface FenceScore {
  verdict: string;
  total: number;
  outOf: number;
  criteria: { label: string; score: number; weight: number; note?: string }[];
}

export interface FenceFlow {
  nodes: { id: string; label: string; column: number; row: number; state: 'done' | 'active' | 'pending' }[];
  edges: { from: string; to: string }[];
}

export interface FenceMath {
  label?: string;
  steps: { expression: string; note?: string }[];
}

type Json = Record<string, unknown>;

const isStr = (v: unknown): v is string => typeof v === 'string';
const isNum = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);
const isBool = (v: unknown): v is boolean => typeof v === 'boolean';
const isObj = (v: unknown): v is Json => v !== null && typeof v === 'object' && !Array.isArray(v);

/** Optional field with a strict type: undefined = absent (fine), null =
 * present but wrong type (reject the fence). */
function opt<T>(v: unknown, check: (x: unknown) => x is T): T | undefined | null {
  if (v === undefined) return undefined;
  return check(v) ? v : null;
}

function isAbsHttpUrl(v: unknown): v is string {
  if (!isStr(v)) return false;
  try {
    const u = new URL(v);
    return u.protocol === 'https:' || u.protocol === 'http:';
  } catch {
    return false; // relative, scheme-less, or garbage — never a preview (2.2)
  }
}

function isDateStr(v: unknown): v is string {
  return isStr(v) && Number.isFinite(Date.parse(v));
}

const FLOW_STATES: readonly string[] = ['done', 'active', 'pending'];

function chartOf(raw: unknown): FenceChart | null {
  if (!isObj(raw)) return null;
  if (!isStr(raw.label) || !isStr(raw.value)) return null; // required (2.2)
  const variant = opt(raw.variant, (v): v is 'area' | 'line' | 'bars' => v === 'area' || v === 'line' || v === 'bars');
  if (variant === null) return null;
  if (!Array.isArray(raw.points) || raw.points.length === 0 || !raw.points.every(isNum)) return null;
  const delta = opt(raw.delta, isStr);
  if (delta === null) return null;
  return {
    label: raw.label,
    value: raw.value,
    ...(delta !== undefined ? { delta } : {}),
    ...(variant !== undefined ? { variant } : {}),
    points: raw.points,
  };
}

function timelineOf(raw: unknown): FenceTimeline | null {
  if (!isObj(raw)) return null;
  if (!Array.isArray(raw.events) || raw.events.length === 0) return null;
  const events = [];
  for (const item of raw.events) {
    if (!isObj(item) || !isStr(item.label)) return null; // label required (2.2)
    const state = opt(item.state, (v): v is 'settled' | 'reference' => v === 'settled' || v === 'reference');
    if (state === null) return null;
    const at = opt(item.at, isStr);
    if (at === null) return null;
    const detail = opt(item.detail, isStr);
    if (detail === null) return null;
    events.push({
      label: item.label,
      ...(at !== undefined ? { at } : {}),
      ...(state !== undefined ? { state } : {}),
      ...(detail !== undefined ? { detail } : {}),
    });
  }
  const title = opt(raw.title, isStr);
  if (title === null) return null;
  return { ...(title !== undefined ? { title } : {}), events };
}

function previewOf(raw: unknown): FencePreview | null {
  if (!isObj(raw)) return null;
  if (!isAbsHttpUrl(raw.url)) return null; // absolute http(s) ONLY (2.2)
  if (!isStr(raw.html)) return null;
  const title = opt(raw.title, isStr);
  if (title === null) return null;
  return { url: raw.url, html: raw.html, ...(title !== undefined ? { title } : {}) };
}

export function tableOf(raw: unknown): FenceTable | null {
  if (!isObj(raw)) return null;
  if (!Array.isArray(raw.columns) || raw.columns.length === 0) return null;
  const columns = [];
  for (const col of raw.columns) {
    if (!isObj(col) || !isStr(col.key) || !isStr(col.label)) return null;
    columns.push({ key: col.key, label: col.label });
  }
  if (!Array.isArray(raw.rows) || raw.rows.length === 0 || !raw.rows.every(isObj)) return null;
  const caption = opt(raw.caption, isStr);
  if (caption === null) return null;
  return {
    ...(caption !== undefined ? { caption } : {}),
    columns,
    rows: raw.rows,
  };
}

export function tickerOf(raw: unknown): FenceTicker | null {
  if (!isObj(raw) || !isNum(raw.value) || !isStr(raw.label)) return null;
  return { value: raw.value, label: raw.label };
}

export function activityOf(raw: unknown): FenceActivity | null {
  if (!isObj(raw)) return null;
  if (!isStr(raw.title) || !isNum(raw.total) || !isDateStr(raw.start) || !isDateStr(raw.end)) return null;
  if (!Array.isArray(raw.data) || raw.data.length === 0) return null;
  const data = [];
  for (const item of raw.data) {
    if (!isObj(item) || !isDateStr(item.date) || !isNum(item.count)) return null;
    data.push({ date: item.date, count: item.count });
  }
  return { title: raw.title, total: raw.total, start: raw.start, end: raw.end, data };
}

export function specOf(raw: unknown): FenceSpec | null {
  if (!isObj(raw) || !isStr(raw.title)) return null;
  if (!Array.isArray(raw.rows) || raw.rows.length === 0) return null;
  const rows = [];
  for (const item of raw.rows) {
    if (!isObj(item) || !isStr(item.label) || !isStr(item.value)) return null;
    const emphasis = opt(item.emphasis, isBool);
    if (emphasis === null) return null;
    rows.push({ label: item.label, value: item.value, ...(emphasis !== undefined ? { emphasis } : {}) });
  }
  const subtitle = opt(raw.subtitle, isStr);
  if (subtitle === null) return null;
  return { title: raw.title, ...(subtitle !== undefined ? { subtitle } : {}), rows };
}

export function compareOf(raw: unknown): FenceCompare | null {
  if (!isObj(raw)) return null;
  if (!Array.isArray(raw.traitLabels) || raw.traitLabels.length === 0 || !raw.traitLabels.every(isStr)) return null;
  if (!Array.isArray(raw.options) || raw.options.length === 0) return null;
  const options = [];
  for (const item of raw.options) {
    if (!isObj(item) || !isStr(item.id) || !isStr(item.name) || !isStr(item.headline)) return null;
    if (!Array.isArray(item.traits) || !item.traits.every((t) => isStr(t) || t === false)) return null;
    options.push({ id: item.id, name: item.name, headline: item.headline, traits: item.traits });
  }
  // Cross-field rule: the recommendation must name one of the options (2.2).
  if (!isStr(raw.recommendedId) || !options.some((o) => o.id === raw.recommendedId)) return null;
  if (!isStr(raw.reason)) return null;
  return { traitLabels: raw.traitLabels, options, recommendedId: raw.recommendedId, reason: raw.reason };
}

export function jobOf(raw: unknown): FenceProgress | null {
  if (!isObj(raw)) return null;
  if (!isStr(raw.title) || !isStr(raw.eta)) return null;
  if (!Array.isArray(raw.stages) || raw.stages.length === 0) return null;
  const stages = [];
  for (const item of raw.stages) {
    if (!isObj(item) || !isStr(item.name) || !isNum(item.weight)) return null;
    stages.push({ name: item.name, weight: item.weight });
  }
  // stageIndex points at a stage (or just past the last one = finished) and
  // stageProgress is declared 0–100 in the fence contract.
  if (!isNum(raw.stageIndex) || !Number.isInteger(raw.stageIndex) || raw.stageIndex < 0 || raw.stageIndex > stages.length) return null;
  if (!isNum(raw.stageProgress) || raw.stageProgress < 0 || raw.stageProgress > 100) return null;
  return { title: raw.title, stages, stageIndex: raw.stageIndex, stageProgress: raw.stageProgress, eta: raw.eta };
}

export function scoreOf(raw: unknown): FenceScore | null {
  if (!isObj(raw)) return null;
  if (!isStr(raw.verdict) || !isNum(raw.total) || !isNum(raw.outOf)) return null;
  if (!Array.isArray(raw.criteria) || raw.criteria.length === 0) return null;
  const criteria = [];
  for (const item of raw.criteria) {
    if (!isObj(item) || !isStr(item.label) || !isNum(item.score) || !isNum(item.weight)) return null;
    const note = opt(item.note, isStr);
    if (note === null) return null;
    criteria.push({ label: item.label, score: item.score, weight: item.weight, ...(note !== undefined ? { note } : {}) });
  }
  return { verdict: raw.verdict, total: raw.total, outOf: raw.outOf, criteria };
}

export function flowOf(raw: unknown): FenceFlow | null {
  if (!isObj(raw)) return null;
  if (!Array.isArray(raw.nodes) || raw.nodes.length === 0) return null;
  const nodes = [];
  const ids = new Set<string>();
  for (const item of raw.nodes) {
    if (!isObj(item) || !isStr(item.id) || !isStr(item.label)) return null;
    if (!isNum(item.column) || !isNum(item.row)) return null;
    if (!FLOW_STATES.includes(String(item.state))) return null;
    if (ids.has(item.id)) return null; // duplicate ids would break row keys
    ids.add(item.id);
    nodes.push({ id: item.id, label: item.label, column: item.column, row: item.row, state: item.state as 'done' | 'active' | 'pending' });
  }
  if (!Array.isArray(raw.edges)) return null;
  const edges = [];
  for (const item of raw.edges) {
    if (!isObj(item) || !isStr(item.from) || !isStr(item.to)) return null;
    // Endpoints must reference declared nodes (2.2) — a dangling edge would
    // draw nothing but reads as a broken card.
    if (!ids.has(item.from) || !ids.has(item.to)) return null;
    edges.push({ from: item.from, to: item.to });
  }
  return { nodes, edges };
}

export function mathOf(raw: unknown): FenceMath | null {
  if (!isObj(raw)) return null;
  if (!Array.isArray(raw.steps) || raw.steps.length === 0) return null;
  const steps = [];
  for (const item of raw.steps) {
    if (!isObj(item) || !isStr(item.expression) || !item.expression.trim()) return null;
    const note = opt(item.note, isStr);
    if (note === null) return null;
    steps.push({ expression: item.expression, ...(note !== undefined ? { note } : {}) });
  }
  const label = opt(raw.label, isStr);
  if (label === null) return null;
  return { ...(label !== undefined ? { label } : {}), steps };
}

/** mermaid/diagram bodies are raw source; `diagram` carries its title in the
 * fence info string (the code node's `meta`) — required there, since the zoom
 * chrome's header is the only place the card names itself. */
export function sourceOf(raw: unknown, meta?: string | null, requireTitle = false): { code: string; title?: string } | null {
  if (!isStr(raw) || !raw.trim()) return null;
  const title = meta == null ? undefined : meta.trim();
  if (requireTitle && !title) return null;
  return { code: raw, ...(title ? { title } : {}) };
}

export function mermaidOf(raw: unknown): { code: string } | null {
  if (!isObj(raw) || !isStr(raw.code) || !raw.code.trim()) return null;
  return { code: raw.code };
}

export function diagramOf(raw: unknown): { title: string; code: string } | null {
  if (!isObj(raw) || !isStr(raw.title) || !raw.title.trim() || !isStr(raw.code) || !raw.code.trim()) return null;
  return { title: raw.title, code: raw.code };
}

// --- the fence gate -----------------------------------------------------------

export type FenceParse =
  | { ok: true; props: Record<string, unknown> }
  | { ok: false; reason: string };

const JSON_TAGS: Record<string, (raw: unknown) => unknown> = {
  chart: chartOf,
  timeline: timelineOf,
  preview: previewOf,
  table: tableOf,
  ticker: tickerOf,
  activity: activityOf,
  spec: specOf,
  compare: compareOf,
  progress: jobOf,
  score: scoreOf,
  flow: flowOf,
  math: mathOf,
};

/** Validate one closed fence. JSON tags parse and shape-check their body;
 * mermaid/diagram keep the raw source (diagram requires the info-string
 * title). Returns typed props for renderSpecByType, or a rejection reason —
 * the caller degrades to the ordinary code block either way (D2). */
export function parseFence(tag: string, body: string, meta?: string | null): FenceParse {
  if (!isFenceTag(tag)) return { ok: false, reason: `unknown fence tag: ${tag}` };
  if (RAW_SOURCE_TAGS.includes(tag)) {
    const src = sourceOf(body, meta ?? null, tag === 'diagram');
    if (!src) {
      return {
        ok: false,
        reason: `empty ${tag} source${tag === 'diagram' && !(meta ?? '').trim() ? ' or missing info-string title' : ''}`,
      };
    }
    return { ok: true, props: src as Record<string, unknown> };
  }
  let obj: unknown;
  // Live-pass tolerance (2026-09-21): models copy the doc's shape catalog and
  // sometimes write the JSON on the tag line (```chart {…}) — markdown reads
  // that as info-string meta and leaves the body blank. A blank body with a
  // non-blank meta means the meta IS the body; a non-blank body always wins.
  const effective = body.trim() ? body : (meta ?? '');
  try {
    obj = JSON.parse(effective);
  } catch (err) {
    return { ok: false, reason: `invalid JSON: ${err instanceof Error ? err.message : String(err)}` };
  }
  const validate = JSON_TAGS[tag];
  const props = validate(obj);
  if (!isObj(props)) return { ok: false, reason: `body does not match the ${tag} shape` };
  return { ok: true, props };
}

// --- mount components (adapters whose target prop is a ReactNode, D6) --------

/** `diagram` fence card (2.6): the catalog zoom chrome around the mermaid
 * render. Degraded first paint (D9): the raw source sits inside the chrome
 * while the lazy mermaid chunk loads, upgrading in place; a parse failure
 * keeps the raw source with a caption — never blank. */
export function FenceDiagramCard({ title, code, className }: { title: string; code: string; className?: string }) {
  const { engine, theme } = useMermaidEngine();
  // Concrete per-theme palette (beautiful-mermaid emits options as custom
  // properties on the svg — a var() value would self-clobber, live-pass fix).
  const colors = useMemo(() => readMermaidPalette(), [theme]);
  const result = useMemo(
    () => (engine ? engine.render(code, colors) : null),
    [engine, code, colors],
  );
  const [zoom, setZoom] = useState(1);
  const zoomStep = (factor: number) => setZoom((z) => Math.min(4, Math.max(0.5, z * factor)));
  return (
    <div className={cx('mb-2 max-w-md', className)}>
      <Diagram
        title={title}
        zoom={zoom}
        onZoomIn={() => zoomStep(1.25)}
        onZoomOut={() => zoomStep(0.8)}
        onReset={() => setZoom(1)}
      >
        {result && !result.error ? (
          <div
            className="flex max-h-80 overflow-auto [&_svg]:mx-auto [&_svg]:h-auto [&_svg]:max-w-full"
            dangerouslySetInnerHTML={{ __html: result.svg }}
          />
        ) : (
          <pre className="od-scroll max-w-full overflow-x-auto text-start font-mono text-[12.5px] leading-5 text-fg2">{code.trim()}</pre>
        )}
      </Diagram>
      {result?.error && (
        <p className="mt-1 text-[11px] text-muted">diagram could not be rendered</p>
      )}
    </div>
  );
}

interface FenceMathStep {
  expression: string;
  note?: string;
}

/** `math` fence card (2.3/2.6): the element's `expression` prop is a
 * ReactNode, so each LaTeX string maps through KaTeX's renderToString. KaTeX
 * stays lazy (the MarkdownBody math lane's pattern — a JSON fence never trips
 * the message-level delimiter gate): first paint shows the LaTeX source in
 * place, upgrading when the shared chunk + stylesheet land (D9); a failed
 * chunk load degrades this mount to the source permanently. */
export function FenceMathBlock({ label, steps, className }: { label?: string; steps: readonly FenceMathStep[]; className?: string }) {
  // Steps re-derive every markdown delta; key the load + render on content so
  // identity churn cannot re-import or re-render KaTeX needlessly.
  const stepsKey = useMemo(
    () => steps.map((s) => `${s.expression}\u0000${s.note ?? ''}`).join('\u0001'),
    [steps],
  );
  const [html, setHtml] = useState<string[] | null>(null);
  useEffect(() => {
    let alive = true;
    Promise.all([
      import('katex'),
      // The stylesheet rides the same lazy chunk (idempotent — the module
      // cache dedupes with the MarkdownBody math lane's import).
      import('katex/dist/katex.min.css'),
    ]).then(([katex]) => {
      if (alive) setHtml(steps.map((s) => katex.default.renderToString(s.expression, { throwOnError: false, displayMode: true })));
    }).catch(() => {
      // Present-only degradation: the source stays readable; never blank.
      if (alive) setHtml([]);
    });
    return () => { alive = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- keyed on content, not identity
  }, [stepsKey]);
  const mapped = steps.map((s, i) => ({
    expression: html && html[i] !== undefined
      ? <span dangerouslySetInnerHTML={{ __html: html[i] }} />
      : s.expression, // degraded first paint: the LaTeX source itself
    note: s.note,
  }));
  return <MathBlock label={label} steps={mapped} visibleSteps={steps.length} className={cx('mb-2', className)} />;
}
