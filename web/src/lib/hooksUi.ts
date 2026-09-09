// Shared hook vocabulary for the Hooks pane and the agent-config Hooks
// section (integrate-agent-hooks). Pure functions and constants over the API
// shapes — no fetching, no DOM — so both surfaces and their tests read one
// source of truth for the event semantics (D1), the event-aware candidate
// sets, and the plain-string matcher readings (D19).
import type { ApiHook, HookEvent } from './api';

// ---------------------------------------------------------------------------
// Events (D1): fires-on copy and the two blocking seams
// ---------------------------------------------------------------------------

export interface HookEventMeta {
  value: HookEvent;
  label: string;
  /** The dialog's fires-on helper line: when it fires and whether it blocks. */
  firesOn: string;
  blocking: boolean;
  /** Which value family the applies-to editor shows for this event (D8). */
  valueKind: 'tools' | 'origins' | 'statuses';
}

export const HOOK_EVENTS: HookEventMeta[] = [
  {
    value: 'run_started',
    label: 'run_started',
    firesOn: 'Fires when a run starts (observes only — never blocks).',
    blocking: false,
    valueKind: 'origins',
  },
  {
    value: 'user_prompt_submit',
    label: 'user_prompt_submit',
    firesOn: 'Fires before the model sees a submitted prompt — can block the whole turn.',
    blocking: true,
    valueKind: 'origins',
  },
  {
    value: 'pre_tool_use',
    label: 'pre_tool_use',
    firesOn: 'Fires before every tool call — can block individual calls.',
    blocking: true,
    valueKind: 'tools',
  },
  {
    value: 'post_tool_use',
    label: 'post_tool_use',
    firesOn: 'Fires after each tool call completes (observes only — never blocks).',
    blocking: false,
    valueKind: 'tools',
  },
  {
    value: 'run_finished',
    label: 'run_finished',
    firesOn: 'Fires when a run reaches any terminal outcome (observes only — never blocks).',
    blocking: false,
    valueKind: 'statuses',
  },
];

export function hookEventMeta(event: string): HookEventMeta {
  return HOOK_EVENTS.find((e) => e.value === event) ?? HOOK_EVENTS[0];
}

// ---------------------------------------------------------------------------
// Event-aware candidate sets (D19): what a matcher's value space is per event
// ---------------------------------------------------------------------------

export const HOOK_ORIGIN_VALUES = ['user', 'cron', 'channel'];
export const HOOK_STATUS_VALUES = ['completed', 'failed', 'cancelled'];

/** One candidate value the matcher counts against: exact names plus family
 * entries (trailing ".*" prefix globs). */
export interface HookValueOption {
  value: string;
  label: string;
  family?: boolean;
}

/** The candidates for a tool event: the workspace-visible tool names (catalog)
 * plus the family entries — `browser.*` for the browser facade expansion and
 * one `mcp__<server>.*` per workspace MCP server (the same sanitized segment
 * the runtime computes, mirroring internal/agents/mcp/naming.go
 * sanitizeSegment). */
export function hookToolValueOptions(toolNames: { key: string; name: string }[], mcpServers: { id: string; name: string }[]): HookValueOption[] {
  const options: HookValueOption[] = toolNames.map((t) => ({ value: t.key, label: t.name }));
  const families: HookValueOption[] = [{ value: 'browser.*', label: 'Browser family', family: true }];
  for (const s of mcpServers) {
    const sanitized = sanitizeMcpSegment(s.name);
    if (!sanitized) continue;
    families.push({ value: `mcp__${sanitized}.*`, label: `${s.name} family`, family: true });
  }
  return [...options, ...families];
}

function sanitizeMcpSegment(seg: string): string {
  return seg
    .toLowerCase()
    .replace(/[^a-z0-9_-]/g, '_');
}

export function hookValueOptionsFor(event: string, toolNames: { key: string; name: string }[], mcpServers: { id: string; name: string }[]): HookValueOption[] {
  const kind = hookEventMeta(event).valueKind;
  if (kind === 'tools') return hookToolValueOptions(toolNames, mcpServers);
  if (kind === 'statuses') return HOOK_STATUS_VALUES.map((v) => ({ value: v, label: v }));
  return HOOK_ORIGIN_VALUES.map((v) => ({ value: v, label: v }));
}

/** The plural noun the live count reads against ("Matches 2 of 24 tools",
 * "… of 3 origins", "… of 3 outcomes" — the gallery's word for statuses). */
export function hookValueNoun(event: string): string {
  const kind = hookEventMeta(event).valueKind;
  return kind === 'tools' ? 'tools' : kind === 'origins' ? 'origins' : 'outcomes';
}

// ---------------------------------------------------------------------------
// The plain matcher string (D19): tier reading and the live count
// ---------------------------------------------------------------------------

/** The tool-entry grammar: charset [A-Za-z0-9_.-]+ with at most one trailing
 * ".*" wildcard — the same shape the backend save-time rule pins. */
export const HOOK_TOOL_ENTRY_PATTERN = /^[A-Za-z0-9_.-]+(\.\*)?$/;

/** Matcher length cap for the regex tier (the backend's save-time rule). */
export const HOOK_MATCHER_MAX_LENGTH = 256;

/** Split a matcher string on `,` `|` whitespace. */
export function splitMatcherEntries(raw: string): string[] {
  return raw
    .trim()
    .split(/[|,]|\s+/)
    .filter(Boolean);
}

export type MatcherTier = 'all' | 'list' | 'regex';

/** Which reading a matcher string takes (D19): empty or `*` = every
 * occurrence; every entry fitting the tool-entry charset = exact/family list;
 * anything else = the whole string is an unanchored regex. */
export function matcherTier(raw: string): MatcherTier {
  const t = raw.trim();
  if (!t || t === '*') return 'all';
  const entries = splitMatcherEntries(t);
  return entries.every((e) => HOOK_TOOL_ENTRY_PATTERN.test(e)) ? 'list' : 'regex';
}

/** Client-side live count: how many of the event's candidates the matcher
 * string selects — the dialog's preview of the server's save-time report.
 * Returns null when the string cannot count (invalid or over-long regex). */
export function countMatcherString(raw: string, options: HookValueOption[]): number | null {
  const tier = matcherTier(raw);
  if (tier === 'all') return options.length;
  if (tier === 'list') {
    const prefixes: string[] = [];
    const exact = new Set<string>();
    for (const e of splitMatcherEntries(raw)) {
      if (e.endsWith('.*')) prefixes.push(e.slice(0, -2));
      else exact.add(e);
    }
    return options.filter((o) => exact.has(o.value) || prefixes.some((p) => o.value.startsWith(p))).length;
  }
  const pattern = raw.trim();
  if (pattern.length > HOOK_MATCHER_MAX_LENGTH) return null;
  try {
    const re = new RegExp(pattern);
    return options.filter((o) => re.test(o.value)).length;
  } catch {
    return null;
  }
}

// ---------------------------------------------------------------------------
// Row summaries
// ---------------------------------------------------------------------------

/** The row summary IS the plain matcher string (D19). Empty/whitespace
 * renders as `*` — the gallery's canonical match-all reading. */
export function matcherSummary(hook: Pick<ApiHook, 'matcher'>): string {
  const raw = typeof hook.matcher === 'string' ? hook.matcher.trim() : '';
  return raw || '*';
}

export function hookHandlerLabel(handlerType: string): string {
  switch (handlerType) {
    case 'http':
      return 'Webhook';
    case 'command':
      return 'Command';
    case 'mcp_tool':
      return 'MCP tool';
    case 'prompt':
      return 'Evaluator';
    case 'script':
      // Short mono chip — the in-process JS lane reads best terse (D22).
      return 'JS';
    default:
      return handlerType;
  }
}

/** Health view of a hook row: the enabled switch wins (Disabled), then the
 * delivery status — ok green, error red with its message for the tooltip. */
export function hookStatusView(hook: Pick<ApiHook, 'enabled' | 'status' | 'status_error'>): {
  dot: string;
  label: string;
  errored: boolean;
} {
  if (!hook.enabled) return { dot: 'bg-muted', label: 'Disabled', errored: false };
  if (hook.status === 'error') return { dot: 'bg-danger', label: 'Error', errored: true };
  return { dot: 'bg-success', label: 'Healthy', errored: false };
}

export const HOOK_LEVEL_LABEL: Record<string, string> = {
  instance: 'Instance',
  workspace: 'Workspace',
  agent: 'Agent',
};
