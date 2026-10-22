/**
 * Entry → assistant-ui message mapping (task 2.1, design D2): derives the
 * ThreadMessageLike list the external-store runtime renders from the chat
 * store's entry array. Pure derivation — no store, React, or transport
 * imports — so the live transcript and a replayed one map identically by
 * construction.
 *
 * Rules (design D2, with the D4 verdict from VENDORED.md (d)):
 * - one user message per user entry; the persisted attachment metadata rides
 *   the message's attachment list, keyed by its capability key;
 * - one assistant message per turn (turnID), content = the turn's entries in
 *   order: reasoning → reasoning part, assistant → text part, tool →
 *   tool-call part, task → a tool-call part under the registry's task
 *   convention (toolName "task", the delegation payload rides args — see
 *   components/assistant-ui/utils/task.ts + task-card.aui.tsx), error →
 *   data-error part + message status incomplete/error, approval → the
 *   turn's tool-call part carrying the native `approval` seam;
 * - a compaction entry becomes its own data-compaction message so the
 *   divider sits between turns;
 * - entries persisted before turn stamping group by adjacency: consecutive
 *   turn-less entries form one message, and a turn-less run never merges
 *   into a stamped turn;
 * - message status: requires-action while the turn holds a pending approval,
 *   incomplete/error after an error entry, running while any entry still
 *   streams, complete otherwise;
 * - part ids are the entry ids (the tool-call part's id lives in
 *   toolCallId), so component state survives reorders.
 */
import type {
  CompleteAttachment,
  MessageStatus,
  ReasoningMessagePart,
  TextMessagePart,
  ThreadMessageLike,
  ToolCallMessagePart,
  ToolCallTiming,
} from "@assistant-ui/react";

import { callDurationMs } from "../tools/parse";
import type {
  Attachment,
  Event as SessionEvent,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import { ToolStatus } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import {
  asApproval,
  asEntry,
  asRichUser,
  type ApprovalEntry,
  type ChatEntry,
} from "../entries";
import type {
  AssistantEntry,
  CompactionEntry,
  ErrorEntry,
  ReasoningEntry,
  TaskEntry,
  ToolEntry,
  UserEntry,
} from "../types";

type JsonPrimitive = string | number | boolean | null;

type JsonValue =
  | JsonPrimitive
  | { readonly [key: string]: JsonValue }
  | readonly JsonValue[];

/** The object shape parsed tool-call arguments carry. */
type JsonObject = { readonly [key: string]: JsonValue };

/** The custom data parts this mapping emits (the `data-<name>` shapes the
 * runtime converts to DataMessagePart). */
type ChatDataPart =
  | {
      readonly type: "data-compaction";
      readonly id: string;
      readonly data: SessionEvent["compaction"];
    }
  | {
      readonly type: "data-error";
      readonly id: string;
      readonly data: { readonly message: string };
    };

type ChatPart =
  | TextMessagePart
  | ReasoningMessagePart
  | ToolCallMessagePart
  | ChatDataPart;

/** One render group: a lone user or compaction entry, or the consecutive run
 * of entries sharing one turn id (or, for legacy records persisted before
 * stamping, one adjacency run of turn-less entries) that forms one assistant
 * message. */
export interface EntryGroup {
  readonly kind: "user" | "compaction" | "turn";
  /** The message id: the entry id for singletons, the lead entry's id for
   * turn groups. */
  readonly id: string;
  /** The lead entry's timestamp — the message's createdAt source. */
  readonly at: string;
  /** The turn id of a turn group; undefined for the legacy adjacency runs. */
  readonly turnID: string | undefined;
  /** The group's entries in transcript order. */
  entries: ChatEntry[];
}

/** Narrows parsed JSON to the object shape tool-call args carry. */
function isJsonObject(value: unknown): value is JsonObject {
  return value !== null && !Array.isArray(value) && typeof value === "object";
}

/** Parses a tool call's raw arguments JSON; unparsable or non-object
 * payloads degrade to empty args (argsText keeps the raw text). */
function parseJsonObject(raw: string | undefined): JsonObject {
  if (raw === undefined || raw === "") return {};

  let parsed: unknown;

  try {
    parsed = JSON.parse(raw);
  } catch {
    return {};
  }

  if (!isJsonObject(parsed)) return {};

  return parsed;
}

/** Splits the entry array into the render groups the messages derive from.
 * Exported for the branch tree, which re-walks the same groups. */
export function groupEntries(entries: readonly ChatEntry[]): EntryGroup[] {
  const groups: EntryGroup[] = [];

  for (const entry of entries) {
    if (entry.kind === "user") {
      groups.push({
        kind: "user",
        id: entry.id,
        at: entry.at,
        turnID: entry.turnID,
        entries: [entry],
      });

      continue;
    }

    if (entry.kind === "compaction") {
      groups.push({
        kind: "compaction",
        id: entry.id,
        at: entry.at,
        turnID: undefined,
        entries: [entry],
      });

      continue;
    }

    // Turn-tagged entries join the open run of their turn; entries without
    // a turn id (records persisted before stamping) join only an equally
    // turn-less open run, so a legacy run never absorbs a stamped turn.
    const last = groups[groups.length - 1];

    if (
      last !== undefined &&
      last.kind === "turn" &&
      last.turnID === entry.turnID
    ) {
      last.entries.push(entry);

      continue;
    }

    groups.push({
      kind: "turn",
      id: entry.id,
      at: entry.at,
      turnID: entry.turnID,
      entries: [entry],
    });
  }

  return groups;
}

/** The message one group renders as. */
export function groupMessage(group: EntryGroup): ThreadMessageLike {
  const lead = group.entries[0];

  if (group.kind === "user" && lead.kind === "user") return userMessage(lead);

  if (group.kind === "compaction" && lead.kind === "compaction") {
    return compactionMessage(lead);
  }

  return turnMessage(group);
}

/** The ThreadMessageLike list for the entry array, in transcript order. */
export function entriesToMessages(
  entries: readonly ChatEntry[]
): ThreadMessageLike[] {
  const messages: ThreadMessageLike[] = [];

  for (const group of groupEntries(entries)) {
    messages.push(groupMessage(group));
  }

  return messages;
}

/** The user message for one user entry: the prompt text plus the persisted
 * attachment metadata, identified by capability key. */
function userMessage(entry: UserEntry): ThreadMessageLike {
  const rich = asRichUser(entry);

  const content: TextMessagePart[] = [
    { type: "text", id: entry.id, text: entry.text },
  ];

  if (rich.attachments === undefined) {
    return {
      role: "user",
      id: entry.id,
      createdAt: new Date(entry.at),
      content,
    };
  }

  return {
    role: "user",
    id: entry.id,
    createdAt: new Date(entry.at),
    content,
    attachments: rich.attachments.map(messageAttachment),
  };
}

/** The message-attachment metadata for one stored upload: the capability key
 * is the identity (the transcript carries no other address for the bytes). */
function messageAttachment(attachment: Attachment): CompleteAttachment {
  return {
    id: attachment.key,
    type: attachmentKind(attachment.mime),
    name: attachment.name,
    contentType: attachment.mime,
    content: [],
    status: { type: "complete" },
  };
}

/** The attachment lane a mime sorts into — the same lanes the backend
 * classifies (image/PDF/other file). */
function attachmentKind(mime: string): "image" | "document" | "file" {
  if (mime === "application/pdf") return "document";

  if (mime.startsWith("image/")) return "image";

  return "file";
}

/** The compaction divider message: its own data-compaction message so the
 * divider sits between turns regardless of turn ids. */
function compactionMessage(entry: CompactionEntry): ThreadMessageLike {
  const content: ChatDataPart[] = [
    { type: "data-compaction", id: entry.id, data: entry.compaction },
  ];

  return {
    role: "assistant",
    id: entry.id,
    createdAt: new Date(entry.at),
    content,
    status: { type: "complete", reason: "stop" },
  };
}

/** The assistant message for one turn group: the turn's entries become the
 * content parts, in order. A stamped turn's message id is the turn id — the
 * handle the branch tree and the branch switch resolve back through; a
 * legacy adjacency run keeps its lead entry id. */
function turnMessage(group: EntryGroup): ThreadMessageLike {
  const content: ChatPart[] = [];

  for (const entry of group.entries) {
    content.push(turnPart(entry));
  }

  return {
    role: "assistant",
    id: group.turnID ?? group.id,
    createdAt: new Date(group.at),
    content,
    status: turnStatus(group.entries),
  };
}

/** The content part one turn member maps to. */
function turnPart(entry: ChatEntry): ChatPart {
  switch (entry.kind) {
    case "reasoning":
      return reasoningPart(entry);
    case "assistant":
      return textPart(entry);
    case "tool":
      return toolPart(entry);
    case "task":
      return taskPart(entry);
    case "error":
      return errorPart(entry);
    case "approval":
      return approvalPart(asApproval(asEntry(entry)));
    default:
      // Unreachable: user and compaction entries never join a turn group.
      throw new Error(`entry kind "${entry.kind}" cannot join a turn`);
  }
}

/** The text part for one assistant entry; a still-streaming entry carries
 * the running part status so indicators can attach to the part. */
function textPart(entry: AssistantEntry): TextMessagePart {
  if (!entry.streaming) {
    return { type: "text", id: entry.id, text: entry.text };
  }

  return {
    type: "text",
    id: entry.id,
    text: entry.text,
    status: { type: "running" },
  };
}

/** The reasoning part for one reasoning entry, streaming flag included. */
function reasoningPart(entry: ReasoningEntry): ReasoningMessagePart {
  if (!entry.streaming) {
    return { type: "reasoning", id: entry.id, text: entry.text };
  }

  return {
    type: "reasoning",
    id: entry.id,
    text: entry.text,
    status: { type: "running" },
  };
}

/** The wall-clock timing one tool record maps onto (task 4.2, design D5):
 * the terminal record's `duration_ms` becomes an end stamp at the record's
 * arrival time with the start derived back from the duration. Records
 * persisted before durations were recorded carry no timing, so their cards
 * show no latency rather than a fabricated one. */
function callTiming(
  at: string,
  durationMs: number | undefined
): ToolCallTiming | undefined {
  if (durationMs === undefined) return undefined;

  const end = Date.parse(at);

  if (Number.isNaN(end)) return undefined;

  return { startedAt: end - durationMs, completedAt: end };
}

/** The tool-call part for one tool entry: args parsed from the raw JSON
 * (argsText keeps it), result and error flag from the call record, and the
 * persisted latency as the part's timing. A result is only set once the
 * terminal record landed, so a live call maps to a result-less part. */
function toolPart(entry: ToolEntry): ToolCallMessagePart {
  const call = entry.call;

  if (call === undefined || call === null) {
    return {
      type: "tool-call",
      toolCallId: entry.id,
      toolName: "unknown",
      args: {},
      argsText: "",
    };
  }

  const base = {
    type: "tool-call",
    toolCallId: entry.id,
    toolName: call.name,
    args: parseJsonObject(call.arguments),
    argsText: call.arguments ?? "",
    isError: call.is_error === true,
  } satisfies ToolCallMessagePart;

  // The timing rides only when the record carries a duration: records
  // persisted before stamping map to a part without the key at all.
  const timing = callTiming(entry.at, callDurationMs(call));

  const part: ToolCallMessagePart =
    timing === undefined ? base : { ...base, timing };

  if (call.result === undefined || call.result === "") return part;

  return { ...part, result: call.result };
}

/** The tool-call part for one delegated task, under the registry's task
 * convention (utils/task.ts + task-card.aui.tsx): a tool call named "task"
 * whose args carry the delegation and whose result is the child's answer.
 * A still-running delegation maps to a result-less part. */
function taskPart(entry: TaskEntry): ToolCallMessagePart {
  const task = entry.task;

  if (task === undefined || task === null) {
    return {
      type: "tool-call",
      toolCallId: entry.id,
      toolName: "task",
      args: {},
      argsText: "",
    };
  }

  // The delegation payload rides args; only the fields the record carries
  // are set (the spread form keeps the literal inference lint-clean).
  const args = {
    task_id: task.task_id,
    ...(task.description !== undefined && { description: task.description }),
    ...(task.transcript_path !== undefined && {
      transcript_path: task.transcript_path,
    }),
    ...(task.background !== undefined && { background: task.background }),
  };

  const part: ToolCallMessagePart = {
    type: "tool-call",
    toolCallId: entry.id,
    toolName: "task",
    args,
    argsText: task.description ?? task.task_id,
    isError: task.status === ToolStatus.ToolStatusFailed,
  };

  if (task.result === undefined || task.result === "") return part;

  return { ...part, result: task.result };
}

/** The data part for one inline error entry; the message carries the
 * incomplete/error status beside it. */
function errorPart(entry: ErrorEntry): ChatDataPart {
  return { type: "data-error", id: entry.id, data: { message: entry.message } };
}

/** The turn's tool-call part for one approval entry (design D4, VENDORED.md
 * (d)): the dangerous command is a call of the shell tool, `approval.id` is
 * the interrupt id — the same key `onRespondToToolApproval` receives — and
 * `approved` stays omitted while the decision is pending. A resolved entry
 * records the outcome on the same part; a cancelled one carries the
 * resolution instead. */
function approvalPart(entry: ApprovalEntry): ToolCallMessagePart {
  const base = {
    type: "tool-call",
    toolCallId: entry.id,
    toolName: "execute",
    args: { command: entry.command },
    argsText: entry.command,
  } satisfies ToolCallMessagePart;

  if (!entry.resolved) {
    return { ...base, approval: { id: entry.interruptID } };
  }

  if (entry.outcome === "approved") {
    return { ...base, approval: { id: entry.interruptID, approved: true } };
  }

  if (entry.outcome === "denied") {
    return { ...base, approval: { id: entry.interruptID, approved: false } };
  }

  return {
    ...base,
    approval: { id: entry.interruptID, resolution: "cancelled" },
  };
}

/** The message status of a turn: an outstanding approval pauses it
 * (requires-action), an inline error fails it, streaming entries keep it
 * running, and otherwise it is complete. */
function turnStatus(entries: readonly ChatEntry[]): MessageStatus {
  let pendingApproval = false;
  let failed = false;
  let streaming = false;

  for (const entry of entries) {
    if (entry.kind === "approval") {
      if (!asApproval(asEntry(entry)).resolved) pendingApproval = true;
    } else if (entry.kind === "error") {
      failed = true;
    } else if (entry.kind === "assistant" || entry.kind === "reasoning") {
      if (entry.streaming) streaming = true;
    }
  }

  if (pendingApproval) return { type: "requires-action", reason: "interrupt" };

  if (failed) return { type: "incomplete", reason: "error" };

  if (streaming) return { type: "running" };

  return { type: "complete", reason: "stop" };
}
