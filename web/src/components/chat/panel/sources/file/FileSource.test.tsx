// File source renderer tests (add-right-panel tasks 3.1/3.3): fetch is mocked
// at the global level — the files API client just calls fetch with a bearer
// header and reads bytes + Content-Type off the response.
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { FileSource } from './FileSource';

const WS = 'acme';
const AGENT = 'atlas';
const CTX = { ws: WS, agentSlug: AGENT };

function tab(path: string) {
  return { id: 't1', kind: 'file', title: path.split('/').pop(), payload: { path } };
}

const encoder = new TextEncoder();

function bytesResponse(body: string | Uint8Array, contentType: string, status = 200) {
  const buf = typeof body === 'string' ? encoder.encode(body).buffer : body.buffer.slice(body.byteOffset, body.byteOffset + body.byteLength);
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
  vi.stubGlobal('fetch', fetchMock);
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function fileUrlPart(path: string) {
  return `path=${encodeURIComponent(path)}`;
}

describe('FileSource — states', () => {
  it('shows a loading state, then the not-found state naming the file on 404', async () => {
    fetchMock.mockResolvedValueOnce(bytesResponse('', 'text/plain', 404));
    const { container } = render(<FileSource tab={tab('reports/gone.md')} ctx={CTX}/>);
    expect(container.querySelector('[data-od-id="panel-file-loading"]')).not.toBeNull();
    await waitFor(() => {
      const nf = container.querySelector('[data-od-id="panel-file-notfound"]');
      expect(nf).not.toBeNull();
      expect(nf!.textContent).toContain('gone.md');
    });
    // Fetched through the agent files API with the jail-relative path.
    expect(fetchMock.mock.calls[0][0]).toContain(`/workspaces/${WS}/agents/${AGENT}/files?${fileUrlPart('reports/gone.md')}`);
  });

  it('renders an explicit error state when the fetch throws', async () => {
    fetchMock.mockRejectedValueOnce(new Error('files api: 500'));
    const { container } = render(<FileSource tab={tab('x.md')} ctx={CTX}/>);
    await waitFor(() => expect(container.querySelector('[data-od-id="panel-file-error"]')).not.toBeNull());
  });
});

describe('FileSource — markdown', () => {
  it('renders the frontmatter chip and resolves relative assets through the API URL', async () => {
    const md = [
      '---',
      'title: Q3 Report',
      'author: Atlas',
      '---',
      '',
      '# Q3 Report',
      '',
      '![chart](assets/x.png)',
      '',
      'Some prose with `inline code`.',
    ].join('\n');
    fetchMock.mockResolvedValueOnce(bytesResponse(md, 'text/markdown'));
    const { container } = render(<FileSource tab={tab('reports/q3.md')} ctx={CTX}/>);
    await waitFor(() => expect(container.querySelector('[data-od-id="panel-file-frontmatter"]')).not.toBeNull());
    const chip = container.querySelector('[data-od-id="panel-file-frontmatter"]')!;
    expect(chip.textContent).toContain('title');
    expect(chip.textContent).toContain('Q3 Report');
    // Body went through the transcript's shared markdown pipeline.
    expect(container.querySelector('[data-od-id="panel-file-markdown"] h1')!.textContent).toBe('Q3 Report');
    // assets/x.png resolved relative to the file's directory → files API URL.
    const img = container.querySelector('[data-od-id="panel-file-markdown"] img') as HTMLImageElement;
    expect(img.getAttribute('src')).toContain(`/agents/${AGENT}/files?${fileUrlPart('reports/assets/x.png')}`);
  });

  it('renders plain markdown without frontmatter untouched', async () => {
    fetchMock.mockResolvedValueOnce(bytesResponse('# Just a heading', 'text/markdown'));
    const { container } = render(<FileSource tab={tab('readme.md')} ctx={CTX}/>);
    await waitFor(() => expect(container.querySelector('[data-od-id="panel-file-markdown"]')).not.toBeNull());
    expect(container.querySelector('[data-od-id="panel-file-frontmatter"]')).toBeNull();
    expect(container.querySelector('h1')!.textContent).toBe('Just a heading');
  });
});

describe('FileSource — code, pdf, image, csv', () => {
  it('renders a code file through the shiki highlighter', async () => {
    const go = 'package main\n\nfunc main() {}\n';
    fetchMock.mockResolvedValueOnce(bytesResponse(go, 'text/plain; charset=utf-8'));
    const { container } = render(<FileSource tab={tab('main.go')} ctx={CTX}/>);
    const codeBlock = await waitFor(() => {
      const el = container.querySelector('[data-od-id="panel-file-code"]');
      expect(el).not.toBeNull();
      return el!;
    });
    expect(codeBlock.textContent).toContain('func main()');
  });

  it('embeds a PDF via the files API URL', async () => {
    fetchMock.mockResolvedValueOnce(bytesResponse(new Uint8Array([1, 2, 3]), 'application/pdf'));
    const { container } = render(<FileSource tab={tab('invoice.pdf')} ctx={CTX}/>);
    await waitFor(() => expect(container.querySelector('[data-od-id="panel-file-pdf"]')).not.toBeNull());
    // Dispatch needed one byte fetch; the <object> itself serves by URL.
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][0]).toContain(fileUrlPart('invoice.pdf'));
  });

  it('renders an image with the filename as alt', async () => {
    fetchMock.mockResolvedValueOnce(bytesResponse(new Uint8Array([137, 80]), 'image/png'));
    const { container } = render(<FileSource tab={tab('shots/shot.png')} ctx={CTX}/>);
    const img = await waitFor(() => {
      const el = container.querySelector('img[data-od-id="panel-file-image"]') as HTMLImageElement | null;
      expect(el).not.toBeNull();
      return el!;
    });
    expect(img.alt).toBe('shot.png');
    expect(img.src).toContain(fileUrlPart('shots/shot.png'));
  });

  it('renders CSV through CsvTable', async () => {
    fetchMock.mockResolvedValueOnce(bytesResponse('name,qty\nbolt,12\n', 'text/csv'));
    const { container } = render(<FileSource tab={tab('parts.csv')} ctx={CTX}/>);
    await waitFor(() => expect(container.querySelector('[data-od-id="panel-csv-table"]')).not.toBeNull());
    expect(container.textContent).toContain('bolt');
  });
});

describe('FileSource — degrades', () => {
  it('degrades xlsx to a captioned card with a download affordance', async () => {
    fetchMock.mockResolvedValueOnce(bytesResponse(new Uint8Array([1, 2]), 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'));
    const { container } = render(<FileSource tab={tab('invoice.xlsx')} ctx={CTX}/>);
    const card = await waitFor(() => {
      const el = container.querySelector('[data-od-id="panel-degrade-card"]');
      expect(el).not.toBeNull();
      return el!;
    });
    expect(card.textContent).toContain('Excel spreadsheet');
    expect(card.textContent).toContain('invoice.xlsx');
    const dl = container.querySelector('a[data-od-id="panel-degrade-download"]') as HTMLAnchorElement;
    expect(dl.getAttribute('href')).toContain(fileUrlPart('invoice.xlsx'));
    expect(dl.getAttribute('download')).toBe('invoice.xlsx');
  });

  it('degrades over-threshold text to a head excerpt plus download', async () => {
    const big = '# Big\n\n' + 'x'.repeat(512 * 1024 + 16);
    fetchMock.mockResolvedValueOnce(bytesResponse(big, 'text/markdown'));
    const { container } = render(<FileSource tab={tab('huge.md')} ctx={CTX}/>);
    await waitFor(() => expect(container.querySelector('[data-od-id="panel-file-oversize"]')).not.toBeNull());
    const excerpt = container.querySelector('[data-od-id="panel-file-excerpt"]')!;
    expect(excerpt.textContent!.length).toBeLessThan(big.length);
    expect(excerpt.textContent).toContain('Big');
    expect(container.querySelector('a[data-od-id="panel-degrade-download"]')).not.toBeNull();
  });

  it('degrades svg (served as an attachment) to a download card', async () => {
    fetchMock.mockResolvedValueOnce(bytesResponse('<svg/>', 'image/svg+xml'));
    const { container } = render(<FileSource tab={tab('logo.svg')} ctx={CTX}/>);
    const card = await waitFor(() => {
      const el = container.querySelector('[data-od-id="panel-degrade-card"]');
      expect(el).not.toBeNull();
      return el!;
    });
    expect(card.textContent).toContain('logo.svg');
    expect(container.querySelector('img[data-od-id="panel-file-image"]')).toBeNull();
  });
});
