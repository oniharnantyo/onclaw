/**
 * Parses an optional positive-integer form field: blank is 0 (provider
 * default), anything that is not a plain positive integer is null (a
 * validation error). The create wizard's max-tokens/context-window fields
 * and the config modal's Advanced section share this rule, so it lives at
 * feature level rather than being re-derived in both modals.
 */
export function parseOptionalInt(value: string): number | null {
  const trimmed = value.trim();

  if (!trimmed) return 0;

  const parsed = parseInt(trimmed, 10);

  if (Number.isNaN(parsed) || parsed <= 0 || String(parsed) !== trimmed) {
    return null;
  }

  return parsed;
}
