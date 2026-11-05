/**
 * Variant turns → branch tree (task 2.2, design D3): the regenerated turns
 * of one prompt become sibling assistant messages under the ORIGINAL turn's
 * parent (the anchor user message), and `ExportedMessageRepository.
 * fromBranchableArray` builds the repository the adapter hands the runtime,
 * with `headId` on the active variant. Pure derivation over the same entry
 * groups the message mapping uses — no store, React, or transport imports.
 *
 * Conversion is per-entry memoized (task 5.2, design D4): a group whose
 * entries are reference-equal to the previous build's hands back its
 * previous message object, so a streaming delta re-converts only the tail
 * group it changed and the library's per-message caches hit for every
 * message that did not move.
 *
 * Head resolution per anchor: an explicit override in `activeVariants`
 * (original turn id → slot index), else the anchor's own `activeVariant`
 * (the store-set state after `setActiveVariant`), else the newest slot —
 * the same "newest live, last-marked on replay" default the reducer uses.
 */
import {
  ExportedMessageRepository,
  type ThreadMessage,
} from "@assistant-ui/react";

import { asRichUser, type ChatEntry } from "../entries";
import { groupEntries, groupMessage, type EntryGroup } from "./mapping";

/** Active variant override by ORIGINAL turn id → slot index; absent entries
 * fall back to the anchor's own state. */
export type ActiveVariants = ReadonlyMap<string, number>;

/** One variant slot of an anchor: the turn id and its position (0 is the
 * original turn, every regeneration appends). */
interface VariantSlot {
  readonly turnID: string;
  readonly index: number;
}

/** A user entry carrying a variant slot — the branch point its reply turns
 * hang off. */
interface VariantAnchor {
  /** The anchor's message id (the user entry's id). */
  readonly anchorID: string;
  /** The ORIGINAL turn id (slot 0); undefined on degraded anchors. */
  readonly originalTurnID: string | undefined;
  readonly slots: readonly VariantSlot[];
  /** The resolved active slot index (override → activeVariant → newest). */
  readonly active: number;
}

/** The anchors (user entries with variant slots), in transcript order. */
function variantAnchors(
  groups: readonly EntryGroup[],
  activeVariants: ActiveVariants | undefined
): VariantAnchor[] {
  const anchors: VariantAnchor[] = [];

  for (const group of groups) {
    if (group.kind !== "user") continue;

    const lead = group.entries[0];

    if (lead.kind !== "user") continue;

    const rich = asRichUser(lead);
    const slotTurns = rich.variantTurns;

    if (slotTurns === undefined || slotTurns.length === 0) continue;

    const slots = slotTurns.map(
      (turnID: string, index: number): VariantSlot => ({ turnID, index })
    );

    const override =
      rich.turnID !== undefined ? activeVariants?.get(rich.turnID) : undefined;

    const resolved = override ?? rich.activeVariant ?? slots.length - 1;

    anchors.push({
      anchorID: group.id,
      originalTurnID: rich.turnID,
      slots,
      active: Math.max(0, Math.min(resolved, slots.length - 1)),
    });
  }

  return anchors;
}

/** One group's normalized message beside the entry identities it was built
 * from — the reuse record design D4 keys stability on. The message is the
 * NORMALIZED form (what `fromBranchableArray` hands the library), so a memo
 * hit hands the library the very object it already holds. */
interface MessageMemo {
  readonly entries: readonly ChatEntry[];
  readonly message: ThreadMessage;
}

/** The previous build's memos, keyed by group kind + id. Replaced wholesale
 * on every build (a group that left the transcript drops out) and verified
 * element-wise against the current group's entries: only reference-equal
 * inputs reuse a message object, so a rebuilt or reloaded transcript can
 * never inherit a stale one. */
let messageMemos = new Map<string, MessageMemo>();

/** True when the cached group was built from exactly these entry objects. */
function sameEntries(
  cached: readonly ChatEntry[],
  current: readonly ChatEntry[]
): boolean {
  if (cached.length !== current.length) return false;

  for (let i = 0; i < cached.length; i += 1) {
    if (cached[i] !== current[i]) return false;
  }

  return true;
}

/** The normalized message one group renders as, memoized across builds
 * (design D4). A reference-equal group returns the previous build's object
 * untouched; otherwise the group converts through the library's own
 * `fromBranchableArray` (one item per call — it maps items independently,
 * so a per-group call and a whole-array build normalize identically).
 * Every current group is recorded into `next`, which replaces the module
 * memo after the walk and drops whatever left the transcript. */
function memoizedMessage(
  group: EntryGroup,
  next: Map<string, MessageMemo>
): ThreadMessage {
  const key = `${group.kind}:${group.id}`;
  const memo = messageMemos.get(key);

  if (memo !== undefined && sameEntries(memo.entries, group.entries)) {
    next.set(key, memo);

    return memo.message;
  }

  const normalized = ExportedMessageRepository.fromBranchableArray([
    { message: groupMessage(group), parentId: null },
  ]).messages[0].message;

  next.set(key, { entries: group.entries, message: normalized });

  return normalized;
}

/** The branch repository for the entry array (design D3, memoized per D4):
 * every turn's assistant message chains linearly except regenerated
 * variants, which take the ORIGINAL turn's parent so the alternatives are
 * siblings; `headId` sits on the newest anchor's active variant, or on the
 * last message when nothing has variants. A group whose entries are
 * reference-equal to the previous build's reuses its message object — a
 * delta converts only the group it changed, and the assembled repository
 * matches `fromBranchableArray(items, { headId })` item for item. */
export function buildRepository(
  entries: readonly ChatEntry[],
  activeVariants?: ActiveVariants
): ExportedMessageRepository {
  const groups = groupEntries(entries);
  const anchors = variantAnchors(groups, activeVariants);

  // Slot turn id → its anchor; the original turn is slot 0 and included.
  const anchorByTurn = new Map<string, VariantAnchor>();

  for (const anchor of anchors) {
    for (const slot of anchor.slots) anchorByTurn.set(slot.turnID, anchor);
  }

  const items: {
    message: ThreadMessage;
    parentId: string | null;
  }[] = [];

  const nextMemos = new Map<string, MessageMemo>();

  // Original turn id → the parent its message got (its siblings re-use it).
  const originalParent = new Map<string, string | null>();
  // Turn id → the assistant message id (the head resolution reads it).
  const messageIDByTurn = new Map<string, string>();
  let previousID: string | null = null;

  for (const group of groups) {
    const message = memoizedMessage(group, nextMemos);

    let parentId = previousID;

    if (group.kind === "turn" && group.turnID !== undefined) {
      messageIDByTurn.set(group.turnID, message.id ?? group.id);

      const anchor = anchorByTurn.get(group.turnID);

      if (anchor !== undefined) {
        if (group.turnID === anchor.originalTurnID) {
          originalParent.set(group.turnID, parentId);
        } else {
          // A regenerated variant re-parents onto the ORIGINAL turn's
          // parent (design D3) — the anchor user message when the original
          // turn's own message is not in the window.
          const fallback =
            anchor.originalTurnID !== undefined
              ? originalParent.get(anchor.originalTurnID)
              : undefined;

          parentId = fallback !== undefined ? fallback : anchor.anchorID;
        }
      }
    }

    items.push({ message, parentId });
    previousID = message.id ?? null;
  }

  messageMemos = nextMemos;

  // The head sits on the last anchor's active variant; without variants the
  // head is simply the newest message.
  let head: string | null = previousID;
  const lastAnchor = anchors[anchors.length - 1];

  if (lastAnchor !== undefined) {
    const slot = lastAnchor.slots[lastAnchor.active];

    const id =
      slot !== undefined ? messageIDByTurn.get(slot.turnID) : undefined;

    if (id !== undefined) head = id;
  }

  // The same shape fromBranchableArray builds (headId always set, items
  // already normalized) — assembled here so the memoized items pass through
  // unchanged instead of being re-normalized per build.
  return { headId: head, messages: items };
}
