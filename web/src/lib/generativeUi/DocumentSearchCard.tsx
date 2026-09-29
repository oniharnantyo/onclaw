// Document search source card (add-reference-documents 10.1) — keyed on the
// `document.search` tool call whose results carry the binding envelope
// {query, hits:[{documentId, document, heading, locator, locatorKind,
// snippet}]}: the query in a pill, a status line ("Searching…" while the call
// runs, the hit count once complete), and one row per hit with the document
// name, its heading, the locator as a monospace chip, and the snippet as
// muted text. Hits stagger on live arrival; hydrated renders are fully
// revealed. Unparsable results fall back to the generic card (the renderer
// returns null) — the same rule WebSearchCard established.

import { Icon } from "../../components/ui/Icon";
import { Reveal, useStaggerReveal } from "./reveal";
import { formatLatency } from "../toolDisplay";

export interface DocumentSearchHit {
  documentId: string;
  document: string;
  heading: string;
  locator: string;
  locatorKind: string;
  snippet: string;
}

/** Strict shape check: a `hits` array whose every entry names its document —
 * anything else is unparsable and the generic tool card remains the fallback.
 * Optional per-hit fields (heading/locator/snippet — txt/csv hits carry no
 * locator) tolerate absence; an unnamed document never renders. */
export function parseDocumentHits(res: Record<string, unknown> | null): DocumentSearchHit[] | null {
  if (!res || !Array.isArray(res.hits)) return null;
  const hits: DocumentSearchHit[] = [];
  for (const item of res.hits) {
    const o = (item !== null && typeof item === 'object' && !Array.isArray(item)
      ? item
      : null) as Record<string, unknown> | null;
    if (!o || typeof o.document !== 'string' || !o.document) return null;
    hits.push({
      documentId: typeof o.documentId === 'string' ? o.documentId : '',
      document: o.document,
      heading: typeof o.heading === 'string' ? o.heading : '',
      locator: typeof o.locator === 'string' ? o.locator : '',
      locatorKind: typeof o.locatorKind === 'string' ? o.locatorKind : '',
      snippet: typeof o.snippet === 'string' ? o.snippet : '',
    });
  }
  return hits;
}

interface DocumentSearchCardProps {
  query: string;
  hits: DocumentSearchHit[];
  running: boolean;
  ms?: number;
  live: boolean;
}

export function DocumentSearchCard({ query, hits, running, ms, live }: DocumentSearchCardProps) {
  const stagger = useStaggerReveal(live);
  return (
    <div
      data-od-id="tool-document.search"
      className="mb-2 overflow-hidden rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]"
    >
      <div className="flex items-center gap-2 px-2.5 py-1.5">
        <Icon name="search" size={13} className="shrink-0 text-meta"/>
        {/* The query IS the card's identity — a pill, like web.search. */}
        <span className="min-w-0 truncate rounded-full bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] px-2 py-0.5 text-[12px] text-fg2" title={query}>
          {query}
        </span>
        <span aria-live="polite" className="ml-auto flex shrink-0 items-center gap-1.5 font-mono text-[10px] text-muted">
          {running ? (
            <>
              Searching
              <span className="od-dot"/><span className="od-dot"/><span className="od-dot"/>
            </>
          ) : (
            <>
              {hits.length === 0 ? 'No matches' : `${hits.length} hit${hits.length === 1 ? '' : 's'}`}
              {typeof ms === 'number' && ms > 0 ? ` · ${formatLatency(ms)}` : ''}
            </>
          )}
        </span>
      </div>
      {hits.length > 0 && (
        <ul className="border-t border-linesoft">
          {hits.map((hit, i) => (
            <Reveal
              as="li"
              key={hit.documentId || `${hit.document}-${i}`}
              index={i}
              stagger={stagger}
              odId={`document-hit-${i}`}
              className="px-2.5 py-1.5 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]"
            >
              <div className="flex items-center gap-2">
                <Icon name="file-text" size={12} className="shrink-0 text-muted"/>
                <span className="min-w-0 shrink-0 truncate text-[12px] font-medium text-fg2" title={hit.document}>
                  {hit.document}
                </span>
                {hit.heading && (
                  <span className="min-w-0 truncate text-[11px] text-muted" title={hit.heading}>
                    {hit.heading}
                  </span>
                )}
                {hit.locator && (
                  <span
                    className="ml-auto shrink-0 rounded border border-line px-1 py-px font-mono text-[9px] uppercase tracking-wide text-muted"
                    title={hit.locatorKind ? `${hit.locatorKind} locator` : 'Locator'}
                  >
                    {hit.locator}
                  </span>
                )}
              </div>
              {hit.snippet && (
                <p className="mt-0.5 line-clamp-2 text-[11px] leading-4 text-muted">{hit.snippet}</p>
              )}
            </Reveal>
          ))}
        </ul>
      )}
    </div>
  );
}
