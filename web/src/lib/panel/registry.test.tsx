/**
 * @vitest-environment jsdom
 */
// Panel registry tests (add-right-panel 1.3): registration, dispatch, and the
// candidate rules — matchers map transcript tool cards, the first match wins,
// and the affordances derived from a live transcript fixture are byte-equal
// to the ones derived from a rehydrated fixture (design D2: candidates come
// from folded tool cards, never raw live events, so old conversations keep
// their affordances).
import { describe, it, expect, beforeEach } from 'vitest';
import {
  collectPanelCandidates,
  matchPanelCandidate,
  panelDedupKey,
  registerPanelCandidate,
  registerPanelSource,
  registeredPanelSources,
  renderPanelSource,
  resetPanelRegistries,
} from './registry';

beforeEach(() => {
  resetPanelRegistries();
});

describe('lib/panel/registry — sources', () => {
  it('registers a source by kind and dispatches the tab through it', () => {
    registerPanelSource('members', ({ tab }) => 'members:' + tab.payload.chatId);
    expect(registeredPanelSources()).toEqual(['members']);
    const out = renderPanelSource('members', {
      tab: { id: 'tab1', kind: 'members', title: 'Members', payload: { chatId: 'c-ops' }, dedupKey: 'k' },
      ctx: {},
    });
    expect(out).toBe('members:c-ops');
  });

  it('an unregistered kind renders nothing (never raw payload JSON)', () => {
    expect(renderPanelSource('mystery', {
      tab: { id: 't', kind: 'mystery', title: '?', payload: { secret: 1 }, dedupKey: 'k' },
      ctx: {},
    })).toBeNull();
  });

  it('re-registering a kind replaces its renderer (last registration wins)', () => {
    registerPanelSource('members', () => 'first');
    registerPanelSource('members', () => 'second');
    expect(registeredPanelSources()).toEqual(['members']);
    expect(renderPanelSource('members', { tab: { id: 't', kind: 'members', title: 'M', payload: {}, dedupKey: 'k' }, ctx: {} })).toBe('second');
  });
});

describe('lib/panel/registry — candidates', () => {
  it('a matcher maps a transcript tool card to a candidate; no match is null', () => {
    registerPanelCandidate((card) =>
      card.name === 'document.create'
        ? { kind: 'file', title: String(JSON.parse(card.res || '{}').name || ''), payload: { path: 'docs/' } }
        : null
    );
    expect(matchPanelCandidate({ name: 'document.create', res: '{"name":"brief.md"}' })).toEqual({
      kind: 'file', title: 'brief.md', payload: { path: 'docs/' },
    });
    expect(matchPanelCandidate({ name: 'execute', args: '{"command":"ls"}' })).toBeNull();
    expect(matchPanelCandidate({})).toBeNull();
  });

  it('the first registered matching matcher wins', () => {
    registerPanelCandidate(() => ({ kind: 'a', title: 'A', payload: {} }));
    registerPanelCandidate(() => ({ kind: 'b', title: 'B', payload: {} }));
    expect(matchPanelCandidate({ name: 'anything' })?.kind).toBe('a');
  });

  it('a matcher returning null falls through to the next matcher', () => {
    registerPanelCandidate((card) => (card.name === 'x' ? null : { kind: 'a', title: 'A', payload: {} }));
    registerPanelCandidate(() => ({ kind: 'b', title: 'B', payload: {} }));
    expect(matchPanelCandidate({ name: 'x' })?.kind).toBe('b');
  });

  it('collectPanelCandidates folds a transcript of tool cards, skipping gaps', () => {
    registerPanelCandidate((card) => (card.name?.startsWith('browser.') ? { kind: 'browser', title: 'Browser', payload: { session: 's1' } } : null));
    const messages = [
      { id: 'u1', author: 'you', text: 'go' },
      { id: 'm1', author: 'agent', tools: [
        { callId: 'c1', name: 'browser.navigate', args: '{"url":"https://x"}', res: '{"ok":true}', ms: 5 },
        { callId: 'c2', name: 'execute', args: '{}', res: 'ok', ms: 1 },
      ] },
      { id: 'm2', author: 'agent', text: 'done', tools: undefined },
      { id: 'm3', author: 'agent', tools: [null, { callId: 'c3', name: 'browser.screenshot', res: 'png', ms: 2 }] },
    ];
    expect(collectPanelCandidates(messages)).toEqual([
      { kind: 'browser', title: 'Browser', payload: { session: 's1' } },
      { kind: 'browser', title: 'Browser', payload: { session: 's1' } },
    ]);
    expect(collectPanelCandidates([])).toEqual([]);
  });

  it('parity: a live turn transcript and its rehydrated form yield identical candidates', () => {
    registerPanelCandidate((card) => {
      if (card.name !== 'document.create') return null;
      let name = '';
      try { name = String(JSON.parse(card.res || '{}').name || ''); } catch { /* torn result */ }
      return { kind: 'file', title: name, payload: { path: 'docs/' + name } };
    });
    // The live fold and the history replay produce the same card shape
    // ({callId, name, args, res, ms, error}) by construction (livechat.ts) —
    // these fixtures differ only in origin, proving the derivation does not.
    const liveTurn = [
      { id: 'm1', author: 'agent', tools: [
        { callId: 'c1', name: 'document.create', args: '{"name":"brief.md"}', res: '{"name":"brief.md","url":"/files/k/brief.md"}', ms: 210 },
      ] },
    ];
    const rehydrated = [
      { id: 'h-m1', author: 'agent', ts: '', tools: [
        { callId: 'c1', name: 'document.create', args: '{"name":"brief.md"}', res: '{"name":"brief.md","url":"/files/k/brief.md"}', ms: 210 },
      ] },
    ];
    expect(collectPanelCandidates(rehydrated)).toEqual(collectPanelCandidates(liveTurn));
  });
});

describe('lib/panel/registry — dedup keys', () => {
  it('payload identity is property-order independent', () => {
    expect(panelDedupKey('file', { path: 'a.md', ws: 'acme' }))
      .toBe(panelDedupKey('file', { ws: 'acme', path: 'a.md' }));
  });

  it('different kind or payload means a different tab', () => {
    expect(panelDedupKey('file', { path: 'a.md' })).not.toBe(panelDedupKey('browser', { path: 'a.md' }));
    expect(panelDedupKey('file', { path: 'a.md' })).not.toBe(panelDedupKey('file', { path: 'b.md' }));
  });
});
