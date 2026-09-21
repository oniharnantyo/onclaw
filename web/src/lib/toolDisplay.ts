// Declarative tool-card formatter layer (change human-readable-tool-cards):
// collapsed one-liner sentences, expanded field specs, and result shaping for
// the single tool-card renderer (ToolCall.tsx). Pure functions over the card
// item's raw `args`/`res` strings — no fetching, no caching, no DOM.
//
// Design decisions cite openspec/changes/human-readable-tool-cards/design.md:
//   D1 — declarative verb table, never mechanical conjugation; MCP/unknown
//        tools get NO sentence (invented English is a trap).
//   D2 — one-liner is a state machine over the card's own data; facts derive
//        only from trusted envelopes; unparseable args mint nothing.
//   D3 — field specs render present-only; arg keys outside the spec append
//        humanized at the end (no silent drops).
//   D4 — results shape by envelope kind: search list, snapshot table,
//        snapshot-refresh line, kv rows, list, clamped text, empty.
//   D5 — ref→name resolution walks sibling cards newest-snapshot-first.
// One-liner strings carry a tiny markup the renderer parses into styled
// spans: `` `x` `` renders as a monospace chip, `'x'` as a quoted literal.
// Verbatim sentences (shell commands) carry no wrapping.

export type FieldKind = 'chip' | 'quote' | 'content' | 'enum' | 'plain';

export interface FieldSpec {
  key: string;
  label: string;
  kind: FieldKind;
}

export interface FieldRow {
  label: string;
  kind: FieldKind;
  value: string;
}

export type ResultView =
  | { kind: 'search_results'; results: { title: string; url: string }[] }
  | { kind: 'snapshot'; title: string; elements: { ref: string; role: string; name: string; value: string }[] }
  | { kind: 'snapshot_refresh'; elementCount: number }
  | { kind: 'kv'; rows: FieldRow[] }
  | { kind: 'list'; items: string[] }
  | { kind: 'text'; text: string }
  | { kind: 'empty' };

/** Catalog tool keys mirrored from internal/agents/tool_catalog.go — the
 * coverage-guard test (tasks 4.2) pins this list so a new backend tool
 * cannot ship without a table entry. */
export const CATALOG_TOOL_KEYS = [
  'ls',
  'read_file',
  'write_file',
  'edit_file',
  'glob',
  'grep',
  'delete_file',
  'document.read',
  'document.create',
  'execute',
  'memory',
  'web.search',
  'web.fetch',
  'browser',
  'schedule',
  // Todos (adopt-assistant-ui-elements): todo_write/todo_read back the
  // checklist card. The echo-UI tools (ui.chart/ui.timeline/ui.preview) were
  // removed — rich cards now come from markdown fences (markdown-fences).
  'todo_write',
  'todo_read',
] as const;

/** Browser facade member ids (runtime expansions of the `browser` alias). */
export const FACADE_TOOL_IDS = [
  'browser.navigate',
  'browser.act',
  'browser.read',
  'browser.screenshot',
  'browser.snapshot',
  'browser.click',
  'browser.type',
  'browser.hover',
  'browser.drag',
  'browser.select_option',
] as const;

// ---------------------------------------------------------------------------
// Sentence table (D1)
// ---------------------------------------------------------------------------

/** One sentence entry: intent (running / error) + outcome (done) templates
 * with an optional inline object and an optional trusted-fact derivation.
 * Templates interpolate `{object}`. */
export interface SentenceEntry {
  intent: string;
  outcome: string;
  object?: { key: string; style: 'chip' | 'quote' | 'verbatim' };
  /** Extra fact appended at done only — e.g. " — 8 results". Returns null
   * when the envelope does not trustably yield the fact (D2). */
  fact?: (ctx: { args: Record<string, unknown>; res?: string }) => string | null;
}

/** Table value: a plain entry, or action-driven variants (memory) dispatched
 * on `args.action` with an optional fallback (absent → no sentence). */
export type SentenceTableEntry = SentenceEntry | { byAction: Record<string, SentenceEntry> };

/** Per-tool sentence table. Exported for the coverage-guard test (4.2);
 * renderers should call toolOneLiner, never read this directly. */
export const SENTENCE_TABLE: Record<string, SentenceTableEntry> = {
  ls: {
    intent: 'Listing {object}',
    outcome: 'Listed {object}',
    object: { key: 'path', style: 'chip' },
  },
  read_file: {
    intent: 'Reading {object}',
    outcome: 'Read {object}',
    object: { key: 'file_path', style: 'chip' },
  },
  write_file: {
    // Irregular verb on purpose (D1) — "Writed" is not English.
    intent: 'Writing {object}',
    outcome: 'Wrote {object}',
    object: { key: 'file_path', style: 'chip' },
    fact: writeFileFact,
  },
  edit_file: {
    intent: 'Editing {object}',
    outcome: 'Edited {object}',
    object: { key: 'file_path', style: 'chip' },
  },
  glob: {
    intent: 'Finding files matching {object}',
    outcome: 'Found files matching {object}',
    object: { key: 'pattern', style: 'quote' },
  },
  grep: {
    intent: 'Grepping for {object}',
    outcome: 'Grepped for {object}',
    object: { key: 'pattern', style: 'quote' },
  },
  delete_file: {
    intent: 'Deleting {object}',
    outcome: 'Deleted {object}',
    object: { key: 'file_path', style: 'chip' },
  },
  'document.read': {
    intent: 'Reading document {object}',
    outcome: 'Read document {object}',
    object: { key: 'path', style: 'chip' },
    fact: documentReadFact,
  },
  'document.create': {
    intent: 'Creating document {object}',
    outcome: 'Created document {object}',
    object: { key: 'path', style: 'chip' },
    fact: documentCreateFact,
  },
  // Command-as-sentence (D1): the command IS the one-liner, verbatim, no
  // wrapping — the card's tool name already says "Shell".
  execute: {
    intent: '{object}',
    outcome: '{object}',
    object: { key: 'command', style: 'verbatim' },
  },
  memory: {
    // Action-driven variants (D1): memory's verbs follow args.action.
    byAction: {
      append: {
        intent: 'Appending to {object}',
        outcome: 'Appended to {object}',
        object: { key: 'path', style: 'chip' },
      },
      read: {
        intent: 'Reading {object}',
        outcome: 'Read {object}',
        object: { key: 'path', style: 'chip' },
      },
    },
  },
  schedule: {
    // Action-driven variants (D1): schedule's verbs follow args.action.
    // update/delete address a schedule by id (the name is optional/absent
    // there), so they identify the object as an id chip; create always
    // carries the name.
    byAction: {
      create: {
        intent: 'Scheduling {object}',
        outcome: 'Scheduled {object}',
        object: { key: 'name', style: 'quote' },
        fact: scheduleLabelFact,
      },
      list: {
        intent: 'Listing schedules',
        outcome: 'Listed schedules',
        fact: scheduleListFact,
      },
      update: {
        intent: 'Updating schedule {object}',
        outcome: 'Updated schedule {object}',
        object: { key: 'id', style: 'chip' },
        fact: scheduleLabelFact,
      },
      delete: {
        intent: 'Deleting schedule {object}',
        outcome: 'Deleted schedule {object}',
        object: { key: 'id', style: 'chip' },
      },
    },
  },
  'web.search': {
    intent: 'Searching for {object}',
    outcome: 'Searched for {object}',
    object: { key: 'query', style: 'quote' },
    fact: searchResultFact,
  },
  'web.fetch': {
    intent: 'Fetching {object}',
    outcome: 'Fetched {object}',
    object: { key: 'url', style: 'chip' },
  },
  // The facade alias never appears on runtime cards (they use member ids)
  // but keeps a bare-verb entry so the coverage guard holds for the full
  // backend catalog universe.
  browser: { intent: 'Driving the browser', outcome: 'Drove the browser' },
  'browser.navigate': {
    intent: 'Navigating to {object}',
    outcome: 'Navigated to {object}',
    object: { key: 'url', style: 'chip' },
  },
  'browser.act': {
    intent: 'Acting on {object}',
    outcome: 'Acted on {object}',
    object: { key: 'selector', style: 'chip' },
  },
  'browser.read': { intent: 'Reading the page', outcome: 'Read the page' },
  'browser.screenshot': { intent: 'Taking a screenshot', outcome: 'Took a screenshot' },
  'browser.snapshot': { intent: 'Capturing a page snapshot', outcome: 'Captured a page snapshot' },
  'browser.click': {
    intent: 'Clicking {object}',
    outcome: 'Clicked {object}',
    object: { key: 'ref', style: 'chip' },
  },
  'browser.type': {
    intent: 'Typing into {object}',
    outcome: 'Typed into {object}',
    object: { key: 'ref', style: 'chip' },
  },
  'browser.hover': {
    intent: 'Hovering {object}',
    outcome: 'Hovered {object}',
    object: { key: 'ref', style: 'chip' },
  },
  'browser.drag': {
    intent: 'Dragging from {object}',
    outcome: 'Dragged from {object}',
    object: { key: 'ref', style: 'chip' },
  },
  'browser.select_option': {
    intent: 'Selecting in {object}',
    outcome: 'Selected in {object}',
    object: { key: 'ref', style: 'chip' },
  },
  // Todos (adopt-assistant-ui-elements). These tools render dedicated
  // generative-UI cards when their envelopes parse; the sentences below carry
  // the collapsed/error fallbacks so the coverage guard holds.
  todo_write: {
    intent: 'Updating the plan',
    outcome: 'Updated the plan',
    fact: todoWriteFact,
  },
  todo_read: {
    intent: 'Reading the plan',
    outcome: 'Read the plan',
    fact: todoReadFact,
  },
};

// ---------------------------------------------------------------------------
// Arg parsing + one-liner state machine (D2)
// ---------------------------------------------------------------------------

/** Safe JSON object parse; returns null on parse failure or non-object
 * (streaming partial JSON edge, design D2). Arrays and scalars are not arg
 * objects. */
export function parseArgs(raw: string | undefined | null): Record<string, unknown> | null {
  if (!raw) return null;
  try {
    const v = JSON.parse(raw);
    if (v !== null && typeof v === 'object' && !Array.isArray(v)) {
      return v as Record<string, unknown>;
    }
    return null;
  } catch {
    return null;
  }
}

/** Resolve the sentence entry for a tool id: MCP/unknown ids → null (D1 —
 * never invent English); memory dispatches on args.action. */
function entryFor(id: string, args: Record<string, unknown>): SentenceEntry | null {
  const entry = SENTENCE_TABLE[id];
  if (!entry) return null;
  if (!('byAction' in entry)) return entry;
  const action = typeof args.action === 'string' ? args.action : '';
  return entry.byAction[action] ?? null;
}

/** Interpolate `{object}` from args. A missing/empty object value drops the
 * phrase and any dangling connector ("Navigating to" → "Navigating") so the
 * bare form stays grammatical without doubling the table. */
function renderSentence(
  template: string,
  object: SentenceEntry['object'],
  args: Record<string, unknown>
): string {
  if (!template.includes('{object}')) return template;
  const raw = object ? args[object.key] : undefined;
  const text = typeof raw === 'string' ? raw : typeof raw === 'number' ? String(raw) : '';
  if (!text.trim()) {
    return template
      .replace('{object}', '')
      .replace(/\s+(to|for|in|into|on|of|from|at)$/i, '')
      .trim();
  }
  const styled =
    object?.style === 'quote' ? `'${text}'` : object?.style === 'chip' ? `\`${text}\`` : text;
  return template.replace('{object}', styled).trim();
}

// Trusted-fact derivations (D2). Facts read only typed envelopes — never
// freeform output — and yield null when the envelope is absent/unparseable.

function searchResultFact(ctx: { args: Record<string, unknown>; res?: string }): string | null {
  const parsed = parseJsonObject(ctx.res);
  if (!parsed || !Array.isArray(parsed.results)) return null;
  return ` — ${parsed.results.length} results`;
}

function writeFileFact(ctx: { args: Record<string, unknown>; res?: string }): string | null {
  const content = ctx.args.content;
  if (typeof content !== 'string') return null;
  return ` · ${humanBytes(content.length)}`;
}

/** document.read fact: the backend's own truncation marker inside the trusted
 * markdown envelope — appended only when the conversion output was capped.
 * Anything else (plain reads, failures, absent envelope) appends nothing. */
function documentReadFact(ctx: { args: Record<string, unknown>; res?: string }): string | null {
  const parsed = parseJsonObject(ctx.res);
  const markdown = typeof parsed?.markdown === 'string' ? parsed.markdown : '';
  return markdown.includes('Output truncated at') ? ' · truncated' : null;
}

/** document.create fact: the created document's format from the trusted
 * result envelope — "Created document `invoice.xlsx` · xlsx". Absent or
 * unparseable envelopes append nothing (D2). */
function documentCreateFact(ctx: { args: Record<string, unknown>; res?: string }): string | null {
  const parsed = parseJsonObject(ctx.res);
  const format = typeof parsed?.format === 'string' ? parsed.format : '';
  return format ? ` · ${format}` : null;
}

/** Download URL for a document.create result envelope: the capability URL the
 * backend stamps on successful delivery, or null when absent/unparseable. */
export function createdDocumentURL(rawResult: string | undefined | null): string | null {
  const parsed = parseJsonObject(rawResult);
  const url = typeof parsed?.url === 'string' ? parsed.url : '';
  return url ? url : null;
}

/** schedule create/update fact: the trusted envelope's backend-derived
 * schedule label ("09:00 · Mon–Fri", or the one-shot RFC3339 instant), plus
 * the channel target when the call delivered to one — "Scheduled
 * 'morning-digest' · 09:00 · Mon–Fri → `ch-ops`". */
function scheduleLabelFact(ctx: { args: Record<string, unknown>; res?: string }): string | null {
  const parsed = parseJsonObject(ctx.res);
  const label = typeof parsed?.schedule === 'string' ? parsed.schedule : '';
  if (!label) return null;
  let fact = ` · ${label}`;
  if (
    ctx.args.delivery === 'channel' &&
    typeof ctx.args.channel_id === 'string' &&
    ctx.args.channel_id
  ) {
    fact += ` → \`${ctx.args.channel_id}\``;
  }
  return fact;
}

/** schedule list fact: the workspace schedule count from the trusted list
 * envelope; absent (null) when the list is empty. */
function scheduleListFact(ctx: { args: Record<string, unknown>; res?: string }): string | null {
  const parsed = parseJsonObject(ctx.res);
  if (!parsed || !Array.isArray(parsed.schedules) || parsed.schedules.length === 0) return null;
  const n = parsed.schedules.length;
  return ` — ${n} schedule${n === 1 ? '' : 's'}`;
}

/** todo_write fact: the item count from the args the checklist card renders —
 * the model's declared plan IS what the card shows. Empty/absent items mint
 * nothing. */
function todoWriteFact(ctx: { args: Record<string, unknown>; res?: string }): string | null {
  const n = Array.isArray(ctx.args.items) ? ctx.args.items.length : 0;
  return n > 0 ? ` — ${n} item${n === 1 ? '' : 's'}` : null;
}

/** todo_read fact: the item count from the trusted result envelope. */
function todoReadFact(ctx: { args: Record<string, unknown>; res?: string }): string | null {
  const parsed = parseJsonObject(ctx.res);
  const n = parsed && Array.isArray(parsed.items) ? parsed.items.length : 0;
  return n > 0 ? ` — ${n} item${n === 1 ? '' : 's'}` : null;
}

/** "Wrote `notes.md` · 1.2 kB" size format: B below 1 kB, one decimal above
 * (trailing .0 trimmed). */
function humanBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const units = ['kB', 'MB', 'GB', 'TB'];
  let value = n;
  let i = -1;
  do {
    value /= 1024;
    i += 1;
  } while (value >= 1024 && i < units.length - 1);
  const text = value >= 100 ? String(Math.round(value)) : value.toFixed(1).replace(/\.0$/, '');
  return `${text} ${units[i]}`;
}

/** Card latency format: milliseconds below one second, seconds above —
 * "500 ms", "1 s", "2.2 s", "11.3 s". One decimal on seconds with a
 * trailing .0 trimmed; seconds never roll over into minutes. */
export function formatLatency(ms: number): string {
  const rounded = Math.round(ms);
  if (!Number.isFinite(rounded) || rounded <= 0) return '0 ms';
  if (rounded < 1000) return `${rounded} ms`;
  const s = rounded / 1000;
  const text = s >= 100 ? String(Math.round(s)) : s.toFixed(1).replace(/\.0$/, '');
  return `${text} s`;
}

/** One-liner state machine (D2): running → intent form; done → outcome form
 * + facts; done with error → intent form (the renderer styles it as an
 * error — past tense would assert success). Returns null when the tool has
 * no table entry (MCP/unknown — NEVER invent English, D1) or when args are
 * absent/unparseable (mid-stream edge → renderer falls back to name + dots). */
export function toolOneLiner(
  name: string,
  card: { args?: string; res?: string; error?: boolean },
  running: boolean
): string | null {
  const args = parseArgs(card?.args);
  if (!args) return null;
  const entry = entryFor(name, args);
  if (!entry) return null;
  const done = !running && card?.error !== true;
  const sentence = renderSentence(done ? entry.outcome : entry.intent, entry.object, args);
  if (!sentence) return null;
  if (done && entry.fact) {
    const fact = entry.fact({ args, res: card?.res });
    if (fact) return sentence + fact;
  }
  return sentence;
}

// ---------------------------------------------------------------------------
// Expanded-view field specs (D3)
// ---------------------------------------------------------------------------

/** Per-tool expanded field spec; null for tools without an entry (renderer
 * uses genericRows). Keys not listed here still render — fieldRows appends
 * them humanized (D3, no silent drops). Specs list only backend-verified arg
 * keys; optional knobs outside the verified set degrade to appended rows. */
const FIELD_SPECS: Record<string, FieldSpec[]> = {
  ls: [{ key: 'path', label: 'Path', kind: 'chip' }],
  read_file: [{ key: 'file_path', label: 'File', kind: 'chip' }],
  write_file: [
    { key: 'file_path', label: 'File', kind: 'chip' },
    { key: 'content', label: 'Content', kind: 'content' },
  ],
  edit_file: [
    { key: 'file_path', label: 'File', kind: 'chip' },
    { key: 'old_string', label: 'Replaced', kind: 'content' },
    { key: 'new_string', label: 'Replacement', kind: 'content' },
  ],
  glob: [
    { key: 'pattern', label: 'Pattern', kind: 'quote' },
    { key: 'path', label: 'Path', kind: 'chip' },
  ],
  grep: [
    { key: 'pattern', label: 'Pattern', kind: 'quote' },
    { key: 'path', label: 'Path', kind: 'chip' },
  ],
  delete_file: [{ key: 'file_path', label: 'File', kind: 'chip' }],
  'document.read': [{ key: 'path', label: 'Path', kind: 'chip' }],
  'document.create': [
    { key: 'path', label: 'Path', kind: 'chip' },
    { key: 'format', label: 'Format', kind: 'enum' },
    { key: 'template', label: 'Template', kind: 'chip' },
  ],
  execute: [{ key: 'command', label: 'Command', kind: 'chip' }],
  memory: [
    { key: 'action', label: 'Action', kind: 'enum' },
    { key: 'path', label: 'Path', kind: 'chip' },
    { key: 'content', label: 'Content', kind: 'content' },
  ],
  schedule: [
    { key: 'action', label: 'Action', kind: 'enum' },
    { key: 'name', label: 'Name', kind: 'quote' },
    { key: 'kind', label: 'Kind', kind: 'enum' },
    { key: 'expression', label: 'Expression', kind: 'quote' },
    { key: 'run_at', label: 'Run At', kind: 'plain' },
    { key: 'delivery', label: 'Delivery', kind: 'enum' },
    { key: 'channel_id', label: 'Channel', kind: 'chip' },
    { key: 'enabled', label: 'Enabled', kind: 'plain' },
    { key: 'prompt', label: 'Prompt', kind: 'content' },
    { key: 'id', label: 'ID', kind: 'chip' },
  ],
  'web.search': [
    { key: 'query', label: 'Query', kind: 'quote' },
    { key: 'max_results', label: 'Max Results', kind: 'plain' },
  ],
  'web.fetch': [{ key: 'url', label: 'URL', kind: 'chip' }],
  browser: [],
  'browser.navigate': [{ key: 'url', label: 'URL', kind: 'chip' }],
  'browser.act': [
    { key: 'action', label: 'Action', kind: 'enum' },
    { key: 'selector', label: 'Selector', kind: 'chip' },
    { key: 'text', label: 'Text', kind: 'content' },
  ],
  'browser.read': [],
  'browser.screenshot': [],
  'browser.snapshot': [],
  'browser.click': [{ key: 'ref', label: 'Ref', kind: 'chip' }],
  'browser.type': [
    { key: 'ref', label: 'Ref', kind: 'chip' },
    { key: 'text', label: 'Text', kind: 'content' },
  ],
  'browser.hover': [{ key: 'ref', label: 'Ref', kind: 'chip' }],
  'browser.drag': [
    { key: 'ref', label: 'From Ref', kind: 'chip' },
    { key: 'to_ref', label: 'To Ref', kind: 'chip' },
  ],
  'browser.select_option': [
    { key: 'ref', label: 'Ref', kind: 'chip' },
    { key: 'value', label: 'Value', kind: 'quote' },
  ],
  todo_write: [
    { key: 'items', label: 'Items', kind: 'content' },
    { key: 'revision', label: 'Revision', kind: 'plain' },
  ],
  todo_read: [],
};

/** Per-tool expanded field spec; null for tools without an entry (renderer
 * uses genericRows). */
export function toolFieldSpec(name: string): FieldSpec[] | null {
  return FIELD_SPECS[name] ?? null;
}

/** Technical tokens rendered in caps rather than title case — mirrors
 * toolCatalog.ts's MCP_ACRONYMS, re-declared locally so this module stays
 * dependency-free of the concurrently-evolving catalog. */
const KEY_ACRONYMS = new Set([
  'api', 'db', 'url', 'uri', 'http', 'https', 'sse', 'sql', 'json', 'xml',
  'csv', 'pdf', 'html', 'css', 'js', 'ts', 'ui', 'id', 'ids', 'io', 'ai',
  'aws', 'gcp', 'cli', 'ssh', 'tcp', 'udp',
]);

/** Humanize a snake_case (or camelCase) arg key for generic rows — "URL",
 * "Max Results", "Old String". */
export function humanizeKey(key: string): string {
  return key
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2') // camelCase → words
    .split(/[-_\s]+/)
    .filter(Boolean)
    .map((w) => (KEY_ACRONYMS.has(w.toLowerCase()) ? w.toUpperCase() : w.charAt(0).toUpperCase() + w.slice(1)))
    .join(' ');
}

/** A value counts as present when it is not undefined/null/empty-string —
 * present-only rendering (D3) keeps noise rows out of the expanded view. */
function isPresent(v: unknown): boolean {
  return v !== undefined && v !== null && v !== '';
}

function scalarText(v: unknown): string {
  if (typeof v === 'string') return v;
  if (typeof v === 'number' || typeof v === 'boolean') return String(v);
  return '';
}

function stringifyValue(v: unknown): string {
  if (typeof v === 'string') return v;
  if (typeof v === 'number' || typeof v === 'boolean') return String(v);
  try {
    return JSON.stringify(v) ?? '';
  } catch {
    return String(v);
  }
}

/** Conservative value kind for keys outside a spec / generic rows (D4/4.1):
 * http(s) strings become url chips, long or multiline strings become clamped
 * content blocks, everything else stays plain. */
function conservativeKind(value: string): FieldKind {
  if (/^https?:\/\//i.test(value)) return 'chip';
  if (value.length > 200 || value.includes('\n')) return 'content';
  return 'plain';
}

function conservativeRow(key: string, value: unknown): FieldRow {
  const text = stringifyValue(value);
  return { label: humanizeKey(key), kind: conservativeKind(text), value: text };
}

/** Render a tool's parsed args through its spec: present-only rows in spec
 * order, then any arg keys NOT in the spec appended as humanized rows (no
 * silent drops, D3). Tools without a spec degrade to the generic mapping. */
export function fieldRows(name: string, args: Record<string, unknown>): FieldRow[] {
  const spec = toolFieldSpec(name);
  const rows: FieldRow[] = [];
  const seen = new Set<string>();
  if (spec) {
    for (const field of spec) {
      if (!isPresent(args[field.key])) continue;
      rows.push({ label: field.label, kind: field.kind, value: stringifyValue(args[field.key]) });
      seen.add(field.key);
    }
  }
  for (const key of Object.keys(args)) {
    if (seen.has(key) || !isPresent(args[key])) continue;
    rows.push(conservativeRow(key, args[key]));
  }
  return rows;
}

/** Generic fallback for non-catalog tools (D4/4.1): humanized key→row
 * mapping with conservative kinds — http(s) strings → chip, long/multiline
 * strings → content, else plain. */
export function genericRows(value: Record<string, unknown>): FieldRow[] {
  return Object.keys(value)
    .filter((k) => isPresent(value[k]))
    .map((k) => conservativeRow(k, value[k]));
}

// ---------------------------------------------------------------------------
// Result shaping by envelope kind (D4)
// ---------------------------------------------------------------------------

/** Strict JSON-object parse for result envelopes — null on failure or when
 * the payload is an array/scalar. */
function parseJsonObject(raw: string | undefined | null): Record<string, unknown> | null {
  if (!raw) return null;
  try {
    const v = JSON.parse(raw);
    if (v !== null && typeof v === 'object' && !Array.isArray(v)) {
      return v as Record<string, unknown>;
    }
    return null;
  } catch {
    return null;
  }
}

/** web.search envelope: `{query, max_results, results: [{title, url, ...}]}`
 * — recognized by the `results` array so provider-side extras stay harmless. */
function searchEnvelope(obj: Record<string, unknown>): ResultView | null {
  if (!Array.isArray(obj.results)) return null;
  const results = obj.results.map((item) => {
    if (item !== null && typeof item === 'object' && !Array.isArray(item)) {
      const o = item as Record<string, unknown>;
      const url = scalarText(o.url);
      return { title: scalarText(o.title) || url, url };
    }
    return { title: scalarText(item), url: '' };
  });
  return { kind: 'search_results', results };
}

/** Snapshot envelope element, coerced field-by-field so a malformed entry
 * degrades to blanks instead of breaking the table. */
interface SnapshotElement {
  ref: string;
  role: string;
  name: string;
  value: string;
}

function coerceElement(item: unknown): SnapshotElement {
  if (item !== null && typeof item === 'object' && !Array.isArray(item)) {
    const o = item as Record<string, unknown>;
    return {
      ref: scalarText(o.ref),
      role: scalarText(o.role),
      name: scalarText(o.name),
      value: scalarText(o.value),
    };
  }
  return { ref: scalarText(item), role: '', name: '', value: '' };
}

function snapshotEnvelope(obj: Record<string, unknown>): { elements: SnapshotElement[] } | null {
  if (!Array.isArray(obj.elements)) return null;
  return { elements: obj.elements.map(coerceElement) };
}

/** Ref-action facade tools: their snapshot-envelope results render as a
 * refresh line, not the full table (D4 — every interaction returns a fresh
 * snapshot; only browser.snapshot/browser.read keep the envelope view). */
const REF_ACTION_TOOLS = new Set([
  'browser.click',
  'browser.type',
  'browser.hover',
  'browser.drag',
  'browser.select_option',
  'browser.act',
]);

function listItem(item: unknown): string {
  if (typeof item === 'string') return item;
  if (typeof item === 'number' || typeof item === 'boolean') return String(item);
  try {
    return JSON.stringify(item) ?? '';
  } catch {
    return String(item);
  }
}

/** Result shaping by envelope kind (D4): web.search envelope →
 * search_results; snapshot envelope → snapshot (or snapshot_refresh when the
 * tool is a ref action); object → kv; array → list; plain/unparseable text
 * → text; empty/blank → empty. */
export function formatResult(name: string, rawResult: string | undefined | null): ResultView {
  if (rawResult == null) return { kind: 'empty' };
  const trimmed = rawResult.trim();
  if (trimmed === '') return { kind: 'empty' };
  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch {
    return { kind: 'text', text: rawResult };
  }
  if (parsed === null || typeof parsed !== 'object') {
    // JSON scalars are content, not structure — clamp them like text (D4).
    return { kind: 'text', text: rawResult };
  }
  if (Array.isArray(parsed)) {
    return { kind: 'list', items: parsed.map(listItem) };
  }
  const obj = parsed as Record<string, unknown>;
  const search = searchEnvelope(obj);
  if (search) return search;
  const snapshot = snapshotEnvelope(obj);
  if (snapshot) {
    if (REF_ACTION_TOOLS.has(name)) {
      return { kind: 'snapshot_refresh', elementCount: snapshot.elements.length };
    }
    return {
      kind: 'snapshot',
      title: scalarText(obj.title),
      elements: snapshot.elements,
    };
  }
  return { kind: 'kv', rows: genericRows(obj) };
}

// ---------------------------------------------------------------------------
// ref→name resolution from sibling cards (D5)
// ---------------------------------------------------------------------------

/** Parse a sibling card's res into a snapshot envelope, or null. */
function snapshotOf(raw: string | undefined | null): { elements: SnapshotElement[] } | null {
  const obj = parseJsonObject(raw);
  return obj ? snapshotEnvelope(obj) : null;
}

/** ref→name lookup (D5): walk the sibling tool cards of the same agent
 * message, newest snapshot envelope first, and return the ref's element
 * name — or null when absent/unnamed. The newest snapshot CONTAINING the ref
 * decides (a named-then-unnamed ref stays unnamed — never resurface stale
 * names); older snapshots are only consulted when the ref is absent. Refs
 * compare string-coerced: args JSON may carry numeric refs. */
export function resolveRefName(
  siblings: { name?: string; args?: string; res?: string }[] | undefined | null,
  ref: string
): string | null {
  if (!Array.isArray(siblings)) return null;
  // Refs compare string-coerced: snapshot envelopes may carry numeric refs.
  const want = scalarText(ref).trim();
  if (!want) return null;
  for (let i = siblings.length - 1; i >= 0; i -= 1) {
    const snapshot = snapshotOf(siblings[i]?.res);
    if (!snapshot) continue;
    const element = snapshot.elements.find((e) => e.ref === want);
    if (!element) continue;
    return element.name || null;
  }
  return null;
}

// ---------------------------------------------------------------------------
// Hook enforcement detection (integrate-agent-hooks D3/D18)
// ---------------------------------------------------------------------------

/** The blocked tool-call result envelope (D3): a pre_tool_use hook block is
 * returned to the model AS the tool result — `{"blocked_by_hook": true,
 * "hook": "<name>", "reason": "<reason>"}` — so the run survives and the
 * transcript card can render the enforcement in place. */
export interface BlockedByHook {
  hook: string;
  reason: string;
}

/** Detect a hook-blocked tool result: strict JSON-object parse, then the
 * `blocked_by_hook: true` marker. Anything else — plain results, error
 * strings, unparseable or partial stream JSON — is not a block (null). */
export function blockedByHook(rawResult: string | undefined | null): BlockedByHook | null {
  const obj = parseJsonObject(rawResult);
  if (!obj || obj.blocked_by_hook !== true) return null;
  const hook = scalarText(obj.hook);
  const reason = scalarText(obj.reason);
  if (!hook && !reason) return null;
  return { hook, reason };
}
