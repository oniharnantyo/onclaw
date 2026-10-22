/**
 * The plan tools' renderers (task 4.2, design D6): `todo_write` /
 * `todo_read` render the plan items as the vendored `AgentPlan` snapshot —
 * steps with their pending, in-progress, and done states — with the failed
 * items listed beneath the element (the element's checklist has no failed
 * state of its own). Items come from the result echo, falling back to the
 * arguments a still-running call was writing.
 */
import { useMemo } from "react";
import type { ReactElement } from "react";
import { X } from "lucide-react";
import type { ToolCallMessagePartProps } from "@assistant-ui/react";

import { AgentPlan } from "@/components/assistant-ui/elements/agent-plan";

import {
  isObject,
  isString,
  parseArgs,
  parseJSONResult,
  type Json,
} from "../parse";
import { ClampedText } from "../primitives/ClampedText";
import { ToolCard } from "./ToolCallFrame";

/** The narrow type guard the bodies read results through. */
function isTextResult(value: unknown): value is string {
  return typeof value === "string";
}

/** One plan item as persisted (the todo tool's `{key,text,status,reason}`). */
export interface TodoItem {
  key: string;
  text: string;
  status: string;
  reason?: string;
}

/** Validates one list of wire items; null on any entry that breaks the
 * shape (the caller then renders the raw text). */
function parseItemList(list: Json[]): TodoItem[] | null {
  const items: TodoItem[] = [];

  for (const entry of list) {
    if (!isObject(entry)) return null;

    if (
      !isString(entry.key) ||
      !isString(entry.text) ||
      !isString(entry.status)
    ) {
      return null;
    }

    items.push({
      key: entry.key,
      text: entry.text,
      status: entry.status,
      reason: isString(entry.reason) ? entry.reason : undefined,
    });
  }

  return items;
}

/** Parses the `{items:[...]}` echo or arguments; null when the payload is
 * not that shape so the raw text shows. */
function parseTodoItems(raw: string): TodoItem[] | null {
  const parsed = parseJSONResult(raw);

  if (!isObject(parsed)) return null;

  const list = parsed.items;

  return Array.isArray(list) ? parseItemList(list) : null;
}

/** The active step the snapshot points at: the first in-progress item, or
 * past the end once every item settled (the element reads all-done). */
function activeIndexOf(items: TodoItem[]): number {
  const active = items.findIndex(
    (item: TodoItem): boolean =>
      item.status === "active" || item.status === "in_progress"
  );

  return active === -1 ? items.length : active;
}

export function TodoWriteTool(props: ToolCallMessagePartProps): ReactElement {
  return <TodoToolBody part={props} />;
}

export function TodoReadTool(props: ToolCallMessagePartProps): ReactElement {
  return <TodoToolBody part={props} />;
}

function TodoToolBody({
  part,
}: {
  part: ToolCallMessagePartProps;
}): ReactElement {
  const resultText = isTextResult(part.result) ? part.result : "";
  const isError = part.isError === true;

  const items = useMemo((): TodoItem[] | null => {
    const fromResult = parseTodoItems(resultText);

    if (fromResult !== null) return fromResult;

    const args = parseArgs(part.argsText);
    const list = args === null ? undefined : args.items;

    return Array.isArray(list) ? parseItemList(list) : null;
  }, [part.argsText, resultText]);

  return (
    <ToolCard part={part}>
      <TodoSnapshot items={items} isError={isError} resultText={resultText} />
    </ToolCard>
  );
}

function TodoSnapshot({
  items,
  isError,
  resultText,
}: {
  items: TodoItem[] | null;
  isError: boolean;
  resultText: string;
}): ReactElement {
  if (items === null) {
    return (
      <div className="px-3 pb-2" data-testid="todo-plain">
        <ClampedText
          text={resultText !== "" ? resultText : "no plan items in the payload"}
          testidPrefix="todo-plain"
        />
      </div>
    );
  }

  const failed = items.filter(
    (item: TodoItem): boolean => item.status === "failed"
  );

  return (
    <div className="px-3 pb-2" data-testid="todo-body">
      <AgentPlan
        steps={items.map((item: TodoItem): string => item.text)}
        activeIndex={activeIndexOf(items)}
        title="Plan"
      />
      {failed.length > 0 ? (
        <ul className="mt-2 space-y-0.5" data-testid="todo-failed">
          {failed.map((item: TodoItem): ReactElement => (
            <li
              key={item.key}
              className="text-dangertext flex items-center gap-2 text-xs"
              data-testid={`todo-item-${item.key}`}
              title={item.reason}
            >
              <X aria-hidden="true" className="size-3 shrink-0" />
              {item.text}
            </li>
          ))}
        </ul>
      ) : null}
      {isError && resultText !== "" ? (
        <ClampedText text={resultText} testidPrefix="todo-error" />
      ) : null}
    </div>
  );
}
