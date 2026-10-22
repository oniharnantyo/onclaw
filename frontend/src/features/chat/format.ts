/**
 * Display formatting shared by the chat surface (compaction markers, context
 * ring): compact token counts (154k, 1.2M; below 1000 stays raw).
 */
export function formatTokens(n: number): string {
  if (n < 1000) return String(n);

  if (n < 1_000_000) {
    const k = Math.round(n / 100) / 10;

    return (Number.isInteger(k) ? String(k) : k.toFixed(1)) + "k";
  }

  const m = Math.round(n / 100_000) / 10;

  return (Number.isInteger(m) ? String(m) : m.toFixed(1)) + "M";
}
