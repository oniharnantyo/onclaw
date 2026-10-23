/**
 * The tool-renderer map (task 2.4 → 4.2, design D6): the map of
 * tool name → renderer the toolkit registration is built from. Every tool
 * the app ships has a shaped renderer reusing tools/parse.ts,
 * tools/toolDisplay.ts and toolPaths.ts; the `fallback` entry covers MCP,
 * unknown names, and any name without its own entry — the vendored
 * `ToolFallback` shows the display name or raw name with pretty-printed
 * (when structured) arguments and result and mints no sentence. The export
 * name and shape are the contract with the transcript (wave 2A): keep both.
 * Own module (not index.ts) so the fold can import the map without a cycle.
 */
import type {
  ToolCallMessagePartComponent,
  ToolCallMessagePartProps,
} from "@assistant-ui/react";
import type { ReactElement } from "react";

import { ToolFallback } from "@/components/assistant-ui/elements/tool-fallback.aui";

import { BrowserTool } from "./BrowserTool";
import { prettyOrVerbatim } from "./fallbackBody";
import { ExecuteTool } from "./ExecuteTool";
import { EditFileTool, ReadFileTool, WriteFileTool } from "./FileTools";
import { CurrentTimeTool, DeleteFileTool, HTTPGetTool } from "./GenericTool";
import { GlobTool, GrepTool, LsTool } from "./SearchTools";
import { TaskTool } from "./TaskTool";
import {
  TaskCreateTool,
  TaskGetTool,
  TaskListTool,
  TaskUpdateTool,
  TodoReadTool,
  TodoWriteTool,
} from "./TodoTool";
import { WebFetchTool, WebSearchTool } from "./WebTools";

/** One tool's renderer: the assistant-ui per-tool component shape. */
export type ToolRenderer = ToolCallMessagePartComponent;

/** Tool name → renderer. Keyed by the raw tool name the backend reports
 * (tools/parse.ts); the `fallback` entry covers MCP and unknown names. */
export type ToolRendererMap = Record<string, ToolRenderer>;

/** The narrow type guard the fallback reads results through. */
function isTextResult(value: unknown): value is string {
  return typeof value === "string";
}

/** The fallback entry (spec "Honest fallback"): the vendored `ToolFallback`
 * with the delivered strings pretty-printed when they are structured JSON
 * and verbatim otherwise — the element shows its payload verbatim, and the
 * mapping delivers args/results as raw strings. */
function FallbackTool(props: ToolCallMessagePartProps): ReactElement {
  return (
    <ToolFallback
      {...props}
      argsText={prettyOrVerbatim(props.argsText)}
      result={
        isTextResult(props.result)
          ? prettyOrVerbatim(props.result)
          : props.result
      }
    />
  );
}

export const toolRenderers = {
  execute: ExecuteTool,
  // `shell` is the backend's alias of `execute` (the same jailed shell) —
  // the same body renders both.
  shell: ExecuteTool,
  edit_file: EditFileTool,
  write_file: WriteFileTool,
  read_file: ReadFileTool,
  grep: GrepTool,
  glob: GlobTool,
  ls: LsTool,
  web_search: WebSearchTool,
  web_fetch: WebFetchTool,
  browser: BrowserTool,
  todo_write: TodoWriteTool,
  todo_read: TodoReadTool,
  task_create: TaskCreateTool,
  task_get: TaskGetTool,
  task_update: TaskUpdateTool,
  task_list: TaskListTool,
  TaskCreate: TaskCreateTool,
  TaskGet: TaskGetTool,
  TaskUpdate: TaskUpdateTool,
  TaskList: TaskListTool,
  task: TaskTool,
  agent: TaskTool,
  task_output: TaskTool,
  task_stop: TaskTool,
  http_get: HTTPGetTool,
  current_time: CurrentTimeTool,
  delete_file: DeleteFileTool,
  fallback: FallbackTool,
} satisfies ToolRendererMap;
