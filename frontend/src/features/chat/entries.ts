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
 *   additionally appends an inline error entry for that turn. turn_completed
 *   carrying a non-empty artifacts list (add-turn-artifacts design D4)
 *   stamps the list and its truncation flag onto the turn's trailing
 *   assistant entry; each regenerated variant's own terminal stamps its own.
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
 * - input_required / input_resolved (add-browser-login-handover, design D3)
 *   fold into input entries the same way: the required event mints the parked
 *   browser-attention card, the resolved event records the receipt — the
 *   typed value rides no event, only client.resolveInput's argument.
 * - user_message payloads carry attachment metadata (task 5.5, design D5):
 *   the minted user entry keeps it for the transcript renderer.
 * - stream_sync (enhance-resumable-streams design D7) is the composite
 *   catch-up snapshot: it REPLACES the turn's streaming entries from the
 *   snapshot's partials and reconciles the running tool/task rows — never
 *   appends — so a reattach or resync reconstructs the in-flight state
 *   atomically. stream_ping folds as a no-op; the store reads it before the
 *   fold.
 */
import {
  Kind,
  type Event as SessionEvent,
} from "../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type {
  AssistantEntry,
  Entry,
  PlanEntry,
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
  PlanEntry,
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
  /** Who started the turn (add-telegram-channel 9.2): the resolved send
   * origin — user, scheduler, or telegram. Absent on rows persisted before
   * origins were recorded, which read as a user turn. */
  origin?: string;
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

/**
 * The browser-attention entry (add-browser-login-handover tasks 3.3/4.1,
 * design D3): a turn parked on a secret or takeover request — minted by
 * input_required, marked by input_resolved. The struct is receipt-safe by
 * construction: there is NO field a secret's value could occupy — the value
 * lives in the card's local state only and rides client.resolveInput's text
 * argument; the resolution records a receipt (outcome, redacted length,
 * optional saved-login id) and nothing else.
 */
export interface InputEntry {
  kind: "input";
  id: string;
  turnID?: string;
  interruptID: string;
  /** The card text — composed by the runtime from the origin, never a value. */
  prompt: string;
  /** secret | takeover (input payload attention_kind). */
  attention?: "secret" | "takeover";
  /** password | otp | authenticator (secrets only). */
  method?: string;
  /** The OTP length the card renders digit boxes for. */
  digits?: number;
  /** The HTTPS origin of the sign-in page. */
  origin?: string;
  /** The account name the agent passed (secrets, optional). */
  username?: string;
  /** The card also collects a username-or-email identifier (the agent
   * declared an identifierTarget; secrets only). */
  identifier?: boolean;
  /** False while the request is pending; the card's inputs live only on
   * the pending card. */
  resolved: boolean;
  /** completed / cancelled / expired / failed (input_resolved payload). */
  outcome?: string;
  /** The REDACTED length of the submitted value — the receipt's only trace
   * of what the user typed. */
  length?: number;
  /** The vault login id the value was saved under, when save-to-vault ran. */
  savedLoginId?: string;
  at: string;
}

/** The transcript entry union this feature's reducer mints: the shared Entry
 * union plus the kinds this wave adds through the structural seam. */
export type ChatEntry = Entry | ApprovalEntry | InputEntry;

/** Bridges one of this module's widened records onto the shared Entry seam.
 * The widened kinds are minted only by applyEvent and always carry the
 * `kind` of the Entry member they stand for. */
export function asEntry(
  entry: RichUserEntry | ApprovalEntry | InputEntry
): Entry {
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

/** Reads an entry as the input entry this module minted. */
export function asInput(entry: Entry): InputEntry {
  // SAFETY: only applyEvent mints kind "input" and always sets the
  // InputEntry fields alongside it.
  const wide = entry as object;

  // SAFETY: as above — the minted record is exactly this module's
  // InputEntry shape.
  return wide as InputEntry;
}

let nextEntryID = 0;

/** Mint a transcript-internal entry id (stable React key, never persisted). */
export function entryID(): string {
  nextEntryID += 1;

  return `entry-${nextEntryID}`;
}

/** True while the transcript holds an approval entry the backend has not
 * resolved yet (task 6.3, design D4: pending = an approval_required with no
 * matching approval_resolved). */
export function hasPendingApproval(entries: Entry[]): boolean {
  return entries.some((entry: Entry): boolean => {
    // SAFETY: only applyEvent mints kind "approval" (ApprovalEntry); the
    // widened read keeps the literal-union comparison legal.
    const kind: string = entry.kind;

    if (kind === "approval") return !asApproval(entry).resolved;

    // A pending browser-attention request parks the turn the same way an
    // approval does (add-browser-login-handover, design D3).
    if (kind === "input") return !asInput(entry).resolved;

    return false;
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

/**
 * Extend the turn's trailing streaming entry of `kind`, or open a new one
 * (enhance-resumable-streams design D7). The target is a bounded backward
 * scan, not the literal last entry: the composite stream-sync (design D7)
 * mint-or-replaces the assistant entry and then the reasoning one, so a cold
 * join can hold the pair [assistant][reasoning] with the reasoning sibling
 * last — both siblings must stay extendable by their own deltas. The scan
 * walks from the end, skips a streaming same-turn entry of the OTHER kind
 * (the sync's sibling), and merges into the first streaming same-turn entry
 * of `kind` while it is still the turn's live tail. It stops — opening a new
 * entry, exactly the mint below — at the first entry that is not such a
 * sibling: a tool/task card, a finalized (streaming:false) entry, a terminal
 * record, or another turn's (a delta after a tool card opens a new entry,
 * keeping the round's text after the card).
 */
function mergeDelta(
  entries: Entry[],
  kind: "assistant" | "reasoning",
  turnID: string | undefined,
  text: string,
  at: string,
  tag: VariantTagged
): Entry[] {
  for (let i = entries.length - 1; i >= 0; i -= 1) {
    const entry = entries[i];

    // Not the turn's live tail: the segment boundary ends the scan.
    if (!isStreamingEntry(entry) || !sameTurn(entry.turnID, turnID)) break;

    // The sync's sibling (the other half of its [assistant][reasoning]
    // pair): skip over it; its own deltas scan past this entry likewise.
    if (entry.kind !== kind) continue;

    // SAFETY: streaming entries this reducer mints carry the variant stamp
    // fields even though the shared union does not declare them (the seam
    // above); the scan only reaches this read on a streaming entry.
    const entryTag: VariantTagged = entry as VariantTagged;

    // The variant-stamp contract holds for the scanned target: a delta of
    // another variant opens its own entry rather than extending this one.
    if (entryTag.variantOf !== tag.variantOf) break;

    const merged: AssistantEntry | ReasoningEntry = {
      ...entry,
      text: entry.text + text,
    };

    return [...entries.slice(0, i), merged, ...entries.slice(i + 1)];
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

/** The turn's trailing streaming entry of `kind`, scanned from the end
 * (enhance-resumable-streams design D7): the snapshot's partials describe the
 * CURRENT model call, whose segment is the trailing one — a delta after a
 * tool card opens a new entry, and each completed call's durable record
 * finalizes its own segment before the next call streams. */
function trailingStreamingIndex(
  entries: Entry[],
  kind: "assistant" | "reasoning",
  turnID: string | undefined
): number {
  for (let i = entries.length - 1; i >= 0; i -= 1) {
    const entry = entries[i];

    if (
      entry.kind === kind &&
      entry.streaming &&
      sameTurn(entry.turnID, turnID)
    ) {
      return i;
    }
  }

  return -1;
}

/** The variant stamp for a sync-minted entry (design D7): the snapshot event
 * carries no variant_of, so the turn's already-stamped entries lend theirs —
 * a live resync mid-variant must leave the minted entry extendable by the
 * turn's stamped deltas (mergeDelta matches on the stamp). Untagged when
 * nothing holds it: a fresh reattach folds the durable page without live
 * markers, and the turn's durable assistant_message re-stamps the entry when
 * it lands. */
function streamVariantTag(entries: Entry[], e: SessionEvent): VariantTagged {
  if (e.variant_of) return variantTag(entries, e);

  if (e.turn_id) {
    for (const entry of entries) {
      // SAFETY: only reply entries of a variant slot carry the stamp fields;
      // reading them off other shapes is guarded by the turnID + variantOf
      // check (the seam above).
      const tag = entry as VariantTagged;

      if (
        (entry as { turnID?: string }).turnID === e.turn_id &&
        tag.variantOf !== undefined
      ) {
        return { variantOf: tag.variantOf, variantIndex: tag.variantIndex };
      }
    }
  }

  return {};
}

/** Mint-or-REPLACE the streaming entry `kind` from one snapshot partial
 * (design D7): the snapshot's value is authoritative, so an existing entry
 * takes the text verbatim — mergeDelta appends, which would double the
 * partial onto already-received deltas. An absent entry mints one; empty
 * text mints nothing (an empty bubble would only be dropped at finalize). */
function applyStreamPartial(
  entries: Entry[],
  kind: "assistant" | "reasoning",
  turnID: string | undefined,
  text: string,
  at: string,
  tag: VariantTagged
): Entry[] {
  const idx = trailingStreamingIndex(entries, kind, turnID);

  if (idx !== -1) {
    // SAFETY: the index scan matched the kind, so the widened record is
    // exactly that streaming member.
    const existing = entries[idx] as AssistantEntry | ReasoningEntry;
    const replaced: AssistantEntry | ReasoningEntry = { ...existing, text };

    return [...entries.slice(0, idx), replaced, ...entries.slice(idx + 1)];
  }

  if (text === "") return entries;

  const minted: AssistantEntry | ReasoningEntry =
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

  return [...entries, minted];
}

/** Reconciles the running tool rows to the snapshot's list (design D7): rows
 * the snapshot lists are running — a known call id mutates its card in place
 * (the same dedupe-by-call-id contract as KindToolActivity), an unknown one
 * mints. Rows NOT listed stay untouched: their terminal records carry the
 * result, and fabricating one here would be worse than a stale spinner. */
function applyStreamTools(entries: Entry[], e: SessionEvent): Entry[] {
  const tools = e.stream?.tools;

  if (!tools || tools.length === 0) return entries;

  let next = entries;

  for (const tool of tools) {
    const idx = next.findIndex(
      (en: Entry) => en.kind === "tool" && en.call?.call_id === tool.call_id
    );

    if (idx === -1) {
      next = [
        ...next,
        {
          kind: "tool",
          id: entryID(),
          turnID: e.turn_id,
          call: tool,
          at: e.occurred_at,
          ...streamVariantTag(next, e),
        },
      ];

      continue;
    }

    const existing = next[idx];

    if (existing.kind !== "tool") continue;
    next = [
      ...next.slice(0, idx),
      { ...existing, call: tool },
      ...next.slice(idx + 1),
    ];
  }

  return next;
}

/** Reconciles the running task rows the same way (design D7): dedupe by
 * task id, replace the live running form (fresh progress included), mint
 * unknown rows. */
function applyStreamTasks(entries: Entry[], e: SessionEvent): Entry[] {
  const tasks = e.stream?.tasks;

  if (!tasks || tasks.length === 0) return entries;

  let next = entries;

  for (const task of tasks) {
    const idx = next.findIndex(
      (en: Entry) => en.kind === "task" && en.task?.task_id === task.task_id
    );

    if (idx === -1) {
      next = [
        ...next,
        {
          kind: "task",
          id: entryID(),
          turnID: e.turn_id,
          task,
          at: e.occurred_at,
          ...streamVariantTag(next, e),
        },
      ];

      continue;
    }

    const existing = next[idx];

    if (existing.kind !== "task") continue;
    next = [
      ...next.slice(0, idx),
      { ...existing, task },
      ...next.slice(idx + 1),
    ];
  }

  return next;
}

/** Stamps the turn_completed event's artifacts onto that turn's trailing
 * assistant entry (add-turn-artifacts design D4): the list and the
 * truncation flag ride the entry the mapping later mints the trailing chips
 * part from. An empty or absent list stamps nothing; regenerated variants
 * are keyed by turn id, so each variant's own turn_completed stamps its own
 * entry. */
function stampArtifacts(entries: Entry[], e: SessionEvent): Entry[] {
  const artifacts = e.artifacts;

  if (e.turn_id === undefined || !artifacts || artifacts.length === 0) {
    return entries;
  }

  for (let i = entries.length - 1; i >= 0; i -= 1) {
    const entry = entries[i];

    if (entry.kind !== "assistant" || entry.turnID !== e.turn_id) continue;

    const stamped: AssistantEntry = {
      ...entry,
      artifacts,
      artifactsTruncated: e.artifacts_truncated === true,
    };

    return [...entries.slice(0, i), stamped, ...entries.slice(i + 1)];
  }

  return entries;
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

/**
 * Fold-time expiry normalization (add-browser-login-handover, spec
 * "Expired pending request"): an input entry still pending when its session
 * is NOT parked on the request — a transcript that ends in a bare
 * input_required after a restart, or a live turn that ended without a
 * receipt — renders as expired. This folds the receipt client-side only;
 * the transcript log stays append-only-from-turns (no event is written),
 * and the backend refuses a stale ResolveInput with ErrInputNotPending, so
 * the agent may re-request. Callers decide when the session counts as not
 * busy (the replay fold in selectSession and the busy→false transition in
 * setBusy); the helper itself only normalizes the entries.
 */
export function expireUnresolvedInputs(entries: Entry[]): Entry[] {
  let changed = false;

  const next = entries.map((en: Entry): Entry => {
    // SAFETY: only applyEvent mints kind "input" (InputEntry); the widened
    // read keeps the literal-union comparison legal.
    const kind: string = en.kind;

    if (kind !== "input") return en;

    const input = asInput(en);

    if (input.resolved) return en;

    changed = true;

    const expired: InputEntry = {
      ...input,
      resolved: true,
      outcome: "expired",
    };

    return asEntry(expired);
  });

  return changed ? next : entries;
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
        origin: e.origin,
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

    case Kind.KindPlanActivity: {
      if (!e.plan) return entries;

      const idx = entries.findIndex((en: Entry) => en.kind === "plan");

      const next: PlanEntry = {
        kind: "plan",
        id: idx === -1 ? entryID() : entries[idx].id,
        turnID: e.turn_id,
        plan: e.plan,
        at: e.occurred_at,
      };

      if (idx === -1) return [...entries, next];

      return [...entries.slice(0, idx), next, ...entries.slice(idx + 1)];
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

    // Browser attention (add-browser-login-handover tasks 3.3/4.1, design
    // D3): the required event mints the pending card entry, the resolved
    // event marks the matching card with its receipt — outcome, redacted
    // length, optional saved-login id; the value never rides either event.
    case Kind.KindInputRequired: {
      if (!e.input?.interrupt_id) return entries;

      const payload = e.input;

      // SAFETY: the minted record carries the InputEntry fields; kind
      // "input" rides the Entry seam through asEntry.
      const minted: InputEntry = {
        kind: "input",
        id: entryID(),
        turnID: e.turn_id,
        interruptID: payload.interrupt_id,
        prompt: payload.prompt ?? "",
        attention:
          payload.attention_kind === "takeover" ? "takeover" : "secret",
        method: payload.method,
        digits: payload.digits,
        origin: payload.origin,
        username: payload.username,
        identifier: payload.identifier === true,
        resolved: false,
        at: e.occurred_at,
      };

      return [...entries, asEntry(minted)];
    }

    case Kind.KindInputResolved: {
      if (!e.input?.interrupt_id) return entries;

      const receipt = e.input;

      const idx = entries.findIndex((en: Entry): boolean => {
        // SAFETY: the input kind rides the Entry seam (InputEntry); the
        // widened read keeps the literal union comparison legal.
        const kind: string = en.kind;

        return (
          kind === "input" && asInput(en).interruptID === receipt.interrupt_id
        );
      });

      if (idx === -1) {
        // SAFETY: as KindInputRequired above — the resolved marker mints the
        // same InputEntry record with its receipt (degraded windowing case).
        const minted: InputEntry = {
          kind: "input",
          id: entryID(),
          turnID: e.turn_id,
          interruptID: receipt.interrupt_id,
          prompt: receipt.prompt ?? "",
          attention:
            receipt.attention_kind === "takeover" ? "takeover" : "secret",
          resolved: true,
          outcome: receipt.outcome,
          length: receipt.length,
          savedLoginId: receipt.saved_login_id,
          at: e.occurred_at,
        };

        return [...entries, asEntry(minted)];
      }

      const marked = asInput(entries[idx]);

      const resolvedEntry: InputEntry = {
        ...marked,
        resolved: true,
        outcome: receipt.outcome,
        length: receipt.length,
        savedLoginId: receipt.saved_login_id,
      };

      return [
        ...entries.slice(0, idx),
        asEntry(resolvedEntry),
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
      return stampArtifacts(finalize(entries), e);
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

    // Stream resume (enhance-resumable-streams design D7): the composite
    // snapshot REPLACES the turn's live streaming state atomically — one
    // event, one fold, so old and new partials can never mix. The partials
    // mint-or-replace the streaming entries, the running tool/task rows
    // reconcile to the snapshot's lists, and a later terminal record
    // finalizes the reconstructed rows exactly like the subscribe-time
    // snapshot's. Live-only: the durable vocabulary is untouched. The store
    // rebaselines its received-delta counter off stream.deltas BEFORE this
    // folds (design D6) — the two halves of one resync must not race.
    case Kind.KindStreamSync: {
      const tag = streamVariantTag(entries, e);

      let next = applyStreamPartial(
        entries,
        "assistant",
        e.turn_id,
        e.stream?.text ?? "",
        e.occurred_at,
        tag
      );

      next = applyStreamPartial(
        next,
        "reasoning",
        e.turn_id,
        e.stream?.reasoning ?? "",
        e.occurred_at,
        tag
      );
      next = applyStreamTools(next, e);
      next = applyStreamTasks(next, e);

      return next;
    }

    case Kind.KindStreamPing:
      // The liveness beacon changes nothing in the transcript (design D4):
      // the store reads the ping's counter for gap detection BEFORE the
      // fold — this arm only pins the no-op contract.
      return entries;

    default:
      return entries;
  }
}
