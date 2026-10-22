/**
 * Transcript reducer (tasks 9.2, design D7): folds session events into the
 * ordered entry list the transcript renders. One code path serves both the
 * durable replay (chat.transcript) and the live stream (chat.subscribe):
 * persisted events carry the store-assigned id on their live form too, so
 * dedupe by event id keeps the two views from duplicating rows — deltas are
 * live-only and never persisted, so they always apply.
 *
 * Folding rules:
 * - text/reasoning deltas extend the trailing streaming entry of their turn,
 *   or open one (a delta after a tool card opens a new entry, keeping the
 *   round's text after the card).
 * - the durable assistant_message / reasoning events finalize (replace) the
 *   turn's streaming entry with the full text; without one they append.
 * - tool_activity cards dedupe by call id: a repeat event mutates the card
 *   (e.g. the result landing) instead of minting a second one.
 * - task_activity cards (background subagent tasks, W5) dedupe by task id the
 *   same way: the live running form is replaced in place by the terminal
 *   record with the child's answer.
 * - turn_completed / turn_error finalize every streaming entry; empty ones
 *   (a turn that died before producing output) are dropped, and turn_error
 *   additionally appends an inline error entry for that turn.
 * - turn_cancelled (task 3.3) is the cancellation terminal: streaming entries
 *   finalize KEEPING their partial text. Live, the trailing assistant entry
 *   already holds the streamed deltas; on replay no delta was persisted, so
 *   the event payload's partial output mints the finalized entry instead.
 * - a user_message carrying variant_of (task 7.2, design D7) is a regeneration
 *   marker, not a fresh prompt: it appends the new turn id to the ORIGINAL
 *   turn's variant slot on its user entry, and every reply event of the marked
 *   turn is stamped with (variantOf, variantIndex) so the transcript can show
 *   exactly the active alternative. A failed regeneration (turn_error, task
 *   7.3) drops its EMPTY variant and restores the previous active index.
 * - approval_required / approval_resolved (task 4.5, design D4) fold into
 *   approval entries: the required event mints the pending card, the resolved
 *   event marks the matching card with its outcome.
 * - user_message payloads carry attachment metadata (task 5.5, design D5):
 *   the minted user entry keeps it for the transcript renderer.
 */
import {
  Kind,
  ToolStatus,
  type Event as SessionEvent,
} from "../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type {
  AssistantEntry,
  Entry,
  ReasoningEntry,
  TaskEntry,
  ToolEntry,
  UserEntry,
} from "./types";

export type {
  AssistantEntry,
  CompactionEntry,
  Entry,
  ErrorEntry,
  ReasoningEntry,
  TaskEntry,
  ToolEntry,
  UserEntry,
} from "./types";

import type { Attachment as SessionAttachment } from "../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";

/**
 * The user entry widened with this wave's payload fields (tasks 5.5, 7.2).
 * The shared types file sits outside this wave's file scope, so the widened
 * shape lives here and the reducer mints it through the Entry seam; readers
 * narrow with `as RichUserEntry` once `kind === "user"` matched.
 */
export interface RichUserEntry extends UserEntry {
  /** Attachment metadata persisted with the message (design D5); replay
   * reconstructs the chips/thumbnails from it. */
  attachments?: SessionAttachment[];
  /** The variant slot (task 7.2): turn ids of this prompt's alternative
   * answers in creation order — index 0 is the original turn, every
   * regeneration appends. Absent while the turn has no alternatives. */
  variantTurns?: string[];
  /** Which alternative renders; defaults to the newest (design D7: newest
   * live, last-marked on replay — both are "last appended"). */
  activeVariant?: number;
}

/** The reply-entry stamp linking an entry to its variant slot (task 7.2). */
export interface VariantTagged {
  /** The ORIGINAL turn id this entry's alternative belongs to. */
  variantOf?: string;
  /** Index into the anchor's variantTurns. */
  variantIndex?: number;
}

/** The approval card entry (task 4.5, design D4): a dangerous command a turn
 * paused on, and — once approval_resolved folds — its recorded decision.
 * Minted through the Entry seam like RichUserEntry. */
export interface ApprovalEntry {
  kind: "approval";
  id: string;
  turnID?: string;
  interruptID: string;
  command: string;
  /** False while the decision is pending; the card's actions live only on
   * the pending card. */
  resolved: boolean;
  /** approved / denied / cancelled once resolved (approval_resolved payload). */
  outcome?: string;
  at: string;
}

/** The transcript entry union this feature's reducer mints: the shared Entry
 * union plus the kinds this wave adds through the structural seam. */
export type ChatEntry = Entry | ApprovalEntry;

/** Bridges one of this module's widened records onto the shared Entry seam.
 * The widened kinds are minted only by applyEvent and always carry the
 * `kind` of the Entry member they stand for. */
export function asEntry(entry: RichUserEntry | ApprovalEntry): Entry {
  // SAFETY: the widened records are minted only here and always carry the
  // `kind` of the Entry member they stand for, so the runtime shape is that
  // member's.
  const wide = entry as object;

  // SAFETY: as above — the widened shape structurally carries its member.
  return wide as Entry;
}

/** Reads an entry as the approval entry this module minted. */
export function asApproval(entry: Entry): ApprovalEntry {
  // SAFETY: only applyEvent mints kind "approval" and always sets the
  // ApprovalEntry fields alongside it.
  const wide = entry as object;

  // SAFETY: as above — the minted record is exactly this module's
  // ApprovalEntry shape.
  return wide as ApprovalEntry;
}

/** Reads a reply entry's variant stamp (task 7.2): stamped entries carry
 * the fields without the shared union declaring them (the seam above). */
export function asVariantTagged(entry: Entry): VariantTagged {
  // SAFETY: only applyEvent attaches the stamp fields, always to reply
  // entries of a variant slot.
  const wide = entry as object;

  // SAFETY: as above — the stamp fields ride the minted record.
  return wide as VariantTagged;
}

/** Reads a user entry with this module's widened fields attached. */
export function asRichUser(entry: Entry): RichUserEntry {
  // SAFETY: only kind "user" entries are passed; applyEvent attaches the
  // widened fields it mints.
  const wide = entry as object;

  // SAFETY: as above — the minted record is exactly this module's
  // RichUserEntry shape.
  return wide as RichUserEntry;
}

let nextEntryID = 0;

/** Mint a transcript-internal entry id (stable React key, never persisted). */
export function entryID(): string {
  nextEntryID += 1;

  return `entry-${nextEntryID}`;
}

/** One plan-lane checklist item (task 9.2): a todo_write item as persisted. */
export interface PlanItem {
  key: string;
  text: string;
  status: "pending" | "active" | "done" | "failed";
  reason?: string;
}

/** The wire shape of one todo_write item before it is trusted. */
interface RawPlanItem {
  key?: string;
  text?: string;
  status?: string;
  reason?: string;
}

const PLAN_STATUSES = new Set<string>(["pending", "active", "done", "failed"]);

/** Parses a todo_write payload (the {items:[...]} result echo or the call
 * arguments) into plan items. Entries lacking a key/text or carrying an
 * unknown status are dropped; anything but a JSON object with an items array
 * returns null so the caller can fall through. */
function parsePlanItems(raw: string | undefined): PlanItem[] | null {
  if (!raw) return null;

  let parsed: unknown;

  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }

  if (!parsed) return null;

  // SAFETY: todo_write payloads are strict backend-validated JSON of this
  // shape; malformed entries surface as undefined fields and are dropped by
  // the checks below rather than trusted.
  const items = (parsed as { items?: RawPlanItem[] }).items;

  if (!Array.isArray(items)) return null;

  const plan: PlanItem[] = [];

  for (const item of items) {
    if (!item?.key || !item?.text) continue;

    if (item.status === undefined || !PLAN_STATUSES.has(item.status)) continue;

    // SAFETY: the PLAN_STATUSES membership check above narrows the wire
    // status to exactly the four values PlanItem.status admits.
    const status = item.status as PlanItem["status"];
    const planItem: PlanItem = { key: item.key, text: item.text, status };

    if (item.reason) planItem.reason = item.reason;

    plan.push(planItem);
  }

  return plan;
}

/** The run card's plan lane data (task 9.2, design D8): the persisted items of
 * the LATEST todo_write tool activity — the validated result echo when the
 * terminal record landed, else the call arguments of a still-running call so
 * the checklist updates live mid-turn. A failed call persisted nothing, so it
 * yields no lane; with no todo_write activity at all the lane is absent. */
export function latestPlanItems(entries: Entry[]): PlanItem[] {
  for (let i = entries.length - 1; i >= 0; i -= 1) {
    const entry = entries[i];

    if (entry.kind !== "tool" || entry.call?.name !== "todo_write") continue;

    if (
      entry.call.status === ToolStatus.ToolStatusFailed ||
      entry.call.is_error
    ) {
      return [];
    }

    const fromResult = parsePlanItems(entry.call.result);

    if (fromResult !== null) return fromResult;

    return parsePlanItems(entry.call.arguments) ?? [];
  }

  return [];
}

/** True while the transcript holds an approval entry the backend has not
 * resolved yet (task 6.3, design D4: pending = an approval_required with no
 * matching approval_resolved). */
export function hasPendingApproval(entries: Entry[]): boolean {
  return entries.some((entry: Entry): boolean => {
    // SAFETY: only applyEvent mints kind "approval" (ApprovalEntry); the
    // widened read keeps the literal-union comparison legal.
    const kind: string = entry.kind;

    if (kind !== "approval") return false;

    return !asApproval(entry).resolved;
  });
}

function sameTurn(
  entryTurn: string | undefined,
  turnID: string | undefined
): boolean {
  return (
    turnID === undefined || entryTurn === undefined || entryTurn === turnID
  );
}

function isStreamingEntry(
  entry: Entry
): entry is AssistantEntry | ReasoningEntry {
  return (
    (entry.kind === "assistant" || entry.kind === "reasoning") &&
    entry.streaming
  );
}

/** Extend the trailing streaming entry of `kind` or open a new one. */
function mergeDelta(
  entries: Entry[],
  kind: "assistant" | "reasoning",
  turnID: string | undefined,
  text: string,
  at: string,
  tag: VariantTagged
): Entry[] {
  const last = entries[entries.length - 1];

  // SAFETY: streaming entries this reducer mints carry the variant stamp
  // fields even though the shared union does not declare them (the seam
  // above); reading the stamp off a non-streaming tail is guarded below.
  const lastTag: VariantTagged = last as VariantTagged;

  if (
    last &&
    last.kind === kind &&
    last.streaming &&
    sameTurn(last.turnID, turnID) &&
    lastTag.variantOf === tag.variantOf
  ) {
    const merged: AssistantEntry | ReasoningEntry = {
      ...last,
      text: last.text + text,
    };

    return [...entries.slice(0, -1), merged];
  }

  const opened: AssistantEntry | ReasoningEntry =
    kind === "assistant"
      ? {
          kind: "assistant",
          id: entryID(),
          turnID,
          text,
          streaming: true,
          at,
          ...tag,
        }
      : {
          kind: "reasoning",
          id: entryID(),
          turnID,
          text,
          streaming: true,
          at,
          ...tag,
        };

  return [...entries, opened];
}

/** Finalize streaming entries; drop the empty ones a dead turn left behind. */
function finalize(entries: Entry[]): Entry[] {
  return entries.flatMap((e: Entry): Entry[] => {
    if (isStreamingEntry(e) && e.text === "") return [];

    return [isStreamingEntry(e) ? { ...e, streaming: false } : e];
  });
}

/** The user entry anchoring the variant slot `turnID` belongs to, with the
 * turn's slot index (task 7.2). Absent anchor → null (a page boundary can
 * surface a regeneration marker before the original prompt loaded). */
function variantAnchor(
  entries: Entry[],
  turnID: string | undefined
): { anchor: RichUserEntry; index: number } | null {
  if (!turnID) return null;

  for (const entry of entries) {
    if (entry.kind !== "user") continue;

    const anchor = asRichUser(entry);
    const index = anchor.variantTurns?.indexOf(turnID) ?? -1;

    if (index !== -1) return { anchor, index };
  }

  return null;
}

/** The (variantOf, variantIndex) stamp for a reply event: events carrying
 * variant_of resolve their slot from the anchor minted by the regeneration's
 * user_message marker; unmarked events stay untagged. */
function variantTag(entries: Entry[], e: SessionEvent): VariantTagged {
  if (!e.variant_of) return {};

  const found = variantAnchor(entries, e.turn_id);

  if (!found) return { variantOf: e.variant_of };

  return { variantOf: e.variant_of, variantIndex: found.index };
}

/** Folds a regeneration marker (user_message with variant_of) into the
 * original turn's slot: the marked prompt is NOT re-persisted by the backend,
 * so no user entry is minted — the original bubble stands for every
 * alternative (design D7). Live the newest alternative becomes active; replay
 * folds in log order, so the last-marked variant ends active too. */
function appendVariant(entries: Entry[], e: SessionEvent): Entry[] {
  const original = e.variant_of ?? "";
  const turnID = e.turn_id ?? "";

  if (original === "" || turnID === "" || turnID === original) return entries;

  const idx = entries.findIndex(
    (en: Entry): boolean => en.kind === "user" && en.turnID === original
  );

  if (idx === -1) {
    // Degraded windowing case: the marker arrived before its anchor. Mint the
    // marker as a plain user entry so the turn's replies stay visible.
    const minted: RichUserEntry & VariantTagged = {
      kind: "user",
      id: entryID(),
      turnID,
      text: e.text ?? "",
      at: e.occurred_at,
      variantOf: original,
    };

    return [...entries, asEntry(minted)];
  }

  const anchor = asRichUser(entries[idx]);
  // Slot index 0 is the original turn itself; the marker's turn appends.
  const slot = anchor.variantTurns ?? [original];
  const variantTurns = slot.includes(turnID) ? slot : [...slot, turnID];

  const next: RichUserEntry = {
    ...anchor,
    variantTurns,
    activeVariant: variantTurns.length - 1,
  };

  return [...entries.slice(0, idx), asEntry(next), ...entries.slice(idx + 1)];
}

/** The anchor entry with a failed regeneration's EMPTY variant dropped and
 * the previous active index restored (task 7.3, spec: "Failed regeneration
 * keeps the old answer") — or null when the turn has no slot or produced
 * partial text (a variant with content keeps its slot). */
function dropEmptyVariant(
  entries: Entry[],
  turnID: string
): RichUserEntry | null {
  const found = variantAnchor(entries, turnID);

  if (!found) return null;

  const { anchor } = found;
  const slot = anchor.variantTurns ?? [];

  // Empty means the failed turn produced no answer text at all.
  const produced = entries.some(
    (en: Entry): boolean =>
      en.kind === "assistant" && en.turnID === turnID && en.text !== ""
  );

  if (produced) return null;

  const variantTurns = slot.filter((id: string): boolean => id !== turnID);

  if (variantTurns.length === slot.length) return null;

  return {
    ...anchor,
    variantTurns,
    activeVariant: Math.min(anchor.activeVariant ?? 0, variantTurns.length - 1),
  };
}

/** Prepends an older, independently folded page to the live window (task 6.1,
 * design D6): a boundary falling inside a turn merges by turn id — the older
 * segment's leftover streaming entries for turns the newer segment already
 * holds finalized are dropped, so the turn renders as one unbroken entry. */
export function mergePrepended(older: Entry[], newer: Entry[]): Entry[] {
  const newerTurns = new Set<string>();

  for (const entry of newer) {
    // SAFETY: compaction entries carry no turn id; the cast keeps the union
    // access legal (only turn-tagged kinds declare the field).
    const t = entry as { turnID?: string };

    if (t.turnID) newerTurns.add(t.turnID);
  }

  const merged = older.filter((entry: Entry): boolean => {
    if (!isStreamingEntry(entry) || !entry.turnID) return true;

    return !newerTurns.has(entry.turnID);
  });

  return [...merged, ...newer];
}

/** The rendered window (task 7.2): keeps reply entries of the ACTIVE variant
 * only. Regeneration events carry the (variantOf, variantIndex) stamp; the
 * ORIGINAL turn's reply events carry none, so they resolve their slot from
 * the anchor's turn list (index 0). Entries outside any slot — and replies
 * whose anchor never loaded — stay visible, degrading to a plain transcript. */
export function visibleEntries(entries: Entry[]): Entry[] {
  // turnID → its slot position, for the untagged original-turn replies.
  const slotIndex = new Map<string, { anchorID: string; index: number }>();

  for (const entry of entries) {
    if (entry.kind !== "user") continue;

    const anchor = asRichUser(entry);

    anchor.variantTurns?.forEach((turnID: string, index: number): void => {
      slotIndex.set(turnID, { anchorID: entry.turnID ?? "", index });
    });
  }

  const activeOf = (anchorID: string): number | null => {
    const anchor = entries.find(
      (en: Entry): boolean => en.kind === "user" && en.turnID === anchorID
    );

    if (!anchor) return null;

    const a = asRichUser(anchor);

    return a.activeVariant ?? (a.variantTurns?.length ?? 1) - 1;
  };

  return entries.filter((entry: Entry): boolean => {
    // The anchor's own bubble always renders; a slot's alternative markers
    // never minted one.
    if (entry.kind === "user") return true;

    // SAFETY: reply entries minted with a variant stamp carry the tag fields
    // even though the shared union does not declare them (the seam above).
    const tag = entry as VariantTagged;

    if (tag.variantOf !== undefined && tag.variantIndex !== undefined) {
      const active = activeOf(tag.variantOf);

      return active === null || active === tag.variantIndex;
    }

    // SAFETY: compaction entries carry no turn id; the cast keeps the union
    // access legal (only turn-tagged kinds declare the field).
    const turnID = (entry as { turnID?: string }).turnID;
    const slot = turnID !== undefined ? slotIndex.get(turnID) : undefined;

    if (!slot) return true;

    const active = activeOf(slot.anchorID);

    return active === null || active === slot.index;
  });
}

/** Fold one event into the entry list, returning the new list. */
export function applyEvent(entries: Entry[], e: SessionEvent): Entry[] {
  switch (e.kind) {
    case Kind.KindUserMessage: {
      if (e.variant_of) return appendVariant(entries, e);

      const attachments =
        e.attachments && e.attachments.length > 0 ? e.attachments : undefined;

      // SAFETY: the minted record carries the UserEntry fields plus this
      // wave's widened payload (kind "user" is the Entry member it stands
      // for).
      const minted: RichUserEntry = {
        kind: "user",
        id: entryID(),
        turnID: e.turn_id,
        text: e.text ?? "",
        attachments,
        at: e.occurred_at,
      };

      return [...entries, asEntry(minted)];
    }

    case Kind.KindTextDelta: {
      const tag = variantTag(entries, e);

      return mergeDelta(
        entries,
        "assistant",
        e.turn_id,
        e.text ?? "",
        e.occurred_at,
        tag
      );
    }

    case Kind.KindReasoningDelta: {
      const tag = variantTag(entries, e);

      return mergeDelta(
        entries,
        "reasoning",
        e.turn_id,
        e.text ?? "",
        e.occurred_at,
        tag
      );
    }

    case Kind.KindAssistantMessage: {
      const idx = entries.findIndex(
        (en: Entry) =>
          en.kind === "assistant" &&
          en.streaming &&
          sameTurn(en.turnID, e.turn_id)
      );

      if (idx === -1) {
        return [
          ...entries,
          {
            kind: "assistant",
            id: entryID(),
            turnID: e.turn_id,
            text: e.text ?? "",
            streaming: false,
            at: e.occurred_at,
            ...variantTag(entries, e),
          },
        ];
      }

      const replaced: Entry = {
        ...entries[idx],
        kind: "assistant",
        text: e.text ?? "",
        streaming: false,
      };

      return [...entries.slice(0, idx), replaced, ...entries.slice(idx + 1)];
    }

    case Kind.KindReasoning: {
      const idx = entries.findIndex(
        (en: Entry) =>
          en.kind === "reasoning" &&
          en.streaming &&
          sameTurn(en.turnID, e.turn_id)
      );

      if (idx === -1) {
        return [
          ...entries,
          {
            kind: "reasoning",
            id: entryID(),
            turnID: e.turn_id,
            text: e.text ?? "",
            streaming: false,
            at: e.occurred_at,
            ...variantTag(entries, e),
          },
        ];
      }

      const replaced: Entry = {
        ...entries[idx],
        kind: "reasoning",
        text: e.text ?? "",
        streaming: false,
      };

      return [...entries.slice(0, idx), replaced, ...entries.slice(idx + 1)];
    }

    case Kind.KindToolActivity: {
      if (!e.tool) return entries;

      const idx = entries.findIndex(
        (en: Entry) =>
          en.kind === "tool" && en.call?.call_id === e.tool?.call_id
      );

      if (idx === -1) {
        return [
          ...entries,
          {
            kind: "tool",
            id: entryID(),
            turnID: e.turn_id,
            call: e.tool,
            at: e.occurred_at,
            ...variantTag(entries, e),
          },
        ];
      }

      const existing = entries[idx];

      if (existing.kind !== "tool") return entries;
      const replaced: ToolEntry = { ...existing, call: e.tool };

      return [...entries.slice(0, idx), replaced, ...entries.slice(idx + 1)];
    }

    case Kind.KindTaskActivity: {
      if (!e.task) return entries;

      const idx = entries.findIndex(
        (en: Entry) =>
          en.kind === "task" && en.task?.task_id === e.task?.task_id
      );

      if (idx === -1) {
        return [
          ...entries,
          {
            kind: "task",
            id: entryID(),
            turnID: e.turn_id,
            task: e.task,
            at: e.occurred_at,
            ...variantTag(entries, e),
          },
        ];
      }

      const existing = entries[idx];

      if (existing.kind !== "task") return entries;
      const replaced: TaskEntry = { ...existing, task: e.task };

      return [...entries.slice(0, idx), replaced, ...entries.slice(idx + 1)];
    }

    case Kind.KindCompaction:
      return [
        ...entries,
        {
          kind: "compaction",
          id: entryID(),
          compaction: e.compaction,
          at: e.occurred_at,
        },
      ];
    case Kind.KindApprovalRequired: {
      if (!e.approval?.interrupt_id) return entries;

      // SAFETY: the minted record carries the ApprovalEntry fields; kind
      // "approval" rides the Entry seam through asEntry.
      const minted: ApprovalEntry = {
        kind: "approval",
        id: entryID(),
        turnID: e.turn_id,
        interruptID: e.approval.interrupt_id,
        command: e.approval.command ?? "",
        resolved: false,
        at: e.occurred_at,
      };

      return [...entries, asEntry(minted)];
    }

    case Kind.KindApprovalResolved: {
      if (!e.approval?.interrupt_id) return entries;

      const idx = entries.findIndex((en: Entry): boolean => {
        // SAFETY: the approval kind rides the Entry seam (ApprovalEntry);
        // the widened read keeps the literal union comparison legal.
        const kind: string = en.kind;

        return (
          kind === "approval" &&
          asApproval(en).interruptID === e.approval?.interrupt_id
        );
      });

      if (idx === -1) {
        // SAFETY: as KindApprovalRequired above — the resolved marker mints
        // the same ApprovalEntry record with its outcome.
        const minted: ApprovalEntry = {
          kind: "approval",
          id: entryID(),
          turnID: e.turn_id,
          interruptID: e.approval.interrupt_id,
          command: e.approval.command ?? "",
          resolved: true,
          outcome: e.approval.outcome,
          at: e.occurred_at,
        };

        return [...entries, asEntry(minted)];
      }

      const resolved = asApproval(entries[idx]);

      const marked: ApprovalEntry = {
        ...resolved,
        resolved: true,
        outcome: e.approval.outcome,
      };

      return [
        ...entries.slice(0, idx),
        asEntry(marked),
        ...entries.slice(idx + 1),
      ];
    }

    case Kind.KindTurnError: {
      let next = finalize(entries);

      // A failed regeneration drops its empty variant and restores the
      // previous active index (task 7.3). The error entry itself stays
      // untagged — the failure must be visible in the transcript.
      if (e.variant_of) {
        const updated = dropEmptyVariant(next, e.turn_id ?? "");

        if (updated) {
          next = next.map((en: Entry): Entry =>
            en.kind === "user" && en.id === updated.id ? asEntry(updated) : en
          );
        }
      }

      return [
        ...next,
        {
          kind: "error",
          id: entryID(),
          turnID: e.turn_id,
          message: e.error ?? "Turn failed",
          at: e.occurred_at,
        },
      ];
    }

    case Kind.KindTurnCompleted:
      return finalize(entries);
    case Kind.KindTurnCancelled: {
      // The partial output stays (task 3.3): live, the trailing streaming
      // assistant entry keeps its accumulated deltas; with none — a replay,
      // or a call cancelled before its first delta — the payload's partial
      // output fills it. finalize() then closes every streaming entry
      // (keeping their text) and drops the empty ones a dead turn left.
      const partial = e.text ?? "";
      const tag = variantTag(entries, e);

      const idx = entries.findIndex(
        (en: Entry) =>
          en.kind === "assistant" &&
          en.streaming &&
          sameTurn(en.turnID, e.turn_id)
      );

      let next = entries;

      if (idx === -1) {
        if (partial !== "") {
          next = [
            ...entries,
            {
              kind: "assistant",
              id: entryID(),
              turnID: e.turn_id,
              text: partial,
              streaming: false,
              at: e.occurred_at,
              ...tag,
            },
          ];
        }
      } else {
        const existing = entries[idx];

        if (existing.kind === "assistant") {
          const filled: AssistantEntry = {
            ...existing,
            text: existing.text === "" ? partial : existing.text,
          };

          next = [...entries.slice(0, idx), filled, ...entries.slice(idx + 1)];
        }
      }

      return finalize(next);
    }

    default:
      return entries;
  }
}
