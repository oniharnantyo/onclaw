/**
 * Variant turns → branch tree (task 2.2, design D3): the regenerated turns
 * of one prompt become sibling assistant messages under the ORIGINAL turn's
 * parent (the anchor user message), and `ExportedMessageRepository.
 * fromBranchableArray` builds the repository the adapter hands the runtime,
 * with `headId` on the active variant. Pure derivation over the same entry
 * groups the message mapping uses — no store, React, or transport imports.
 *
 * Head resolution per anchor: an explicit override in `activeVariants`
 * (original turn id → slot index), else the anchor's own `activeVariant`
 * (the store-set state after `setActiveVariant`), else the newest slot —
 * the same "newest live, last-marked on replay" default the reducer uses.
 */
import {
  ExportedMessageRepository,
  type ThreadMessageLike,
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

/** The branch repository for the entry array (design D3): every turn's
 * assistant message chains linearly except regenerated variants, which take
 * the ORIGINAL turn's parent so the alternatives are siblings; `headId`
 * sits on the newest anchor's active variant, or on the last message when
 * nothing has variants. */
export function entriesToRepository(
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
    message: ThreadMessageLike;
    parentId: string | null;
  }[] = [];

  // Original turn id → the parent its message got (its siblings re-use it).
  const originalParent = new Map<string, string | null>();
  // Turn id → the assistant message id (the head resolution reads it).
  const messageIDByTurn = new Map<string, string>();
  let previousID: string | null = null;

  for (const group of groups) {
    const message = groupMessage(group);
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

  return ExportedMessageRepository.fromBranchableArray(items, { headId: head });
}
