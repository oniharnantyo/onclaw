/**
 * ToolCallFrame (task 4.3, design D6; flattened by `flatten-chat-transcript`
 * design D3/D4): the card anatomy and states every renderer composes, built
 * on the vendored tool-call element's anatomy (a collapsible disclosure) —
 * the card paints its OWN line: it consumes no surface (not `surfaces.paper`
 * any more), the tool name recedes to the muted foreground, the sentence
 * carries the payload, and the body opens under a left hairline rail.
 * Header: icon + display name
 * (raw name for unknown tools), a one-line sentence only for known tools
 * (intent while running, outcome after), latency only when a timing exists,
 * a running label without a spinner plus a live elapsed clock, and failure
 * styling with the error readable in the body. Body: labeled arguments and
 * result sections via the shaped body, the >10-line clamp, and a raw toggle
 * showing the delivered strings verbatim. Collapse state persists through
 * tools/collapse.ts (start collapsed; edit/write auto-open once on the
 * running→completed edge; an explicit close wins); the card updates in place
 * keyed by its tool call id. Approval parts render the approval gate
 * (task 4.4) instead of the tool card.
 */
import { useEffect, useMemo, useReducer, useState } from "react";
import type { ReactElement } from "react";
import { ChevronRightIcon } from "lucide-react";
import {
  useToolCallElapsed,
  type ToolCallMessagePartProps,
} from "@assistant-ui/react";
import type { ToolCallTiming } from "@assistant-ui/react";

import { Icon } from "../../../../components/ui/Icon";
import { cn } from "../../../../lib/cn";
import {
  CollapsibleContent,
  CollapsibleRoot,
  CollapsibleTrigger,
} from "@/components/assistant-ui/elements/collapsible-root";
import { ShimmerLabel } from "@/components/assistant-ui/elements/surfaces";

import { ClampedText } from "../primitives/ClampedText";
import type { Entry } from "../../types";
import { useChatStore, type ExtendedChatStore } from "../../store";
import {
  isOpen,
  recordCallStatus,
  setOpen as setCollapseOpen,
} from "../collapse";
import { parseArgs } from "../parse";
import {
  formatElapsed,
  formatLatency,
  parseSentenceMarkup,
  toolDisplayInfo,
  toolSentence,
  type SentenceSegment,
} from "../toolDisplay";
import { useReducedMotion } from "../useReducedMotion";
import { ApprovalGate } from "./ApprovalGate";
import { BodyBoundary, FallbackBody } from "./fallbackBody";

/** The narrow type guard the frame reads results through (the part's result
 * is delivered as the persisted string). */
function isTextResult(value: unknown): value is string {
  return typeof value === "string";
}

/** The first entry timestamp for a call (the running event's time — the
 * live clock's start when no timing rides the part). */
function entryAtOf(entries: Entry[], id: string): string | undefined {
  for (const entry of entries) {
    if (entry.id === id) return entry.at;
  }

  return undefined;
}

/** The latency a part's timing carries, formatted; absent until the call
 * completes — a timing without an end stamps no latency. */
function timingLatency(timing: ToolCallTiming | undefined): number | undefined {
  if (timing === undefined || timing.completedAt === undefined) {
    return undefined;
  }

  return Math.max(0, timing.completedAt - timing.startedAt);
}

/** Formats live elapsed milliseconds as the m:ss clock. */
function formatClock(ms: number): string {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000));
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;

  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

/** One markup segment of a header sentence (the D3 grammar). The sentence
 * carries the payload the call names, so it reads at full contrast while
 * the tool name beside it recedes (design D3). */
function Sentence({
  sentence,
  testId,
}: {
  sentence: string;
  testId: string;
}): ReactElement {
  return (
    <span
      className="min-w-0 flex-1 truncate text-xs text-foreground/90"
      data-testid={testId}
    >
      {parseSentenceMarkup(sentence).map(
        (segment: SentenceSegment, i: number): ReactElement => {
          if (segment.kind === "chip") {
            return (
              <code
                key={i}
                className="rounded bg-foreground/[0.07] px-1 py-px font-mono text-[11px] text-foreground"
              >
                {segment.text}
              </code>
            );
          }

          if (segment.kind === "literal") {
            return (
              <span key={i} className="text-foreground/70">
                '{segment.text}'
              </span>
            );
          }

          return <span key={i}>{segment.text}</span>;
        }
      )}
    </span>
  );
}

/** The card shell every renderer mounts: header chrome, collapse
 * persistence, raw toggle, boundary, and the approval detour. */
export function ToolCard({
  part,
  children,
}: {
  part: ToolCallMessagePartProps;
  children?: ReactElement;
}): ReactElement {
  if (part.approval !== undefined) return <ApprovalGate part={part} />;

  return <ToolCallFrame part={part}>{children}</ToolCallFrame>;
}

function ToolCallFrame({
  part,
  children,
}: {
  part: ToolCallMessagePartProps;
  children?: ReactElement;
}): ReactElement {
  const reducedMotion = useReducedMotion();
  const [, bump] = useReducer((x: number): number => x + 1, 0);
  // The raw view replaces the formatted body until toggled back; per-card
  // local state — the collapse map owns the open state.
  const [raw, setRaw] = useState(false);

  const catalog = useChatStore((s: ExtendedChatStore) => s.toolCatalog);

  const entryAt = useChatStore((s: ExtendedChatStore) =>
    entryAtOf(s.entries, part.toolCallId)
  );

  // The display catalog loads once per app run; the card triggers the
  // idempotent fetch so any mount context seeds it.
  useEffect((): void => {
    void useChatStore.getState().ensureToolCatalog();
  }, []);

  const running = part.status.type === "running";
  const isError = part.isError === true;
  const resultText = isTextResult(part.result) ? part.result : "";

  // Auto-open bookkeeping: the transition detector lives in the module-level
  // map; a bump re-renders the card when the map opened underneath us.
  useEffect((): void => {
    const status = running ? "running" : "completed";

    if (recordCallStatus(part.toolCallId, part.toolName, status)) bump();
  }, [part.toolCallId, part.toolName, running]);

  // Live elapsed: the runtime hook (parts carrying timing) first, then the
  // store entry's running-event clock; one 1s ticker per running card.
  const timingElapsed = useToolCallElapsed();

  useEffect((): void | (() => void) => {
    if (!running) return undefined;

    const timer = setInterval(bump, 1000);

    return (): void => clearInterval(timer);
  }, [running]);

  const args = useMemo(
    (): ReturnType<typeof parseArgs> => parseArgs(part.argsText),
    [part.argsText]
  );

  const { displayName, iconKey } = useMemo(
    (): ReturnType<typeof toolDisplayInfo> =>
      toolDisplayInfo(part.toolName, catalog),
    [part.toolName, catalog]
  );

  const sentence = useMemo(
    (): string | null =>
      toolSentence({
        name: part.toolName,
        args,
        result: resultText,
        state: running ? "running" : "done",
        isError,
      }),
    [args, isError, part.toolName, resultText, running]
  );

  const open = isOpen(part.toolCallId);

  const handleOpenChange = (next: boolean): void => {
    setCollapseOpen(part.toolCallId, next);
    bump();
  };

  const latency = timingLatency(part.timing);

  const elapsed = running
    ? timingElapsed !== undefined
      ? formatClock(timingElapsed)
      : entryAt !== undefined
        ? formatElapsed(entryAt, Date.now())
        : null
    : null;

  const fallback = (
    <FallbackBody
      argsText={part.argsText}
      resultText={resultText}
      isError={isError}
      testidPrefix={`tool-fallback-${part.toolCallId}`}
    />
  );

  const body =
    args === null ? (
      fallback
    ) : (
      <BodyBoundary fallback={fallback}>{children}</BodyBoundary>
    );

  return (
    <div
      className={cn("w-full", isError && "text-dangertext")}
      data-testid={`entry-tool-${part.toolCallId}`}
      data-tool-state={running ? "running" : isError ? "failed" : "completed"}
    >
      <CollapsibleRoot open={open} onOpenChange={handleOpenChange}>
        <CollapsibleTrigger
          className="group/tt text-muted-foreground hover:text-foreground/90 flex w-full items-center gap-2 rounded-sm py-1 text-left transition-colors outline-none hover:bg-foreground/[0.03]"
          data-testid={`tool-header-${part.toolCallId}`}
        >
          <ChevronRightIcon className="group-data-[state=open]/tt:rotate-90 size-3.5 shrink-0 opacity-60 transition-transform duration-200 motion-reduce:transition-none" />
          <Icon name={iconKey} size={13} className="shrink-0 text-muted" />
          <span
            className="shrink-0 text-xs font-medium text-muted-foreground"
            data-testid={`tool-name-${part.toolCallId}`}
          >
            {displayName}
          </span>
          {sentence !== null ? (
            <Sentence
              sentence={sentence}
              testId={`tool-sentence-${part.toolCallId}`}
            />
          ) : (
            <span className="min-w-0 flex-1 truncate text-xs">
              {running ? "running" : isError ? "failed" : "completed"}
            </span>
          )}
          {running ? (
            <span
              className="text-muted-foreground shrink-0 text-[11px] tabular-nums"
              data-testid={`tool-status-${part.toolCallId}`}
              data-reduced-motion={reducedMotion ? "true" : "false"}
            >
              <ShimmerLabel
                active={!reducedMotion}
                className="inline-block leading-none"
              >
                running{elapsed === null ? "" : ` · ${elapsed}`}
              </ShimmerLabel>
            </span>
          ) : (
            <>
              {latency !== undefined ? (
                <span
                  className="text-muted-foreground shrink-0 text-[11px] tabular-nums"
                  data-testid={`tool-duration-${part.toolCallId}`}
                >
                  {formatLatency(latency)}
                </span>
              ) : null}
              {isError ? (
                <span
                  className="text-dangertext shrink-0 rounded-[6px] bg-danger/10 px-1.5 py-0.5 text-[11px] leading-4"
                  data-testid={`tool-error-${part.toolCallId}`}
                >
                  failed
                </span>
              ) : null}
            </>
          )}
        </CollapsibleTrigger>
        <CollapsibleContent>
          <div
            className="border-border/60 border-s ps-3"
            data-testid={`tool-body-${part.toolCallId}`}
          >
            <RawToggleRow
              raw={raw}
              onToggle={(): void => setRaw((r: boolean): boolean => !r)}
              argsText={part.argsText}
              resultText={resultText}
              isError={isError}
              testidPrefix={`tool-raw-${part.toolCallId}`}
            />
            {raw ? null : body}
          </div>
        </CollapsibleContent>
      </CollapsibleRoot>
    </div>
  );
}

/** The raw view's toggle row: the verbatim panes render beneath it while
 * active, replacing the formatted body (spec "Raw toggle shows delivered
 * text"). */
function RawToggleRow({
  raw,
  onToggle,
  argsText,
  resultText,
  isError,
  testidPrefix,
}: {
  raw: boolean;
  onToggle: () => void;
  argsText: string;
  resultText: string;
  isError: boolean;
  testidPrefix: string;
}): ReactElement {
  return (
    <div>
      <div className="flex items-center justify-end px-3 pt-1">
        <button
          type="button"
          aria-pressed={raw}
          className="text-muted-foreground hover:bg-foreground/[0.08] hover:text-foreground rounded px-1.5 py-0.5 text-[10px]"
          data-testid={`${testidPrefix}-toggle`}
          onClick={onToggle}
        >
          {raw ? "Formatted" : "Raw"}
        </button>
      </div>
      {raw ? (
        <div
          className="space-y-2 px-3 pb-2"
          data-testid={`${testidPrefix}-panes`}
        >
          {argsText !== "" ? (
            <ClampedPane
              label="Arguments"
              text={argsText}
              isError={false}
              testidPrefix={`${testidPrefix}-args`}
            />
          ) : null}
          {resultText !== "" ? (
            <ClampedPane
              label={isError ? "Error" : "Result"}
              text={resultText}
              isError={isError}
              testidPrefix={`${testidPrefix}-result`}
            />
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

/** One labeled, clamped pane of the raw view. */
function ClampedPane({
  label,
  text,
  isError,
  testidPrefix,
}: {
  label: string;
  text: string;
  isError: boolean;
  testidPrefix: string;
}): ReactElement {
  return (
    <div>
      <p
        className={`text-xs ${isError ? "text-dangertext" : "text-muted-foreground"}`}
      >
        {label}
      </p>
      <ClampedText text={text} testidPrefix={testidPrefix} />
    </div>
  );
}
