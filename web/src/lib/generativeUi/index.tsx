// Generative-UI registry public surface (change adopt-assistant-ui-elements,
// design D4). Importing this module registers the built-in renderers — built-
// ins are ordinary registrations through the same API a plugin would use
// (AGENTS.md). The transcript renderer (ToolCall) only calls
// renderGenerativeUi and never learns about specific cards.

import "./generativeUi.css";

import { registerSpecRenderer, registerToolRenderer } from "./registry";
import { ChartCard, parseChartSpec } from "./ChartCard";
import { PreviewCard } from "./PreviewCard";
import { renderTodoCard } from "./TodoChecklistCard";
import { TimelineCard, parseTimelineEvents } from "./TimelineCard";
import { WebSearchCard, parseSearchSources } from "./WebSearchCard";

/** The pinned registry universe — the coverage-guard test asserts the live
 * registries equal these sets so a removed entry cannot ship silently
 * (mirrors toolDisplay's catalog guard). */
export const GENERATIVE_UI_TYPES = ['chart', 'timeline', 'preview'] as const;

/** Tool-keyed registry universe (cards keyed on the tool id, not a `$type`). */
export const GENERATIVE_UI_TOOLS = ['todo_write', 'web.search'] as const;

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

// --- public re-exports ------------------------------------------------------

export type { GenerativeUiCtx, SpecRenderInput, SpecRenderer, ToolRenderInput, ToolRenderer } from './registry';
export {
  registerSpecRenderer,
  registerToolRenderer,
  registeredSpecTypes,
  registeredToolKeys,
  renderGenerativeUi,
} from './registry';
export { REVEAL_STEP_MS, Reveal, revealDelay, useStaggerReveal } from './reveal';
export { parseChartSpec } from './ChartCard';
export { parseTodoPlan, todoRatio } from './TodoChecklistCard';
export { parseTimelineEvents } from './TimelineCard';
export { parseSearchSources } from './WebSearchCard';
export { TodoUpdatedSummary } from './TodoChecklistCard';
