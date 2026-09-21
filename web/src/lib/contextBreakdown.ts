// Context breakdown (assistant-ui "context breakdown" element adoption, D2).
//
// The wire carries only totals; no provider reports a per-category split. The
// four client segments below are ESTIMATES from what the transcript can see —
// rendered at face value, never scaled to fit the reported total. Everything
// the client cannot count (this turn's retrieved memory, persona docs, tool
// schemas, compaction summaries) is the Server context remainder:
// used − sum(estimates), floored at zero. When the wire does carry a
// server-provided context_breakdown, its numbers replace the client estimate
// for those segments and the "estimated" caption drops (D7). Headroom is
// always the real window minus the real used.

export interface ContextSegment {
  label: string;
  tokens: number;
  /** Shared verbatim by the bar slice and the legend dot, so they never disagree. */
  tint: string;
  /** True for a client estimate (the row carries the "estimated" caption);
   * false when the number came from the server's context_breakdown. */
  estimated: boolean;
}

export interface ContextSplit {
  segments: ContextSegment[];
  /** used − sum(segments), floored at zero — the server-composed share the
   * client cannot measure (memory, persona docs, tool schemas, summaries). */
  serverContext: number;
  /** window − used, floored at zero — computed, never passed in. */
  headroom: number;
}

/** Server-provided per-segment split (wire `usage.context_breakdown`,
 * omitempty — absent on every session before the backend ships D7). */
export interface ServerContextBreakdown {
  instructions?: number;
  tools?: number;
  files?: number;
  conversation?: number;
}

/** The last terminal turn's wire-reported details beyond the meter's
 * final-call input: the turn's input/output rows for the popover, and the
 * server-provided breakdown when the backend reports one (D7). Present-only —
 * fields the wire did not report stay absent from the usage record. */
export interface TurnUsageDetail {
  input?: number;
  output?: number;
  contextBreakdown?: ServerContextBreakdown;
}

// Estimator constants (design D2) — the same ~4 chars/token estimator the
// compaction divider uses; skew vs. provider BPE is bounded and identical.
const CHAR_PER_TOKEN = 4;
/** Role + framing overhead per transcript entry. */
const PER_MESSAGE_OVERHEAD = 8;
/** Rough definition cost per exposed tool. */
const TOOL_DEF_TOKENS = 120;
/** Rough header cost per attached skill. */
const SKILL_HEADER_TOKENS = 40;
/** Flat input-cost estimate per image attachment. */
const IMAGE_TOKENS = 1100;

const TINT_INSTRUCTIONS = 'bg-[color-mix(in_oklab,var(--fg)_32%,transparent)]';
const TINT_TOOLS = 'bg-[color-mix(in_oklab,var(--fg)_20%,transparent)]';
const TINT_FILES = 'bg-[color-mix(in_oklab,var(--fg)_10%,transparent)]';
const TINT_CONVERSATION = 'bg-accent';

const est = (s: string | undefined) => Math.ceil((s || '').length / CHAR_PER_TOKEN);

const estAttachment = (a: any) => {
  if (!a) return 0;
  if ((a.mime || '').startsWith('image/')) return IMAGE_TOKENS;
  return Math.max(64, Math.ceil((a.size || 0) / CHAR_PER_TOKEN));
};

/** The entries a session's messages put into the window AFTER the latest
 * compaction divider: pre-compaction bulk was summarized server-side (the
 * summary lands in Server context naturally), so only the entries after the
 * divider still sit verbatim in the model's window. */
export const postCompactionEntries = (messages: any[] | undefined): any[] => {
  const list = messages || [];
  for (let i = list.length - 1; i >= 0; i--) {
    if (list[i]?.author === 'compaction') return list.slice(i + 1);
  }
  return list;
};

/** Sums the text the post-compaction entries contribute: bodies, reasoning,
 * and tool-card args/results — the parts the runner re-sends every call.
 * Attachments are NOT counted here; they are the Files segment (no double
 * counting). */
const estConversation = (messages: any[] | undefined) =>
  postCompactionEntries(messages).reduce((sum, m) => {
    let t = PER_MESSAGE_OVERHEAD + est(m.text) + est(m.reasoning);
    for (const tool of m.tools || []) t += est(tool.args) + est(tool.res);
    return sum + t;
  }, 0);

const estFiles = (messages: any[] | undefined) =>
  postCompactionEntries(messages).reduce(
    (sum, m) => sum + (m.attachments || []).reduce((s: number, a: any) => s + estAttachment(a), 0),
    0
  );

const isFiniteNumber = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v) && v >= 0;

/**
 * Builds the breakdown for the context popover. Categories with no visible
 * share are excluded (a zero-width slice is noise). Segments stay at face
 * value: when their sum lands under `used`, the difference is the Server
 * context remainder; when it overshoots `used`, the remainder floors at zero
 * and the caption still reads "estimated" — the total is always the
 * provider's real number, never the estimate.
 */
export function computeContextBreakdown(
  agent: { prompt?: string; role?: string; tools?: string[]; skills?: string[] } | undefined,
  messages: any[] | undefined,
  used: number,
  effective: number,
  serverBreakdown?: ServerContextBreakdown | null,
): ContextSplit {
  const headroom = Math.max(0, (effective || 0) - (used || 0));

  // Face-value values per category: server numbers when the wire reported
  // them (estimated: false), the client estimate otherwise.
  const clientValues: Array<{ label: string; tokens: number; tint: string }> = [
    { label: 'Instructions', tokens: est(agent?.prompt) + est(agent?.role), tint: TINT_INSTRUCTIONS },
    {
      label: 'Tools & skills',
      tokens: (agent?.tools?.length || 0) * TOOL_DEF_TOKENS + (agent?.skills?.length || 0) * SKILL_HEADER_TOKENS,
      tint: TINT_TOOLS,
    },
    { label: 'Files', tokens: estFiles(messages), tint: TINT_FILES },
    { label: 'Conversation', tokens: estConversation(messages), tint: TINT_CONVERSATION },
  ];
  const segments: ContextSegment[] = clientValues
    .map(({ label, tokens, tint }) => {
      const server = serverBreakdown ? (serverBreakdown as any)[labelToKey(label)] : undefined;
      return isFiniteNumber(server)
        ? { label, tokens: server, tint, estimated: false }
        : { label, tokens, tint, estimated: true };
    })
    .filter((seg) => seg.tokens > 0);

  const total = segments.reduce((s, seg) => s + seg.tokens, 0);
  const serverContext = Math.max(0, (used || 0) - total);
  return { segments, serverContext, headroom };
}

const labelToKey = (label: string): keyof ServerContextBreakdown =>
  label === 'Tools & skills' ? 'tools' : (label.toLowerCase() as keyof ServerContextBreakdown);
