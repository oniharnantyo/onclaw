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

import { useMemo, useState } from "react";
import { Diagram } from "@/components/assistant-ui/elements/diagram";
import { readMermaidPalette, useMermaidEngine } from "@/lib/assistantUi/mermaidEngine";
import { cx } from "../helpers";

// --- the fence-tag universe --------------------------------------------------

/** The pinned fence universe — the coverage-guard set (GENERATIVE_UI_TYPES in
 * index.tsx is exactly this). shiki is deliberately NOT a tag: it upgrades
 * ordinary code blocks and has no fence of its own. */
export const FENCE_TAGS = [
  'chart', 'timeline', 'preview', 'ticker', 'activity', 'spec',
  'compare', 'progress', 'score', 'flow', 'mermaid', 'diagram', 'ui',
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

// --- `ui` (composition trees) ------------------------------------------------
//
// The `ui` fence carries a `{$type, ...props}` TREE rendered by the vendored
// generative-ui element (uiLibrary). Unlike the flat tags above, its shape is
// recursive, so it cannot be a per-field validator.
//
// The gate is SCHEMA-DRIVEN: every vocabulary component declares its props as
// a zod schema (`properties`), and that schema is the validator. Everything I
// would otherwise hand-write is already encoded there — enums (Icon.name,
// Icon.size), ranges (gap/padding 0–8), required props (Text.value,
// Button.label, …) — plus safeParse strips unknown keys by default, so an
// invented prop can never reach a renderer. Hand-writing guards would only
// duplicate (and drift from) the schemas.
//
// ONE rule survives as hand-written: `Card.background` is deleted. It is a
// schema-VALID string, but upstream emits it as an inline CSS value AND forces
// `color:white` on the card — `background:"#ffffff"` is white text on a white
// card (the trap that made a gallery render blank). Host CSS owns card
// surfaces here, so the prop is deleted; no generated validator can know that.
//
// Failure semantics: the ROOT node is strict (a root that fails rejects the
// fence → the caller degrades to the ordinary code block, D2). INTERIOR
// failures drop just that node and keep the siblings — a 400-char composition
// missing one caption is a far better outcome than 400 chars of raw JSON, and
// matches the fence philosophy (fail-open within a unit, fail-closed at the
// boundary).

import { uiLibrary } from "@/components/assistant-ui/elements/generative-ui";

/** Depth guard: a tree nested past this is a model loop, not a layout. */
const UI_MAX_DEPTH = 12;
/** Node-count guard: keeps one fence from minting an unbounded DOM. */
const UI_MAX_NODES = 400;

export interface FenceUi {
  /** The validated tree, handed to renderGenerativeUI as-is. */
  spec: Record<string, unknown>;
}

/** `gap`/`padding` are 0–8 tokens (4px units) in the vocabulary's CSS. Models
 * think in pixels — a live glm-4.7 turn wrote `gap: 16` for a comfortable
 * spacing, which upstream silently ignores (no CSS rule above 8). Clamping
 * keeps the layout intent instead of dropping the node. */
function clampTokens(props: Record<string, unknown>): void {
  for (const k of ['gap', 'padding']) {
    const v = props[k];
    if (typeof v === 'number' && Number.isFinite(v)) props[k] = Math.max(0, Math.min(8, Math.round(v)));
  }
}

function uiNode(raw: unknown, depth: number, budget: { n: number }): Record<string, unknown> | string | null {
  if (budget.n-- <= 0 || depth > UI_MAX_DEPTH) return null;
  // Text children are legal (the renderer accepts primitives).
  if (typeof raw === 'string') return raw;
  if (!isObj(raw)) return null;

  // `type` is accepted as an alias of `$type` (live-model tolerance,
  // 2026-09-21: glm-4.7 wrote `"type"` in its first ui fence — `$`-prefixed
  // keys are not a universal convention, and the fence failing closed on it
  // degraded the whole composition). No component declares a `type` prop, so
  // the fallback can never shadow a real one.
  const type = isStr(raw.$type) && raw.$type.trim() ? raw.$type : isStr(raw.type) && raw.type.trim() ? raw.type : null;
  if (!type) return null;
  const entry = uiLibrary[type];
  if (!entry) return null; // interior: this node drops; root: the fence degrades

  // The schema gate. `children` recurses separately (the schemas don't declare
  // it, and safeParse strips it); `$type`/`type` are framework keys.
  const { $type: _t, type: _legacyType, children: _c, ...rawProps } = raw;
  const props: Record<string, unknown> = { ...rawProps };
  clampTokens(props);
  const parsed = entry.properties.safeParse(props);
  if (!parsed.success) return null;

  const out: Record<string, unknown> = { $type: type, ...parsed.data };
  if (type === 'Card') delete out.background; // the one hand-written rule (see above)

  if (raw.children !== undefined) {
    const kids = Array.isArray(raw.children) ? raw.children : [raw.children];
    const resolved: unknown[] = [];
    for (const kid of kids) {
      const n = uiNode(kid, depth + 1, budget);
      if (n !== null) resolved.push(n);
    }
    if (resolved.length === 1) out.children = resolved[0];
    else if (resolved.length > 1) out.children = resolved;
  }
  return out;
}

/** Validate a `ui` composition tree. Strict at the root (a root that fails
 * rejects the fence → degrade to code block); tolerant inside (a node that
 * fails drops, its siblings render). */
export function uiOf(raw: unknown): FenceUi | null {
  const budget = { n: UI_MAX_NODES };
  if (Array.isArray(raw)) {
    const nodes: unknown[] = [];
    for (const item of raw) {
      const n = uiNode(item, 0, budget);
      if (n !== null) nodes.push(n);
    }
    return nodes.length ? { spec: { $type: 'Col', children: nodes } } : null;
  }
  const node = uiNode(raw, 0, budget);
  if (node === null || typeof node === 'string') return null;
  return { spec: node };
}

// --- the fence gate -----------------------------------------------------------

export type FenceParse =
  | { ok: true; props: Record<string, unknown> }
  | { ok: false; reason: string };

const JSON_TAGS: Record<string, (raw: unknown) => unknown> = {
  chart: chartOf,
  timeline: timelineOf,
  preview: previewOf,
  ticker: tickerOf,
  activity: activityOf,
  spec: specOf,
  compare: compareOf,
  progress: jobOf,
  score: scoreOf,
  flow: flowOf,
  ui: uiOf,
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
