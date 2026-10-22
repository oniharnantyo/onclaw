/**
 * The file tools' renderers (task 4.2, design D6): `edit_file` renders the
 * vendored `CodeDiff` from the shared jsdiff pass beside a buddies-side gutter
 * of old/new file line numbers (a header naming the file with the
 * added/removed counts, the all-occurrences marker and the path chip in the
 * frame), and `write_file` / `read_file` render the written or read content as
 * a code preview with its line count — the read window keeping the
 * middleware's real line numbers. Required payload fields are read
 * defensively; a wrong-typed field throws into the card's boundary, which
 * degrades to the fallback body.
 */
import { useMemo, useState } from "react";
import type { ReactElement } from "react";
import type { ToolCallMessagePartProps } from "@assistant-ui/react";

import { CodeDiff } from "@/components/assistant-ui/elements/code-diff";

import { isString, parseArgs, str } from "../parse";
import { countLines } from "../toolDisplay";
import { FileChip } from "../primitives/FileChip";
import { ClampedText } from "../primitives/ClampedText";
import { buildDiffLines, countDiffLines, diffViewport } from "./diffRows";
import type { DiffRow, DiffSeam, DiffViewport } from "./diffRows";
import { CodePreview } from "./codePreview";
import { ToolCard } from "./ToolCallFrame";

/** The narrow type guard the bodies read results through. */
function isTextResult(value: unknown): value is string {
  return typeof value === "string";
}

/** The language label a written file's extension suggests. */
function languageOf(path: string): string {
  const ext = path.slice(path.lastIndexOf(".") + 1).toLowerCase();

  const LANGUAGES = {
    go: "go",
    ts: "typescript",
    tsx: "tsx",
    js: "javascript",
    jsx: "jsx",
    md: "markdown",
    json: "json",
    css: "css",
    html: "html",
    sh: "shell",
    py: "python",
    rs: "rust",
    yml: "yaml",
    yaml: "yaml",
    toml: "toml",
    sql: "sql",
  } satisfies Record<string, string>;

  return LANGUAGES[ext] ?? "text";
}

/** The numbered form the middleware persists a read as: `     1\tcontent`. */
const NUMBERED_LINE = /^\s*(\d+)\t(.*)$/;

interface ReadLine {
  no: number;
  text: string;
}

/** Parses the numbered read result; null when the payload is not the
 * numbered form (a refusal message) and the raw text should show. */
function parseNumberedLines(result: string): ReadLine[] | null {
  if (result.trim() === "") return null;

  const lines = result.replace(/\n$/, "").split("\n");
  const parsed: ReadLine[] = [];

  for (const line of lines) {
    const match = NUMBERED_LINE.exec(line);

    if (match === null) return null;

    parsed.push({ no: Number(match[1]), text: match[2] });
  }

  return parsed;
}

/** The error pane a failed file call shows instead of its shaped body. */
function ErrorPane({ text }: { text: string }): ReactElement {
  return (
    <p
      className="border-danger/35 text-dangertext mx-3 mb-2 rounded-md border px-2 py-1 font-mono text-xs break-all whitespace-pre-wrap"
      data-testid="file-error"
    >
      {text}
    </p>
  );
}

export function EditFileTool(props: ToolCallMessagePartProps): ReactElement {
  const oldText = useMemo((): string | null => {
    const args = parseArgs(props.argsText);

    if (args === null) return null;

    return isString(args.old_string) ? args.old_string : null;
  }, [props.argsText]);

  const newText = useMemo((): string | null => {
    const args = parseArgs(props.argsText);

    if (args === null) return null;

    return isString(args.new_string) ? args.new_string : null;
  }, [props.argsText]);

  const path = useMemo((): string => {
    const args = parseArgs(props.argsText);

    return args === null ? "" : str(args, "file_path");
  }, [props.argsText]);

  const rows = useMemo((): readonly DiffRow[] => {
    if (oldText === null || newText === null) return [];

    return buildDiffLines(oldText, newText);
  }, [oldText, newText]);

  const counts = useMemo((): { additions: number; deletions: number } => {
    return countDiffLines(rows);
  }, [rows]);

  const replaceAll = useMemo((): boolean => {
    const args = parseArgs(props.argsText);

    return args?.replace_all === true;
  }, [props.argsText]);

  const resultText = isTextResult(props.result) ? props.result : "";
  const isError = props.isError === true;

  return (
    <ToolCard part={props}>
      <EditBody
        path={path}
        oldText={oldText}
        newText={newText}
        rows={rows}
        counts={counts}
        replaceAll={replaceAll}
        isError={isError}
        resultText={resultText}
      />
    </ToolCard>
  );
}

function EditBody({
  path,
  oldText,
  newText,
  rows,
  counts,
  replaceAll,
  isError,
  resultText,
}: {
  path: string;
  oldText: string | null;
  newText: string | null;
  rows: readonly DiffRow[];
  counts: { additions: number; deletions: number };
  replaceAll: boolean;
  isError: boolean;
  resultText: string;
}): ReactElement {
  if (path === "") throw new Error("edit_file: no file_path in arguments");

  if (oldText === null || newText === null) {
    throw new Error("edit_file: old_string/new_string missing");
  }

  return (
    <div className="space-y-1.5 px-3 pb-2" data-testid="edit-body">
      <div className="flex items-center gap-2">
        <FileChip path={path} />
        {replaceAll ? (
          <span
            className="border-line text-foreground/70 rounded-[6px] border px-1.5 py-0.5 text-[10px] leading-4"
            data-testid="edit-replace-all"
          >
            all occurrences
          </span>
        ) : null}
        {isError && resultText !== "" ? (
          <span className="text-dangertext min-w-0 flex-1 truncate text-[11px]">
            {resultText}
          </span>
        ) : null}
      </div>
      {isError ? (
        <ErrorPane text={resultText} />
      ) : (
        <EditDiff path={path} counts={counts} rows={rows} />
      )}
    </div>
  );
}

/** The vendored element's overrides for the shared surface (D13: the file is
 * never edited). The card chrome moves to the column wrapper; the inner
 * horizontal scroll goes so the gutter scrolls with the rows; the header's
 * path cannot wrap (that would shift every row below it); the per-row reveal
 * stops (a static gutter cannot fade in with it); and every row pins the
 * gutter's box — a blank line has no text to give its row a line box, and
 * without the pin the two columns would drift. */
const ELEMENT_SURFACE = [
  "max-w-none rounded-none border-0",
  "[&>div:first-child>span:first-child]:min-w-0",
  "[&>div:first-child>span:first-child]:truncate",
  "[&>div:last-child]:overflow-visible",
  "[&>div:last-child>div>div]:animate-none",
  "[&>div:last-child>div>div]:min-h-[1.46875rem]",
].join(" ");

/** A row's gutter: the old-file and new-file numbers (blank on the side the
 * row does not exist in — removals carry only the old, additions only the
 * new), in the element's own box — `py-0.5` around a `text-xs` line at
 * `leading-relaxed` (1.625 × 0.75rem = 1.46875rem) — so the columns stay
 * aligned. */
function DiffGutterRow({ row }: { row: DiffRow }): ReactElement {
  return (
    <div
      className="text-muted-foreground flex min-h-[1.46875rem] items-center justify-end gap-2 py-0.5 pr-2 leading-relaxed tabular-nums"
      data-testid="diff-gutter-row"
    >
      <span className="w-8 text-right" data-testid="diff-old-lineno">
        {row.oldNo}
      </span>
      <span className="w-8 text-right" data-testid="diff-new-lineno">
        {row.newNo}
      </span>
    </div>
  );
}

/** The edit diff: the vendored element renders the rows and a buddies-side
 * gutter column carries their line numbers inside one shared scroll surface
 * (gutter cells and rows pin the same height, so the columns stay aligned row
 * for row). The element owns its line list (children are omitted from its
 * props), so the seams and the cap control ride beneath it — expanding
 * re-renders the same element with the run restored at its true positions. */
function EditDiff({
  path,
  counts,
  rows,
}: {
  path: string;
  counts: { additions: number; deletions: number };
  rows: readonly DiffRow[];
}): ReactElement {
  const [expanded, setExpanded] = useState<ReadonlySet<number>>(
    (): ReadonlySet<number> => new Set<number>()
  );

  const [showAll, setShowAll] = useState(false);

  const view = useMemo(
    (): DiffViewport => diffViewport(rows, expanded, showAll),
    [rows, expanded, showAll]
  );

  const expand = (start: number): void => {
    setExpanded(
      (prev: ReadonlySet<number>): ReadonlySet<number> =>
        new Set([...prev, start])
    );
  };

  return (
    <div data-testid="edit-diff">
      <div className="border-border/60 bg-card overflow-x-auto rounded-2xl border">
        <div className="grid w-max min-w-full grid-cols-[max-content_1fr] font-mono text-xs">
          <div className="pl-2" data-testid="diff-gutter">
            {/* The header's blank twin: the numbers start level with row one. */}
            <div aria-hidden="true" className="px-2 pt-3 pb-2">
              &nbsp;
            </div>
            {view.lines.map((row: DiffRow): ReactElement => (
              <DiffGutterRow key={`${row.oldNo}-${row.newNo}`} row={row} />
            ))}
          </div>
          <CodeDiff
            className={ELEMENT_SURFACE}
            filename={path}
            additions={counts.additions}
            deletions={counts.deletions}
            lines={view.lines}
            cycle={0}
          />
        </div>
      </div>
      {view.seams.length > 0 || view.hidden > 0 ? (
        <div
          className="mt-1 flex flex-wrap items-center gap-1"
          data-testid="diff-controls"
        >
          {view.seams.map((seam: DiffSeam): ReactElement => (
            <button
              key={`seam-${seam.start}`}
              type="button"
              className="hover:bg-foreground/[0.04] hover:text-foreground w-full rounded-md px-1.5 py-0.5 text-left font-mono text-[10px] text-muted-foreground"
              data-testid="diff-expand"
              onClick={() => expand(seam.start)}
            >
              ··· Expand {seam.count} unchanged{" "}
              {seam.count === 1 ? "line" : "lines"}
            </button>
          ))}
          {view.hidden > 0 ? (
            <button
              type="button"
              className="hover:bg-foreground/[0.04] hover:text-foreground rounded-md px-1.5 py-0.5 text-[11px] text-muted-foreground"
              data-testid="diff-show-all"
              onClick={() => setShowAll(true)}
            >
              Show all ({view.hidden} more {view.hidden === 1 ? "row" : "rows"})
            </button>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

export function WriteFileTool(props: ToolCallMessagePartProps): ReactElement {
  const path = useMemo((): string => {
    const args = parseArgs(props.argsText);

    return args === null ? "" : str(args, "file_path");
  }, [props.argsText]);

  const content = useMemo((): string | null => {
    const args = parseArgs(props.argsText);

    if (args === null) return null;

    return isString(args.content) ? args.content : null;
  }, [props.argsText]);

  const resultText = isTextResult(props.result) ? props.result : "";
  const isError = props.isError === true;

  return (
    <ToolCard part={props}>
      <WriteBody
        path={path}
        content={content}
        isError={isError}
        resultText={resultText}
      />
    </ToolCard>
  );
}

function WriteBody({
  path,
  content,
  isError,
  resultText,
}: {
  path: string;
  content: string | null;
  isError: boolean;
  resultText: string;
}): ReactElement {
  if (path === "") throw new Error("write_file: no file_path in arguments");

  if (content === null) throw new Error("write_file: content missing");

  const lines = countLines(content);

  return (
    <div className="space-y-1.5 px-3 pb-2" data-testid="write-body">
      <div className="flex items-center gap-2">
        <FileChip path={path} />
        <span
          className="text-muted-foreground text-[11px]"
          data-testid="write-line-count"
        >
          {lines} {lines === 1 ? "line" : "lines"}
        </span>
        {isError && resultText !== "" ? (
          <span className="text-dangertext min-w-0 flex-1 truncate text-[11px]">
            {resultText}
          </span>
        ) : null}
      </div>
      {isError ? (
        <ErrorPane text={resultText} />
      ) : content !== "" ? (
        <CodePreview
          code={content}
          language={languageOf(path)}
          lineNumbers
          testidPrefix="write-code"
        />
      ) : null}
    </div>
  );
}

export function ReadFileTool(props: ToolCallMessagePartProps): ReactElement {
  const path = useMemo((): string => {
    const args = parseArgs(props.argsText);

    return args === null ? "" : str(args, "file_path");
  }, [props.argsText]);

  const resultText = isTextResult(props.result) ? props.result : "";
  const isError = props.isError === true;

  return (
    <ToolCard part={props}>
      <ReadBody path={path} resultText={resultText} isError={isError} />
    </ToolCard>
  );
}

function ReadBody({
  path,
  resultText,
  isError,
}: {
  path: string;
  resultText: string;
  isError: boolean;
}): ReactElement {
  if (path === "") throw new Error("read_file: no file_path in arguments");

  const lines = isError ? null : parseNumberedLines(resultText);

  return (
    <div className="space-y-1.5 px-3 pb-2" data-testid="read-body">
      <FileChip path={path} />
      {lines !== null ? (
        <>
          <span
            className="text-muted-foreground text-[11px]"
            data-testid="read-line-count"
          >
            {lines.length} {lines.length === 1 ? "line" : "lines"}
          </span>
          <CodePreview
            code={lines.map((l: ReadLine): string => l.text).join("\n")}
            lineNumbers
            startLine={lines.length > 0 ? lines[0].no : 1}
            testidPrefix="read-code"
          />
        </>
      ) : (
        <ClampedText text={resultText} testidPrefix="read-plain" />
      )}
    </div>
  );
}
