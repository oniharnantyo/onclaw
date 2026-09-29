// Document preview source tests (add-reference-documents 8.3): registration
// through the panel registry, fetch-backed markdown/txt/csv rendering, the
// pdf/html iframe embed, and the office-format degrade card. fetch is stubbed
// at the global level — the capability URL is fetched as-is. The
// resolve-by-name fallback (10.2) mocks the documents API client.
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { registerPanelSource, renderPanelSource, resetPanelRegistries } from '../../../../../lib/panel/registry';
import { DocumentSource, resolveCapabilityUrl } from './DocumentSource';
import { documentsApi } from '../../../../../lib/documentsApi';

vi.mock('../../../../../lib/documentsApi', () => ({
  documentsApi: { list: vi.fn().mockResolvedValue({ documents: [] }) },
}));

function tab(name: string, url: string) {
  return {
    id: 't1',
    kind: 'document',
    title: name,
    payload: { name, url },
    dedupKey: 'document::{"name":' + JSON.stringify(name) + ',"url":' + JSON.stringify(url) + '}',
  };
}

const encoder = new TextEncoder();

function bytesResponse(body: string, contentType: string, status = 200) {
  const buf = encoder.encode(body).buffer;
  return {
    ok: status === 200,
    status,
    arrayBuffer: async () => buf,
    headers: { get: (n: string) => (n === 'Content-Type' ? contentType : null) },
  };
}

const fetchMock = vi.fn();

beforeEach(() => {
  fetchMock.mockReset();
  vi.mocked(documentsApi.list).mockReset().mockResolvedValue({ documents: [] });
  vi.stubGlobal('fetch', fetchMock);
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('DocumentSource — registration', () => {
  it('registers the document kind and dispatches tabs through it', async () => {
    resetPanelRegistries();
    registerPanelSource('document', ({ tab }) => <DocumentSource tab={tab} />);
    expect(renderPanelSource('mystery', { tab: tab('x', '/u'), ctx: {} })).toBeNull();

    fetchMock.mockResolvedValueOnce(bytesResponse('# Handbook', 'text/markdown'));
    const { container } = render(
      <>{renderPanelSource('document', { tab: tab('handbook.md', '/files/wk/docs/handbook.md'), ctx: {} })}</>
    );
    await waitFor(() => {
      expect(container.querySelector('[data-od-id="panel-document-markdown"] h1')).not.toBeNull();
    });
    expect(container.querySelector('[data-od-id="panel-document-markdown"] h1')!.textContent).toBe('Handbook');
  });
});

describe('DocumentSource — markdown and text', () => {
  it('fetches the capability URL and renders markdown through the shared pipeline', async () => {
    const md = '---\ntitle: Q3 Brief\n---\n\n# Q3 Brief\n\nBody prose.';
    fetchMock.mockResolvedValueOnce(bytesResponse(md, 'text/markdown'));
    const { container } = render(<DocumentSource tab={tab('q3-brief.md', '/files/wk/docs/q3-brief.md')} />);
    await waitFor(() => {
      expect(container.querySelector('[data-od-id="panel-document-markdown"] h1')).not.toBeNull();
    });
    expect(container.querySelector('h1')!.textContent).toBe('Q3 Brief');
    expect(container.textContent).toContain('Body prose.');
    // frontmatter surfaced as the compact chip, like the file source
    const chip = container.querySelector('[data-od-id="panel-document-frontmatter"]')!;
    expect(chip.textContent).toContain('Q3 Brief');
    // capability URL fetched as-is (relative resolved against the API origin)
    expect(fetchMock.mock.calls[0][0]).toContain('/files/wk/docs/q3-brief.md');
  });

  it('renders txt through the markdown body', async () => {
    fetchMock.mockResolvedValueOnce(bytesResponse('plain release notes', 'text/plain'));
    const { container } = render(<DocumentSource tab={tab('notes.txt', '/files/wk/docs/notes.txt')} />);
    await waitFor(() => {
      expect(container.querySelector('[data-od-id="panel-document-markdown"]')).not.toBeNull();
    });
    expect(container.textContent).toContain('plain release notes');
  });

  it('renders csv through CsvTable', async () => {
    fetchMock.mockResolvedValueOnce(bytesResponse('name,qty\nbolt,12\n', 'text/csv'));
    const { container } = render(<DocumentSource tab={tab('parts.csv', '/files/wk/docs/parts.csv')} />);
    await waitFor(() => {
      expect(container.querySelector('[data-od-id="panel-csv-table"]')).not.toBeNull();
    });
    expect(container.textContent).toContain('bolt');
  });

  it('degrades over-threshold text to a head excerpt plus download', async () => {
    const big = '# Big\n\n' + 'x'.repeat(512 * 1024 + 16);
    fetchMock.mockResolvedValueOnce(bytesResponse(big, 'text/markdown'));
    const { container } = render(<DocumentSource tab={tab('huge.md', '/files/wk/docs/huge.md')} />);
    await waitFor(() => {
      expect(container.querySelector('[data-od-id="panel-document-oversize"]')).not.toBeNull();
    });
    const excerpt = container.querySelector('[data-od-id="panel-document-excerpt"]')!;
    expect(excerpt.textContent!.length).toBeLessThan(big.length);
    expect(container.querySelector('a[data-od-id="panel-degrade-download"]')).not.toBeNull();
  });

  it('shows a loading state, then an explicit error state on failure', async () => {
    fetchMock.mockRejectedValueOnce(new Error('capability revoked'));
    const { container } = render(<DocumentSource tab={tab('gone.md', '/files/wk/docs/gone.md')} />);
    expect(container.querySelector('[data-od-id="panel-document-loading"]')).not.toBeNull();
    await waitFor(() => {
      const err = container.querySelector('[data-od-id="panel-document-error"]');
      expect(err).not.toBeNull();
      expect(err!.textContent).toContain('capability revoked');
    });
  });
});

describe('DocumentSource — frames and degrades', () => {
  it('embeds pdf and html through an iframe with the capability URL, no fetch', async () => {
    const { container } = render(<DocumentSource tab={tab('invoice.pdf', '/files/wk/docs/invoice.pdf')} />);
    const frame = container.querySelector('iframe[data-od-id="panel-document-frame"]') as HTMLIFrameElement;
    expect(frame).not.toBeNull();
    expect(frame.getAttribute('src')).toContain('/files/wk/docs/invoice.pdf');
    expect(fetchMock).not.toHaveBeenCalled();

    const { container: htmlContainer } = render(
      <DocumentSource tab={tab('spec.html', '/files/wk/docs/spec.html')} />
    );
    const htmlFrame = htmlContainer.querySelector('iframe[data-od-id="panel-document-frame"]') as HTMLIFrameElement;
    expect(htmlFrame).not.toBeNull();
  });

  it('degrades office formats to a captioned card naming the document and download', async () => {
    for (const [ext, label] of [
      ['docx', 'Word document'],
      ['xlsx', 'Excel spreadsheet'],
      ['pptx', 'PowerPoint deck'],
    ] as const) {
      const { container, unmount } = render(
        <DocumentSource tab={tab(`deck.${ext}`, `/files/wk/docs/deck.${ext}`)} />
      );
      await waitFor(() => {
        expect(container.querySelector('[data-od-id="panel-degrade-card"]')).not.toBeNull();
      });
      const card = container.querySelector('[data-od-id="panel-degrade-card"]')!;
      expect(card.textContent).toContain(label);
      expect(card.textContent).toContain(`deck.${ext}`);
      const dl = container.querySelector('a[data-od-id="panel-degrade-download"]') as HTMLAnchorElement;
      expect(dl.getAttribute('download')).toBe(`deck.${ext}`);
      unmount();
    }
  });

  it('resolves a name-only payload through the workspace library, then not-found when unmatched', async () => {
    // Citation-chip tabs (10.2) carry only the document's name — the library
    // lookup runs, misses, and lands in the not-found state.
    const { container } = render(
      <DocumentSource tab={{ id: 't', kind: 'document', title: 'x', payload: { name: 'x' } }} />
    );
    expect(container.querySelector('[data-od-id="panel-document-resolving"]')).not.toBeNull();
    await waitFor(() => {
      expect(container.querySelector('[data-od-id="panel-document-notfound"]')).not.toBeNull();
    });
    expect(documentsApi.list).toHaveBeenCalledTimes(1);
  });

  it('no payload name at all stays the immediate unavailable state', () => {
    const { container } = render(
      <DocumentSource tab={{ id: 't', kind: 'document', title: '?', payload: {} }} />
    );
    expect(container.querySelector('[data-od-id="panel-document-notfound"]')).not.toBeNull();
    expect(documentsApi.list).not.toHaveBeenCalled();
  });
});

describe('DocumentSource — capability URL resolution', () => {
  it('prefixes relative URLs with the API origin and rides absolute ones as-is', () => {
    expect(resolveCapabilityUrl('/files/wk/docs/a.pdf')).toMatch(/\/files\/wk\/docs\/a\.pdf$/);
    expect(resolveCapabilityUrl('https://blob.example.com/wk/a.pdf?sig=1')).toBe(
      'https://blob.example.com/wk/a.pdf?sig=1'
    );
  });
});

describe('DocumentSource — resolve-by-name fallback (10.2)', () => {
  it('matches a name-only payload against the library and previews the resolved URL', async () => {
    vi.mocked(documentsApi.list).mockResolvedValueOnce({
      documents: [
        {
          id: 'doc-9',
          name: 'twilio-api.pdf',
          description: '',
          mime: 'application/pdf',
          size: 1,
          url: '/files/wk/docs/twilio-api.pdf',
          indexStatus: 'ready',
          scope: 'workspace',
          pageCount: 0,
          agents: [],
          channels: [],
          createdAt: '',
        },
      ],
    } as any);
    const { container } = render(
      <DocumentSource tab={{ id: 't', kind: 'document', title: 'twilio-api.pdf', payload: { name: 'twilio-api.pdf' } }} />
    );
    await waitFor(() => {
      const frame = container.querySelector('iframe[data-od-id="panel-document-frame"]') as HTMLIFrameElement | null;
      expect(frame).not.toBeNull();
      expect(frame!.getAttribute('src')).toContain('/files/wk/docs/twilio-api.pdf');
    });
    expect(documentsApi.list).toHaveBeenCalledTimes(1);
  });

  it('does not consult the library when the payload already carries a URL', async () => {
    fetchMock.mockResolvedValueOnce(bytesResponse('# Direct', 'text/markdown'));
    const { container } = render(<DocumentSource tab={tab('handbook.md', '/files/wk/docs/handbook.md')} />);
    await waitFor(() => {
      expect(container.querySelector('[data-od-id="panel-document-markdown"]')).not.toBeNull();
    });
    expect(documentsApi.list).not.toHaveBeenCalled();
  });

  it('a failed library lookup lands in the not-found state', async () => {
    vi.mocked(documentsApi.list).mockRejectedValueOnce(new Error('offline'));
    const { container } = render(
      <DocumentSource tab={{ id: 't', kind: 'document', title: 'g.md', payload: { name: 'g.md' } }} />
    );
    await waitFor(() => {
      expect(container.querySelector('[data-od-id="panel-document-notfound"]')).not.toBeNull();
    });
  });

  it('a resolved text document renders through the markdown pipeline', async () => {
    vi.mocked(documentsApi.list).mockResolvedValueOnce({
      documents: [
        {
          id: 'doc-2',
          name: 'handbook.md',
          description: '',
          mime: 'text/markdown',
          size: 1,
          url: '/files/wk/docs/handbook.md',
          indexStatus: 'ready',
          scope: 'attached',
          pageCount: 0,
          agents: [],
          channels: [],
          createdAt: '',
        },
      ],
    } as any);
    fetchMock.mockResolvedValueOnce(bytesResponse('# Resolved', 'text/markdown'));
    const { container } = render(
      <DocumentSource tab={{ id: 't', kind: 'document', title: 'handbook.md', payload: { name: 'handbook.md' } }} />
    );
    await waitFor(() => {
      expect(container.querySelector('[data-od-id="panel-document-markdown"] h1')!.textContent).toBe('Resolved');
    });
    expect(fetchMock.mock.calls[0][0]).toContain('/files/wk/docs/handbook.md');
  });
});
