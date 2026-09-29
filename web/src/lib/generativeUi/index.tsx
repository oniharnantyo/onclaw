// Generative-UI registry public surface (change adopt-assistant-ui-elements,
// design D4). Importing this module registers the built-in renderers — built-
// ins are ordinary registrations through the same API a plugin would use
// (AGENTS.md). The transcript renderer (ToolCall) only calls
// renderGenerativeUi and never learns about specific cards.

import "./generativeUi.css";

import { NumberTicker } from "@/components/assistant-ui/elements/number-ticker";
import { ActivityGraph } from "@/components/assistant-ui/elements/activity-graph";
import { SpecSheet } from "@/components/assistant-ui/elements/spec-sheet";
import { ComparisonCard } from "@/components/assistant-ui/elements/comparison-card";
import { JobProgress } from "@/components/assistant-ui/elements/job-progress";
import { ScoreBreakdown } from "@/components/assistant-ui/elements/score-breakdown";
import { FlowGraph } from "@/components/assistant-ui/elements/flow-graph";
import { MermaidDiagram } from "@/components/assistant-ui/elements/mermaid-diagram";
import { styledGenerativeUILibrary } from "@/components/assistant-ui/elements/generative-ui";
import { renderGenerativeUI } from "@assistant-ui/react-generative-ui";
import { registerSpecRenderer, registerToolRenderer } from "./registry";
import {
  FENCE_TAGS,
  FenceDiagramCard,
  activityOf,
  compareOf,
  diagramOf,
  flowOf,
  jobOf,
  mermaidOf,
  scoreOf,
  specOf,
  tickerOf,
  uiOf,
} from "./fences";
import { ChartCard, parseChartSpec } from "./ChartCard";
import { PreviewCard } from "./PreviewCard";
import { renderTodoCard } from "./TodoChecklistCard";
import { TimelineCard, parseTimelineEvents } from "./TimelineCard";
import { WebSearchCard, parseSearchSources } from "./WebSearchCard";
import { DocumentSearchCard, parseDocumentHits } from "./DocumentSearchCard";

/** The pinned registry universe — the coverage-guard test asserts the live
 * registries equal these sets so a removed entry cannot ship silently
 * (mirrors toolDisplay's catalog guard). Since markdown-card-elements these
 * are the markdown fence tags themselves (design D1/D3): every tag has
 * exactly one registered renderer, shared by the fence transport and the
 * legacy `$type` echo envelopes. shiki is not a tag — it upgrades ordinary
 * code blocks. */
export const GENERATIVE_UI_TYPES = FENCE_TAGS;

/** Tool-keyed registry universe (cards keyed on the tool id, not a `$type`). */
export const GENERATIVE_UI_TOOLS = ['todo_write', 'web.search', 'document.search'] as const;

// --- $type envelopes (echo tools, design D3) -------------------------------

registerSpecRenderer('chart', ({ props, ctx }) => {
  const spec = parseChartSpec(props);
  // Present-only: with nothing to show, the generic card keeps the result.
  if (!spec.label && !spec.value && !spec.delta) return null;
  return <ChartCard spec={spec} odId={'tool-' + ctx.tool}/>;
});

registerSpecRenderer('timeline', ({ props, ctx }) => {
  const events = parseTimelineEvents(props);
  if (!events) return null;
  const title = typeof props.title === 'string' ? props.title : '';
  return <TimelineCard title={title} events={events} odId={'tool-' + ctx.tool}/>;
});

registerSpecRenderer('preview', ({ props, ctx }) => {
  // While the producing call runs, the element renders nothing (spec) — the
  // generic card keeps the in-flight state.
  if (ctx.running) return null;
  const url = typeof props.url === 'string' ? props.url : '';
  const html = typeof props.html === 'string' ? props.html : '';
  const title = typeof props.title === 'string' ? props.title : '';
  // No content at all is a failure, never a blank frame.
  const failed = !!ctx.error || (!url && !html);
  return (
    <PreviewCard
      url={url}
      html={html}
      title={title}
      failed={failed}
      errorText={ctx.errorText}
      odId={'tool-' + ctx.tool}
    />
  );
});

// --- fence transport (markdown-card-elements 2.1–2.3, design D3/D6) --------
// The SAME registered renderers serve both transports: a validated fence body
// arrives via renderSpecByType, a legacy `$type` envelope via
// renderGenerativeUi. Fence bodies carry semantic data only (D6), so the
// presentational props are derived here: visibleCount is the full collection
// (cards pop in complete — the design accepts it), progress's onCancel is
// dropped (a fence has nothing to cancel), and job progress converts its
// 0–100 contract to the element's 0–1 fraction.

registerSpecRenderer('ticker', ({ props }) => {
  const tick = tickerOf(props);
  if (!tick) return null;
  return <NumberTicker value={tick.value} label={tick.label} className="mb-2"/>;
});

registerSpecRenderer('activity', ({ props }) => {
  const activity = activityOf(props);
  if (!activity) return null;
  // The element renders `total` as a mono string beside the title; the fence
  // keeps it numeric (D6: semantic data only).
  return (
    <ActivityGraph
      title={activity.title}
      total={String(activity.total)}
      start={activity.start}
      end={activity.end}
      data={activity.data}
      className="mb-2"
    />
  );
});

registerSpecRenderer('spec', ({ props }) => {
  const spec = specOf(props);
  if (!spec) return null;
  return (
    <SpecSheet
      title={spec.title}
      subtitle={spec.subtitle}
      rows={spec.rows}
      visibleCount={spec.rows.length}
      className="mb-2"
    />
  );
});

registerSpecRenderer('compare', ({ props }) => {
  const cmp = compareOf(props);
  if (!cmp) return null;
  return (
    <ComparisonCard
      traitLabels={cmp.traitLabels}
      options={cmp.options}
      recommendedId={cmp.recommendedId}
      reason={cmp.reason}
      className="mb-2"
    />
  );
});

registerSpecRenderer('progress', ({ props }) => {
  const job = jobOf(props);
  if (!job) return null;
  // onCancel deliberately omitted (D6) — the element's cancel control stays
  // inert; a fence has no producing call to interrupt.
  return (
    <JobProgress
      title={job.title}
      stages={job.stages}
      stageIndex={job.stageIndex}
      stageProgress={job.stageProgress / 100}
      eta={job.eta}
      className="mb-2"
    />
  );
});

registerSpecRenderer('score', ({ props }) => {
  const score = scoreOf(props);
  if (!score) return null;
  return (
    <ScoreBreakdown
      verdict={score.verdict}
      total={score.total}
      outOf={score.outOf}
      criteria={score.criteria}
      visibleCount={score.criteria.length}
      className="mb-2"
    />
  );
});

registerSpecRenderer('flow', ({ props }) => {
  const flow = flowOf(props);
  if (!flow) return null;
  return (
    <FlowGraph
      nodes={flow.nodes}
      edges={flow.edges}
      visibleCount={flow.nodes.length}
      className="mb-2"
    />
  );
});

registerSpecRenderer('mermaid', ({ props }) => {
  const src = mermaidOf(props);
  if (!src) return null;
  // The vendored element owns the lazy-engine lane (D9): raw source first
  // paint, upgrading in place; a parse failure keeps the source readable.
  return <div className="mb-2"><MermaidDiagram code={src.code}/></div>;
});

registerSpecRenderer('diagram', ({ props }) => {
  const d = diagramOf(props);
  if (!d) return null;
  return <FenceDiagramCard title={d.title} code={d.code}/>;
});

// Composition trees (the `ui` fence): the vendored generative-ui element
// renders the model's `{$type, ...props}` tree against the shadcn-installed
// vocabulary library. uiOf already walked the tree (trap guards: Icon
// name/size, gap/padding tokens, `background` dropped), so this is a pure
// render — the upstream library validates nothing itself, by design.
registerSpecRenderer('ui', ({ props }) => {
  // parseFence has already run uiOf and handed us `{spec}` — re-running uiOf
  // here would double-wrap (the wrapper has no $type, so it rejects itself)
  // and the fence would render NOTHING, silently. Accept the validated spec;
  // still tolerate a raw tree for the $type tool-result path.
  const p = props as Record<string, unknown> | null;
  const spec = p !== null && typeof p === 'object' && p.spec instanceof Object ? (p.spec as Record<string, unknown>) : p;
  if (!spec || typeof spec.$type !== 'string') return null;
  return <div className="mb-2">{renderGenerativeUI(spec, styledGenerativeUILibrary)}</div>;
});

// --- tool-keyed cards (envelope sniffed from the call's own args/result) ---

registerToolRenderer('web.search', ({ args, res, ms, ctx }) => {
  const query = args && typeof args.query === 'string' ? args.query : '';
  // Without a usable query, or on a failed call, the generic card stays.
  if (!query.trim() || ctx.error) return null;
  const sources = parseSearchSources(res);
  // In flight → query pill + searching status, no rows yet; done with an
  // unparsable envelope → generic fallback (never both renderings).
  if (!sources && !ctx.running) return null;
  return <WebSearchCard query={query} sources={sources ?? []} running={ctx.running} ms={ms} live={ctx.live}/>;
});

registerToolRenderer('todo_write', ({ args, res, ctx }) => {
  return renderTodoCard({ args, res, ctx });
});

// Document search (add-reference-documents 10.1): the binding envelope carries
// the query too, so the pill survives even when the model omitted it from the
// call args. Same flow as web.search: in flight → query pill + searching
// status; done with an unparsable envelope → generic fallback (never both).
registerToolRenderer('document.search', ({ args, res, ms, ctx }) => {
  const argQuery = args && typeof args.query === 'string' ? args.query : '';
  const envelopeQuery = res && typeof res.query === 'string' ? res.query : '';
  const query = argQuery.trim() ? argQuery : envelopeQuery;
  // Without a usable query, or on a failed call, the generic card stays.
  if (!query.trim() || ctx.error) return null;
  const hits = parseDocumentHits(res);
  if (!hits && !ctx.running) return null;
  return <DocumentSearchCard query={query} hits={hits ?? []} running={ctx.running} ms={ms} live={ctx.live}/>;
});

// --- public re-exports ------------------------------------------------------

export type { GenerativeUiCtx, SpecRenderInput, SpecRenderer, ToolRenderInput, ToolRenderer } from './registry';
export {
  registerSpecRenderer,
  registerToolRenderer,
  registeredSpecTypes,
  registeredToolKeys,
  renderGenerativeUi,
  renderSpecByType,
} from './registry';
export { REVEAL_STEP_MS, Reveal, revealDelay, useStaggerReveal } from './reveal';
export { parseChartSpec } from './ChartCard';
export { parseTodoPlan, todoRatio } from './TodoChecklistCard';
export { parseTimelineEvents } from './TimelineCard';
export { parseSearchSources } from './WebSearchCard';
export { parseDocumentHits } from './DocumentSearchCard';
export { TodoUpdatedSummary } from './TodoChecklistCard';
export { TodoPlanRows } from './TodoChecklistCard';
export { FENCE_TAGS, isFenceTag, parseFence, uiOf } from './fences';
