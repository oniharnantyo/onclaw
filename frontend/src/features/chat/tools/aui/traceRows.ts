/**
 * Trace-row derivation for the heavy-turn fold (task 4.5, design D6): turns
 * a tool-call part into the vendored `ToolTimeline`'s step shape — a short
 * verb, a target chip, and a lucide glyph (the element wants a LucideIcon
 * component, so the tool vocabulary maps onto the closest lucide glyph here)
 * — plus the trailing per-file change statistics from completed edit/write
 * calls. Pure functions; the counts come only from the delivered payloads.
 */
import type { LucideIcon } from "lucide-react";
import {
  FilePlusIcon,
  FilePenIcon,
  FileTextIcon,
  FileXIcon,
  FolderTreeIcon,
  GlobeIcon,
  ListTodoIcon,
  LinkIcon,
  PlugIcon,
  SearchIcon,
  SquareTerminalIcon,
  TimerIcon,
  UsersIcon,
} from "lucide-react";

import type { DiffLine } from "@/components/assistant-ui/elements/code-diff";
import type { TimelineStat } from "@/components/assistant-ui/elements/tool-timeline";

import { isString, parseArgs, str, type Json } from "../parse";
import { countLines } from "../toolDisplay";
import { buildDiffLines, countDiffLines, type DiffCounts } from "./diffRows";

/** The short verb a trace row leads with. Unknown tools keep the raw name —
 * the fold invents nothing a card would not say. */
export function toolVerb(toolName: string): string {
  const VERBS = {
    read_file: "Read",
    write_file: "Write",
    edit_file: "Edit",
    delete_file: "Delete",
    ls: "List",
    glob: "Find",
    grep: "Search",
    execute: "Run",
    shell: "Run",
    web_search: "Search",
    web_fetch: "Fetch",
    http_get: "Fetch",
    current_time: "Time",
    browser: "Browse",
    todo_write: "Plan",
    todo_read: "Plan",
    task_create: "Task",
    task_get: "Task",
    task_update: "Task",
    task_list: "Task",
    TaskCreate: "Task",
    TaskGet: "Task",
    TaskUpdate: "Task",
    TaskList: "Task",
    task: "Task",
    agent: "Agent",
    task_output: "Output",
    task_stop: "Stop",
  } satisfies Record<string, string>;

  return VERBS[toolName] ?? toolName;
}

/** The lucide glyph a trace row shows for one tool. */
export function toolIcon(toolName: string): LucideIcon {
  const ICONS = {
    read_file: FileTextIcon,
    write_file: FilePlusIcon,
    edit_file: FilePenIcon,
    delete_file: FileXIcon,
    ls: FolderTreeIcon,
    glob: FolderTreeIcon,
    grep: SearchIcon,
    execute: SquareTerminalIcon,
    shell: SquareTerminalIcon,
    web_search: GlobeIcon,
    web_fetch: LinkIcon,
    http_get: LinkIcon,
    current_time: TimerIcon,
    browser: GlobeIcon,
    todo_write: ListTodoIcon,
    todo_read: ListTodoIcon,
    task_create: ListTodoIcon,
    task_get: ListTodoIcon,
    task_update: ListTodoIcon,
    task_list: ListTodoIcon,
    TaskCreate: ListTodoIcon,
    TaskGet: ListTodoIcon,
    TaskUpdate: ListTodoIcon,
    TaskList: ListTodoIcon,
    task: UsersIcon,
    agent: UsersIcon,
    task_output: ListTodoIcon,
    task_stop: FileXIcon,
  } satisfies Record<string, LucideIcon>;

  return ICONS[toolName] ?? PlugIcon;
}

/** The target chip a trace row names: the file, command head, pattern, or
 * description the call addresses — empty string when the args carry none. */
export function toolChip(toolName: string, argsText: string): string {
  const args = parseArgs(argsText);

  if (args === null) {
    // A mapped task record rides a plain-text payload (the description),
    // not a JSON args object — its first line is the chip.
    if (toolName === "task") return firstLineChip(argsText);

    return "";
  }

  switch (toolName) {
    case "read_file":
    case "write_file":
    case "edit_file":
    case "delete_file":
      return baseName(str(args, "file_path"));
    case "ls":
      return baseName(str(args, "path"));
    case "glob":
      return str(args, "pattern");
    case "grep":
      return str(args, "pattern");
    case "execute":
    case "shell":
      return firstToken(str(args, "command"));
    case "web_search":
      return str(args, "query");
    case "web_fetch":
    case "http_get":
      return str(args, "url");
    case "browser":
      return str(args, "action");
    case "todo_write":
    case "todo_read":
      return "plan";
    case "task_create":
    case "TaskCreate":
      return str(args, "subject") || "task";
    case "task_get":
    case "TaskGet":
    case "task_update":
    case "TaskUpdate":
      return str(args, "taskId") ? `#${str(args, "taskId")}` : "task";
    case "task_list":
    case "TaskList":
      return "tasks";
    case "task":
      return str(args, "description") || str(args, "task_id");
    case "agent":
      return (
        str(args, "description") ||
        str(args, "prompt") ||
        str(args, "subagent_type")
      );
    case "task_output":
    case "task_stop":
      return str(args, "task_id") ? `#${str(args, "task_id")}` : "";
    default:
      return "";
  }
}

/** The last path segment (the chip names the file, not its directory). */
function baseName(path: string): string {
  const cut = path.lastIndexOf("/");

  return cut === -1 ? path : path.slice(cut + 1);
}

/** The first non-empty line of a plain-text payload, chip-sized (the same
 * 80/77 cut the backend's task summary applies). */
function firstLineChip(text: string): string {
  for (const line of text.split("\n")) {
    const trimmed = line.trim();

    if (trimmed === "") continue;

    return trimmed.length > 80 ? trimmed.slice(0, 77) + "..." : trimmed;
  }

  return "";
}

/** The command's first token (the program run). */
function firstToken(command: string): string {
  return command.trim().split(/\s+/)[0] ?? "";
}

/** The fields the fold reads off an edit call's arguments (ToolArgs's index
 * signature feeds these optional Json fields without another alias). */
interface EditFields {
  file_path?: Json;
  old_string?: Json;
  new_string?: Json;
  content?: Json;
}

/** One file's accumulated change statistics. */
interface FileStat {
  added: number;
  removed: number;
}

/** One call record the stats derive from, in transcript order. */
export interface StatCall {
  toolName: string;
  argsText: string;
  completed: boolean;
  failed: boolean;
}

/** The trailing stats row of a revealed fold: the distinct files the run's
 * completed edit/write calls changed, with per-file added/removed counts.
 * Files whose payload cannot be interpreted still count by name, without
 * invented numbers. */
export function changeStats(calls: readonly StatCall[]): TimelineStat[] {
  const byFile = new Map<string, FileStat>();

  for (const call of calls) {
    if (call.toolName !== "edit_file" && call.toolName !== "write_file") {
      continue;
    }

    if (!call.completed || call.failed) continue;

    const fields: EditFields | null = parseArgs(call.argsText);

    if (fields === null) continue;

    const path = isString(fields.file_path) ? fields.file_path : "";

    if (path === "") continue;

    const stat = byFile.get(path) ?? { added: 0, removed: 0 };

    if (call.toolName === "edit_file") {
      const counted = countEdit(fields);

      stat.added += counted.additions;
      stat.removed += counted.deletions;
    } else if (isString(fields.content)) {
      stat.added += countLines(fields.content);
    }

    byFile.set(path, stat);
  }

  const stats: TimelineStat[] = [];

  for (const [file, stat] of byFile) {
    stats.push({
      file,
      added: stat.added > 0 ? stat.added : undefined,
      removed: stat.removed > 0 ? stat.removed : undefined,
    });
  }

  return stats;
}

/** The added/removed counts of one edit's arguments, from the same jsdiff
 * pass the diff body renders; an uninterpretable payload counts zero. */
function countEdit(fields: EditFields): DiffCounts {
  if (!isString(fields.old_string) || !isString(fields.new_string)) {
    return { additions: 0, deletions: 0 };
  }

  const lines: readonly DiffLine[] = buildDiffLines(
    fields.old_string,
    fields.new_string
  );

  return countDiffLines(lines);
}
