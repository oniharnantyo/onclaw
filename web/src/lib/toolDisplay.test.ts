// Unit tests for the declarative tool-card formatter layer (change
// human-readable-tool-cards, tasks 1.1–1.3 and 4.1–4.2). The module is pure
// over strings — no DOM, no network — so the default environment suffices and
// none of the repo's jsdom/localStorage quirks apply.
import { describe, it, expect } from 'vitest';
import {
  CATALOG_TOOL_KEYS,
  FACADE_TOOL_IDS,
  SENTENCE_TABLE,
  blockedByHook,
  createdDocumentURL,
  fieldRows,
  formatLatency,
  formatResult,
  genericRows,
  humanizeKey,
  parseArgs,
  resolveRefName,
  toolFieldSpec,
  toolOneLiner,
} from './toolDisplay';

// Verified envelope fixtures — shapes come from the backend sources:
// websearch.go ({query, max_results, results}), browser.go snapshots
// ({title, elements: [{ref, role, name, value}]}), memory.go ({path, result}).

const SEARCH_ENVELOPE = JSON.stringify({
  query: 'onclaw agent',
  max_results: 8,
  results: [
    { title: 'OnClaw', url: 'https://onclaw.dev', content: 'agent workspace' },
    { title: 'Docs', url: 'https://onclaw.dev/docs', content: 'docs' },
  ],
});

const SNAPSHOT_ENVELOPE = JSON.stringify({
  title: 'Example Domain',
  elements: [
    { ref: '12', role: 'button', name: 'Sign in', value: '' },
    { ref: '13', role: 'textbox', name: 'Email', value: '' },
  ],
});

describe('parseArgs (D2 streaming edge)', () => {
  it('parses JSON objects', () => {
    expect(parseArgs('{"file_path":"main.go"}')).toEqual({ file_path: 'main.go' });
    expect(parseArgs('{}')).toEqual({});
  });

  it('returns null for absent, partial, or non-object payloads', () => {
    expect(parseArgs(undefined)).toBeNull();
    expect(parseArgs(null)).toBeNull();
    expect(parseArgs('')).toBeNull();
    expect(parseArgs('   ')).toBeNull();
    expect(parseArgs('{"file_path":"ma')).toBeNull(); // mid-stream partial JSON
    expect(parseArgs('not json')).toBeNull();
    expect(parseArgs('[1,2]')).toBeNull(); // arrays are not arg objects
    expect(parseArgs('42')).toBeNull();
    expect(parseArgs('null')).toBeNull();
  });
});

describe('toolOneLiner state machine (D1/D2)', () => {
  it('memory append: intent while running, outcome when done', () => {
    const card = { args: '{"action":"append","path":"USER.md","content":"hi"}' };
    expect(toolOneLiner('memory', card, true)).toBe('Appending to `USER.md`');
    expect(toolOneLiner('memory', card, false)).toBe('Appended to `USER.md`');
  });

  it('memory read: uses the read verb pair', () => {
    const card = { args: '{"action":"read","path":"USER.md"}' };
    expect(toolOneLiner('memory', card, true)).toBe('Reading `USER.md`');
    expect(toolOneLiner('memory', card, false)).toBe('Read `USER.md`');
  });

  it('error state shows the intent form (never asserts success)', () => {
    const card = { args: '{"action":"append","path":"USER.md"}', error: true };
    expect(toolOneLiner('memory', card, false)).toBe('Appending to `USER.md`');
  });

  it('execute shows the command verbatim — no wrapping, running or done', () => {
    const card = { args: '{"command":"ls -la /tmp"}' };
    expect(toolOneLiner('execute', card, true)).toBe('ls -la /tmp');
    expect(toolOneLiner('execute', card, false)).toBe('ls -la /tmp');
  });

  it('web.search: fact appends only when done and the envelope is trusted', () => {
    const args = '{"query":"onclaw agent"}';
    // Running: no fact yet.
    expect(toolOneLiner('web.search', { args }, true)).toBe("Searching for 'onclaw agent'");
    // Done with a trusted results envelope: canonical " — 8 results" shape.
    const done = { args, res: SEARCH_ENVELOPE };
    expect(toolOneLiner('web.search', done, false)).toBe(
      "Searched for 'onclaw agent' — 2 results"
    );
    // Done with an unparseable result: outcome without a fact (D2 — trusted
    // envelopes only).
    expect(toolOneLiner('web.search', { args, res: 'gateway timeout' }, false)).toBe(
      "Searched for 'onclaw agent'"
    );
  });

  it('write_file: irregular verb (Wrote) and size fact from args content', () => {
    const content = 'x'.repeat(1200); // 1200 B → "1.2 kB"
    const card = { args: JSON.stringify({ file_path: 'notes.md', content }) };
    expect(toolOneLiner('write_file', card, false)).toBe('Wrote `notes.md` · 1.2 kB');
    // Running form carries no fact (D2 — facts are done-only).
    expect(toolOneLiner('write_file', card, true)).toBe('Writing `notes.md`');
    // Error form: intent, no fact.
    expect(toolOneLiner('write_file', { ...card, error: true }, false)).toBe('Writing `notes.md`');
  });

  it('browser.click: ref renders as a chip', () => {
    const card = { args: '{"ref":"42"}' };
    expect(toolOneLiner('browser.click', card, true)).toBe('Clicking `42`');
    expect(toolOneLiner('browser.click', card, false)).toBe('Clicked `42`');
  });

  it('unparseable or absent args mint nothing (name + dots fallback)', () => {
    expect(toolOneLiner('read_file', { args: '{"file_path":"ma' }, true)).toBeNull();
    expect(toolOneLiner('read_file', {}, false)).toBeNull();
    expect(toolOneLiner('execute', { args: '{}' }, false)).toBeNull(); // no command → no sentence
  });

  it('bare form when the object key is absent from parseable args', () => {
    expect(toolOneLiner('read_file', { args: '{}' }, false)).toBe('Read');
    expect(toolOneLiner('browser.read', { args: '{}' }, false)).toBe('Read the page');
  });

  it('MCP and unknown ids never invent English (D1)', () => {
    const card = { args: '{"issue":1}' };
    expect(toolOneLiner('mcp__github__create_issue', card, false)).toBeNull();
    expect(toolOneLiner('mcp__github__create_issue', card, true)).toBeNull();
    expect(toolOneLiner('not.a.tool', card, false)).toBeNull();
  });
});

describe('field specs and field rows (D3)', () => {
  it('exposes a spec for catalog tools and null for unknown ids', () => {
    expect(toolFieldSpec('grep')).toEqual([
      { key: 'pattern', label: 'Pattern', kind: 'quote' },
      { key: 'path', label: 'Path', kind: 'chip' },
    ]);
    expect(toolFieldSpec('mcp__github__create_issue')).toBeNull();
    expect(toolFieldSpec('totally.unknown')).toBeNull();
  });

  it('renders present-only rows in spec order — absent keys show nothing', () => {
    const rows = fieldRows('memory', { action: 'read', path: 'USER.md' });
    expect(rows).toEqual([
      { label: 'Action', kind: 'enum', value: 'read' },
      { label: 'Path', kind: 'chip', value: 'USER.md' },
    ]);
  });

  it('appends arg keys outside the spec as humanized rows (no silent drops)', () => {
    const rows = fieldRows('grep', { pattern: 'TODO', extra_flag: '-i' });
    expect(rows).toEqual([
      { label: 'Pattern', kind: 'quote', value: 'TODO' },
      { label: 'Extra Flag', kind: 'plain', value: '-i' },
    ]);
  });

  it('degrades to the generic mapping for tools without a spec', () => {
    expect(fieldRows('mcp__github__create_issue', { repo: 'onclaw' })).toEqual([
      { label: 'Repo', kind: 'plain', value: 'onclaw' },
    ]);
  });
});

describe('genericRows (D4/4.1)', () => {
  it('maps http(s) strings to url chips', () => {
    expect(genericRows({ url: 'https://example.com/a' })).toEqual([
      { label: 'URL', kind: 'chip', value: 'https://example.com/a' },
    ]);
  });

  it('maps long and multiline strings to clamped content blocks', () => {
    expect(genericRows({ body: 'x'.repeat(201) })[0].kind).toBe('content');
    expect(genericRows({ log: 'line1\nline2' })[0].kind).toBe('content');
  });

  it('keeps short scalars plain and humanizes keys (acronyms, camelCase)', () => {
    expect(genericRows({ note: 'ok', maxTokens: 4096 })).toEqual([
      { label: 'Note', kind: 'plain', value: 'ok' },
      { label: 'Max Tokens', kind: 'plain', value: '4096' },
    ]);
  });

  it('skips absent (null/undefined) values', () => {
    expect(genericRows({ a: 'x', b: null, c: undefined })).toEqual([
      { label: 'A', kind: 'plain', value: 'x' },
    ]);
  });
});

describe('formatResult shaping (D4)', () => {
  it('web.search envelope → search_results with title/url pairs', () => {
    const view = formatResult('web.search', SEARCH_ENVELOPE);
    expect(view).toEqual({
      kind: 'search_results',
      results: [
        { title: 'OnClaw', url: 'https://onclaw.dev' },
        { title: 'Docs', url: 'https://onclaw.dev/docs' },
      ],
    });
  });

  it('snapshot envelope via browser.snapshot → full snapshot view', () => {
    const view = formatResult('browser.snapshot', SNAPSHOT_ENVELOPE);
    expect(view).toEqual({
      kind: 'snapshot',
      title: 'Example Domain',
      elements: [
        { ref: '12', role: 'button', name: 'Sign in', value: '' },
        { ref: '13', role: 'textbox', name: 'Email', value: '' },
      ],
    });
  });

  it('snapshot envelope via a ref action → snapshot_refresh with count', () => {
    expect(formatResult('browser.click', SNAPSHOT_ENVELOPE)).toEqual({
      kind: 'snapshot_refresh',
      elementCount: 2,
    });
    expect(formatResult('browser.type', SNAPSHOT_ENVELOPE)).toEqual({
      kind: 'snapshot_refresh',
      elementCount: 2,
    });
  });

  it('plain text results stay text, raw string preserved', () => {
    const out = 'total 24\ndrwxr-xr-x  src';
    expect(formatResult('ls', out)).toEqual({ kind: 'text', text: out });
  });

  it('garbage JSON, blank, and absent results degrade safely', () => {
    expect(formatResult('ls', 'not json {')).toEqual({ kind: 'text', text: 'not json {' });
    expect(formatResult('ls', undefined)).toEqual({ kind: 'empty' });
    expect(formatResult('ls', null)).toEqual({ kind: 'empty' });
    expect(formatResult('ls', '   ')).toEqual({ kind: 'empty' });
    // JSON scalars are content, not structure.
    expect(formatResult('ls', '42')).toEqual({ kind: 'text', text: '42' });
  });

  it('JSON arrays → list; other objects → humanized kv rows', () => {
    expect(formatResult('ls', '["a.go","b.go"]')).toEqual({
      kind: 'list',
      items: ['a.go', 'b.go'],
    });
    expect(formatResult('memory', '{"path":"USER.md","result":"ok"}')).toEqual({
      kind: 'kv',
      rows: [
        { label: 'Path', kind: 'plain', value: 'USER.md' },
        { label: 'Result', kind: 'plain', value: 'ok' },
      ],
    });
  });
});

describe('resolveRefName sibling walk (D5)', () => {
  const oldSnapshot = JSON.stringify({
    title: 'Before',
    elements: [{ ref: '42', role: 'button', name: 'Old Button', value: '' }],
  });
  const newSnapshot = JSON.stringify({
    title: 'After',
    elements: [{ ref: '42', role: 'button', name: 'Sign in', value: '' }],
  });
  const siblings = [
    { name: 'browser.snapshot', res: oldSnapshot },
    { name: 'browser.click', res: newSnapshot },
  ];

  it('resolves from the newest snapshot containing the ref', () => {
    expect(resolveRefName(siblings, '42')).toBe('Sign in');
  });

  it('walks to older snapshots when the ref is absent from newer ones', () => {
    const partial = [
      { name: 'browser.snapshot', res: JSON.stringify({ title: 'X', elements: [{ ref: '7', role: 'link', name: 'Home', value: '' }] }) },
      { name: 'browser.snapshot', res: oldSnapshot },
    ];
    expect(resolveRefName(partial, '42')).toBe('Old Button');
  });

  it('an unnamed ref in the deciding snapshot stays null (never stale names)', () => {
    const unnamed = JSON.stringify({
      title: 'After',
      elements: [{ ref: '42', role: 'button', value: '' }],
    });
    expect(resolveRefName([{ name: 'browser.snapshot', res: oldSnapshot }, { name: 'browser.click', res: unnamed }], '42')).toBeNull();
  });

  it('no siblings or unknown ref → null', () => {
    expect(resolveRefName(undefined, '42')).toBeNull();
    expect(resolveRefName([], '42')).toBeNull();
    expect(resolveRefName(siblings, '99')).toBeNull();
  });

  it('compares refs string-coerced (numeric refs in the envelope)', () => {
    const numeric = JSON.stringify({
      title: 'T',
      elements: [{ ref: 42, role: 'button', name: 'Go', value: '' }],
    });
    expect(resolveRefName([{ name: 'browser.snapshot', res: numeric }], '42')).toBe('Go');
  });
});

describe('humanizeKey', () => {
  it('title-cases snake_case keys and caps known acronyms', () => {
    expect(humanizeKey('max_results')).toBe('Max Results');
    expect(humanizeKey('file_path')).toBe('File Path');
    expect(humanizeKey('url')).toBe('URL');
    expect(humanizeKey('to_ref')).toBe('To Ref');
    expect(humanizeKey('id')).toBe('ID');
    expect(humanizeKey('maxTokens')).toBe('Max Tokens');
  });
});

// ---------------------------------------------------------------------------
// 4.2 coverage guard: the full expected tool universe — the 18 backend
// catalog keys (internal/agents/tool_catalog.go, incl. the todos from
// adopt-assistant-ui-elements and document.search from
// add-reference-documents; the ui.* echo tools were removed — rich cards
// now come from markdown fences) + the 10 browser facade member ids
// (toolCatalog.ts facadeToolNames) — MUST have a sentence-table entry that
// mints a one-liner. When the backend gains a tool, extend this list AND
// SENTENCE_TABLE together; the lengths below make drift loud.
// ---------------------------------------------------------------------------

const SUITABLE_ARGS: Record<string, Record<string, unknown>> = {
  ls: { path: 'src' },
  read_file: { file_path: 'main.go' },
  write_file: { file_path: 'notes.md', content: 'hello' },
  edit_file: { file_path: 'main.go', old_string: 'a', new_string: 'b' },
  glob: { pattern: '*.go' },
  grep: { pattern: 'TODO' },
  delete_file: { file_path: 'tmp.txt' },
  'document.read': { path: 'invoice.pdf' },
  'document.create': { path: 'invoice.xlsx', format: 'xlsx' },
  'document.search': { query: 'sandbox rate limit' },
  execute: { command: 'ls -la' },
  memory: { action: 'read', path: 'USER.md' },
  'web.search': { query: 'onclaw' },
  'web.fetch': { url: 'https://example.com' },
  browser: {},
  schedule: { action: 'create', name: 'morning-digest', kind: 'recurring', expression: '0 9 * * 1-5' },
  todo_write: { revision: 2, items: [{ key: 'a', text: 'First', status: 'done' }] },
  todo_read: {},
  'browser.navigate': { url: 'https://example.com' },
  'browser.act': { action: 'click', selector: '.btn' },
  'browser.read': {},
  'browser.screenshot': {},
  'browser.snapshot': {},
  'browser.click': { ref: '42' },
  'browser.type': { ref: '42', text: 'hi' },
  'browser.hover': { ref: '7' },
  'browser.drag': { ref: '1', to_ref: '2' },
  'browser.select_option': { ref: '7', value: 'pro' },
};

describe('coverage guard (4.2)', () => {
  it('pins the expected universe sizes', () => {
    expect(CATALOG_TOOL_KEYS).toHaveLength(18);
    expect(FACADE_TOOL_IDS).toHaveLength(10);
  });

  it('every catalog key and facade member has a mintable one-liner', () => {
    const universe = [...CATALOG_TOOL_KEYS, ...FACADE_TOOL_IDS];
    for (const id of universe) {
      const args = SUITABLE_ARGS[id];
      expect(args, `no test args declared for ${id}`).toBeDefined();
      expect(SENTENCE_TABLE[id], `${id} missing from SENTENCE_TABLE`).toBeDefined();
      const running = toolOneLiner(id, { args: JSON.stringify(args) }, true);
      const done = toolOneLiner(id, { args: JSON.stringify(args) }, false);
      expect(running, `${id} has no running one-liner`).toBeTruthy();
      expect(done, `${id} has no done one-liner`).toBeTruthy();
    }
  });

  it('covers both memory action variants', () => {
    const append = { action: 'append', path: 'USER.md', content: 'x' };
    expect(toolOneLiner('memory', { args: JSON.stringify(append) }, false)).toBe(
      'Appended to `USER.md`'
    );
    const read = { action: 'read', path: 'USER.md' };
    expect(toolOneLiner('memory', { args: JSON.stringify(read) }, false)).toBe(
      'Read `USER.md`'
    );
    // An unknown action must not invent a sentence (D1).
    expect(toolOneLiner('memory', { args: '{"action":"wipe"}' }, false)).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// document.search (add-reference-documents 10.1): web.search's pattern —
// verb + query object, hit-count fact from the trusted envelope.
// ---------------------------------------------------------------------------

describe('document.search one-liners (add-reference-documents 10.1)', () => {
  it('verbs the query and appends the trusted hit count on done', () => {
    const args = JSON.stringify({ query: 'sandbox rate limit' });
    expect(toolOneLiner('document.search', { args }, true)).toBe(
      "Searching documents for 'sandbox rate limit'"
    );
    expect(
      toolOneLiner('document.search', { args, res: '{"hits":[{},{},{}]}' }, false)
    ).toBe("Searched documents for 'sandbox rate limit' — 3 hits");
    expect(toolOneLiner('document.search', { args, res: '{"hits":[]}' }, false)).toBe(
      "Searched documents for 'sandbox rate limit' — 0 hits"
    );
    // No result yet (or unparseable): outcome without a fact (D2).
    expect(toolOneLiner('document.search', { args }, false)).toBe(
      "Searched documents for 'sandbox rate limit'"
    );
    expect(toolOneLiner('document.search', { args, res: 'nope' }, false)).toBe(
      "Searched documents for 'sandbox rate limit'"
    );
  });

  it('drops the object phrase when the query is absent', () => {
    // The connector trimming is the shared renderSentence behavior (web.search
    // with an empty query reads the same way).
    expect(toolOneLiner('document.search', { args: '{}' }, true)).toBe(
      'Searching documents for'
    );
    expect(toolOneLiner('document.search', { args: '{}' }, false)).toBe(
      'Searched documents for'
    );
  });
});

// ---------------------------------------------------------------------------
// Todos (adopt-assistant-ui-elements): one-liners back the collapsed/error
// fallbacks of the dedicated generative-UI card. The ui.* echo tools were
// removed — their ids no longer mint one-liners (markdown-fences).
// ---------------------------------------------------------------------------

describe('todo + removed ui.* one-liners (adopt-assistant-ui-elements)', () => {
  it('todo_write: bare verbs with the trusted item-count fact', () => {
    const args = JSON.stringify({ items: [{ key: 'a' }, { key: 'b' }, { key: 'c' }] });
    expect(toolOneLiner('todo_write', { args }, true)).toBe('Updating the plan');
    expect(toolOneLiner('todo_write', { args }, false)).toBe('Updated the plan — 3 items');
    expect(toolOneLiner('todo_write', { args: '{"revision":1}' }, false)).toBe('Updated the plan');
  });

  it('todo_read: appends the item count only from a trusted envelope', () => {
    expect(toolOneLiner('todo_read', { args: '{}' }, true)).toBe('Reading the plan');
    expect(toolOneLiner('todo_read', { args: '{}', res: '{"items":[{"key":"a"}]}' }, false)).toBe(
      'Read the plan — 1 item'
    );
    expect(toolOneLiner('todo_read', { args: '{}', res: 'nope' }, false)).toBe('Read the plan');
  });

  it('removed ui.* echo tools no longer mint one-liners (D1: never invent English)', () => {
    expect(toolOneLiner('ui.chart', { args: '{"label":"Revenue"}' }, true)).toBeNull();
    expect(toolOneLiner('ui.timeline', { args: '{"events":[]}' }, false)).toBeNull();
    expect(toolOneLiner('ui.preview', { args: '{"url":"https://x"}' }, false)).toBeNull();
    expect(SENTENCE_TABLE['ui.chart']).toBeUndefined();
    expect(SENTENCE_TABLE['ui.timeline']).toBeUndefined();
    expect(SENTENCE_TABLE['ui.preview']).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// schedule tool (integrate-scheduler): action-driven one-liners and facts
// ---------------------------------------------------------------------------

const SCHEDULE_CREATED = JSON.stringify({
  id: 'sch_01',
  name: 'morning-digest',
  kind: 'recurring',
  schedule: '09:00 · Mon–Fri',
  next_run_at: '2026-09-14T09:00:00+07:00',
  delivery: 'thread',
  result: 'Created schedule "morning-digest" (09:00 · Mon–Fri) — it fires next at 2026-09-14T09:00:00+07:00.',
});

describe('schedule tool one-liners (integrate-scheduler)', () => {
  it('create: names the schedule and appends the trusted label on done', () => {
    const args = JSON.stringify({ action: 'create', name: 'morning-digest', kind: 'recurring', expression: '0 9 * * 1-5' });
    expect(toolOneLiner('schedule', { args }, true)).toBe("Scheduling 'morning-digest'");
    expect(toolOneLiner('schedule', { args, res: SCHEDULE_CREATED }, false)).toBe(
      "Scheduled 'morning-digest' · 09:00 · Mon–Fri"
    );
    // No result yet (or unparseable): outcome without a fact (D2).
    expect(toolOneLiner('schedule', { args }, false)).toBe("Scheduled 'morning-digest'");
  });

  it('create: channel delivery appends the target channel chip', () => {
    const args = JSON.stringify({ action: 'create', name: 'morning-digest', kind: 'recurring', expression: '0 9 * * 1-5', delivery: 'channel', channel_id: 'ch-ops' });
    expect(toolOneLiner('schedule', { args, res: SCHEDULE_CREATED }, false)).toBe(
      "Scheduled 'morning-digest' · 09:00 · Mon–Fri → `ch-ops`"
    );
  });

  it('list: bare sentence with a count fact only when schedules exist', () => {
    const args = '{"action":"list"}';
    expect(toolOneLiner('schedule', { args }, true)).toBe('Listing schedules');
    const three = JSON.stringify({ schedules: [{ id: 'a' }, { id: 'b' }, { id: 'c' }], result: '3 schedules' });
    expect(toolOneLiner('schedule', { args, res: three }, false)).toBe('Listed schedules — 3 schedules');
    const one = JSON.stringify({ schedules: [{ id: 'a' }], result: '1' });
    expect(toolOneLiner('schedule', { args, res: one }, false)).toBe('Listed schedules — 1 schedule');
    expect(toolOneLiner('schedule', { args, res: JSON.stringify({ schedules: [] }) }, false)).toBe(
      'Listed schedules'
    );
  });

  it('update: identifies the schedule by id chip and appends the new label', () => {
    const args = '{"action":"update","id":"sch_01","enabled":false}';
    expect(toolOneLiner('schedule', { args }, true)).toBe('Updating schedule `sch_01`');
    const res = JSON.stringify({ id: 'sch_01', name: 'morning-digest', schedule: '09:00 · Mon–Fri', enabled: false, result: 'Updated' });
    expect(toolOneLiner('schedule', { args, res }, false)).toBe(
      'Updated schedule `sch_01` · 09:00 · Mon–Fri'
    );
  });

  it('delete: identifies the schedule by id chip', () => {
    const args = '{"action":"delete","id":"sch_01"}';
    expect(toolOneLiner('schedule', { args }, true)).toBe('Deleting schedule `sch_01`');
    expect(toolOneLiner('schedule', { args, res: '{"id":"sch_01","result":"Deleted"}' }, false)).toBe(
      'Deleted schedule `sch_01`'
    );
  });

  it('unknown action mints nothing (D1)', () => {
    expect(toolOneLiner('schedule', { args: '{"action":"noop"}' }, false)).toBeNull();
  });

  it('expanded view lists the verified arg keys in spec order', () => {
    expect(toolFieldSpec('schedule')).toEqual([
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
    ]);
  });
});

// ---------------------------------------------------------------------------
// document.* tools (add-document-read-tool / add-document-create-tool):
// one-liners, trusted facts, expanded field specs, and the download URL
// ---------------------------------------------------------------------------

const DOCUMENT_READ_OK = JSON.stringify({
  path: 'invoice.pdf',
  name: 'invoice.pdf',
  markdown: 'Invoice #42\n\nSeller: Acme',
});
const DOCUMENT_READ_TRUNCATED = JSON.stringify({
  path: 'big.pdf',
  name: 'big.pdf',
  markdown: 'x'.repeat(32) + '\n\n[Output truncated at 200 KB — use filesystem or grep tools if you need to search specific sections]',
});
const DOCUMENT_CREATED = JSON.stringify({
  path: 'invoice.xlsx',
  format: 'xlsx',
  url: '/api/v1/files/abc123',
  result: 'Created invoice.xlsx',
});

describe('document tools one-liners (document.* family)', () => {
  it('document.read: intent while running, outcome when done', () => {
    const args = '{"path":"invoice.pdf"}';
    expect(toolOneLiner('document.read', { args }, true)).toBe('Reading document `invoice.pdf`');
    expect(toolOneLiner('document.read', { args, res: DOCUMENT_READ_OK }, false)).toBe(
      'Read document `invoice.pdf`'
    );
  });

  it('document.read: appends the truncation fact only from the trusted marker', () => {
    const args = '{"path":"big.pdf"}';
    expect(toolOneLiner('document.read', { args, res: DOCUMENT_READ_TRUNCATED }, false)).toBe(
      'Read document `big.pdf` · truncated'
    );
    // Plain reads and unparseable results append nothing (D2).
    expect(toolOneLiner('document.read', { args, res: DOCUMENT_READ_OK }, false)).toBe(
      'Read document `big.pdf`'
    );
    expect(toolOneLiner('document.read', { args, res: 'gateway timeout' }, false)).toBe(
      'Read document `big.pdf`'
    );
  });

  it('document.create: outcome appends the format fact from the result envelope', () => {
    const args = '{"path":"invoice.xlsx","format":"xlsx","data":{}}';
    expect(toolOneLiner('document.create', { args }, true)).toBe('Creating document `invoice.xlsx`');
    expect(toolOneLiner('document.create', { args, res: DOCUMENT_CREATED }, false)).toBe(
      'Created document `invoice.xlsx` · xlsx'
    );
    // No result yet (or unparseable): outcome without a fact (D2).
    expect(toolOneLiner('document.create', { args }, false)).toBe('Created document `invoice.xlsx`');
    expect(toolOneLiner('document.create', { args, res: 'nope' }, false)).toBe(
      'Created document `invoice.xlsx`'
    );
  });

  it('expanded view lists the verified arg keys in spec order', () => {
    expect(toolFieldSpec('document.read')).toEqual([
      { key: 'path', label: 'Path', kind: 'chip' },
    ]);
    expect(toolFieldSpec('document.create')).toEqual([
      { key: 'path', label: 'Path', kind: 'chip' },
      { key: 'format', label: 'Format', kind: 'enum' },
      { key: 'template', label: 'Template', kind: 'chip' },
    ]);
  });
});

describe('createdDocumentURL (document.create delivery)', () => {
  it('extracts the capability URL from the trusted envelope', () => {
    expect(createdDocumentURL(DOCUMENT_CREATED)).toBe('/api/v1/files/abc123');
  });

  it('returns null for absent, empty, or unparseable envelopes', () => {
    expect(createdDocumentURL(undefined)).toBeNull();
    expect(createdDocumentURL('')).toBeNull();
    expect(createdDocumentURL('not json')).toBeNull();
    expect(createdDocumentURL('{"format":"xlsx"}')).toBeNull();
    expect(createdDocumentURL('{"url":""}')).toBeNull();
  });
});

describe('formatLatency', () => {
  it('renders milliseconds below one second', () => {
    expect(formatLatency(0)).toBe('0 ms');
    expect(formatLatency(7)).toBe('7 ms');
    expect(formatLatency(500)).toBe('500 ms');
    expect(formatLatency(999.4)).toBe('999 ms');
  });

  it('rolls over to seconds at 1000ms, one decimal, trailing .0 trimmed', () => {
    expect(formatLatency(999.6)).toBe('1 s');
    expect(formatLatency(1000)).toBe('1 s');
    expect(formatLatency(11264)).toBe('11.3 s');
    expect(formatLatency(2200)).toBe('2.2 s');
    expect(formatLatency(119950)).toBe('120 s');
  });

  it('never goes negative or NaN', () => {
    expect(formatLatency(-5)).toBe('0 ms');
    expect(formatLatency(NaN)).toBe('0 ms');
  });
});

// ---------------------------------------------------------------------------
// blockedByHook — hook enforcement detection (integrate-agent-hooks)
// ---------------------------------------------------------------------------

describe('blockedByHook', () => {
  it('detects the block envelope and extracts hook + reason', () => {
    expect(
      blockedByHook('{"blocked_by_hook":true,"hook":"Policy Gate","reason":"shell is not allowed"}')
    ).toEqual({ hook: 'Policy Gate', reason: 'shell is not allowed' });
  });

  it('tolerates a missing hook name when a reason is present', () => {
    expect(blockedByHook('{"blocked_by_hook":true,"reason":"denied"}')).toEqual({
      hook: '',
      reason: 'denied',
    });
  });

  it('returns null for plain tool results, error strings, and non-hook JSON', () => {
    expect(blockedByHook('page body')).toBeNull();
    expect(blockedByHook('')).toBeNull();
    expect(blockedByHook(undefined)).toBeNull();
    expect(blockedByHook('{"results":[]}')).toBeNull();
    expect(blockedByHook('{"blocked_by_hook":false,"hook":"x","reason":"y"}')).toBeNull();
    expect(blockedByHook('[1,2,3]')).toBeNull();
  });

  it('returns null for partial/stream JSON (mid-stream edge)', () => {
    expect(blockedByHook('{"blocked_by_hook":tru')).toBeNull();
  });
});
