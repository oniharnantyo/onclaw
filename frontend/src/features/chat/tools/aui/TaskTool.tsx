/**
 * TaskTool (task 4.2): the delegated subagent task rendered through the
 * vendored presentational `TaskCard` pieces — the description as the label,
 * the delegation's lifecycle inline (running / done / failed / cancelled via
 * `taskStateOf`), and the child's answer plus the materialized transcript
 * path in the card's result slot (the path opens the trajectory in the right
 * panel through the jailed reader, via FileChip). The card updates in place
 * when the backend re-reports the same task id — the runtime keys the part
 * by its id.
 */
import type { ReactElement } from "react";
import type { ToolCallMessagePartProps } from "@assistant-ui/react";

import { TaskCard } from "@/components/assistant-ui/elements/task-card";
import { taskStateOf } from "@/components/assistant-ui/utils/task";

import { parseArgs, str } from "../parse";
import { FileChip } from "../primitives/FileChip";
import { ClampedText } from "../primitives/ClampedText";
import { ToolCard } from "./ToolCallFrame";

/** The narrow type guard the body reads results through. */
function isTextResult(value: unknown): value is string {
  return typeof value === "string";
}

export function TaskTool(props: ToolCallMessagePartProps): ReactElement {
  const args = parseArgs(props.argsText);
  const resultText = isTextResult(props.result) ? props.result : "";
  const isError = props.isError === true;
  const state = taskStateOf(props.status, isError);

  const description =
    (args === null ? "" : str(args, "description")) || "Background task";

  const taskID = args === null ? "" : str(args, "task_id");
  const transcriptPath = args === null ? "" : str(args, "transcript_path");

  // The delegation args can be unreadable while the payload streams; the
  // fallback body carries the verbatim text instead of an empty card.
  if (args === null) {
    return (
      <ToolCard part={props}>
        <FallbackPane argsText={props.argsText} resultText={resultText} />
      </ToolCard>
    );
  }

  return (
    <TaskCard
      label={description}
      {...(taskID !== "" ? { meta: taskID } : {})}
      state={state}
      className="w-full"
      data-testid={`entry-task-${taskID}`}
      {...(resultText !== "" || transcriptPath !== ""
        ? {
            result: (
              <div className="space-y-1.5" data-testid="task-result">
                {transcriptPath !== "" ? (
                  <FileChip
                    path={transcriptPath}
                    testidPrefix="task-transcript"
                  />
                ) : null}
                {resultText !== "" ? (
                  <ClampedText text={resultText} testidPrefix="task-answer" />
                ) : null}
              </div>
            ),
          }
        : {})}
    />
  );
}

/** The verbatim pane an unparsable delegation payload degrades to. */
function FallbackPane({
  argsText,
  resultText,
}: {
  argsText: string;
  resultText: string;
}): ReactElement {
  return (
    <div className="space-y-2 px-3 pb-2" data-testid="task-plain">
      {argsText !== "" ? (
        <ClampedText text={argsText} testidPrefix="task-plain-args" />
      ) : null}
      {resultText !== "" ? (
        <ClampedText text={resultText} testidPrefix="task-plain-result" />
      ) : null}
    </div>
  );
}
