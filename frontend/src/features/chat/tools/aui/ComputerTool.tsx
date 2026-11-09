/**
 * ComputerTool (add-computer-use task 6.2, design D8): the computer-use
 * trajectory card. ChatToolGroup routes every maximal run of consecutive
 * `mcp__computer-use__*` parts here as ONE card (ComputerTrajectory); an
 * isolated computer-use call renders the same card with its single step
 * (ComputerTool, the registry's renderer for the prefix). The desktop frame
 * (the vendored element with the backdrop through the bound
 * `computerScreenshot` read — name-only, the backend jail does the path
 * work, spec chat-tool-cards "Computer screenshot bound read" — and the
 * animated cursor trail) renders ONLY when the run carries an observation:
 * no image, no frame (user feedback — an empty chrome frame per call was
 * noise; the steps footer renders alone). An animated cursor marks each
 * completed coordinate action with a fading trail (pixel→percent conversion
 * from the backdrop's reported dimensions), and a step footer lists every
 * call in order — non-coordinate calls (keyboard, element-value, clipboard)
 * as footer steps without cursor markers, the running step highlighted live,
 * `type`/`set_value`-type text masked to a length-only
 * placeholder (the args ride ToolActivity.Arguments verbatim, so masking
 * happens at render), and approval-gated calls carrying their approval state
 * (a chip on the step; the actionable gate card beneath). The finished
 * trajectory persists on the transcript.
 */
import { useEffect, useMemo, useState } from "react";
import type { ReactElement } from "react";
import { ImageOff } from "lucide-react";
import { MessagePrimitive } from "@assistant-ui/react";
import type {
  ToolCallMessagePart,
  ToolCallMessagePartProps,
} from "@assistant-ui/react";

import { client, type BrowserScreenshotContent } from "../../../../lib/client";
import { cn } from "../../../../lib/cn";
import { useChatStore } from "../../store";
import type { ChatStore } from "../../types";

import {
  isNumber,
  isObject,
  isString,
  parseArgs,
  parseToolResult,
  str,
  type ToolArgs,
} from "../parse";
import { useReducedMotion } from "../useReducedMotion";
import { ApprovalGate } from "./ApprovalGate";
import {
  ComputerUse,
  type ComputerStep,
} from "@/components/assistant-ui/elements/computer-use";
import { ShimmerLabel } from "@/components/assistant-ui/elements/surfaces";

/** The MCP namespace prefix the Computer Use system server surfaces its
 * tools under (add-computer-use design D4); the tool NAME carries the action
 * after it, with a numeric collision suffix the naming pass may add. */
export const COMPUTER_USE_PREFIX = "mcp__computer-use__";

/** The exact name shape the observation lane writes (design D4 — the
 * browseruse lane's same jail). */
const SCREENSHOT_NAME = /^\d+\.png$/;

/** The marker text the observation lane emits
 * (`[screenshot: sessions/<sid>/computer/<name>.png]`). */
const MARKER_TEXT = /\[screenshot:\s*([^\]]+)\]/;

/** The always-gated computer-use actions (design D7): an approval payload's
 * first word names one. Mirrors ApprovalGate's set — the payload vocabulary
 * is the backend's (`<tool> <target>`), so both sides key on the same words. */
const COMPUTER_USE_GATED_ACTIONS = new Set([
  "kill_app",
  "write_clipboard",
  "type",
  "set_value",
]);

/** The action one computer-use tool name carries, with the naming pass's
 * numeric collision suffix trimmed ("type_2" → "type"). */
export function computerUseActionOf(toolName: string): string {
  const segment = toolName.slice(COMPUTER_USE_PREFIX.length);
  const underscore = segment.lastIndexOf("_");

  if (underscore > 0 && /^\d+$/.test(segment.slice(underscore + 1))) {
    return segment.slice(0, underscore);
  }

  return segment;
}

/** Whether one tool-call part belongs to the Computer Use server. */
export function isComputerUseTool(toolName: string): boolean {
  return toolName.startsWith(COMPUTER_USE_PREFIX);
}

/** The computer-use action one approval payload names, when it names one:
 * the first word of the synthesized `<tool> <target>` payload, or null for
 * anything else (a shell command rides the same "execute" tool). */
export function computerUseApprovalActionOf(command: string): string | null {
  const separator = command.indexOf(" ");

  if (separator <= 0) return null;

  const action = command.slice(0, separator);

  return COMPUTER_USE_GATED_ACTIONS.has(action) ? action : null;
}

/** The narrow type guard the card reads results through (the part's result
 * is delivered as the persisted string). */
function isTextResult(value: unknown): value is string {
  return typeof value === "string";
}

/** The masked, length-only placeholder one masked argument renders as
 * (design D8: never the verbatim text). */
function maskedText(text: string): string {
  if (text.length === 0) return "••••";

  return `•••• (${text.length} chars)`;
}

/** The observation fields one result carries (observe.go's
 * ObservationResult): the written screenshot's marker/path plus the pixel
 * dimensions. Null when the result carries no capture. */
interface Observation {
  name: string;
  width?: number;
  height?: number;
}

/** Parses one result text's observation fields: the marker
 * (`[screenshot: <path>]`) is the persisted, relocation-safe form and the
 * screenshot path the fallback — either way only the file NAME survives
 * (never a path; the bound read is name-addressed). A name outside the
 * `^\d+\.png$` shape is dropped (the bound read would refuse it). */
function observationOf(resultText: string): Observation | null {
  const parsed = parseToolResult(resultText);

  let source = isObject(parsed) && isString(parsed.marker) ? parsed.marker : "";

  if (source === "" && isObject(parsed) && isString(parsed.screenshotPath)) {
    source = parsed.screenshotPath;
  }

  if (source === "") {
    source = MARKER_TEXT.exec(resultText)?.[1] ?? "";
  }

  // A marker text yields its inner path; a bare path passes through.
  const path = MARKER_TEXT.exec(source)?.[1] ?? source;

  const name = path.split("/").pop() ?? "";

  if (!SCREENSHOT_NAME.test(name)) return null;

  const width =
    isObject(parsed) && isNumber(parsed.width) ? parsed.width : undefined;

  const height =
    isObject(parsed) && isNumber(parsed.height) ? parsed.height : undefined;

  return { name, width, height };
}

/** The coordinate pair one step marks on the backdrop, in pixels from its
 * capture: click-family clicks, drags (from-point), and scrolls. Element
 * token actions carry no coordinates (footer-only steps, design D8). */
function stepCoords(
  action: string,
  args: ToolArgs | null
): { x: number; y: number } | null {
  if (args === null) return null;

  if (
    action === "click" ||
    action === "double_click" ||
    action === "right_click" ||
    action === "scroll"
  ) {
    const x = args.x;
    const y = args.y;

    return isNumber(x) && isNumber(y) ? { x, y } : null;
  }

  if (action === "drag") {
    const x = args.fromX;
    const y = args.fromY;

    return isNumber(x) && isNumber(y) ? { x, y } : null;
  }

  return null;
}

/** The target chip one footer step names: the element token, key, direction,
 * app, or masked text the call addresses — never a masked argument's
 * verbatim text. */
function stepChip(action: string, args: ToolArgs | null): string {
  if (args === null) return "";

  const element = str(args, "element");

  switch (action) {
    case "observe_window":
      return str(args, "app") || str(args, "title") || "window";
    case "observe_desktop":
      return "desktop";
    case "list_apps":
      return "apps";
    case "list_windows":
      return "windows";
    case "click":
    case "double_click":
    case "right_click":
      return element;
    case "drag":
      return "drag";
    case "scroll":
      return str(args, "direction");
    case "key":
      return str(args, "key");
    case "hotkey":
      return str(args, "keys");
    case "type":
      // The typed text masks to a length-only placeholder (design D8); the
      // field's token may ride beside it.
      return element === ""
        ? maskedText(str(args, "text"))
        : `${element} ${maskedText(str(args, "text"))}`;
    case "set_value":
      // The set value masks like typed text (the element token names the
      // field; the value never renders).
      return element === ""
        ? maskedText(str(args, "value"))
        : `${element} ${maskedText(str(args, "value"))}`;
    case "read_clipboard":
      return "clipboard";
    case "write_clipboard":
      // The clipboard text masks like typed text (the write is approval-
      // gated; the chip never carries the content).
      return maskedText(str(args, "text"));
    case "verify":
      return str(args, "expectation");
    case "zoom":
      return "zoom";
    case "wait":
      return isNumber(args.ms) ? `${args.ms}ms` : "settle";
    case "stop":
      return "stop";
    case "kill_app":
      return str(args, "app") || str(args, "target");
    default:
      return "";
  }
}

/** One trajectory step: the call's derived view, in transcript order. */
interface TrajectoryStep {
  index: number;
  callId: string;
  action: string;
  args: ToolArgs | null;
  isError: boolean;
  completed: boolean;
  observation: Observation | null;
  /** The approval state riding the matching approval part; undefined when
   * the call is not approval-gated. */
  approval: ToolCallMessagePart["approval"];
}

/** One approval part joined into the run (the "execute"-named part the
 * approval gate mints, its payload naming tool + target). */
interface RunApproval {
  index: number;
  payload: string;
  approval: ToolCallMessagePart["approval"];
}

/** The narrowed part shape the collector reads: the group's indices address
 * tool-call parts (the same narrowed read ChatToolGroup's collectCalls
 * makes). */
type CallPart = ToolCallMessagePart;

/** The run view one collect pass derives: the steps in transcript order and
 * the approval parts that joined them. */
interface Trajectory {
  steps: TrajectoryStep[];
  approvals: RunApproval[];
}

/** Collects the run's steps and joined approvals from the segment's parts
 * (indices into the message's content). Approval parts join the run by
 * payload first word; their state rides the matching step's chip. */
function collectTrajectory(
  parts: readonly { type: string }[],
  indices: readonly number[]
): Trajectory {
  const steps: TrajectoryStep[] = [];
  const approvals: RunApproval[] = [];

  for (const index of indices) {
    // SAFETY: the group's indices address tool-call parts only (the
    // group-tool path coalesces exactly those); the narrowed read checks the
    // type before trusting the fields.
    const part = parts[index] as CallPart | undefined;

    if (part?.type !== "tool-call") continue;

    if (part.toolName === "execute") {
      if (computerUseApprovalActionOf(part.argsText) !== null) {
        approvals.push({
          index,
          payload: part.argsText,
          approval: part.approval,
        });
      }

      continue;
    }

    if (!isComputerUseTool(part.toolName)) continue;

    const resultText = isTextResult(part.result) ? part.result : "";

    steps.push({
      index,
      callId: part.toolCallId,
      action: computerUseActionOf(part.toolName),
      args: parseArgs(part.argsText),
      isError: part.isError === true,
      completed: part.result !== undefined,
      observation: observationOf(resultText),
      // An approval riding the part itself wins; the join below fills only
      // steps the gate parked separately.
      approval: part.approval,
    });
  }

  // Each approval joins the first unmatched step naming the same action —
  // the gated call and its park are one event pair, and a run gates a given
  // action at most once in practice.
  for (const approval of approvals) {
    const action = computerUseApprovalActionOf(approval.payload);

    const step = steps.find(
      (candidate: TrajectoryStep): boolean =>
        candidate.approval === undefined && candidate.action === action
    );

    if (step !== undefined) step.approval = approval.approval;
  }

  return { steps, approvals };
}

/** The approval state chip one step renders. */
interface ApprovalChip {
  label: string;
  tone: "pending" | "ok" | "bad" | "muted";
}

/** Derives one approval part's state chip: the state word and its tone. */
function approvalChipOf(
  approval: NonNullable<ToolCallMessagePart["approval"]>
): ApprovalChip {
  if (approval.resolution !== undefined) {
    const label = approval.resolution === "cancelled" ? "cancelled" : "expired";

    const chip: ApprovalChip = { label, tone: "muted" };

    return chip;
  }

  if (approval.approved === true) {
    const chip: ApprovalChip = { label: "approved", tone: "ok" };

    return chip;
  }

  if (approval.approved === false) {
    const chip: ApprovalChip = { label: "denied", tone: "bad" };

    return chip;
  }

  const chip: ApprovalChip = { label: "approval required", tone: "pending" };

  return chip;
}

/** The chip classes one approval tone renders with (both themes ride the
 * buddies token vocabulary the other cards use). */
const APPROVAL_TONE_CLASSES: Record<
  "pending" | "ok" | "bad" | "muted",
  string
> = {
  pending: "border-danger/35 text-dangertext bg-danger/10",
  ok: "border-line text-successtext",
  bad: "border-danger/35 text-dangertext",
  muted: "border-line text-muted-foreground",
};

/** The backdrop: the bound read returns base64 PNG (the browser read's same
 * shape); the bytes become a Blob object URL sized by the observation's
 * reported dimensions so the transcript does not shift on arrival and the
 * cursor percentages land exactly on the image. A refused or missing read
 * degrades to an unavailable state. */
function ScreenshotBackdrop({
  observation,
}: {
  observation: Observation;
}): ReactElement {
  const sessionID = useChatStore((s: ChatStore) => s.activeSessionID);

  const valid = isString(sessionID) && sessionID !== "";

  const [url, setURL] = useState<string | null>(null);
  const [failed, setFailed] = useState(!valid);

  useEffect((): void | (() => void) => {
    if (!valid) return undefined;

    let mounted = true;
    let created: string | null = null;

    client.chat
      .computerScreenshot(sessionID, observation.name)
      .then((content: BrowserScreenshotContent): void => {
        if (!mounted || !isTextResult(content?.data)) {
          if (mounted) setFailed(true);

          return;
        }

        const binary = window.atob(content.data);
        const bytes = new Uint8Array(binary.length);

        for (let i = 0; i < binary.length; i += 1) {
          bytes[i] = binary.charCodeAt(i);
        }

        created = URL.createObjectURL(new Blob([bytes], { type: "image/png" }));
        setURL(created);
      })
      .catch((): void => {
        if (mounted) setFailed(true);
      });

    return (): void => {
      mounted = false;

      if (created) URL.revokeObjectURL(created);
    };
  }, [valid, sessionID, observation.name]);

  if (failed || !url) {
    return (
      <div
        className="text-muted-foreground flex min-h-[8.5rem] items-center justify-center gap-1.5 text-[12px]"
        data-testid="computer-unavailable"
      >
        <ImageOff aria-hidden="true" className="size-3.5" />
        Screenshot unavailable
      </div>
    );
  }

  return (
    <img
      src={url}
      alt="Computer observation"
      width={observation.width}
      height={observation.height}
      className="block h-auto w-full"
      data-testid="computer-backdrop-image"
    />
  );
}

/** The approval-slot renderer: the joined approval parts render through
 * PartByIndex so the runtime enriches them with `respondToApproval`; the
 * gate handles every state (pending controls, decided receipts). Approval
 * parts only ever reach this slot, so the same component stands in for the
 * fallback key (it renders nothing for a part without an approval). */
function ComputerApprovalStep(props: ToolCallMessagePartProps): ReactElement {
  return <ApprovalGate part={props} />;
}

/** The components map the approval slot renders through: only this slot's
 * indices pass here, and every one of them is an approval part. */
const APPROVAL_COMPONENTS = {
  tools: {
    by_name: { execute: ComputerApprovalStep },
    Fallback: ComputerApprovalStep,
  },
};

/** One footer step row: the action verb, the target chip, the failure tint,
 * the live highlight on the running step, and the approval state chip. */
function TrajectoryStepRow({
  step,
  running,
}: {
  step: TrajectoryStep;
  running: boolean;
}): ReactElement {
  const reducedMotion = useReducedMotion();

  const chip = stepChip(step.action, step.args);

  const approvalChip =
    step.approval !== undefined ? approvalChipOf(step.approval) : null;

  return (
    <div
      className="text-muted-foreground flex items-center gap-2 px-1 py-0.5 text-left text-[13px]"
      data-testid="computer-step"
      data-step={step.action}
      data-running={running ? "true" : "false"}
      data-failed={step.isError ? "true" : "false"}
    >
      <ShimmerLabel
        active={running && !reducedMotion}
        className="relative inline-block shrink-0 leading-none"
      >
        <span
          className={cn(
            "font-mono text-[11px]",
            running ? "text-foreground" : undefined
          )}
        >
          {step.action}
        </span>
      </ShimmerLabel>
      {chip !== "" ? (
        <span
          className="bg-foreground/[0.06] text-foreground/70 rounded-md px-1.5 py-0.5 font-mono text-[11px]"
          data-testid="computer-step-chip"
        >
          {chip}
        </span>
      ) : null}
      {step.isError ? (
        <span
          className="text-dangertext shrink-0 rounded-[6px] bg-danger/10 px-1.5 py-0.5 text-[11px] leading-4"
          data-testid="computer-step-failed"
        >
          failed
        </span>
      ) : null}
      {approvalChip !== null ? (
        <span
          className={cn(
            "shrink-0 rounded-[6px] border px-1.5 py-0.5 text-[10px] leading-4",
            APPROVAL_TONE_CLASSES[approvalChip.tone]
          )}
          data-testid="computer-step-approval"
          data-approval-state={approvalChip.label}
        >
          {approvalChip.label}
        </span>
      ) : null}
    </div>
  );
}

/** The trajectory card: the vendored element's desktop frame (title, the
 * backdrop slot with the cursor trail) plus the run's full step footer and
 * the joined approval cards. */
function TrajectoryCard({
  steps,
  approvals,
  streaming,
}: {
  steps: TrajectoryStep[];
  approvals: RunApproval[];
  streaming: boolean;
}): ReactElement {
  // The backdrop is the latest observation's capture; its pixel dimensions
  // are what the cursor percentages convert from.
  const backdrop = useMemo((): Observation | null => {
    for (let i = steps.length - 1; i >= 0; i -= 1) {
      if (steps[i].observation !== null) return steps[i].observation;
    }

    return null;
  }, [steps]);

  // The title names the app the latest observation drove (the observe call's
  // app argument); a desktop observation keeps the generic title.
  const title = useMemo((): string => {
    for (let i = steps.length - 1; i >= 0; i -= 1) {
      const step = steps[i];

      if (step.observation === null) continue;

      const app = str(step.args, "app");

      return app === "" ? "Desktop" : app;
    }

    return "Desktop";
  }, [steps]);

  // The cursor steps: coordinate actions converted to percents of the
  // backdrop's reported dimensions. Without a backdrop (or pixel dims) the
  // markers cannot land — the trail stands down rather than guessing.
  const cursorSteps = useMemo((): ComputerStep[] => {
    if (backdrop === null) return [];

    const width = backdrop.width;
    const height = backdrop.height;

    if (!isNumber(width) || !isNumber(height)) return [];

    const markers: ComputerStep[] = [];

    for (const step of steps) {
      const coords = stepCoords(step.action, step.args);

      if (coords === null) continue;

      markers.push({
        id: step.callId,
        action: step.action,
        target: stepChip(step.action, step.args),
        x: Math.min(100, Math.max(0, (coords.x / width) * 100)),
        y: Math.min(100, Math.max(0, (coords.y / height) * 100)),
      });
    }

    return markers;
  }, [backdrop, steps]);

  // The running step: the last call still without its result (live), or
  // none once every call completed — the finished trajectory persists
  // without a highlight.
  const live =
    streaming || steps.some((step: TrajectoryStep): boolean => !step.completed);

  const runningIndex = useMemo((): number => {
    for (let i = steps.length - 1; i >= 0; i -= 1) {
      if (!steps[i].completed) return i;
    }

    return -1;
  }, [steps]);

  return (
    <div
      className="w-full"
      data-testid="computer-trajectory"
      data-live={live ? "true" : "false"}
    >
      {/* The desktop frame IS the observation (user feedback): it renders
       * only when the run carries a capture — a failed or not-yet-run call
       * shows the step rows alone, an empty chrome frame per call was
       * noise. */}
      {backdrop !== null ? (
        <ComputerUse
          title={title}
          steps={cursorSteps}
          activeIndex={Math.max(0, cursorSteps.length - 1)}
          className="max-w-none"
        >
          <ScreenshotBackdrop observation={backdrop} />
        </ComputerUse>
      ) : null}
      {steps.length > 0 ? (
        <div
          className="border-border/60 mt-2 flex flex-col border-l ps-3"
          data-testid="computer-steps"
        >
          {steps.map((step: TrajectoryStep, i: number): ReactElement => (
            <TrajectoryStepRow
              key={step.callId}
              step={step}
              running={live && i === runningIndex}
            />
          ))}
        </div>
      ) : null}
      {approvals.map((approval: RunApproval): ReactElement => (
        <div
          key={approval.index}
          className="mt-2"
          data-testid="computer-approval"
        >
          <MessagePrimitive.PartByIndex
            index={approval.index}
            components={APPROVAL_COMPONENTS}
          />
        </div>
      ))}
    </div>
  );
}

/** The run card ChatToolGroup routes a computer-use segment to: `indices`
 * address the message's parts (the segment's calls and joined approvals). */
export function ComputerTrajectory({
  parts,
  indices,
  streaming,
}: {
  parts: readonly { type: string }[];
  indices: readonly number[];
  streaming: boolean;
}): ReactElement {
  const { steps, approvals } = useMemo(
    (): Trajectory => collectTrajectory(parts, indices),
    [parts, indices]
  );

  // A segment holding only approvals (no computer-use call reached the
  // transcript) renders the approval cards alone — no trajectory chrome.
  if (steps.length === 0) {
    return (
      <div className="flex flex-col gap-2">
        {approvals.map((approval: RunApproval): ReactElement => (
          <div key={approval.index} data-testid="computer-approval">
            <MessagePrimitive.PartByIndex
              index={approval.index}
              components={APPROVAL_COMPONENTS}
            />
          </div>
        ))}
      </div>
    );
  }

  return (
    <TrajectoryCard steps={steps} approvals={approvals} streaming={streaming} />
  );
}

/** The single-call renderer (the registry's entry for any
 * `mcp__computer-use__*` name): the same trajectory card with the one call
 * as its only step. An approval riding the part itself renders the gate —
 * the tool card's detour. */
export function ComputerTool(props: ToolCallMessagePartProps): ReactElement {
  if (props.approval !== undefined) {
    return <ApprovalGate part={props} />;
  }

  return (
    <ComputerTrajectory
      parts={[props]}
      indices={[0]}
      streaming={props.status.type === "running"}
    />
  );
}
