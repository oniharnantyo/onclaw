/**
 * Collapse-state store for tool cards (task 3.5, design D9): a module-level
 * bounded LRU map keyed by call id, not React state — a card re-mounted by
 * windowing, variant switches, or heavy-turn folding keeps the reader's
 * choice. Cards start collapsed (a missing entry reads as closed).
 *
 * Auto-open (design D9): `edit_file` and `write_file` open once on the
 * running→completed edge — the diff is the point of the card. An explicit
 * close always wins: it sets `userClosed`, which no later automatic open
 * reverses. Losing an entry (LRU eviction) only resets a card to collapsed;
 * the map is a cache, not state.
 */

/** Bound of the map (design D9: ZCode's ~800-entry number). */
export const COLLAPSE_MAP_LIMIT = 800;

/** Tools whose card auto-opens once when the call completes. */
export const AUTO_OPEN_TOOLS = new Set(["edit_file", "write_file"]);

/** One call's remembered disclosure state. */
interface CollapseEntry {
  /** Whether the card currently renders open. */
  open: boolean;
  /** The last status seen for the call (the transition detector's memory). */
  lastStatus: string | undefined;
  /** The automatic open already happened for this call. */
  autoOpened: boolean;
  /** The reader closed this card by hand — auto-open must never reverse it. */
  userClosed: boolean;
}

/** Insertion-ordered map doubling as the LRU (recent use re-inserts). */
const entries = new Map<string, CollapseEntry>();

function entryFor(callID: string): CollapseEntry {
  const existing = entries.get(callID);

  if (existing) {
    // LRU touch: re-inserting moves the key to the newest position.
    entries.delete(callID);
    entries.set(callID, existing);

    return existing;
  }

  const fresh: CollapseEntry = {
    open: false,
    lastStatus: undefined,
    autoOpened: false,
    userClosed: false,
  };

  entries.set(callID, fresh);

  if (entries.size > COLLAPSE_MAP_LIMIT) {
    // Evict the least-recently used (the map's oldest key).
    const oldest = entries.keys().next().value;

    if (oldest !== undefined) entries.delete(oldest);
  }

  return fresh;
}

/** Whether the card currently renders open; absent means collapsed. */
export function isOpen(callID: string): boolean {
  return entries.get(callID)?.open ?? false;
}

/** Applies the reader's explicit toggle. A close records `userClosed` so a
 * later terminal re-report cannot auto-open the card again. */
export function setOpen(callID: string, open: boolean): void {
  const entry = entryFor(callID);

  entry.open = open;

  if (!open) entry.userClosed = true;
}

/**
 * Folds one status observation for a call and reports whether the card
 * should auto-open now: exactly when an auto-open tool transitions
 * running→completed, the automatic open has not been spent, and the reader
 * has not closed the card by hand. Terminal records replayed from a reload
 * (no prior running observation) never trigger it.
 */
export function recordCallStatus(
  callID: string,
  toolName: string,
  status: string | undefined
): boolean {
  const entry = entryFor(callID);
  const transitioned = entry.lastStatus === "running" && status === "completed";

  entry.lastStatus = status;

  if (!transitioned || entry.autoOpened || entry.userClosed) return false;

  if (!AUTO_OPEN_TOOLS.has(toolName)) return false;

  entry.autoOpened = true;
  entry.open = true;

  return true;
}

/** Test/seed seam: resets the whole map (module state is otherwise process-
 * wide and each suite must start from cards-collapsed). */
export function resetCollapseMap(): void {
  entries.clear();
}
