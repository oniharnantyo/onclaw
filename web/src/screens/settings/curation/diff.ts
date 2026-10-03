// Minimal line diff for the candidate review takeover (add-skill-curation-
// from-traces 9.2): before/after rendering of an edit proposal against the
// current skill content. A plain LCS over lines — SKILL.md bodies are small,
// so the O(n·m) table is fine and no dependency is added.

export type DiffLineKind = 'same' | 'added' | 'removed';

export interface DiffLine {
  kind: DiffLineKind;
  text: string;
}

/**
 * Line diff of `before` → `after` with unchanged lines kept as context.
 * Trailing-newline differences are ignored (both sides split identically).
 */
export function lineDiff(before: string, after: string): DiffLine[] {
  const a = before.split('\n');
  const b = after.split('\n');
  const n = a.length;
  const m = b.length;

  // LCS length table.
  const table: number[][] = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      table[i][j] = a[i] === b[j] ? table[i + 1][j + 1] + 1 : Math.max(table[i + 1][j], table[i][j + 1]);
    }
  }

  // Walk the table, emitting same/removed/added rows in document order.
  const out: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      out.push({ kind: 'same', text: a[i] });
      i++;
      j++;
    } else if (table[i + 1][j] >= table[i][j + 1]) {
      out.push({ kind: 'removed', text: a[i] });
      i++;
    } else {
      out.push({ kind: 'added', text: b[j] });
      j++;
    }
  }
  while (i < n) out.push({ kind: 'removed', text: a[i++] });
  while (j < m) out.push({ kind: 'added', text: b[j++] });
  return out;
}
