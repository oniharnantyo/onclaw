/**
 * Group transcript mapping (add-agent-groups tasks 5.2, design D7): folds
 * `AgentGroupMessage` rows into the SHARED `Entry` union the chat transcript
 * renders, so the group surface reuses the one renderer instead of forking
 * it. Each row maps to one standalone entry; the row's stable `MessageID` is
 * the entry id (and the assistant turn id), so re-mapping after a live
 * append or a copy-back replace keeps React keys and the attribution
 * lookup stable.
 *
 * Attribution (design D7, chat-view spec "Turn attribution"):
 * - an `agent` row maps to an assistant entry carrying `author: member` —
 *   the member's avatar + name resolve from `author.agentID` at render time;
 * - a `coordinator` row (assigned / change / error lines) maps to an
 *   assistant entry carrying `author: coordinator` — the system-row label;
 *   the row's text names the assignee or the failure;
 * - a `user` row maps to the ordinary user entry — user messages carry no
 *   attribution stub;
 *
 * The mapping is pure: no store, React, or transport imports.
 */
import type { Attachment as SessionAttachment } from "../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type { AgentGroupMessage } from "../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { asEntry, type RichUserEntry } from "../chat/entries";
import { isNumber, isString, type JsonObject } from "../chat/tools/parse";
import type { Entry, EntryAuthor } from "../chat/types";

/** The GroupMessageKind vocabulary (internal/agents/groupturn.go), mirrored
 * as strings — the generated bindings carry the vocabulary as plain text. */
export const GroupMessageKind = {
  Message: "message",
  Assigned: "assigned",
  Result: "result",
  Error: "error",
  Change: "change",
} as const;

/** The GroupAuthorKind vocabulary (internal/store/types.go). */
export const GroupAuthorKind = {
  User: "user",
  Agent: "agent",
  Coordinator: "coordinator",
} as const;

/** The GroupTaskState vocabulary (internal/store/types.go). */
export const GroupTaskState = {
  Queued: "queued",
  Running: "running",
  Waiting: "waiting",
  Paused: "paused",
  Completed: "completed",
  Failed: "failed",
  Cancelled: "cancelled",
} as const;

/** The GroupEventKind vocabulary (internal/groups/service.go). */
export const GroupEventKind = {
  MessageAppended: "message-appended",
  TaskUpdated: "task-updated",
} as const;

/** The row payload the groups service writes (internal/groups/service.go
 * groupPayload): the display text plus the attachments a user message rode
 * in with. */
interface GroupPayload {
  text?: string;
  attachments?: SessionAttachment[];
}

/** Decodes a row's display text and attachments; a payload that fails to
 * parse degrades to empty — the coordinator rows always carry text, so a
 * degraded decode renders nothing rather than nonsense. */
function decodePayload(row: AgentGroupMessage): GroupPayload {
  if (row.Payload === null || row.Payload === "") return {};

  let parsed: unknown;

  try {
    parsed = JSON.parse(row.Payload);
  } catch {
    return {};
  }

  if (!isRecord(parsed)) return {};

  // A non-array or foreign attachment row degrades to none (the guard
  // drops rows that are not sessions.Attachment JSON).
  const attachments: SessionAttachment[] = [];

  if (Array.isArray(parsed.attachments)) {
    for (const row of parsed.attachments) {
      if (isAttachment(row)) attachments.push(row);
    }
  }

  return {
    text: isString(parsed.text) ? parsed.text : "",
    attachments: attachments.length > 0 ? attachments : undefined,
  };
}

/** The record-shape guard (the ChatAssistantMessage payload predicates'
 * idiom): a parsed JSON value is an object before its fields read. */
function isRecord(value: unknown): value is JsonObject {
  return typeof value === "object" && value !== null;
}

/** Type guard: one payload attachment row — the host-written
 * sessions.Attachment JSON (service.go groupPayload); a foreign row is
 * dropped rather than rendered. */
function isAttachment(value: unknown): value is SessionAttachment {
  return (
    isRecord(value) &&
    isString(value.key) &&
    isString(value.name) &&
    isString(value.mime) &&
    isNumber(value.size)
  );
}

/** The author one row attributes to (design D7): member rows carry their
 * agent id so avatar + name resolve; coordinator rows carry the system
 * marker. User rows attribute to the user — no author stub. */
function authorOf(row: AgentGroupMessage): EntryAuthor | undefined {
  if (row.AuthorKind === GroupAuthorKind.Agent && row.AuthorAgentID) {
    return { kind: "member", agentID: row.AuthorAgentID };
  }

  if (row.AuthorKind === GroupAuthorKind.Coordinator) {
    return { kind: "coordinator" };
  }

  return undefined;
}

/** True when the row has nothing to render (no text, no attachments). */
function isEmptyRow(row: AgentGroupMessage, payload: GroupPayload): boolean {
  return (
    (payload.text ?? "") === "" &&
    (payload.attachments === undefined || payload.attachments.length === 0)
  );
}

/** Maps one transcript page (or a single live row) into the shared Entry
 * union, in transcript order. Rows with nothing to render are skipped. */
export function mapGroupMessages(rows: AgentGroupMessage[]): Entry[] {
  const entries: Entry[] = [];

  for (const row of rows) {
    const payload = decodePayload(row);

    if (isEmptyRow(row, payload)) continue;

    const author = authorOf(row);

    if (row.AuthorKind === GroupAuthorKind.User) {
      // The user's own message: the ordinary user entry — attachments ride
      // the widened user shape the session path mints, no attribution stub.
      const minted: RichUserEntry = {
        kind: "user",
        id: row.MessageID,
        text: payload.text ?? "",
        attachments:
          payload.attachments && payload.attachments.length > 0
            ? payload.attachments
            : undefined,
        at: row.OccurredAt,
      };

      entries.push(asEntry(minted));

      continue;
    }

    // Member and coordinator rows both render as assistant entries: the
    // attribution branch reads the author; the text is the row's payload
    // text. MessageID is the entry id AND the turn id, so the renderer's
    // message id resolves the entry (and its author) from the store.
    entries.push({
      kind: "assistant",
      id: row.MessageID,
      turnID: row.MessageID,
      text: payload.text ?? "",
      streaming: false,
      author,
      at: row.OccurredAt,
    });
  }

  return entries;
}

/**
 * The author of the transcript entry the renderer's message id resolves to
 * (add-agent-groups design D7): the aui message id is the mapped entry's
 * turn id (assistant rows) or entry id (user/compaction/error groups), so
 * the scan matches on `turnID ?? id`. Null when the id resolves to nothing
 * or the entry carries no author — the render then shows no attribution.
 */
export function authorOfMessage(
  entries: Entry[],
  messageID: string
): EntryAuthor | null {
  if (messageID === "") return null;

  for (const entry of entries) {
    // SAFETY: compaction entries carry no turn id; the cast keeps the union
    // access legal (only turn-tagged kinds declare the field).
    const turnID = (entry as { turnID?: string }).turnID;

    if ((turnID ?? entry.id) !== messageID) continue;

    // SAFETY: only the group mapping mints authors; the widened read keeps
    // the union access legal.
    const wide = entry as { author?: EntryAuthor };

    return wide.author ?? null;
  }

  return null;
}
