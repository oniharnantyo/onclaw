/**
 * @vitest-environment jsdom
 */
// Document search card tests (add-reference-documents 10.1): the registry
// entry keyed on `document.search` renders the card for the binding envelope
// ({query, hits}), falls back to the generic card on unparsable results or a
// failed call, and the parser is strict about hits naming their document.
import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import {
  GENERATIVE_UI_TOOLS,
  parseDocumentHits,
  renderGenerativeUi,
} from './index';
import type { GenerativeUiCtx } from './registry';

const ctx = (over: Partial<GenerativeUiCtx> = {}): GenerativeUiCtx => ({
  live: false,
  running: false,
  error: false,
  tool: 'document.search',
  ...over,
});

const ENVELOPE = {
  query: 'sandbox rate limit',
  hits: [
    {
      documentId: 'doc-1',
      document: 'twilio-api.pdf',
      heading: 'Rate Limits',
      locator: 'p. 31',
      locatorKind: 'page',
      snippet: 'Sandbox accounts allow 3 messages per second.',
    },
    {
      documentId: 'doc-2',
      document: 'integration-notes.md',
      heading: '',
      locator: '',
      locatorKind: '',
      snippet: '',
    },
  ],
};

const mount = (
  rawRes: unknown,
  over: Partial<GenerativeUiCtx> = {},
  rawArgs = '{"query":"sandbox rate limit"}',
  ms?: number
) => {
  const el = renderGenerativeUi({
    tool: 'document.search',
    rawArgs,
    rawRes: typeof rawRes === 'string' ? rawRes : JSON.stringify(rawRes),
    ms,
    ctx: ctx(over),
  });
  expect(el).not.toBeNull();
  return render(<>{el}</>);
};

describe('parseDocumentHits — strict shape', () => {
  it('maps a well-formed hits array, tolerating absent optional fields', () => {
    expect(parseDocumentHits(ENVELOPE)).toEqual([
      {
        documentId: 'doc-1',
        document: 'twilio-api.pdf',
        heading: 'Rate Limits',
        locator: 'p. 31',
        locatorKind: 'page',
        snippet: 'Sandbox accounts allow 3 messages per second.',
      },
      { documentId: 'doc-2', document: 'integration-notes.md', heading: '', locator: '', locatorKind: '', snippet: '' },
    ]);
  });

  it('rejects anything without a hits array', () => {
    expect(parseDocumentHits(null)).toBeNull();
    expect(parseDocumentHits({})).toBeNull();
    expect(parseDocumentHits({ hits: 'nope' })).toBeNull();
    expect(parseDocumentHits({ results: [{}] })).toBeNull();
  });

  it('rejects when any hit lacks a non-string document name', () => {
    expect(parseDocumentHits({ hits: [{ document: 'a.pdf' }, {}] })).toBeNull();
    expect(parseDocumentHits({ hits: [{ document: '' }] })).toBeNull();
    expect(parseDocumentHits({ hits: ['twilio-api.pdf'] })).toBeNull();
  });
});

describe('document.search card — registry rendering (10.1)', () => {
  it('is part of the pinned tool-keyed universe', () => {
    expect(GENERATIVE_UI_TOOLS).toContain('document.search');
  });

  it('renders the query pill, hit-count status, and one row per hit', () => {
    const { container } = mount(ENVELOPE, {}, undefined, 1200);
    const card = container.querySelector('[data-od-id="tool-document.search"]')!;
    expect(card).not.toBeNull();
    expect(card.textContent).toContain('sandbox rate limit');
    expect(card.textContent).toContain('2 hits');
    expect(card.textContent).toContain('· 1.2 s');
    expect(container.querySelector('[data-od-id="document-hit-0"]')).not.toBeNull();
    expect(container.querySelector('[data-od-id="document-hit-1"]')).not.toBeNull();
    expect(card.textContent).toContain('twilio-api.pdf');
    expect(card.textContent).toContain('Rate Limits');
    expect(card.textContent).toContain('p. 31');
    expect(card.textContent).toContain('3 messages per second');
  });

  it('a hit with no locator renders without a locator chip (present-only)', () => {
    const { container } = mount({ query: 'q', hits: [{ document: 'notes.md', heading: '', locator: '', locatorKind: '', snippet: '' }] });
    const card = container.querySelector('[data-od-id="tool-document.search"]')!;
    expect(card.textContent).toContain('notes.md');
    expect(card.textContent).not.toContain('p.');
  });

  it('renders the searching state in flight, no rows yet', () => {
    const { container } = mount(null, { running: true });
    const card = container.querySelector('[data-od-id="tool-document.search"]')!;
    expect(card.textContent).toContain('Searching');
    expect(container.querySelector('[data-od-id="document-hit-0"]')).toBeNull();
  });

  it('a completed call with an unparsable envelope falls back to the generic card', () => {
    expect(
      renderGenerativeUi({ tool: 'document.search', rawArgs: '{"query":"q"}', rawRes: 'plain text', ctx: ctx() })
    ).toBeNull();
    expect(
      renderGenerativeUi({ tool: 'document.search', rawArgs: '{"query":"q"}', rawRes: '{"no":"hits"}', ctx: ctx() })
    ).toBeNull();
  });

  it('a failed call falls back even with a parsable envelope', () => {
    expect(
      renderGenerativeUi({ tool: 'document.search', rawArgs: '{"query":"q"}', rawRes: JSON.stringify(ENVELOPE), ctx: ctx({ error: true }) })
    ).toBeNull();
  });

  it('zero hits render the card with a no-matches status and no rows', () => {
    const { container } = mount({ query: 'nothing', hits: [] });
    const card = container.querySelector('[data-od-id="tool-document.search"]')!;
    expect(card.textContent).toContain('No matches');
    expect(container.querySelector('[data-od-id="document-hit-0"]')).toBeNull();
  });

  it('falls back when neither the args nor the envelope carry a query', () => {
    expect(
      renderGenerativeUi({
        tool: 'document.search',
        rawArgs: '{}',
        rawRes: JSON.stringify({ hits: [{ document: 'a.pdf' }] }),
        ctx: ctx(),
      })
    ).toBeNull();
  });
});
