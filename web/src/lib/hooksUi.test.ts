// Unit tests for the shared hook vocabulary helpers (integrate-agent-hooks).
// Pure functions — no DOM, no network — so the default environment suffices.
import { describe, it, expect } from 'vitest';
import {
  HOOK_EVENTS,
  hookEventMeta,
  hookToolValueOptions,
  hookValueOptionsFor,
  hookValueNoun,
  matcherTier,
  countMatcherString,
  matcherSummary,
  hookStatusView,
  hookHandlerLabel,
} from './hooksUi';

describe('hooksUi — events', () => {
  it('marks exactly the two blocking seams (D1)', () => {
    const blocking = HOOK_EVENTS.filter((e) => e.blocking).map((e) => e.value);
    expect(blocking).toEqual(['user_prompt_submit', 'pre_tool_use']);
  });

  it('maps each event to its applies-to value family (D8)', () => {
    expect(hookEventMeta('pre_tool_use').valueKind).toBe('tools');
    expect(hookEventMeta('post_tool_use').valueKind).toBe('tools');
    expect(hookEventMeta('run_started').valueKind).toBe('origins');
    expect(hookEventMeta('user_prompt_submit').valueKind).toBe('origins');
    expect(hookEventMeta('run_finished').valueKind).toBe('statuses');
  });

  it('names the count noun per event (tools / origins / outcomes)', () => {
    expect(hookValueNoun('pre_tool_use')).toBe('tools');
    expect(hookValueNoun('post_tool_use')).toBe('tools');
    expect(hookValueNoun('run_started')).toBe('origins');
    expect(hookValueNoun('user_prompt_submit')).toBe('origins');
    expect(hookValueNoun('run_finished')).toBe('outcomes');
  });
});

describe('hooksUi — applies-to options', () => {
  it('builds tool options plus the family entries over the sanitized MCP segments', () => {
    const options = hookToolValueOptions(
      [
        { key: 'web.fetch', name: 'Web Fetch' },
        { key: 'execute', name: 'Shell' },
      ],
      [
        { id: 's1', name: 'GitHub' },
        { id: 's2', name: 'PostgreSQL Prod' },
      ]
    );

    const values = options.map((o) => o.value);
    expect(values).toContain('web.fetch');
    expect(values).toContain('execute');
    // The browser facade expansion and the per-server families.
    expect(values).toContain('browser.*');
    expect(values).toContain('mcp__github.*');
    // Segments sanitize like the runtime's naming: lowercase, [a-z0-9_-] only.
    expect(values).toContain('mcp__postgresql_prod.*');
    // Family entries are flagged so the UI can group them.
    expect(options.find((o) => o.value === 'browser.*')?.family).toBe(true);
  });

  it('serves the fixed origin and status sets for the other events', () => {
    expect(hookValueOptionsFor('user_prompt_submit', [], []).map((o) => o.value)).toEqual([
      'user',
      'scheduler',
      'channel',
    ]);
    expect(hookValueOptionsFor('run_finished', [], []).map((o) => o.value)).toEqual([
      'completed',
      'failed',
      'cancelled',
    ]);
  });
});

describe('hooksUi — matcher tiers (D19)', () => {
  it('reads empty and * as match-all', () => {
    expect(matcherTier('')).toBe('all');
    expect(matcherTier('   ')).toBe('all');
    expect(matcherTier('*')).toBe('all');
  });

  it('reads charset-valid comma/pipe/space lists as the list tier', () => {
    expect(matcherTier('shell')).toBe('list');
    expect(matcherTier('shell, read_file')).toBe('list');
    expect(matcherTier('shell|read_file')).toBe('list');
    expect(matcherTier('web.* mcp__github.*')).toBe('list');
  });

  it('pushes anything else into the regex tier', () => {
    expect(matcherTier('^web\\.')).toBe('regex');
    expect(matcherTier('^(user|scheduler)$')).toBe('regex');
    // A `*` only belongs at the end of an entry as ".*".
    expect(matcherTier('web.*extra')).toBe('regex');
  });
});

describe('hooksUi — countMatcherString', () => {
  const options = [{ value: 'a' }, { value: 'b' }, { value: 'c' }].map((v) => ({ ...v, label: v.value }));

  it('counts every candidate for the match-all tier', () => {
    expect(countMatcherString('', options)).toBe(3);
    expect(countMatcherString('*', options)).toBe(3);
  });

  it('counts exact and family entries across separators', () => {
    expect(countMatcherString('a, c', options)).toBe(2);
    expect(countMatcherString('a|c', options)).toBe(2);
    expect(countMatcherString('a c', options)).toBe(2);
    // Family "b.*" prefixes "b".
    expect(countMatcherString('b.*', options)).toBe(1);
  });

  it('counts regex-tier strings against the candidates and returns null when uncountable', () => {
    expect(countMatcherString('^(a|c)$', options)).toBe(2);
    expect(countMatcherString('(', options)).toBeNull();
    // Regex tier only: charset-invalid, so the 256 cap applies.
    expect(countMatcherString(`^${'x'.repeat(256)}`, options)).toBeNull();
  });
});

describe('hooksUi — matcherSummary and status', () => {
  it('carries the plain matcher string; empty renders as *', () => {
    expect(matcherSummary({ matcher: 'shell|read_file' })).toBe('shell|read_file');
    expect(matcherSummary({ matcher: '  shell, read_file  ' })).toBe('shell, read_file');
    expect(matcherSummary({ matcher: '^web\\.' })).toBe('^web\\.');
    expect(matcherSummary({ matcher: '*' })).toBe('*');
    expect(matcherSummary({ matcher: '' })).toBe('*');
  });

  it('labels every handler for the row chips, script included (D22)', () => {
    expect(hookHandlerLabel('http')).toBe('Webhook');
    expect(hookHandlerLabel('command')).toBe('Command');
    expect(hookHandlerLabel('mcp_tool')).toBe('MCP tool');
    expect(hookHandlerLabel('prompt')).toBe('Evaluator');
    expect(hookHandlerLabel('script')).toBe('JS');
    expect(hookHandlerLabel('future_thing')).toBe('future_thing');
  });

  it('derives the row health view: enabled switch wins, then delivery status', () => {
    expect(hookStatusView({ enabled: false, status: 'error', status_error: 'x' }).label).toBe('Disabled');
    const errored = hookStatusView({ enabled: true, status: 'error', status_error: 'boom' });
    expect(errored.label).toBe('Error');
    expect(errored.errored).toBe(true);
    expect(hookStatusView({ enabled: true, status: 'ok', status_error: '' }).label).toBe('Healthy');
  });
});
