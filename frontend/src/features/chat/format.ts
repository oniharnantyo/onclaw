/**
 * Display formatting shared by the chat surface (compaction markers, context
 * ring): compact token counts (154k, 1.2M; below 1000 stays raw), and the
 * model family name (the last `/`-delimited segment of a model id) used
 * wherever space is tight — the full id stays in pickers and tooltips.
 */
export function shortModelName(modelId: string): string {
  const lastSlash = modelId.lastIndexOf("/");

  return lastSlash === -1 ? modelId : modelId.slice(lastSlash + 1);
}

export function formatTokens(n: number): string {
  if (n < 1000) return String(n);

  if (n < 1_000_000) {
    const k = Math.round(n / 100) / 10;

    return (Number.isInteger(k) ? String(k) : k.toFixed(1)) + "k";
  }

  const m = Math.round(n / 100_000) / 10;

  return (Number.isInteger(m) ? String(m) : m.toFixed(1)) + "M";
}
