/**
 * The search tools' renderers (task 4.2, design D6): `grep` groups its
 * matches by file with the pattern highlighted in each line, `glob` and
 * `ls` show the returned paths as a file list, and every path opens the
 * right panel through the jailed workspace reader (FileChip). All three
 * middleware output modes parse for grep: `content`
 * (`path:line:content` rows), `files_with_matches`, and `count`
 * (`path:count`). The pattern is a regular expression; an invalid one
 * degrades to no highlighting.
 */
import { useMemo } from "react";
import type { ReactElement } from "react";
import type { ToolCallMessagePartProps } from "@assistant-ui/react";

import { isString, parseArgs, str } from "../parse";
import { FileChip, useOpenWorkspacePath } from "../primitives/FileChip";
import { ClampedText } from "../primitives/ClampedText";
import { ToolCard } from "./ToolCallFrame";

/** The narrow type guard the bodies read results through. */
function isTextResult(value: unknown): value is string {
  return typeof value === "string";
}

/** One content-mode row: `path:line:content` (line numbers default on). */
const CONTENT_ROW = /^([^:\n]+):(\d+):(.*)$/;

/** One files-with-matches/count row: `path[:count]`. */
const PLAIN_ROW = /^([^:\n]+)(?::(\d+))?$/;

interface GrepMatch {
  path: string;
  line: number | null;
  content: string;
}

/** Parses a content-mode result into matches; null when the text is not the
 * expected form (refusals, offload stubs) so the raw text shows. */
function parseContentMatches(result: string): GrepMatch[] | null {
  if (result.trim() === "" || /^No (files found|matches found)/.test(result)) {
    return [];
  }

  const rows = result.replace(/\n$/, "").split("\n");
  const matches: GrepMatch[] = [];

  for (const row of rows) {
    const numbered = CONTENT_ROW.exec(row);
    const plain = numbered === null ? PLAIN_ROW.exec(row) : null;

    if (numbered !== null) {
      matches.push({
        path: numbered[1],
        line: Number(numbered[2]),
        content: numbered[3],
      });
    } else if (plain !== null && plain[2] === undefined) {
      matches.push({ path: plain[1], line: null, content: "" });
    } else {
      return null;
    }
  }

  return matches;
}

/** Groups matches by file preserving delivery order. */
function groupByFile(
  matches: GrepMatch[]
): { path: string; matches: GrepMatch[] }[] {
  const groups: { path: string; matches: GrepMatch[] }[] = [];
  const byPath = new Map<string, GrepMatch[]>();

  for (const match of matches) {
    const group = byPath.get(match.path);

    if (group) {
      group.push(match);
    } else {
      const fresh: GrepMatch[] = [match];

      byPath.set(match.path, fresh);
      groups.push({ path: match.path, matches: fresh });
    }
  }

  return groups;
}

/** Highlights the searched pattern within one line; an invalid pattern
 * degrades to plain text. */
function highlightPattern(text: string, pattern: string): ReactElement {
  if (pattern === "") return <>{text}</>;

  let regex: RegExp | null = null;

  try {
    regex = new RegExp(pattern, "gi");
  } catch {
    regex = null;
  }

  if (regex === null) return <>{text}</>;

  const parts: ReactElement[] = [];
  let cursor = 0;
  let key = 0;

  regex.lastIndex = 0;

  for (;;) {
    const match = regex.exec(text);

    if (match === null) break;

    if (match.index > cursor) {
      parts.push(<span key={key++}>{text.slice(cursor, match.index)}</span>);
    }

    parts.push(
      <mark
        key={key++}
        className="bg-[color-mix(in_oklab,var(--success)_30%,transparent)] text-fg rounded-sm px-0.5"
      >
        {match[0] === "" ? text.slice(match.index, match.index + 1) : match[0]}
      </mark>
    );

    // Zero-length matches would loop forever; advance by one character.
    if (match[0] === "") regex.lastIndex = match.index + 1;
    cursor = regex.lastIndex;
  }

  if (cursor < text.length) {
    parts.push(<span key={key++}>{text.slice(cursor)}</span>);
  }

  return <>{parts}</>;
}

/** Splits a glob/ls result into paths; null when the payload is a refusal
 * rather than a path list. */
function parsePathList(result: string): string[] | null {
  const body = result.replace(/^Found \d+ files?\n/, "");

  if (body.trim() === "" || /^No files found/.test(body)) return [];

  const paths = body.replace(/\n$/, "").split("\n");

  return paths.every((p: string): boolean => p !== "") ? paths : null;
}

/** The shared path list: one clickable chip row per returned path. */
function PathList({
  paths,
  testidPrefix,
}: {
  paths: string[];
  testidPrefix: string;
}): ReactElement {
  return (
    <ul className="space-y-0.5" data-testid={testidPrefix}>
      {paths.map((path: string): ReactElement => (
        <li key={path} className="flex items-center gap-1.5">
          <FileChip
            path={path}
            testidPrefix={`${testidPrefix}-path`}
            className="min-w-0"
          />
        </li>
      ))}
    </ul>
  );
}

/** The plain-text pane a result that is not the expected form falls back
 * to (refusals, offload stubs). */
function PlainPane({
  text,
  testidPrefix,
}: {
  text: string;
  testidPrefix: string;
}): ReactElement {
  return <ClampedText text={text} testidPrefix={testidPrefix} />;
}

export function GrepTool(props: ToolCallMessagePartProps): ReactElement {
  const pattern = useMemo((): string => {
    const args = parseArgs(props.argsText);

    return args === null ? "" : str(args, "pattern");
  }, [props.argsText]);

  const outputMode = useMemo((): string => {
    const args = parseArgs(props.argsText);

    return args === null
      ? "files_with_matches"
      : str(args, "output_mode", "files_with_matches");
  }, [props.argsText]);

  const resultText = isTextResult(props.result) ? props.result : "";
  const isError = props.isError === true;

  return (
    <ToolCard part={props}>
      <GrepBody
        pattern={pattern}
        outputMode={outputMode}
        resultText={resultText}
        isError={isError}
      />
    </ToolCard>
  );
}

function GrepBody({
  pattern,
  outputMode,
  resultText,
  isError,
}: {
  pattern: string;
  outputMode: string;
  resultText: string;
  isError: boolean;
}): ReactElement {
  const openPath = useOpenWorkspacePath();

  if (pattern === "") throw new Error("grep: no pattern in arguments");

  if (isError) return <ErrorResultPane text={resultText} />;

  if (resultText === "") {
    return (
      <div className="text-muted-foreground px-3 pb-2 text-[11px]">
        Searching…
      </div>
    );
  }

  // Content mode: grouped rows with the pattern highlighted.
  if (outputMode === "content") {
    const matches = parseContentMatches(resultText);

    if (matches === null) {
      return <PlainPane text={resultText} testidPrefix="grep-plain" />;
    }

    if (matches.length === 0) {
      return (
        <p
          className="text-muted-foreground px-3 pb-2 text-[11px]"
          data-testid="grep-empty"
        >
          No matches found
        </p>
      );
    }

    return (
      <div className="space-y-2 px-3 pb-2" data-testid="grep-body">
        {groupByFile(matches).map(
          (group: { path: string; matches: GrepMatch[] }): ReactElement => (
            <div key={group.path}>
              <FileChip path={group.path} testidPrefix="grep-file" />
              <ul className="mt-0.5 space-y-0.5">
                {group.matches.map(
                  (match: GrepMatch, i: number): ReactElement => (
                    // SAFETY: the row is identified by its stable position
                    // within the delivered match list, which never reorders.
                    <li key={i}>
                      <button
                        type="button"
                        className="text-foreground/70 hover:bg-foreground/[0.06] flex w-full items-start gap-2 rounded px-0.5 py-px text-left font-mono text-xs"
                        data-testid="grep-match"
                        title="Preview in the right panel"
                        onClick={(): void => openPath(group.path)}
                      >
                        {match.line !== null ? (
                          <span className="text-muted-foreground w-8 shrink-0 select-none text-right text-[10px]">
                            {match.line}
                          </span>
                        ) : null}
                        <span className="min-w-0 break-all">
                          {highlightPattern(match.content, pattern)}
                        </span>
                      </button>
                    </li>
                  )
                )}
              </ul>
            </div>
          )
        )}
      </div>
    );
  }

  // Count mode: `path:count` rows.
  if (outputMode === "count") {
    const rows = resultText
      .replace(/\n$/, "")
      .split("\n")
      .filter(
        (row: string): boolean => !/^Found \d+ total occurrences/.test(row)
      );

    const counts: { path: string; count: number }[] = [];
    let malformed = false;

    for (const row of rows) {
      const match = /^(.+):(\d+)$/.exec(row);

      if (match === null) {
        malformed = true;

        break;
      }

      counts.push({ path: match[1], count: Number(match[2]) });
    }

    if (malformed) {
      return <PlainPane text={resultText} testidPrefix="grep-plain" />;
    }

    if (counts.length === 0) {
      return (
        <p
          className="text-muted-foreground px-3 pb-2 text-[11px]"
          data-testid="grep-empty"
        >
          No matches found
        </p>
      );
    }

    return (
      <ul className="space-y-0.5 px-3 pb-2" data-testid="grep-body">
        {counts.map((entry: { path: string; count: number }): ReactElement => (
          <li key={entry.path} className="flex items-center gap-2">
            <FileChip path={entry.path} testidPrefix="grep-file" />
            <span
              className="border-line text-foreground/70 rounded-[6px] border px-1.5 py-0.5 text-[10px] leading-4"
              data-testid={`grep-count-${entry.path}`}
            >
              {entry.count}
            </span>
          </li>
        ))}
      </ul>
    );
  }

  // files_with_matches (the default): the shared path list.
  const paths = parsePathList(resultText);

  if (paths === null) {
    return <PlainPane text={resultText} testidPrefix="grep-plain" />;
  }

  if (paths.length === 0) {
    return (
      <p
        className="text-muted-foreground px-3 pb-2 text-[11px]"
        data-testid="grep-empty"
      >
        No matches found
      </p>
    );
  }

  return (
    <div className="px-3 pb-2" data-testid="grep-body">
      <PathList paths={paths} testidPrefix="grep-files" />
    </div>
  );
}

/** The error pane a failed search shows (the error text readable). */
function ErrorResultPane({ text }: { text: string }): ReactElement {
  return (
    <p
      className="border-danger/35 text-dangertext mx-3 mb-2 rounded-md border px-2 py-1 font-mono text-xs break-all whitespace-pre-wrap"
      data-testid="grep-error"
    >
      {text}
    </p>
  );
}

export function GlobTool(props: ToolCallMessagePartProps): ReactElement {
  const resultText = isTextResult(props.result) ? props.result : "";

  return (
    <ToolCard part={props}>
      <FileListBody
        argsText={props.argsText}
        requireField="pattern"
        resultText={resultText}
        emptyNote="No files matched"
      />
    </ToolCard>
  );
}

export function LsTool(props: ToolCallMessagePartProps): ReactElement {
  const resultText = isTextResult(props.result) ? props.result : "";

  return (
    <ToolCard part={props}>
      <FileListBody
        argsText={props.argsText}
        requireField="path"
        resultText={resultText}
        emptyNote="The directory is empty"
      />
    </ToolCard>
  );
}

function FileListBody({
  argsText,
  requireField,
  resultText,
  emptyNote,
}: {
  argsText: string;
  requireField: "pattern" | "path";
  resultText: string;
  emptyNote: string;
}): ReactElement {
  const args = parseArgs(argsText);

  if (args === null || !isString(args[requireField])) {
    throw new Error(`glob/ls: no ${requireField} in arguments`);
  }

  if (resultText === "") return <div className="px-3 pb-2" />;

  const paths = parsePathList(resultText);

  if (paths === null) {
    return <PlainPane text={resultText} testidPrefix="file-list-plain" />;
  }

  if (paths.length === 0) {
    return (
      <p
        className="text-muted-foreground px-3 pb-2 text-[11px]"
        data-testid="file-list-empty"
      >
        {emptyNote}
      </p>
    );
  }

  return (
    <div className="px-3 pb-2" data-testid="file-list-body">
      <PathList paths={paths} testidPrefix="file-list" />
    </div>
  );
}
