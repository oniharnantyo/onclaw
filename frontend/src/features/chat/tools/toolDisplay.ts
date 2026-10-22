/**
 * Tool display grammar (task 3.2, design D3): one-liner sentences for the
 * fifteen builtin tools — an intent form while the call runs, an outcome
 * form when it ends — ported from onclaw-v2's table and re-keyed to
 * buddies' tool catalog. A tiny markup grammar rides inside the sentence
 * strings: `` `x` `` renders a mono chip, `'x'` a quoted literal; the card
 * splits them with `parseSentenceMarkup`. Unknown and MCP tools mint no
 * sentence (null) — the header shows the display name and status word only.
 * Nothing here invents information: counts come from the delivered result,
 * and a form that cannot be computed degrades to the plain intent form.
 */
import type { IconName } from "../../../components/ui/Icon";
import {
  isObject,
  isString,
  parseJSONResult,
  str,
  type ToolArgs,
} from "./parse";

/** Which form of the sentence the card needs. "failed" and legacy terminal
 * records (no status) read the outcome form when one exists. */
export type SentenceState = "running" | "done";

/** One renderable chunk of a sentence. */
export interface SentenceSegment {
  text: string;
  /** "chip" (`` `x` ``) renders mono; "literal" ('x') renders quoted. */
  kind: "text" | "chip" | "literal";
}

/** The display metadata the settings tools catalog carries per tool. */
export interface ToolCatalogEntry {
  name: string;
  displayName: string;
  iconKey: string;
}

/** The catalog keyed by raw tool name (the chat store's slice, task 3.3). */
export type ToolCatalog = Record<string, ToolCatalogEntry>;

/** Splits a sentence string into renderable segments (markup per D3). */
export function parseSentenceMarkup(sentence: string): SentenceSegment[] {
  const segments: SentenceSegment[] = [];

  let plain = "";
  let cursor = 0;

  const flushPlain = (): void => {
    if (plain !== "") {
      segments.push({ text: plain, kind: "text" });
      plain = "";
    }
  };

  while (cursor < sentence.length) {
    const char = sentence[cursor];

    if (char === "`" || char === "'") {
      const end = sentence.indexOf(char, cursor + 1);

      if (end > cursor + 1) {
        flushPlain();
        segments.push({
          text: sentence.slice(cursor + 1, end),
          kind: char === "`" ? "chip" : "literal",
        });
        cursor = end + 1;

        continue;
      }
    }

    plain += char;
    cursor += 1;
  }

  flushPlain();

  return segments;
}

/** Counts the lines of a text payload (a trailing newline opens no line). */
export function countLines(text: string): number {
  if (text === "") return 0;

  return text.replace(/\n$/, "").split("\n").length;
}

/** The number carried by a web_search / browser result list, or null. */
function resultCount(result: string, listKey?: string): number | null {
  if (listKey === undefined) return null;

  const parsed = parseJSONResult(result);

  if (!isObject(parsed)) return null;

  const list = parsed[listKey];

  return Array.isArray(list) ? list.length : null;
}

/** Line count of a read_file result (the `%6d\t` numbered form) or null. */
function readLineCount(result: string): number | null {
  if (/^No content read from /.test(result)) return 0;

  const numbered = result
    .split("\n")
    .filter((line: string): boolean => /^\s*\d+\t/.test(line)).length;

  return numbered > 0 ? numbered : null;
}

/** Path count of a glob/ls result (newline-joined paths, optional header). */
function pathCount(result: string): number | null {
  const header = /^Found \d+ files?\n/.exec(result);

  const body = header === null ? result : result.slice(header[0].length);

  if (body === "" || /^No files found/.test(body)) return 0;

  return body.replace(/\n$/, "").split("\n").length;
}

/** Match/file counts of a grep result, per its output mode. */
interface GrepCounts {
  matches: number | null;
  files: number | null;
}

function grepCounts(result: string, outputMode: string): GrepCounts {
  if (/^No (files found|matches found)/.test(result))
    return { matches: 0, files: 0 };

  if (outputMode === "content") {
    const lines = result.replace(/\n$/, "").split("\n");

    if (lines.length === 0 || (lines.length === 1 && lines[0] === ""))
      return { matches: 0, files: 0 };

    const files = new Set(
      lines.map((line: string): string => {
        // `path:line:content` (line numbers default on) or `path:content`.
        const secondColon = line.indexOf(":", line.indexOf(":") + 1);
        const cut = secondColon > 0 ? secondColon : line.indexOf(":");

        return line.slice(0, cut);
      })
    );

    return { matches: lines.length, files: files.size };
  }

  const files = pathCount(result);

  return { matches: files, files };
}

/** Splits a URL into a display host, or returns the input unchanged. */
export function urlHost(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}

interface SentenceInput {
  name: string;
  args: ToolArgs | null;
  result: string;
  state: SentenceState;
  isError: boolean;
}

type SentenceFn = (input: SentenceInput) => string | null;

/** The `path` field of the browser screenshot's typed JSON result. */
function resultJSONPath(result: string): string | null {
  const parsed = parseJSONResult(result);

  if (!isObject(parsed)) return null;

  const path = parsed.path;

  return isString(path) && path !== "" ? path : null;
}

function sentenceReadFile(input: SentenceInput): string | null {
  const { args, result, state } = input;
  const path = str(args, "file_path");

  if (path === "") return null;

  if (state === "running") return `Reading \`${path}\``;

  const lines = readLineCount(result);

  return lines === null
    ? `Read \`${path}\``
    : `Read \`${path}\` · ${lines} ${lines === 1 ? "line" : "lines"}`;
}

function sentenceWriteFile(input: SentenceInput): string | null {
  const { args, state } = input;
  const path = str(args, "file_path");

  if (path === "") return null;

  if (state === "running") return `Writing \`${path}\``;

  const lines = countLines(str(args, "content"));

  return lines > 0
    ? `Wrote \`${path}\` · ${lines} ${lines === 1 ? "line" : "lines"}`
    : `Wrote \`${path}\``;
}

function sentenceEditFile(input: SentenceInput): string | null {
  const { args, state } = input;
  const path = str(args, "file_path");

  if (path === "") return null;

  return state === "running" ? `Editing \`${path}\`` : `Edited \`${path}\``;
}

function sentenceLs(input: SentenceInput): string | null {
  const { args, result, state } = input;
  const path = str(args, "path");

  const target = path === "" ? "the workspace" : `\`${path}\``;

  if (state === "running") return `Listing ${target}`;

  const count = pathCount(result);

  return count === null
    ? `Listed ${target}`
    : `Listed ${target} · ${count} ${count === 1 ? "entry" : "entries"}`;
}

function sentenceGlob(input: SentenceInput): string | null {
  const { args, result, state } = input;
  const pattern = str(args, "pattern");

  if (pattern === "") return null;

  if (state === "running") return `Finding \`${pattern}\``;

  const count = pathCount(result);

  return count === null
    ? `Found \`${pattern}\` matches`
    : `Found ${count} ${count === 1 ? "path" : "paths"}`;
}

function sentenceGrep(input: SentenceInput): string | null {
  const { args, result, state } = input;
  const pattern = str(args, "pattern");
  const outputMode = str(args, "output_mode");

  if (pattern === "") return null;

  if (state === "running") return `Searching \`${pattern}\``;

  const { matches, files } = grepCounts(result, outputMode);

  if (matches === null || files === null) return `Searched \`${pattern}\``;

  if (matches === 0) return `No matches for \`${pattern}\``;

  if (outputMode === "content" && files > 1)
    return `Found ${matches} ${matches === 1 ? "match" : "matches"} in ${files} files`;

  return `Found ${matches} ${matches === 1 ? "match" : "matches"}`;
}

/** The `execute` special case (design D3): the command IS the sentence. */
function sentenceExecute(input: SentenceInput): string | null {
  const command = str(input.args, "command").trim();

  if (command === "") return null;

  return `\`${command}\``;
}

function sentenceDeleteFile(input: SentenceInput): string | null {
  const { args, state } = input;
  const path = str(args, "file_path");

  if (path === "") return null;

  return state === "running" ? `Deleting \`${path}\`` : `Deleted \`${path}\``;
}

function sentenceTodoWrite(input: SentenceInput): string | null {
  const { args, result, state } = input;

  if (state === "running") return "Updating the plan";

  const items = resultCount(result, "items");
  const argItems = args?.items;
  const count = items ?? (Array.isArray(argItems) ? argItems.length : null);

  return count === null
    ? "Plan updated"
    : `Plan updated · ${count} ${count === 1 ? "item" : "items"}`;
}

function sentenceTodoRead(input: SentenceInput): string | null {
  const { args, result, state } = input;

  if (state === "running") return "Reading the plan";

  const items = resultCount(result, "items");
  const argItems = args?.items;
  const count = items ?? (Array.isArray(argItems) ? argItems.length : null);

  return count === null
    ? "Read the plan"
    : `Read the plan · ${count} ${count === 1 ? "item" : "items"}`;
}

function sentenceWebSearch(input: SentenceInput): string | null {
  const { args, result, state } = input;
  const query = str(args, "query");

  if (query === "") return null;

  if (state === "running") return `Searching '${query}'`;

  const count = resultCount(result, "results");

  return count === null
    ? `Searched '${query}'`
    : `Found ${count} ${count === 1 ? "result" : "results"}`;
}

function sentenceWebFetch(input: SentenceInput): string | null {
  const { args, state } = input;
  const url = str(args, "url");

  if (url === "") return null;

  return state === "running"
    ? `Fetching '${url}'`
    : `Fetched '${urlHost(url)}'`;
}

function sentenceHttpGet(input: SentenceInput): string | null {
  const { args, state } = input;
  const url = str(args, "url");

  if (url === "") return null;

  return state === "running" ? `Requesting '${url}'` : `Got '${urlHost(url)}'`;
}

function sentenceCurrentTime(input: SentenceInput): string | null {
  return input.state === "running" ? "Getting the time" : "Got the time";
}

/** The browser sentence is keyed by the call's action (design D3). */
function sentenceBrowser(input: SentenceInput): string | null {
  const { args, result, state } = input;
  const action = str(args, "action");

  switch (action) {
    case "":
      return null;
    case "navigate": {
      const url = str(args, "url");

      if (url === "") return "Opening the page";

      return state === "running"
        ? `Opening '${url}'`
        : `Opened '${urlHost(url)}'`;
    }

    case "snapshot":
      return state === "running"
        ? "Capturing the page"
        : `Captured ${resultCount(result, "elements") ?? "the page's"} elements`;
    case "screenshot": {
      const path = resultJSONPath(result);

      return state === "running" || path === null
        ? "Taking a screenshot"
        : `Screenshot saved \`${path}\``;
    }

    case "cookies":
      return state === "running"
        ? "Reading cookies"
        : `Read ${resultCount(result, "cookies") ?? "the"} cookies`;
    default:
      // Click, type, press, scroll, and friends: the action names the step.
      return state === "running"
        ? `Performing '${action}'`
        : `Performed '${action}'`;
  }
}

/**
 * The sentence table (design D3): one entry per builtin tool. The running
 * form states intent; the completed one states the outcome — when an
 * outcome cannot be computed from the payload the intent form stands in.
 */
const SENTENCES = new Map<string, SentenceFn>([
  ["read_file", sentenceReadFile],
  ["write_file", sentenceWriteFile],
  ["edit_file", sentenceEditFile],
  ["ls", sentenceLs],
  ["glob", sentenceGlob],
  ["grep", sentenceGrep],
  ["execute", sentenceExecute],
  ["delete_file", sentenceDeleteFile],
  ["todo_write", sentenceTodoWrite],
  ["todo_read", sentenceTodoRead],
  ["web_search", sentenceWebSearch],
  ["web_fetch", sentenceWebFetch],
  ["http_get", sentenceHttpGet],
  ["current_time", sentenceCurrentTime],
  ["browser", sentenceBrowser],
]);

/**
 * The sentence for one call, or null when the tool is unknown to the table
 * (MCP and future tools mint no sentence — design D3). A malformed args
 * payload degrades to the plain intent form rather than throwing.
 */
export function toolSentence(input: SentenceInput): string | null {
  const entry = SENTENCES.get(input.name);

  if (entry === undefined) return null;

  try {
    return entry(input);
  } catch {
    return null;
  }
}

/** The display name and kit glyph a card header renders for one tool. */
export interface ToolDisplayInfo {
  displayName: string;
  iconKey: IconName;
}

/**
 * Resolves a tool's display name and icon key (task 3.3, design D3): the
 * settings catalog wins, the raw name is the fallback — an unknown or MCP
 * tool displays exactly the name the runtime delivered.
 */
export function toolDisplayInfo(
  name: string,
  catalog: ToolCatalog | null | undefined
): ToolDisplayInfo {
  const entry = catalog?.[name];

  // SAFETY: IconKey names a kit glyph per the Go catalog contract; an absent
  // or unknown key falls back to the neutral plug glyph.
  const iconKey = (entry?.iconKey ?? "plug") as IconName;

  return {
    displayName: entry?.displayName || name,
    iconKey,
  };
}

/**
 * Latency formatting for the header (design D5): only a real duration
 * renders — the caller passes `undefined` through for records without one.
 */
export function formatLatency(ms: number): string {
  if (ms < 1000) return `${Math.max(0, Math.round(ms))}ms`;

  if (ms < 60_000) {
    const s = ms / 1000;

    return `${s < 10 ? s.toFixed(1) : Math.round(s)}s`;
  }

  const minutes = Math.floor(ms / 60_000);
  const seconds = Math.floor((ms % 60_000) / 1000);

  return `${minutes}m ${String(seconds).padStart(2, "0")}s`;
}

/** Live elapsed clock for a running card, from the running event's time. */
export function formatElapsed(sinceISO: string, now: number): string {
  const started = new Date(sinceISO).getTime();

  const elapsed = Number.isNaN(started)
    ? 0
    : Math.max(0, Math.floor((now - started) / 1000));

  const minutes = Math.floor(elapsed / 60);
  const seconds = elapsed % 60;

  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}
