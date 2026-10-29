/**
 * Context meter (task 5.4): the session's context ring on the composer's
 * control rail, with the breakdown popover above it (design D7). Accounting
 * keeps the sidebar's old rules: with a session model override the limit is
 * that model's catalog context window (the store's capability rows); without
 * one the backend's agent-keyed usage limit stands. Nothing renders before
 * the first turn — never a fabricated value.
 *
 * The popover composes the breakdown data: the agent's prompt documents
 * through the per-agent cache, the enabled tool count (the settings catalog
 * narrowed by the agent's opt-in allowlist, refreshed when the popover
 * opens), the store's enabled skills, and the session's entries. The
 * displayed total is the reported `used`, never the estimate sum; the four
 * client segments carry an "estimated" caption and are never scaled, the
 * server remainder is exact by construction, headroom is `limit − used`, and
 * the last turn's input/output rows render only once the backend reports
 * them. The opening animation is skipped under reduced motion.
 */
import { useEffect, useMemo, useRef, useState } from "react";
import type { ReactElement } from "react";

import type { ContextUsage } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/agents/models";
import type {
  Agent,
  Skill,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type {
  ModelCapability,
  PromptDocuments,
  ToolEntry,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";

import { client } from "../../../lib/client";
import {
  computeContextBreakdown,
  type ContextBreakdown,
  type ContextSegment,
  type SegmentKey,
} from "../lib/contextBreakdown";
import { cachedPromptDocuments, loadPromptDocuments } from "../lib/promptDocs";
import { formatTokens } from "../format";
import { useChatStore } from "../store";
import { useReducedMotion } from "../tools/useReducedMotion";
import type { ChatStore } from "../types";

/** Severity ladder for the ring (task 9.3): accent below 65%, warning
 * 65–85%, error above 85% — token classes, so both themes re-skin. The
 * percentage label rides the lifted text variants (design D6), the same
 * grammar the links and error rows use; the strokes stay as they were. */
const TIER_CLASSES: { stroke: string; text: string }[] = [
  { stroke: "stroke-accent", text: "text-accenttext" },
  { stroke: "stroke-warn", text: "text-warntext" },
  { stroke: "stroke-danger", text: "text-dangertext" },
];

/** Legend/bar color per segment — token mixes, so both themes stay legible. */
const SEGMENT_COLORS: Record<SegmentKey, string> = {
  instructions: "var(--accent)",
  tools: "var(--success)",
  files: "var(--warn)",
  conversation: "color-mix(in oklab, var(--fg) 65%, transparent)",
  server: "color-mix(in oklab, var(--fg) 30%, transparent)",
};

/** Below 10% the label keeps one decimal: at a 200k window a short turn moves
 * the fill by well under a point, and integer rounding would leave the ring
 * reading the same percentage for many turns. */
function pctLabel(pct: number): string {
  return pct > 0 && pct < 10
    ? String(Math.round(pct * 10) / 10)
    : String(Math.round(pct));
}

/** The ring visual, shared by the meter and the standalone component tests. */
function RingVisual({
  used,
  limit,
}: {
  used: number;
  limit: number;
}): ReactElement {
  const pct = Math.min(100, Math.max(0, (used / limit) * 100));
  const tier = TIER_CLASSES[pct > 85 ? 2 : pct >= 65 ? 1 : 0];
  const radius = 7;
  const circumference = 2 * Math.PI * radius;

  return (
    <>
      <svg
        width="18"
        height="18"
        viewBox="0 0 18 18"
        aria-hidden="true"
        className="-rotate-90"
      >
        <circle
          cx="9"
          cy="9"
          r={radius}
          fill="none"
          strokeWidth="2.5"
          className="stroke-linesoft"
        />
        <circle
          cx="9"
          cy="9"
          r={radius}
          fill="none"
          strokeWidth="2.5"
          strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={circumference * (1 - pct / 100)}
          className={`stroke-current ${tier.stroke}`}
          data-testid="context-ring-fill"
        />
      </svg>
      <span
        className={`font-mono text-xs ${tier.text}`}
        data-testid="context-ring-pct"
      >
        {pctLabel(pct)}%
      </span>
    </>
  );
}

/** The ring readout: fill proportional to used/limit, severity ladder, and
 * the used/limit title the header's seams read. Renders nothing without a
 * limit. */
export function ContextRing({
  used,
  limit,
}: {
  used: number;
  limit: number;
}): ReactElement | null {
  if (limit <= 0) return null;

  return (
    <span
      className="flex items-center gap-1.5"
      data-testid="context-ring"
      title={`${formatTokens(used)} / ${formatTokens(limit)} tokens`}
    >
      <RingVisual used={used} limit={limit} />
    </span>
  );
}

/** One legend row: color chip, label, optional "estimated" caption, and the
 * token figure. */
function LegendRow({ segment }: { segment: ContextSegment }): ReactElement {
  return (
    <div
      className="flex items-center gap-2"
      data-testid={`context-segment-${segment.key}`}
    >
      <span
        aria-hidden="true"
        className="h-2 w-2 shrink-0 rounded-sm"
        style={{ background: SEGMENT_COLORS[segment.key] }}
      />
      <span className="min-w-0 flex-1 truncate text-[12px] text-fg2">
        {segment.label}
        {segment.estimated && (
          <span
            className="ms-1.5 text-[10px] text-muted"
            data-testid={`context-estimated-${segment.key}`}
          >
            estimated
          </span>
        )}
      </span>
      <span className="shrink-0 font-mono text-[11px] text-fg2">
        {formatTokens(segment.tokens)}
      </span>
    </div>
  );
}

/** The breakdown panel: opened above the ring, closed by Escape or an outside
 * click. Rendered as a span with flex display — the ring's wrapper is
 * phrasing content, and a span keeps the HTML honest. */
function BreakdownPopover({
  breakdown,
  lastInput,
  lastOutput,
  reducedMotion,
}: {
  breakdown: ContextBreakdown;
  lastInput: number | null;
  lastOutput: number | null;
  reducedMotion: boolean;
}): ReactElement {
  const { limit } = breakdown;

  const barWidth = (tokens: number): string =>
    `${Math.min(100, Math.max(0, (tokens / limit) * 100))}%`;

  return (
    <span
      role="dialog"
      aria-label="Context breakdown"
      data-testid="context-popover"
      className={`absolute right-0 bottom-full z-30 mb-2 flex w-72 flex-col overflow-hidden rounded-md border border-line bg-warm shadow-[var(--elev-raised)] ${
        reducedMotion ? "" : "fade-in zoom-in-95 animate-in duration-150"
      }`}
    >
      <span className="border-b border-linesoft px-3 py-1.5 font-mono text-[10px] tracking-wider text-muted uppercase">
        Context
      </span>
      <span className="flex flex-col gap-2 px-3 py-2.5">
        <div
          className="font-mono text-[12px] text-fg"
          data-testid="context-popover-total"
        >
          {formatTokens(breakdown.used)} / {formatTokens(limit)} tokens
        </div>
        <div
          aria-hidden="true"
          className="flex h-1.5 w-full overflow-hidden rounded-full bg-linesoft"
          data-testid="context-popover-bar"
        >
          {breakdown.segments.map((segment: ContextSegment): ReactElement => (
            <span
              key={segment.key}
              className="h-full"
              style={{
                width: barWidth(segment.tokens),
                background: SEGMENT_COLORS[segment.key],
              }}
            />
          ))}
        </div>
        <div className="flex flex-col gap-1">
          {breakdown.segments.map((segment: ContextSegment): ReactElement => (
            <LegendRow key={segment.key} segment={segment} />
          ))}
        </div>
        <div
          className="flex items-center justify-between border-t border-linesoft pt-1.5"
          data-testid="context-headroom"
        >
          <span className="text-[12px] text-fg2">Headroom</span>
          <span className="font-mono text-[11px] text-fg2">
            {formatTokens(breakdown.headroom)}
          </span>
        </div>
        {lastInput !== null && (
          <div
            className="flex items-center justify-between"
            data-testid="context-last-input"
          >
            <span className="text-[12px] text-fg2">Last turn input</span>
            <span className="font-mono text-[11px] text-fg2">
              {formatTokens(lastInput)}
            </span>
          </div>
        )}
        {lastOutput !== null && (
          <div
            className="flex items-center justify-between"
            data-testid="context-last-output"
          >
            <span className="text-[12px] text-fg2">Last turn output</span>
            <span className="font-mono text-[11px] text-fg2">
              {formatTokens(lastOutput)}
            </span>
          </div>
        )}
      </span>
    </span>
  );
}

/** Reads the usage record's last-turn fields (design D7); a count is null
 * before any completed turn, so the popover renders no fabricated zero
 * rows. */
interface LastTurnRead {
  input: number | null;
  output: number | null;
}

function readLastTurn(usage: ContextUsage): LastTurnRead {
  return {
    input: usage.last_input > 0 ? usage.last_input : null,
    output: usage.last_output > 0 ? usage.last_output : null,
  };
}

/** The rail meter: ring trigger plus the breakdown popover, fed by the store
 * and the estimator. */
export function ContextMeter(): ReactElement | null {
  const agents = useChatStore((s: ChatStore) => s.agents);
  const skills = useChatStore((s: ChatStore) => s.skills);
  const entries = useChatStore((s: ChatStore) => s.entries);
  const models = useChatStore((s: ChatStore) => s.models);
  const modelOverride = useChatStore((s: ChatStore) => s.modelOverride);
  const activeAgentID = useChatStore((s: ChatStore) => s.activeAgentID);
  const usage = useChatStore((s: ChatStore) => s.usage);

  const reducedMotion = useReducedMotion();
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLSpanElement | null>(null);

  const agent = agents.find((a: Agent) => a.ID === activeAgentID) ?? null;

  // Prompt documents per agent (design D7): the per-agent cache answers
  // during render, and one background load per agent refreshes the keyed
  // state — a failure leaves the Instructions segment at its cached (or
  // zero) estimate.
  const [loadedDocs, setLoadedDocs] = useState<{
    agentID: string;
    docs: PromptDocuments;
  } | null>(null);

  useEffect((): (() => void) | undefined => {
    if (!agent) return undefined;

    const agentID = agent.ID;
    let mounted = true;

    void loadPromptDocuments(agentID)
      .then((docs: PromptDocuments): void => {
        if (mounted) setLoadedDocs({ agentID, docs });
      })
      .catch((): void => {
        // Uncached failure: the segment stays where it was.
      });

    return (): void => {
      mounted = false;
    };
  }, [agent?.ID]);

  const promptDocs = agent
    ? loadedDocs && loadedDocs.agentID === agent.ID
      ? loadedDocs.docs
      : cachedPromptDocuments(agent.ID)
    : null;

  // Enabled tools (design D7): the settings catalog narrowed by the agent's
  // opt-in allowlist when one is set. Fetched on mount and refreshed when
  // the popover opens, so a settings toggle shows up on the next look.
  const [toolCatalog, setToolCatalog] = useState<ToolEntry[] | null>(null);

  const refreshTools = (): void => {
    // Optional chain: a client surface without the member (narrow mocked
    // surfaces, a settings regression) keeps the previous catalog instead of
    // crashing the composer rail.
    Promise.resolve(client.settings?.tools?.())
      .then((tools: ToolEntry[] | undefined): void => {
        if (tools) setToolCatalog(Array.isArray(tools) ? tools : []);
      })
      .catch((): void => {
        // Keep the previous catalog; the estimate is captioned anyway.
      });
  };

  useEffect((): void => {
    // One-shot per mount; opening the popover refreshes the catalog.
    refreshTools();
  }, []);

  const enabledTools = useMemo((): number => {
    if (!toolCatalog) return 0;

    const allowed = agent?.Tools;

    return toolCatalog.filter(
      (tool: ToolEntry): boolean =>
        tool.Enabled && (!allowed || allowed.includes(tool.Name))
    ).length;
  }, [toolCatalog, agent?.Tools]);

  const enabledSkills = useMemo((): number => {
    const allowed = agent?.Skills;

    return allowed
      ? skills.filter((skill: Skill): boolean => allowed.includes(skill.Name))
          .length
      : skills.length;
  }, [skills, agent?.Skills]);

  // The override's catalog window (design D1); an unknown or zero-window
  // model falls back to the backend's reported limit.
  const overrideLimit =
    models.find((m: ModelCapability) => m.ID === modelOverride)
      ?.ContextWindow ?? 0;

  const limit = overrideLimit > 0 ? overrideLimit : (usage?.limit ?? 0);

  const breakdown = useMemo((): ContextBreakdown | null => {
    if (!usage) return null;

    return computeContextBreakdown({
      used: usage.used,
      limit,
      promptDocs,
      enabledTools,
      enabledSkills,
      entries,
    });
  }, [usage, limit, promptDocs, enabledTools, enabledSkills, entries]);

  const lastTurn = usage ? readLastTurn(usage) : null;

  // Open popover: outside click and Escape close (the same contract as the
  // composer's rail pickers).
  useEffect((): (() => void) | undefined => {
    if (!open) return undefined;

    const onMouseDown = (event: MouseEvent): void => {
      // SAFETY: DOM event targets are nodes, and contains() takes one.
      const target = event.target as Node | null;

      if (rootRef.current && target && !rootRef.current.contains(target)) {
        setOpen(false);
      }
    };

    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key === "Escape") setOpen(false);
    };

    document.addEventListener("mousedown", onMouseDown);
    document.addEventListener("keydown", onKeyDown);

    return (): void => {
      document.removeEventListener("mousedown", onMouseDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  if (!usage || !breakdown || limit <= 0) return null;

  const openPopover = (): void => {
    setOpen(true);
    refreshTools();
  };

  return (
    <span
      ref={rootRef}
      className="relative flex items-center gap-1.5"
      data-testid="context-meter"
    >
      <button
        type="button"
        data-testid="context-ring-trigger"
        aria-label="Context usage breakdown"
        aria-expanded={open}
        aria-haspopup="dialog"
        className="flex items-center gap-1.5 rounded-md"
        onClick={() => (open ? setOpen(false) : openPopover())}
      >
        <ContextRing used={usage.used} limit={limit} />
      </button>
      {open && lastTurn && (
        <BreakdownPopover
          breakdown={breakdown}
          lastInput={lastTurn.input}
          lastOutput={lastTurn.output}
          reducedMotion={reducedMotion}
        />
      )}
    </span>
  );
}
