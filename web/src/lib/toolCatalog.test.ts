/**
 * @vitest-environment jsdom
 */
import { describe, it, expect, vi } from 'vitest';
import {
  SEARCH_PROVIDERS,
  blankSearchDraft,
  searchConfigPayload,
  searchCredentialKind,
  searchDraftsFromConfig,
  searchEntryCount,
  searchTimeoutFromConfig,
  mcpDisplayName,
  toolCatalog,
  validateSearchDrafts,
} from './toolCatalog';
import { api } from './api';

vi.mock('./api', () => ({
  api: {
    tools: {
      list: vi.fn().mockResolvedValue({ tools: [] }),
    },
  },
}));

// This environment's jsdom exposes no localStorage; the api module touches
// it at import — install a minimal stub.
if (typeof (globalThis as any).localStorage === 'undefined') {
  const backing = new Map<string, string>();
  (globalThis as any).localStorage = {
    getItem: (k: string) => (backing.has(k) ? backing.get(k)! : null),
    setItem: (k: string, v: string) => void backing.set(k, String(v)),
    removeItem: (k: string) => void backing.delete(k),
    clear: () => void backing.clear(),
    key: (i: number) => Array.from(backing.keys())[i] ?? null,
    get length() { return backing.size; },
  };
}

describe('toolCatalog displayName', () => {
  it('maps browser facade members to readable wording without a catalog fetch', () => {
    expect(toolCatalog.displayName('browser.navigate')).toBe('Navigate Page');
    expect(toolCatalog.displayName('browser.snapshot')).toBe('Page Snapshot');
    expect(toolCatalog.displayName('browser.select_option')).toBe('Select Option');
    // Unknown non-MCP ids stay unmapped so callers fall back to the raw id.
    expect(toolCatalog.displayName('totally.unknown')).toBeNull();
  });

  it('lets the workspace catalog override a facade name', async () => {
    vi.mocked(api.tools.list).mockResolvedValueOnce({
      tools: [{
        key: 'browser.click',
        display_name: 'Press',
        description: '',
        group: 'browser',
        icon_key: 'globe',
        configurable: false,
        enabled: true,
        configured: false,
        config: {},
        toggleable: true,
      }],
    });
    await toolCatalog.ensure('ws-catalog-override');
    expect(toolCatalog.displayName('browser.click')).toBe('Press');
    // Facade members the catalog does not govern keep their wording.
    expect(toolCatalog.displayName('browser.type')).toBe('Type Text');
  });
});

describe('toolCatalog mcp__ display names (design D7)', () => {
  it('humanizes server and tool segments with the spec example', () => {
    expect(toolCatalog.displayName('mcp__github__create_issue')).toBe('GitHub · Create Issue');
    expect(mcpDisplayName('mcp__github__create_issue')).toBe('GitHub · Create Issue');
  });

  it('splits underscores and dashes into words and caps known acronyms', () => {
    expect(mcpDisplayName('mcp__web_search__fetch_url')).toBe('Web Search · Fetch URL');
    expect(mcpDisplayName('mcp__my-db__get-user')).toBe('My DB · Get User');
    expect(mcpDisplayName('mcp__fs__read_json_file')).toBe('Fs · Read JSON File');
  });

  it('recovers brand casing the title-caser cannot', () => {
    expect(mcpDisplayName('mcp__gitlab__list_projects')).toBe('GitLab · List Projects');
    expect(mcpDisplayName('mcp__postgres__run_sql')).toBe('PostgreSQL · Run SQL');
  });

  it('drops collision suffixes from server and tool segments', () => {
    // github_2 is the sanitizer's de-dup artifact, not part of the name.
    expect(mcpDisplayName('mcp__github_2__create_issue')).toBe('GitHub · Create Issue');
    expect(mcpDisplayName('mcp__sentry__get_issues_2')).toBe('Sentry · Get Issues');
    // Version-like suffixes with a non-numeric part are kept.
    expect(mcpDisplayName('mcp__sentry__get_issue_v2')).toBe('Sentry · Get Issue V2');
  });

  it('returns null for ids lacking the server__tool shape', () => {
    expect(mcpDisplayName('mcp__github')).toBeNull();
    expect(mcpDisplayName('mcp__github__')).toBeNull();
    expect(mcpDisplayName('mcp____tool')).toBeNull();
    expect(mcpDisplayName('mcp__')).toBeNull();
    // Non-MCP ids never take the branch.
    expect(mcpDisplayName('web.search')).toBeNull();
    expect(mcpDisplayName('browser.navigate')).toBeNull();
  });
});

describe('toolCatalog icon (design D8)', () => {
  it('returns the stored icon_key for catalog ids', async () => {
    vi.mocked(api.tools.list).mockResolvedValueOnce({
      tools: [
        {
          key: 'files.write',
          display_name: 'Write File',
          description: '',
          group: 'files',
          icon_key: 'file-plus',
          configurable: false,
          enabled: true,
          configured: false,
          config: {},
          toggleable: true,
        },
      ],
    });
    await toolCatalog.ensure('ws-icons');
    expect(toolCatalog.icon('files.write')).toBe('file-plus');
    // An id the loaded catalog does not govern resolves to null.
    expect(toolCatalog.icon('totally.unknown')).toBeNull();
  });

  it('maps browser facade members to the browser family icon', () => {
    expect(toolCatalog.icon('browser.navigate')).toBe('globe');
    expect(toolCatalog.icon('browser.select_option')).toBe('globe');
  });

  it('maps every mcp__ id to the default plug icon', () => {
    expect(toolCatalog.icon('mcp__github__create_issue')).toBe('plug');
    expect(toolCatalog.icon('mcp__x')).toBe('plug');
  });

  it('returns null for non-facade non-MCP ids when the catalog has not loaded them', async () => {
    // Fresh workspace against the default empty-catalog mock: nothing stored.
    await toolCatalog.ensure('ws-icons-empty');
    expect(toolCatalog.icon('files.write')).toBeNull();
    // Facade and MCP ids still resolve without a catalog entry.
    expect(toolCatalog.icon('browser.read')).toBe('globe');
    expect(toolCatalog.icon('mcp__fs__read_file')).toBe('plug');
  });
});

describe('web.search provider stacks', () => {
  it('mirrors the provider registry without DuckDuckGo', () => {
    expect(SEARCH_PROVIDERS.map((p) => p.value)).toEqual([
      'tavily', 'brave', 'exa', 'perplexity', 'firecrawl', 'searxng',
    ]);
    expect(searchCredentialKind('searxng')).toBe('base_url');
    expect(searchCredentialKind('tavily')).toBe('api_key');
    expect(searchCredentialKind('nonsense')).toBe('api_key');
  });

  it('hydrates drafts from a config view payload without echoing secrets', () => {
    const drafts = searchDraftsFromConfig({
      entries: [
        { id: 'e1', name: 'Tavily 1', provider: 'tavily', api_key_hint: 'ab12' },
        { id: 'e2', name: 'SearXNG', provider: 'searxng', base_url: 'http://searxng:8080' },
      ],
      request_timeout_seconds: 20,
    });
    expect(drafts).toHaveLength(2);
    expect(drafts[0]).toMatchObject({ id: 'e1', name: 'Tavily 1', provider: 'tavily', apiKey: '', hint: 'ab12' });
    expect(drafts[1]).toMatchObject({ id: 'e2', provider: 'searxng', baseUrl: 'http://searxng:8080' });
    expect(searchTimeoutFromConfig({ request_timeout_seconds: 20 })).toBe('20');
    expect(searchTimeoutFromConfig({})).toBe('10');
    expect(searchEntryCount({ entries: [{}, {}] })).toBe(2);
    // Malformed/absent config yields an empty stack.
    expect(searchDraftsFromConfig(undefined)).toEqual([]);
    expect(searchDraftsFromConfig({ entries: 'nope' } as any)).toEqual([]);
    expect(searchEntryCount({})).toBe(0);
  });

  it('validates rows: empty name, missing key on new rows, keep-stored known ids, duplicates', () => {
    // A known-id row with an empty key legally keeps its stored secret.
    const stored = searchDraftsFromConfig({ entries: [{ id: 'e1', name: 'Tavily 1', provider: 'tavily' }] });
    expect(validateSearchDrafts(stored, '10')).toEqual({ rows: {}, timeout: null });

    const noName = blankSearchDraft();
    noName.provider = 'tavily';
    noName.apiKey = 'k';
    expect(validateSearchDrafts([noName], '10').rows[noName.localKey]).toBe('Name is required');

    const noKey = blankSearchDraft();
    noKey.name = 'Exa 2';
    noKey.provider = 'exa';
    expect(validateSearchDrafts([noKey], '10').rows[noKey.localKey]).toBe('API key is required for exa');

    const dupA = searchDraftsFromConfig({ entries: [{ id: 'e1', name: 'Tavily 1', provider: 'tavily' }] });
    const dupB = blankSearchDraft();
    dupB.name = 'tavily 1';
    dupB.provider = 'tavily';
    dupB.apiKey = 'k';
    const errs = validateSearchDrafts([...dupA, dupB], '10');
    expect(errs.rows[dupA[0].localKey]).toBe('Name must be unique');
    expect(errs.rows[dupB.localKey]).toBe('Name must be unique');

    expect(validateSearchDrafts(stored, '90').timeout).toContain('between 1 and 60');
    expect(validateSearchDrafts(stored, '').timeout).toBeNull();
  });

  it('builds the upsert payload: keep-stored by omission, new rows carry keys', () => {
    const drafts = searchDraftsFromConfig({
      entries: [
        { id: 'e1', name: 'Tavily 1', provider: 'tavily', api_key_hint: 'ab12' },
        { id: 'e2', name: 'SearXNG', provider: 'searxng', base_url: 'http://old:8080' },
      ],
    });
    drafts[1].baseUrl = 'http://searxng:8080';
    const added = blankSearchDraft();
    added.name = '  Brave 1  ';
    added.provider = 'brave';
    added.apiKey = ' brk ';
    const payload = searchConfigPayload([...drafts, added], '30');
    expect(payload).toEqual({
      entries: [
        { id: 'e1', name: 'Tavily 1', provider: 'tavily' },
        { id: 'e2', name: 'SearXNG', provider: 'searxng', base_url: 'http://searxng:8080' },
        { name: 'Brave 1', provider: 'brave', api_key: 'brk' },
      ],
      request_timeout_seconds: 30,
    });
    // Empty timeout omits the field so the server default (10) applies.
    expect(searchConfigPayload(drafts, '').request_timeout_seconds).toBeUndefined();
  });
});

describe('static built-in mirror (card names/icons resolve before the catalog loads)', () => {
  it('resolves built-in display names without any catalog fetch', () => {
    expect(toolCatalog.displayName('web.fetch')).toBe('Web Fetch');
    expect(toolCatalog.displayName('web.search')).toBe('Web Search');
    expect(toolCatalog.displayName('execute')).toBe('Shell');
    expect(toolCatalog.displayName('ls')).toBe('List Files');
    // Document family (add-reference-documents): search mirrors the backend
    // catalog's display name and search icon.
    expect(toolCatalog.displayName('document.search')).toBe('Search Documents');
  });

  it('resolves built-in icons without any catalog fetch', () => {
    expect(toolCatalog.icon('web.fetch')).toBe('link');
    expect(toolCatalog.icon('web.search')).toBe('search');
    expect(toolCatalog.icon('execute')).toBe('terminal');
    expect(toolCatalog.icon('memory')).toBe('memory');
    expect(toolCatalog.icon('document.search')).toBe('search');
  });

  it('resolves channel/session tool names and icons without any catalog fetch', () => {
    expect(toolCatalog.displayName('channel.post')).toBe('Channel Post');
    expect(toolCatalog.displayName('channel.history')).toBe('Channel History');
    expect(toolCatalog.displayName('session.close')).toBe('Close Work Session');
    expect(toolCatalog.icon('channel.post')).toBe('message');
    expect(toolCatalog.icon('channel.history')).toBe('history');
    expect(toolCatalog.icon('session.close')).toBe('check-circle');
  });

  it('still returns null for ids outside the built-in set', () => {
    expect(toolCatalog.displayName('totally.unknown')).toBeNull();
    expect(toolCatalog.icon('totally.unknown')).toBeNull();
  });

  it('no longer mirrors the removed ui.* echo tools (markdown-fences)', () => {
    expect(toolCatalog.displayName('ui.chart')).toBeNull();
    expect(toolCatalog.displayName('ui.timeline')).toBeNull();
    expect(toolCatalog.displayName('ui.preview')).toBeNull();
    expect(toolCatalog.icon('ui.chart')).toBeNull();
    expect(toolCatalog.icon('ui.timeline')).toBeNull();
    expect(toolCatalog.icon('ui.preview')).toBeNull();
  });
});
