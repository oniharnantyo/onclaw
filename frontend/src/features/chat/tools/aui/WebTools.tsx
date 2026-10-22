/**
 * The web tools' renderers (task 4.2, design D6): `web_search` renders the
 * vendored `WebSearch` — the query pill, a searching state while the call
 * runs, results landing one after another (staggered arrival disabled under
 * reduced motion) — plus the sources line and the full result cards (linked
 * title, hostname, snippet) the spec requires and the element's landing rows
 * do not carry. `web_fetch` shows the fetched address and the readable text,
 * clamped.
 */
import { useMemo } from "react";
import type { ReactElement } from "react";
import type { ToolCallMessagePartProps } from "@assistant-ui/react";

import { WebSearch } from "@/components/assistant-ui/elements/web-search";
import type { WebSearchResult } from "@/components/assistant-ui/elements/web-search";

import { isObject, isString, parseArgs, parseJSONResult, str } from "../parse";
import { urlHost } from "../toolDisplay";
import { useReducedMotion } from "../useReducedMotion";
import { ClampedText } from "../primitives/ClampedText";
import { useRevealCount } from "./reveal";
import { ToolCard } from "./ToolCallFrame";

/** The narrow type guard the bodies read results through. */
function isTextResult(value: unknown): value is string {
  return typeof value === "string";
}

/** The trusted wire shape of one web_search result. */
interface SearchResult {
  title: string;
  url: string;
  snippet: string;
}

/** Parses the `{"results":[...]}` payload; null when the result is not that
 * shape (a plain-text failure message) so the raw text shows. */
function parseSearchResults(result: string): SearchResult[] | null {
  const parsed = parseJSONResult(result);

  if (!isObject(parsed)) return null;

  const list = parsed.results;

  if (!Array.isArray(list)) return null;

  const results: SearchResult[] = [];

  for (const entry of list) {
    if (!isObject(entry)) return null;

    if (!isString(entry.title) || !isString(entry.url)) return null;

    results.push({
      title: entry.title,
      url: entry.url,
      snippet: isString(entry.snippet) ? entry.snippet : "",
    });
  }

  return results;
}

export function WebSearchTool(props: ToolCallMessagePartProps): ReactElement {
  const reducedMotion = useReducedMotion();

  const query = useMemo((): string => {
    const args = parseArgs(props.argsText);

    return args === null ? "" : str(args, "query");
  }, [props.argsText]);

  const results = useMemo((): SearchResult[] | null => {
    const raw = isTextResult(props.result) ? props.result : "";

    if (raw === "") return [];

    return parseSearchResults(raw);
  }, [props.result]);

  const running = props.status.type === "running";
  const landed = results !== null && results.length > 0;

  const visibleResults = useRevealCount(
    landed ? results.length : 0,
    landed,
    !reducedMotion
  );

  return (
    <ToolCard part={props}>
      <SearchBody
        query={query}
        results={results}
        rawText={isTextResult(props.result) ? props.result : ""}
        visibleResults={visibleResults}
        searching={running}
        cycle={landed ? 1 : 0}
      />
    </ToolCard>
  );
}

function SearchBody({
  query,
  results,
  rawText,
  visibleResults,
  searching,
  cycle,
}: {
  query: string;
  results: SearchResult[] | null;
  rawText: string;
  visibleResults: number;
  searching: boolean;
  cycle: number;
}): ReactElement {
  if (query === "") throw new Error("web_search: no query in arguments");

  // A plain-text failure or a non-JSON result shows verbatim.
  if (results === null) {
    return (
      <p
        className="border-line text-muted-foreground mx-3 mb-2 rounded-md border px-2 py-1 font-mono text-xs break-all whitespace-pre-wrap"
        data-testid="search-plain"
      >
        {rawText}
      </p>
    );
  }

  const landing: readonly WebSearchResult[] = results.map(
    (item: SearchResult): WebSearchResult => ({
      title: item.title,
      domain: urlHost(item.url),
    })
  );

  return (
    <div className="space-y-2 px-3 pb-2" data-testid="search-body">
      <WebSearch
        query={query}
        results={landing}
        visibleResults={visibleResults}
        searching={searching}
        cycle={cycle}
      />
      {!searching && results.length > 0 ? (
        <p
          className="text-muted-foreground text-[11px]"
          data-testid="search-sources"
        >
          Read {results.length} {results.length === 1 ? "source" : "sources"}
        </p>
      ) : null}
      {searching ? null : (
        <ul className="space-y-1.5" data-testid="search-results">
          {results.map((item: SearchResult): ReactElement => (
            <li
              key={item.url}
              className="border-line rounded-md border px-2 py-1.5"
              data-testid="search-result"
            >
              <a
                href={item.url}
                target="_blank"
                rel="noreferrer"
                className="text-foreground hover:text-foreground/70 block truncate text-xs font-medium underline decoration-dotted underline-offset-2"
                data-testid="search-result-title"
              >
                {item.title}
              </a>
              <span
                className="text-muted-foreground block truncate font-mono text-[10px]"
                data-testid="search-result-host"
              >
                {urlHost(item.url)}
              </span>
              {item.snippet !== "" ? (
                <p
                  className="text-foreground/70 mt-0.5 line-clamp-3 text-[11px]"
                  data-testid="search-result-snippet"
                >
                  {item.snippet}
                </p>
              ) : null}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export function WebFetchTool(props: ToolCallMessagePartProps): ReactElement {
  const url = useMemo((): string => {
    const args = parseArgs(props.argsText);

    return args === null ? "" : str(args, "url");
  }, [props.argsText]);

  const resultText = isTextResult(props.result) ? props.result : "";
  const isError = props.isError === true;

  return (
    <ToolCard part={props}>
      <FetchBody url={url} resultText={resultText} isError={isError} />
    </ToolCard>
  );
}

function FetchBody({
  url,
  resultText,
  isError,
}: {
  url: string;
  resultText: string;
  isError: boolean;
}): ReactElement {
  if (url === "") throw new Error("web_fetch: no url in arguments");

  return (
    <div className="space-y-1.5 px-3 pb-2" data-testid="webfetch-body">
      <span
        className="border-line text-foreground/70 inline-flex max-w-full items-center gap-1.5 rounded-md border px-1.5 py-0.5 font-mono text-[11px]"
        data-testid="webfetch-url"
      >
        {url}
      </span>
      {resultText !== "" ? (
        isError ? (
          <p
            className="border-danger/35 text-dangertext rounded-md border px-2 py-1 font-mono text-xs break-all whitespace-pre-wrap"
            data-testid="webfetch-error"
          >
            {resultText}
          </p>
        ) : (
          <ClampedText text={resultText} testidPrefix="webfetch-text" />
        )
      ) : null}
    </div>
  );
}
