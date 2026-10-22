/**
 * ExecuteTool (task 4.2, design D7): the shell body — the command and its
 * output as the vendored `TerminalBlock`, bounded by eino's trailing
 * markers (markers.ts): a failed exit styled as a failure, a timeout and a
 * truncation each their own state, the no-output outcome said in words, the
 * call's duration when known, a copy control, and the cursor at the output's
 * tail while the call still runs. The failure note line itself is never
 * duplicated in the body once parsed into the status.
 */
import { useMemo } from "react";
import type { ReactElement } from "react";
import { TimerIcon } from "lucide-react";
import type { ToolCallMessagePartProps } from "@assistant-ui/react";

import { TerminalBlock } from "@/components/assistant-ui/elements/terminal-block";

import { parseArgs, str } from "../parse";
import { useReducedMotion } from "../useReducedMotion";
import { parseExecuteOutput, type ExecuteOutput } from "./markers";
import { useRevealCount } from "./reveal";
import { ToolCard } from "./ToolCallFrame";

/** The narrow type guard the body reads results through. */
function isTextResult(value: unknown): value is string {
  return typeof value === "string";
}

/** The timeout chip: the timeout is its own state, distinct from a failed
 * exit (the element has no timedOut prop, so the frame body renders the
 * status line itself). */
function TimeoutChip(): ReactElement {
  return (
    <span
      className="border-warn/40 text-warntext inline-flex items-center gap-1 rounded-[6px] border px-1.5 py-0.5 text-[11px] leading-4"
      data-testid="execute-timeout"
    >
      <TimerIcon aria-hidden="true" className="size-3" />
      timed out
    </span>
  );
}

function ExecuteBody({
  command,
  parsed,
  done,
  running,
  isError,
  durationMs,
  animate,
}: {
  command: string;
  parsed: ExecuteOutput;
  done: boolean;
  running: boolean;
  isError: boolean;
  durationMs?: number;
  animate: boolean;
}): ReactElement {
  // Staggered reveal replay (design D7): whole results land at once and the
  // element replays them line by line; reduced motion shows all lines.
  const lines = parsed.noOutput ? [] : parsed.lines;
  const revealed = useRevealCount(lines.length, done, animate);

  if (command === "") throw new Error("execute: no command in arguments");

  // A failed call whose payload carried no exit marker still reads as a
  // failure: its output rides the element's stderr lane (styled red) instead
  // of the success-tinted stdout lane.
  const failedNoMarker = isError && parsed.exitCode === undefined;

  return (
    <div className="space-y-1.5 px-3 pb-2" data-testid="execute-body">
      {parsed.timedOut && done ? <TimeoutChip /> : null}
      <TerminalBlock
        command={command}
        lines={failedNoMarker ? [] : lines}
        stderr={failedNoMarker ? lines : undefined}
        visibleCount={running ? lines.length : revealed}
        done={done}
        exitCode={parsed.exitCode}
        durationMs={durationMs}
        truncated={parsed.truncated}
        maxCollapsedLines={10}
      />
      {parsed.noOutput && done ? (
        <p
          className="text-muted-foreground text-[11px]"
          data-testid="execute-no-output"
        >
          The command executed successfully with no output.
        </p>
      ) : null}
    </div>
  );
}

export function ExecuteTool(props: ToolCallMessagePartProps): ReactElement {
  const reducedMotion = useReducedMotion();

  const command = useMemo((): string => {
    const args = parseArgs(props.argsText);

    return args === null ? "" : str(args, "command");
  }, [props.argsText]);

  const parsed = useMemo(
    (): ExecuteOutput =>
      parseExecuteOutput(isTextResult(props.result) ? props.result : ""),
    [props.result]
  );

  const durationMs = useMemo((): number | undefined => {
    const timing = props.timing;

    if (timing === undefined || timing.completedAt === undefined) {
      return undefined;
    }

    return Math.max(0, timing.completedAt - timing.startedAt);
  }, [props.timing]);

  return (
    <ToolCard part={props}>
      <ExecuteBody
        command={command}
        parsed={parsed}
        done={props.status.type !== "running"}
        running={props.status.type === "running"}
        isError={props.isError === true}
        durationMs={durationMs}
        animate={!reducedMotion}
      />
    </ToolCard>
  );
}
