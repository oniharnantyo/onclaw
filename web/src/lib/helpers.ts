
import { PROVIDER_MODELS } from "../lib/constants";

export const cx = (...a: any[]) => a.filter(Boolean).join(' ');

let _uid = 100;
export const uid = (p: string) => p + '_' + (++_uid);

export const slugify = (name: string) => name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
export const fmtUses = (n: number) => (n >= 1000 ? (n / 1000).toFixed(1) + 'k' : String(n));
export const formatTokens = (n: number) => {
  if (n < 1000) return String(n);
  if (n < 1_000_000) {
    const k = Math.round(n / 100) / 10;
    if (k < 1000) return (Number.isInteger(k) ? String(k) : k.toFixed(1)) + 'k';
  }
  const m = Math.round(n / 100_000) / 10;
  return (Number.isInteger(m) ? String(m) : m.toFixed(1)) + 'M';
};
export const nowTime = () => new Date().toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });

// ---------------------------------------------------------------------------
// Live-data display formatting (change integrate-scheduler): scheduler runs
// and schedules carry ISO instants and millisecond durations on the wire; the
// tables render them in the workspace timezone with the prototype's monospace
// treatment handled by the callers.
// ---------------------------------------------------------------------------

/** Formats an ISO instant in the given IANA timezone; '—' when absent. */
export const fmtInTz = (iso: string | null | undefined, tz: string | undefined,
  opts: Intl.DateTimeFormatOptions): string => {
  const t = iso ? Date.parse(iso) : NaN;
  if (!Number.isFinite(t)) return '—';
  try {
    return new Intl.DateTimeFormat('en-US', { timeZone: tz, hour12: false, hourCycle: 'h23', ...opts })
      .format(new Date(t));
  } catch {
    // Unknown timezone — fall back to the browser's zone rather than failing.
    return new Intl.DateTimeFormat('en-US', { hour12: false, hourCycle: 'h23', ...opts }).format(new Date(t));
  }
};

/** Next fire instant, e.g. "Tue, 09:00". */
export const fmtNextRun = (iso: string | null | undefined, tz: string | undefined): string =>
  fmtInTz(iso, tz, { weekday: 'short', hour: '2-digit', minute: '2-digit' });

/** Run start stamp, e.g. "Sep 10, 07:00". */
export const fmtRunStarted = (iso: string | null | undefined, tz: string | undefined): string =>
  fmtInTz(iso, tz, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });

/** Coarse relative age for last-run cells, e.g. "just now", "2h ago". */
export const relativeTime = (iso: string | null | undefined): string => {
  const t = iso ? Date.parse(iso) : NaN;
  if (!Number.isFinite(t)) return '—';
  const mins = Math.floor((Date.now() - t) / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return mins + 'm ago';
  const hours = Math.floor(mins / 60);
  if (hours < 24) return hours + 'h ago';
  return Math.floor(hours / 24) + 'd ago';
};

/** Millisecond duration in the prototype's compact form: 42s, 2m 10s, 1h 04m. */
export const formatDuration = (ms: number | null | undefined): string => {
  if (typeof ms !== 'number' || !Number.isFinite(ms) || ms < 0) return '—';
  const s = Math.round(ms / 1000);
  if (s < 60) return s + 's';
  const m = Math.floor(s / 60);
  if (m < 60) return m + 'm ' + String(s % 60).padStart(2, '0') + 's';
  const h = Math.floor(m / 60);
  return h + 'h ' + String(m % 60).padStart(2, '0') + 'm';
};

export const memberHandle = (m: any) => (m.kind === 'agent' ? m.name : m.name.split(' ')[0]).toLowerCase();

// Ordered turn body (parts): reasoning segments and tool cards render in the
// stream order they occurred, so round-2 reasoning lands BETWEEN the tool
// cards and the final text. Shared by the live bridge and transcript hydration.
export const appendReasoningPart = (m: any, delta: string) => {
  const parts = m.parts || (m.parts = []);
  const tail = parts[parts.length - 1];
  // A whitespace-only delta with no open segment would mint a phantom empty
  // bubble — skip it (some providers emit blank reasoning chunks).
  if ((!tail || tail.k !== 'reasoning') && !delta.trim()) return;
  if (tail && tail.k === 'reasoning') tail.text += delta;
  else parts.push({ k: 'reasoning', text: delta });
};
export const appendToolPart = (m: any, index: number) => {
  const parts = m.parts || (m.parts = []);
  parts.push({ k: 'tool', i: index });
};
export const parseMentions = (text: string, members: any[]) => {
  const tokens = (text.match(/@([A-Za-z]+)/g) || []).map((t: any) => t.slice(1).toLowerCase());
  return (members || []).filter((m: any) => tokens.includes(memberHandle(m)));
};

export const providerOf = (model: string): string => {
  for (const [type, models] of Object.entries(PROVIDER_MODELS)) {
    if (models.includes(model)) return type;
  }
  if (model.startsWith('claude')) return 'anthropic';
  if (model.startsWith('gpt') || model.startsWith('o1') || model.startsWith('o3')) return 'openai';
  if (model.startsWith('gemini')) return 'gemini';
  if (model.startsWith('llama')) return 'openai-compatible';
  return 'anthropic';
};
