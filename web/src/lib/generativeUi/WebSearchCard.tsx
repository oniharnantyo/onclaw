// Web search source card (generative-ui spec) — the first real-tool registry
// entry, keyed on the existing `web.search` tool call whose results carry
// {title,url,snippet}: the query in a pill, a status line ("Searching…" while
// the call runs, "Read N sources" once complete), and one row per source with
// a domain-mark avatar, the result title, and the source domain in monospace.
// Sources stagger on live arrival; hydrated renders are fully revealed.
// Unparsable results fall back to the generic card (the renderer returns
// null) — the generic list and this card never render together.

import { Icon } from "../../components/ui/Icon";
import { Reveal, useStaggerReveal } from "./reveal";
import { formatLatency } from "../toolDisplay";

export interface SearchSource {
  title: string;
  url: string;
  host: string;
}

function hostOf(url: string): string {
  try {
    return new URL(url).hostname;
  } catch {
    return '';
  }
}

/** Strict shape check: a `results` array of {title,url} objects — anything
 * else is unparsable and the generic tool card remains the fallback. */
export function parseSearchSources(res: Record<string, unknown> | null): SearchSource[] | null {
  if (!res || !Array.isArray(res.results)) return null;
  return res.results.map((item: unknown) => {
    const o = (item !== null && typeof item === 'object' ? item : {}) as Record<string, unknown>;
    const url = typeof o.url === 'string' ? o.url : '';
    const title = typeof o.title === 'string' && o.title ? o.title : url;
    return { title, url, host: hostOf(url) };
  });
}

interface WebSearchCardProps {
  query: string;
  sources: SearchSource[];
  running: boolean;
  ms?: number;
  live: boolean;
}

export function WebSearchCard({ query, sources, running, ms, live }: WebSearchCardProps) {
  const stagger = useStaggerReveal(live);
  return (
    <div
      data-od-id="tool-web.search"
      className="mb-2 overflow-hidden rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]"
    >
      <div className="flex items-center gap-2 px-2.5 py-1.5">
        <Icon name="search" size={13} className="shrink-0 text-meta"/>
        {/* The query IS the card's identity — a pill, not the generic one-liner. */}
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
              Read {sources.length} source{sources.length === 1 ? '' : 's'}
              {typeof ms === 'number' && ms > 0 ? ` · ${formatLatency(ms)}` : ''}
            </>
          )}
        </span>
      </div>
      {sources.length > 0 && (
        <ul className="border-t border-linesoft">
          {sources.map((source, i) => (
            <Reveal
              as="li"
              key={source.url || `row-${i}`}
              index={i}
              stagger={stagger}
              odId={`search-source-${i}`}
              className="flex items-center gap-2 px-2.5 py-1.5 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]"
            >
              <span
                aria-hidden="true"
                className="flex h-[18px] w-[18px] shrink-0 items-center justify-center rounded-[4px] bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] text-[10px] font-semibold uppercase text-accent"
              >
                {(source.host || source.title || '?').charAt(0)}
              </span>
              {source.url ? (
                <a
                  href={source.url}
                  target="_blank"
                  rel="noreferrer"
                  title={source.title}
                  className="min-w-0 flex-1 truncate text-[12px] text-fg2 transition-colors hover:text-accent"
                >
                  {source.title || '(untitled)'}
                </a>
              ) : (
                <span className="min-w-0 flex-1 truncate text-[12px] text-fg2">{source.title || '(untitled)'}</span>
              )}
              {source.host && <span className="shrink-0 font-mono text-[10px] text-muted">{source.host}</span>}
            </Reveal>
          ))}
        </ul>
      )}
    </div>
  );
}
