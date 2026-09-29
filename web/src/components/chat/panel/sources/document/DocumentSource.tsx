// Document preview source (add-reference-documents task 8.3): the right-panel
// pane for one workspace reference document. The payload is the capability-URL
// pair the Documents pane hands over ({name, url}) — payload identity is
// name + url, so reopening the same document focuses its existing tab.
// Rendering dispatches by file type: pdf/html embed the capability URL in an
// iframe; md/txt/csv fetch text and ride the same renderers as the file
// source (markdown pipeline / CsvTable); office formats degrade to a captioned
// download card. A tab opened by a chat citation chip (task 10.2) carries only
// the document's name — agents don't know capability URLs — so the workspace
// library is consulted once to resolve the URL by exact name.
import { useEffect, useRef, useState } from "react";
import { API_ORIGIN } from "../../../../../lib/api";
import { documentsApi } from "../../../../../lib/documentsApi";
import { useWorkspace } from "../../../../../store";
import { extractFrontmatter } from "../../../../../lib/panel/prerender";
import { MarkdownBody } from "../../../AgentMessage";
import { CsvTable } from "../../CsvTable";
import { DegradeCard } from "../../DegradeCard";

/** Text documents above this render a head excerpt + download instead of the
 * whole body — the file source's same threshold (task 3.3 precedent). */
const TEXT_MAX_BYTES = 512 * 1024;
const EXCERPT_CHARS = 4096;

type DocumentKind = 'frame' | 'text' | 'csv' | 'degrade';

const FRAME_EXT = new Set(['pdf', 'html', 'htm']);
const TEXT_EXT = new Set(['md', 'markdown', 'txt']);
const OFFICE_EXT: Record<string, string> = {
  docx: 'Word document',
  xlsx: 'Excel spreadsheet',
  pptx: 'PowerPoint deck',
};

function extensionOf(name: string): string {
  const dot = name.lastIndexOf('.');
  return dot === -1 ? '' : name.slice(dot + 1).toLowerCase();
}

function dispatch(ext: string): DocumentKind {
  if (FRAME_EXT.has(ext)) return 'frame';
  if (TEXT_EXT.has(ext)) return 'text';
  if (ext === 'csv') return 'csv';
  return 'degrade';
}

/** Capability URLs may be workspace-relative; absolute URLs ride as-is. */
export function resolveCapabilityUrl(url: string): string {
  if (/^https?:\/\//i.test(url)) return url;
  return `${API_ORIGIN}${url.startsWith('/') ? '' : '/'}${url}`;
}

export function DocumentSource({ tab }: { tab: any; ctx?: any }) {
  const name = typeof tab?.payload?.name === 'string' ? tab.payload.name : '';
  const url = typeof tab?.payload?.url === 'string' ? tab.payload.url : '';

  // Resolve-by-name fallback (task 10.2): citation-chip tabs arrive with only
  // the document's name. One workspace-library lookup resolves the capability
  // URL by exact name; no match (or a failed lookup) is the not-found state.
  const tenant = useWorkspace() as any;
  const ws: string = tenant?.sub ?? tenant?.id ?? tenant?.slug ?? '';
  const [lookup, setLookup] = useState<
    { phase: 'idle' | 'resolving' | 'notfound' } | { phase: 'resolved'; url: string }
  >({ phase: 'idle' });

  useEffect(() => {
    if (url || !name || !ws) {
      setLookup({ phase: 'idle' });
      return;
    }
    let alive = true;
    setLookup({ phase: 'resolving' });
    documentsApi
      .list(ws)
      .then((res) => {
        if (!alive) return;
        const match = (res?.documents || []).find((d) => d.name === name);
        setLookup(match ? { phase: 'resolved', url: match.url } : { phase: 'notfound' });
      })
      .catch(() => {
        if (alive) setLookup({ phase: 'notfound' });
      });
    return () => {
      alive = false;
    };
  }, [name, url, ws]);

  const effectiveUrl = url || (lookup.phase === 'resolved' ? lookup.url : '');
  const ext = extensionOf(name);
  const kind = dispatch(ext);

  const [state, setState] = useState<
    | { phase: 'idle' }
    | { phase: 'loading' }
    | { phase: 'error'; message: string }
    | { phase: 'ready'; text: string }
  >({ phase: 'idle' });
  const seq = useRef(0);

  useEffect(() => {
    if (kind !== 'text' && kind !== 'csv') return;
    if (!effectiveUrl) {
      setState({ phase: 'error', message: 'This document has no file URL.' });
      return;
    }
    const id = ++seq.current;
    const abort = new AbortController();
    setState({ phase: 'loading' });
    fetch(resolveCapabilityUrl(effectiveUrl), { signal: abort.signal })
      .then((res) => {
        if (!res.ok) throw new Error(`document fetch failed (HTTP ${res.status})`);
        return res.arrayBuffer();
      })
      .then((buf) => {
        if (seq.current !== id || abort.signal.aborted) return;
        setState({ phase: 'ready', text: new TextDecoder().decode(buf) });
      })
      .catch((err: unknown) => {
        if (seq.current !== id || abort.signal.aborted) return;
        setState({ phase: 'error', message: err instanceof Error ? err.message : String(err) });
      });
    return () => abort.abort();
  }, [effectiveUrl, kind]);

  if (!name) {
    return (
      <div className="p-4" data-od-id="panel-document-notfound">
        <p className="text-[12.5px] font-medium text-fg2">Document unavailable</p>
        <p className="mt-1 text-[12px] text-muted">This preview has no document attached — it may have been deleted.</p>
      </div>
    );
  }

  if (!effectiveUrl) {
    if (lookup.phase === 'resolving') {
      return (
        <div className="flex items-center gap-2 p-4 text-[12px] text-muted" data-od-id="panel-document-resolving">
          <span className="od-dot"/><span className="od-dot"/><span className="od-dot"/>
          <span>Resolving {name}…</span>
        </div>
      );
    }
    return (
      <div className="p-4" data-od-id="panel-document-notfound">
        <p className="text-[12.5px] font-medium text-fg2">Document unavailable</p>
        <p className="mt-1 text-[12px] text-muted">
          No reference document named {name} was found — it may have been deleted or renamed.
        </p>
      </div>
    );
  }

  if (state.phase === 'loading') {
    return (
      <div className="flex items-center gap-2 p-4 text-[12px] text-muted" data-od-id="panel-document-loading">
        <span className="od-dot"/><span className="od-dot"/><span className="od-dot"/>
        <span>Loading {name}…</span>
      </div>
    );
  }
  if (state.phase === 'error') {
    return (
      <div className="p-4" data-od-id="panel-document-error">
        <p className="text-[12.5px] font-medium text-danger">Couldn't load {name}</p>
        <p className="mt-1 text-[12px] text-muted">{state.message}</p>
      </div>
    );
  }

  const resolvedUrl = resolveCapabilityUrl(effectiveUrl);

  switch (kind) {
    case 'frame':
      return (
        <div className="flex h-full flex-col p-3">
          <iframe
            src={resolvedUrl}
            title={name}
            data-od-id="panel-document-frame"
            className="h-full min-h-[360px] w-full flex-1 rounded-lg border border-line bg-surface"
          />
        </div>
      );
    case 'csv':
      return (
        <div className="p-2">
          <CsvTable text={state.phase === 'ready' ? state.text : ''} />
        </div>
      );
    case 'text': {
      const text = state.phase === 'ready' ? state.text : '';
      if (text.length > TEXT_MAX_BYTES) {
        return (
          <div className="p-3" data-od-id="panel-document-oversize">
            <pre
              className="od-scroll max-h-[60vh] overflow-auto rounded-lg border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] p-3 font-mono text-[12.5px] leading-5 text-fg2"
              data-od-id="panel-document-excerpt"
            >
              {text.slice(0, EXCERPT_CHARS)}
            </pre>
            <p className="mt-1 text-[11px] text-muted">
              Showing the first {EXCERPT_CHARS.toLocaleString()} characters — the file is {(text.length / 1024).toFixed(0)} kB.
            </p>
            <div className="mt-2">
              <DegradeCard title={`Large text document · ${name}`} downloadUrl={resolvedUrl} downloadName={name} />
            </div>
          </div>
        );
      }
      const { meta, body } = extractFrontmatter(text);
      return (
        <div className="p-4">
          {meta && Object.keys(meta).length > 0 ? (
            <div className="mb-3 rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_3%,transparent)] px-2.5 py-2" data-od-id="panel-document-frontmatter">
              {Object.entries(meta).map(([k, v]) => (
                <p key={k} className="truncate text-[11px] leading-5">
                  <span className="font-mono text-muted">{k}</span>
                  <span className="text-fg2"> {v}</span>
                </p>
              ))}
            </div>
          ) : null}
          <div className="md-body text-[14px] leading-relaxed text-fg" data-od-id="panel-document-markdown">
            <MarkdownBody text={body} />
          </div>
        </div>
      );
    }
    default:
      return (
        <div className="p-3">
          <DegradeCard
            title={`${OFFICE_EXT[ext] || 'Document'} · ${name}`}
            subtitle={
              OFFICE_EXT[ext]
                ? "Office formats download — they can't preview inline."
                : "This document type can't be previewed inline."
            }
            downloadUrl={resolvedUrl}
            downloadName={name}
          />
        </div>
      );
  }
}
