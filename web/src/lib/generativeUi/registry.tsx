// `$type`-keyed generative-UI spec registry (change adopt-assistant-ui-elements,
// design D4). Built before any element landed: a recognized `$type` dispatches
// to its registered renderer with the remaining envelope keys as props and
// nested specs resolved under `children`; an unrecognized `$type` mints
// nothing user-visible (the tool card keeps its standard rendering — no raw
// JSON in the transcript body). Tool-shaped cards (web search, todo checklist)
// key on the tool id and register through the same API, so a new renderer
// never edits the transcript renderer — plugins register like any other
// OnClaw registry surface (AGENTS.md).

import { Fragment } from "react";
import { parseArgs } from "../toolDisplay";

/** Render-time context handed to every renderer. */
export interface GenerativeUiCtx {
  /** Arrival during a live turn — reveal-style elements may stagger (spec:
   * streaming honesty; hydrated renders skip the stagger). */
  live: boolean;
  /** The producing tool call is still in flight. */
  running: boolean;
  /** The producing tool call failed. */
  error: boolean;
  /** The producing call's error text, when any. */
  errorText?: string;
  /** Tool id of the producing call (data-od-id anchors, per-tool nuances). */
  tool: string;
}

/** Input to a `$type` renderer: envelope keys minus `$type`, with nested
 * specs already resolved under `children`. */
export interface SpecRenderInput {
  props: Record<string, unknown>;
  children: React.ReactNode;
  ctx: GenerativeUiCtx;
}

/** Input to a tool-keyed renderer: the call's args/result parsed to objects
 * when they parse (null otherwise), the raw strings, latency, and context. */
export interface ToolRenderInput {
  args: Record<string, unknown> | null;
  res: Record<string, unknown> | null;
  rawArgs?: string;
  rawRes?: string;
  ms?: number;
  ctx: GenerativeUiCtx;
}

export type SpecRenderer = (input: SpecRenderInput) => React.ReactNode;
export type ToolRenderer = (input: ToolRenderInput) => React.ReactNode;

const specRenderers = new Map<string, SpecRenderer>();
const toolRenderers = new Map<string, ToolRenderer>();

/** Register a renderer for a `$type` (plugins call this — no core edits). */
export function registerSpecRenderer(type: string, renderer: SpecRenderer): void {
  specRenderers.set(type, renderer);
}

/** Register a renderer keyed on a tool id — for cards whose envelope is
 * sniffed from the call's own args/result rather than a `$type`. */
export function registerToolRenderer(tool: string, renderer: ToolRenderer): void {
  toolRenderers.set(tool, renderer);
}

/** Live `$type` registry contents, sorted — read by the coverage-guard test. */
export function registeredSpecTypes(): string[] {
  return [...specRenderers.keys()].sort();
}

/** Live tool-keyed registry contents, sorted — read by the coverage-guard test. */
export function registeredToolKeys(): string[] {
  return [...toolRenderers.keys()].sort();
}

/** Resolve an envelope's `children`: nested specs render through the registry
 * (recursively), scalars pass through as text, anything else is dropped. */
function resolveChildren(children: unknown, ctx: GenerativeUiCtx): React.ReactNode {
  if (children == null) return null;
  if (Array.isArray(children)) {
    return children.map((child, i) => <Fragment key={i}>{resolveChildren(child, ctx)}</Fragment>);
  }
  if (typeof children === 'object') {
    const obj = children as Record<string, unknown>;
    const type = typeof obj.$type === 'string' ? obj.$type : null;
    const renderer = type ? specRenderers.get(type) : undefined;
    if (!renderer) return null;
    const { $type: _type, children: nested, ...rest } = obj;
    return renderer({ props: rest, children: resolveChildren(nested, ctx), ctx });
  }
  if (typeof children === 'string' || typeof children === 'number') return String(children);
  return null;
}

/** Mount decision for one tool call: a recognized `$type` (result first, then
 * echoed arguments — D3: echo tools return their own args as the result)
 * renders its registered element; an unrecognized `$type` renders nothing
 * user-visible; otherwise a tool-keyed card may sniff the call's envelopes.
 * Null always means "existing generic rendering unchanged". */
export function renderGenerativeUi(input: {
  tool: string;
  rawArgs?: string;
  rawRes?: string;
  ms?: number;
  ctx: GenerativeUiCtx;
}): React.ReactNode {
  const { ctx } = input;
  for (const raw of [input.rawRes, input.rawArgs]) {
    const obj = parseArgs(raw); // safe object parse; arrays/scalars are not envelopes
    const type = obj && typeof obj.$type === 'string' ? obj.$type : null;
    if (!obj || !type) continue;
    const renderer = specRenderers.get(type);
    if (!renderer) return null; // unknown $type → silent (spec)
    const { $type: _type, children, ...rest } = obj;
    return renderer({ props: rest, children: resolveChildren(children, ctx), ctx });
  }
  const renderer = toolRenderers.get(input.tool);
  if (!renderer) return null;
  return renderer({
    args: parseArgs(input.rawArgs),
    res: parseArgs(input.rawRes),
    rawArgs: input.rawArgs,
    rawRes: input.rawRes,
    ms: input.ms,
    ctx,
  });
}
