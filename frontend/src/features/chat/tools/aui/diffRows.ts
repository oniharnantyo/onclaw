/**
 * Diff rows for the vendored `CodeDiff` element (task 4.2, design D6): the
 * row builder lifted out of `tools/primitives/DiffView.tsx` as a pure helper
 * — same jsdiff pass, same deletion-before-addition unified pairing, same
 * old/new-file gutter numbers — reshaped into the element's `{ kind, text }`
 * lines plus the numbers the element does not carry, so the edit_file
 * renderer and the fold's change statistics share one computation.
 *
 * The element renders its line list verbatim, so the viewport the legacy
 * DiffView computed around its table lives here too (`diffViewport`):
 * unchanged runs collapse to `DIFF_CONTEXT` lines each side behind an expand
 * seam, and the rendered slice caps at `DIFF_ROW_CAP` behind "Show all"
 * (spec chat-tool-cards "Edit renders a diff": unchanged surrounding lines
 * are collapsed to brief context the user can expand).
 */
import { diffLines } from "diff";

import type { DiffLine } from "@/components/assistant-ui/elements/code-diff";

/** Rendered-row cap (the legacy DiffView's design D4 number); beyond it the
 * view truncates behind "Show all". */
export const DIFF_ROW_CAP = 400;

/** Context lines kept at each side of a collapsed unchanged run. */
export const DIFF_CONTEXT = 3;

/** One collapsed unchanged run: its first row in the full list (the
 * expansion key) and how many rows it hides. */
export interface DiffSeam {
  start: number;
  count: number;
}

/** What the element renders now, and what its controls reveal. */
export interface DiffViewport {
  /** The rows to hand the element, already collapsed and capped. */
  lines: readonly DiffRow[];
  /** The collapsed runs, in document order; empty once expanded. */
  seams: readonly DiffSeam[];
  /** Rows the cap holds back (0 until the collapsed list exceeds it). */
  hidden: number;
}

/** One unified row: the element's line plus the gutter numbers it does not
 * carry (`oldNo` in the old file, `newNo` in the new one — null on the side
 * the row does not exist in, so removals carry only `oldNo` and additions
 * only `newNo`, the legacy DiffView's convention). */
export interface DiffRow extends DiffLine {
  oldNo: number | null;
  newNo: number | null;
}

/** Flattens jsdiff's change parts into unified rows (deletions first, then
 * the additions they became, then the shared context), numbering each row
 * from the old and the new file as it walks. */
function buildRows(oldText: string, newText: string): DiffRow[] {
  const parts = diffLines(oldText, newText);
  const rows: DiffRow[] = [];

  let oldNo = 1;
  let newNo = 1;

  for (const part of parts) {
    const lines = part.value.replace(/\n$/, "").split("\n");

    if (part.added) {
      for (const text of lines) {
        rows.push({ kind: "added", text: text, oldNo: null, newNo: newNo });
        newNo += 1;
      }
    } else if (part.removed) {
      for (const text of lines) {
        rows.push({ kind: "removed", text: text, oldNo: oldNo, newNo: null });
        oldNo += 1;
      }
    } else {
      for (const text of lines) {
        rows.push({ kind: "context", text: text, oldNo: oldNo, newNo: newNo });
        oldNo += 1;
        newNo += 1;
      }
    }
  }

  return rows;
}

/** The added/removed counts an edit header shows. */
export interface DiffCounts {
  additions: number;
  deletions: number;
}

/** One edit's rows: the element's line shape plus the gutter numbers (a
 * `DiffRow` is a `DiffLine`, so the element takes them as they are). */
export function buildDiffLines(oldText: string, newText: string): DiffRow[] {
  return buildRows(oldText, newText);
}

/** The added/removed counts an edit header shows, from the same rows. */
export function countDiffLines(lines: readonly DiffLine[]): DiffCounts {
  let additions = 0;
  let deletions = 0;

  for (const line of lines) {
    if (line.kind === "added") additions += 1;
    else if (line.kind === "removed") deletions += 1;
  }

  return { additions, deletions };
}

/** The element's visible slice: an unchanged run longer than eight rows
 * collapses to `DIFF_CONTEXT` rows each side plus a seam (unless the run was
 * expanded — `start` in `expanded` — or the whole view was uncapped), and
 * anything past `DIFF_ROW_CAP` waits behind "Show all". */
export function diffViewport(
  lines: readonly DiffRow[],
  expanded: ReadonlySet<number>,
  showAll: boolean
): DiffViewport {
  if (showAll) return { lines, seams: [], hidden: 0 };

  const visible: DiffRow[] = [];
  const seams: DiffSeam[] = [];
  // Each seam's offset in `visible`, so a seam the cap cuts away is not
  // offered as a control that would change nothing.
  const offsets: number[] = [];

  let i = 0;

  while (i < lines.length) {
    if (lines[i].kind !== "context") {
      visible.push(lines[i]);
      i += 1;

      continue;
    }

    let end = i;

    while (end < lines.length && lines[end].kind === "context") end += 1;

    const run = end - i;

    if (run > DIFF_CONTEXT * 2 + 2 && !expanded.has(i)) {
      for (let k = i; k < i + DIFF_CONTEXT; k += 1) visible.push(lines[k]);

      seams.push({ start: i, count: run - DIFF_CONTEXT * 2 });
      offsets.push(visible.length);

      for (let k = end - DIFF_CONTEXT; k < end; k += 1) visible.push(lines[k]);
    } else {
      for (let k = i; k < end; k += 1) visible.push(lines[k]);
    }

    i = end;
  }

  // The cap applies to what the element renders, after the collapse.
  const hidden = Math.max(0, visible.length - DIFF_ROW_CAP);

  const kept = seams.filter(
    (_seam: DiffSeam, idx: number): boolean => offsets[idx] < DIFF_ROW_CAP
  );

  return { lines: visible.slice(0, DIFF_ROW_CAP), seams: kept, hidden };
}
