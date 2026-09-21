import { api, type ApiSearchEntry } from "./api";

// Per-workspace module-level cache over the tools catalog (design D7). The
// catalog is instance-static, so the only invalidation is a workspace switch.
// On failure the cache resets so a later `ensure` retries.
let cacheWorkspace: string | null = null;
let names: Record<string, string> = {};
// Icon mirror (design D8): Icon.tsx name per catalog key, same lifecycle as
// `names` — reset on workspace switch and on failure.
let icons: Record<string, string> = {};
let inflight: Promise<void> | null = null;

// Browser facade members (design D2) are runtime expansions of the `browser`
// alias, not catalog keys — config surfaces show one Browser chip. The
// transcript still renders their cards, so their readable wording lives here;
// the expanded card keeps the raw tool id.
const facadeToolNames: Record<string, string> = {
  'browser.navigate': 'Navigate Page',
  'browser.act': 'Page Action',
  'browser.read': 'Read Page',
  'browser.screenshot': 'Screenshot',
  'browser.snapshot': 'Page Snapshot',
  'browser.click': 'Click Element',
  'browser.type': 'Type Text',
  'browser.hover': 'Hover Element',
  'browser.drag': 'Drag Element',
  'browser.select_option': 'Select Option',
};

// Design D8: facade members share the browser family icon — the same one the
// catalog's `browser` alias entry carries; computed MCP ids get the default
// MCP icon (both exist in ui/Icon.tsx).
const FACADE_FAMILY_ICON = 'globe';
const MCP_DEFAULT_ICON = 'plug';

// Built-in catalog entries mirrored statically (names and icons from the
// backend's ToolCatalog, internal/agents/tool_catalog.go). The workspace
// catalog fetch stays the source of truth for anything beyond these built-ins,
// but transcript cards must never flash raw ids while it loads — or drop to
// them when it fails — so built-ins resolve before the network does. The
// static values equal the server's by construction.
const builtinToolNames: Record<string, string> = {
  ls: 'List Files',
  read_file: 'Read File',
  write_file: 'Write File',
  edit_file: 'Edit File',
  glob: 'Glob',
  grep: 'Grep',
  delete_file: 'Delete File',
  'document.read': 'Read Document',
  'document.create': 'Create Document',
  execute: 'Shell',
  memory: 'Memory',
  'web.search': 'Web Search',
  'web.fetch': 'Web Fetch',
  browser: 'Browser',
  schedule: 'Schedule',
  'channel.post': 'Channel Post',
  'channel.history': 'Channel History',
  'session.close': 'Close Work Session',
  todo_write: 'Write Todos',
  todo_read: 'Read Todos',
};

const builtinToolIcons: Record<string, string> = {
  ls: 'folder',
  read_file: 'file',
  write_file: 'file-plus',
  edit_file: 'edit',
  glob: 'scan',
  grep: 'compass',
  delete_file: 'trash',
  'document.read': 'file-text',
  'document.create': 'file-plus',
  execute: 'terminal',
  memory: 'memory',
  'web.search': 'search',
  'web.fetch': 'link',
  browser: 'globe',
  schedule: 'calendar',
  'channel.post': 'message',
  'channel.history': 'history',
  'session.close': 'check-circle',
  todo_write: 'check-circle',
  todo_read: 'file-text',
};

export const toolCatalog = {
  /** Fetch (once per workspace) the display-name map. Resolves when settled. */
  ensure(wsId: string | null | undefined): Promise<void> {
    if (!wsId || typeof wsId !== 'string') return Promise.resolve();
    if (cacheWorkspace === wsId) {
      if (inflight) return inflight;
      if (Object.keys(names).length > 0) return Promise.resolve();
    } else {
      cacheWorkspace = wsId;
      names = {};
      icons = {};
    }
    inflight = api.tools
      .list(wsId)
      .then((res: any) => {
        const next: Record<string, string> = {};
        const nextIcons: Record<string, string> = {};
        for (const t of res.tools || []) {
          next[t.key] = t.display_name || t.key;
          if (t.icon_key) nextIcons[t.key] = t.icon_key;
        }
        names = next;
        icons = nextIcons;
      })
      .catch(() => {
        // fall back to raw ids; retry on the next ensure call
        cacheWorkspace = null;
      })
      .finally(() => {
        inflight = null;
      });
    return inflight;
  },
  /** Display name for a tool id, or null when neither the static built-in
   * mirror, the workspace catalog, nor a known facade family has an entry. */
  displayName(id: string): string | null {
    // MCP ids are computed, never catalog entries (design D6/D7 — MCP sits
    // outside the static tool catalog), so the branch runs first.
    const mcp = mcpDisplayName(id);
    if (mcp) return mcp;
    return builtinToolNames[id] ?? names[id] ?? facadeToolNames[id] ?? null;
  },
  /** Icon name (an Icon.tsx key) for a tool id, or null when none is known
   * (design D8). Order mirrors displayName: computed MCP ids first, then the
   * static built-in mirror, the catalog mirror, and browser facade members. */
  icon(id: string): string | null {
    if (id.startsWith('mcp__')) return MCP_DEFAULT_ICON;
    if (builtinToolIcons[id]) return builtinToolIcons[id];
    return icons[id] ?? (facadeToolNames[id] ? FACADE_FAMILY_ICON : null);
  },
  /** The workspace-visible tool ids with display names (static built-in
   * mirror merged with the fetched catalog). Populated by ensure(); a never-
   * fetched or failed catalog still yields the built-ins. Consumers that
   * enumerate values (hook matcher pickers) read this after awaiting ensure. */
  entries(): { key: string; name: string }[] {
    const merged: Record<string, string> = { ...builtinToolNames, ...names };
    return Object.keys(merged).map((key) => ({ key, name: merged[key] }));
  },
};

// ---------------------------------------------------------------------------
// MCP tool ids (design D7): mcp__<server>__<tool>, both segments sanitized
// server-side to [a-z0-9_-]; a collision after sanitization appends _<n>.
// ---------------------------------------------------------------------------

/** Brands whose casing the generic title-caser cannot recover from the
 * sanitized (lowercased) server segment. */
const MCP_BRAND_NAMES: Record<string, string> = {
  github: 'GitHub',
  gitlab: 'GitLab',
  bitbucket: 'Bitbucket',
  youtube: 'YouTube',
  linkedin: 'LinkedIn',
  postgres: 'PostgreSQL',
  postgresql: 'PostgreSQL',
  mysql: 'MySQL',
  sqlite: 'SQLite',
  mongodb: 'MongoDB',
  graphql: 'GraphQL',
  javascript: 'JavaScript',
  typescript: 'TypeScript',
};

/** Technical tokens rendered in caps rather than title case. */
const MCP_ACRONYMS = new Set([
  'api', 'db', 'url', 'uri', 'http', 'https', 'sse', 'sql', 'json', 'xml',
  'csv', 'pdf', 'html', 'css', 'js', 'ts', 'ui', 'id', 'ids', 'io', 'ai',
  'aws', 'gcp', 'cli', 'ssh', 'tcp', 'udp',
]);

/** A trailing _<digits> is the server-side collision-suffix artifact —
 * display names drop it; the raw id stays on the card for disambiguation. */
function stripCollisionSuffix(segment: string): string {
  return segment.replace(/_\d+$/, '');
}

function humanizeMcpWords(segment: string): string {
  return segment
    .split(/[-_\s]+/)
    .filter(Boolean)
    .map((w) => (MCP_ACRONYMS.has(w) ? w.toUpperCase() : w.charAt(0).toUpperCase() + w.slice(1)))
    .join(' ');
}

/** "mcp__github__create_issue" → "GitHub · Create Issue". Returns null when
 * the id lacks the server__tool shape so callers keep the raw id. */
export function mcpDisplayName(id: string): string | null {
  if (!id.startsWith('mcp__')) return null;
  const rest = id.slice('mcp__'.length);
  const sep = rest.indexOf('__');
  if (sep <= 0) return null; // no tool segment or empty server segment
  const server = stripCollisionSuffix(rest.slice(0, sep));
  const tool = stripCollisionSuffix(rest.slice(sep + 2));
  if (!server || !tool) return null;
  const serverName = MCP_BRAND_NAMES[server] ?? humanizeMcpWords(server);
  const toolName = humanizeMcpWords(tool);
  if (!serverName || !toolName) return null;
  return `${serverName} · ${toolName}`;
}

// ---------------------------------------------------------------------------
// web.search provider stacks (change web-search-provider-stacks)
// ---------------------------------------------------------------------------

export const WEB_SEARCH_TOOL_KEY = 'web.search';
/** Positional failover window: the first three entries are tried in order. */
export const SEARCH_ROTATION_WINDOW = 3;
export const SEARCH_TIMEOUT_DEFAULT = 10;
export const SEARCH_TIMEOUT_MAX = 60;

export type SearchCredentialKind = 'api_key' | 'base_url';

export interface SearchProviderOption {
  value: string;
  label: string;
  /** Credential the provider requires — SearXNG takes a base URL, the rest API keys. */
  kind: SearchCredentialKind;
}

/** The search provider registry mirrored client-side; DuckDuckGo is removed. */
export const SEARCH_PROVIDERS: SearchProviderOption[] = [
  { value: 'tavily', label: 'Tavily', kind: 'api_key' },
  { value: 'brave', label: 'Brave', kind: 'api_key' },
  { value: 'exa', label: 'Exa', kind: 'api_key' },
  { value: 'perplexity', label: 'Perplexity', kind: 'api_key' },
  { value: 'firecrawl', label: 'Firecrawl', kind: 'api_key' },
  { value: 'searxng', label: 'SearXNG', kind: 'base_url' },
];

export function searchProviderOption(provider: string): SearchProviderOption | null {
  return SEARCH_PROVIDERS.find((p) => p.value === provider) ?? null;
}

export function searchCredentialKind(provider: string): SearchCredentialKind {
  return searchProviderOption(provider)?.kind ?? 'api_key';
}

/** Editable row state for the stack editor: secrets are write-only and never
 * round-trip from the server. */
export interface SearchEntryDraft {
  localKey: string;
  /** Server-assigned stable id; absent for rows added in this session. */
  id?: string;
  name: string;
  provider: string;
  apiKey: string;
  baseUrl: string;
  /** Last-4 hint from the view payload, shown as the key placeholder. */
  hint?: string;
}

let draftSeq = 0;
function nextDraftKey(): string {
  draftSeq += 1;
  return `draft-${draftSeq}`;
}

export function blankSearchDraft(): SearchEntryDraft {
  return { localKey: nextDraftKey(), name: '', provider: '', apiKey: '', baseUrl: '' };
}

function draftFromEntry(entry: ApiSearchEntry): SearchEntryDraft {
  return {
    localKey: nextDraftKey(),
    id: entry.id,
    name: typeof entry.name === 'string' ? entry.name : '',
    provider: typeof entry.provider === 'string' ? entry.provider : '',
    apiKey: '',
    baseUrl: typeof entry.base_url === 'string' ? entry.base_url : '',
    hint: entry.api_key_hint,
  };
}

/** Hydrate editor drafts from a tool config view payload. */
export function searchDraftsFromConfig(config: Record<string, unknown> | undefined | null): SearchEntryDraft[] {
  const entries = (config as { entries?: unknown } | undefined | null)?.entries;
  if (!Array.isArray(entries)) return [];
  return entries.map((e) => draftFromEntry(e as ApiSearchEntry));
}

/** Effective per-attempt timeout for the flat input field (default 10). */
export function searchTimeoutFromConfig(config: Record<string, unknown> | undefined | null): string {
  const n = (config as { request_timeout_seconds?: unknown } | undefined | null)?.request_timeout_seconds;
  return typeof n === 'number' && Number.isFinite(n) && n > 0 ? String(n) : String(SEARCH_TIMEOUT_DEFAULT);
}

export function searchEntryCount(config: Record<string, unknown> | undefined | null): number {
  const entries = (config as { entries?: unknown } | undefined | null)?.entries;
  return Array.isArray(entries) ? entries.length : 0;
}

export interface SearchDraftErrors {
  /** Per-row message keyed by draft localKey. */
  rows: Record<string, string>;
  /** Message for the flat timeout field, when out of bounds. */
  timeout: string | null;
}

/** Client-side validation that blocks Save: empty name, unknown provider,
 * missing credential on a NEW row (a known id with an empty key legally keeps
 * the stored secret), duplicate names case-insensitive, timeout bounds. */
export function validateSearchDrafts(drafts: SearchEntryDraft[], timeout: string): SearchDraftErrors {
  const rows: Record<string, string> = {};
  const seen = new Map<string, number[]>();
  drafts.forEach((d, i) => {
    const name = d.name.trim();
    if (!name) {
      rows[d.localKey] = 'Name is required';
      return;
    }
    const lower = name.toLowerCase();
    seen.set(lower, [...(seen.get(lower) ?? []), i]);
    if (!d.provider || !searchProviderOption(d.provider)) {
      rows[d.localKey] = 'Provider is required';
      return;
    }
    if (searchCredentialKind(d.provider) === 'api_key' && !d.id && !d.apiKey.trim()) {
      rows[d.localKey] = `API key is required for ${d.provider}`;
    }
  });
  for (const indexes of seen.values()) {
    if (indexes.length > 1) {
      for (const i of indexes) rows[drafts[i].localKey] ??= 'Name must be unique';
    }
  }
  let timeoutError: string | null = null;
  const t = timeout.trim();
  if (t !== '') {
    const n = Number(t);
    if (!Number.isInteger(n) || n < 1 || n > SEARCH_TIMEOUT_MAX) {
      timeoutError = `Must be a whole number between 1 and ${SEARCH_TIMEOUT_MAX}`;
    }
  }
  return { rows, timeout: timeoutError };
}

/** Build the UPSERT payload: the whole ordered list; a known id with an empty
 * key omits `api_key` so the server keeps the stored secret (reorder-safe via
 * the stable entry id). */
export function searchConfigPayload(
  drafts: SearchEntryDraft[],
  timeout: string
): { entries: ApiSearchEntry[]; request_timeout_seconds?: number } {
  const entries: ApiSearchEntry[] = drafts.map((d) => {
    const entry: ApiSearchEntry = { name: d.name.trim(), provider: d.provider };
    if (d.id) entry.id = d.id;
    if (d.apiKey.trim() !== '') entry.api_key = d.apiKey.trim();
    if (d.provider === 'searxng' && d.baseUrl.trim() !== '') entry.base_url = d.baseUrl.trim();
    return entry;
  });
  const payload: { entries: ApiSearchEntry[]; request_timeout_seconds?: number } = { entries };
  const t = timeout.trim();
  if (t !== '') payload.request_timeout_seconds = Number(t);
  return payload;
}
