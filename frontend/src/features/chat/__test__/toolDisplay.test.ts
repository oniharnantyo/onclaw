/**
 * toolDisplay.ts sentence-table tests (task 3.2, design D3): every one of
 * the fifteen builtin tools in both forms (intent while running, outcome
 * when done), the `execute` command-as-sentence special case, browser by
 * `args.action`, the chip/literal markup grammar, and an MCP name minting
 * no sentence. Counts come only from the delivered payloads.
 */
import { describe, expect, it } from "vite-plus/test";

import {
  countLines,
  formatElapsed,
  formatLatency,
  parseSentenceMarkup,
  toolDisplayInfo,
  toolSentence,
  urlHost,
  type ToolCatalog,
} from "../tools/toolDisplay";
import type { ToolArgs } from "../tools/parse";

const RUNNING = "running" as const;

const DONE = "done" as const;

/** One row of the table: tool name, args, result, and the two sentences. */
const CASES: {
  name: string;
  args: ToolArgs;
  result?: string;
  intent: string | null;
  outcome: string | null;
}[] = [
  {
    name: "read_file",
    args: { file_path: "/workspace/notes.md" },
    result: "     1\tfirst\n     2\tsecond\n",
    intent: "Reading `/workspace/notes.md`",
    outcome: "Read `/workspace/notes.md` · 2 lines",
  },
  {
    name: "write_file",
    args: { file_path: "/workspace/new.go", content: "package main\n" },
    result: "wrote the file",
    intent: "Writing `/workspace/new.go`",
    outcome: "Wrote `/workspace/new.go` · 1 line",
  },
  {
    name: "edit_file",
    args: { file_path: "/workspace/app.ts" },
    result: "replaced",
    intent: "Editing `/workspace/app.ts`",
    outcome: "Edited `/workspace/app.ts`",
  },
  {
    name: "ls",
    args: { path: "/workspace/src" },
    result: "src/a.ts\nsrc/b.ts\n",
    intent: "Listing `/workspace/src`",
    outcome: "Listed `/workspace/src` · 2 entries",
  },
  {
    name: "ls",
    args: {},
    result: "README.md\n",
    intent: "Listing the workspace",
    outcome: "Listed the workspace · 1 entry",
  },
  {
    name: "glob",
    args: { pattern: "**/*.go" },
    result: "Found 2 files\nmain.go\nutil.go\n",
    intent: "Finding `**/*.go`",
    outcome: "Found 2 paths",
  },
  {
    name: "grep",
    args: { pattern: "TODO", output_mode: "content" },
    result: "/workspace/a.go:4: TODO fix\n/workspace/b.go:9: TODO later\n",
    intent: "Searching `TODO`",
    outcome: "Found 2 matches in 2 files",
  },
  {
    name: "execute",
    args: { command: "go test ./..." },
    result: "ok",
    intent: "`go test ./...`",
    outcome: "`go test ./...`",
  },
  {
    name: "delete_file",
    args: { file_path: "/workspace/old.md" },
    result: "deleted",
    intent: "Deleting `/workspace/old.md`",
    outcome: "Deleted `/workspace/old.md`",
  },
  {
    name: "todo_write",
    args: { items: [{ key: "1", text: "ship", status: "pending" }] },
    result:
      '{"items":[{"key":"1","text":"ship","status":"pending"}],"revision":3}',
    intent: "Updating the plan",
    outcome: "Plan updated · 1 item",
  },
  {
    name: "todo_read",
    args: {},
    result:
      '{"items":[{"key":"1","text":"a","status":"pending"},{"key":"2","text":"b","status":"done"}],"revision":2}',
    intent: "Reading the plan",
    outcome: "Read the plan · 2 items",
  },
  {
    name: "web_search",
    args: { query: "wails v3 release" },
    result:
      '{"results":[{"title":"a","url":"https://a.dev","snippet":"s"},{"title":"b","url":"https://b.dev","snippet":"t"}]}',
    intent: "Searching 'wails v3 release'",
    outcome: "Found 2 results",
  },
  {
    name: "web_fetch",
    args: { url: "https://example.com/docs" },
    result: "<readable text>",
    intent: "Fetching 'https://example.com/docs'",
    outcome: "Fetched 'example.com'",
  },
  {
    name: "http_get",
    args: { url: "https://api.example.com/v1" },
    result: '{"status":"ok"}',
    intent: "Requesting 'https://api.example.com/v1'",
    outcome: "Got 'api.example.com'",
  },
  {
    name: "current_time",
    args: {},
    result: '{"utc":"2026-01-01T00:00:00Z"}',
    intent: "Getting the time",
    outcome: "Got the time",
  },
  {
    name: "browser",
    args: { action: "navigate", url: "https://example.com" },
    result: '{"url":"https://example.com","title":"Example"}',
    intent: "Opening 'https://example.com'",
    outcome: "Opened 'example.com'",
  },
  {
    name: "browser",
    args: { action: "snapshot" },
    result:
      '{"url":"https://example.com","title":"Example","elements":[{"ref":"e1","role":"button","name":"Go"}]}',
    intent: "Capturing the page",
    outcome: "Captured 1 elements",
  },
  {
    name: "browser",
    args: { action: "screenshot" },
    result: '{"path":".browsers/screenshots/1.png","width":800,"height":600}',
    intent: "Taking a screenshot",
    outcome: "Screenshot saved `.browsers/screenshots/1.png`",
  },
  {
    name: "browser",
    args: { action: "click", ref: "e1" },
    result: '{"ok":true,"url":"https://example.com","title":"Example"}',
    intent: "Performing 'click'",
    outcome: "Performed 'click'",
  },
];

describe("toolSentence table (all fifteen builtins, both forms)", () => {
  for (const tc of CASES) {
    it(`${tc.name}: intent and outcome`, () => {
      const intent = toolSentence({
        name: tc.name,
        args: tc.args,
        result: "",
        state: RUNNING,
        isError: false,
      });

      const outcome = toolSentence({
        name: tc.name,
        args: tc.args,
        result: tc.result ?? "",
        state: DONE,
        isError: false,
      });

      expect(intent).toBe(tc.intent);
      expect(outcome).toBe(tc.outcome);
    });
  }

  it("an MCP name mints no sentence (design D3)", () => {
    expect(
      toolSentence({
        name: "mcp__github__search",
        args: { q: "x" },
        result: "{}",
        state: DONE,
        isError: false,
      })
    ).toBeNull();
  });

  it("a tool with no derivable payload mints no sentence", () => {
    expect(
      toolSentence({
        name: "read_file",
        args: {},
        result: "",
        state: DONE,
        isError: false,
      })
    ).toBeNull();
    expect(
      toolSentence({
        name: "execute",
        args: {},
        result: "",
        state: DONE,
        isError: false,
      })
    ).toBeNull();
  });

  it("counts never fabricate: empty search result says zero", () => {
    expect(
      toolSentence({
        name: "web_search",
        args: { query: "q" },
        result: '{"results":[]}',
        state: DONE,
        isError: false,
      })
    ).toBe("Found 0 results");
  });
});

describe("parseSentenceMarkup (D3 grammar)", () => {
  it("splits chips, literals, and plain runs", () => {
    const segments = parseSentenceMarkup("Read `/a.md` · 2 lines");

    expect(segments).toEqual([
      { text: "Read ", kind: "text" },
      { text: "/a.md", kind: "chip" },
      { text: " · 2 lines", kind: "text" },
    ]);
  });

  it("parses quoted literals", () => {
    expect(parseSentenceMarkup("Searching 'wails v3' now")).toEqual([
      { text: "Searching ", kind: "text" },
      { text: "wails v3", kind: "literal" },
      { text: " now", kind: "text" },
    ]);
  });

  it("an unterminated marker stays plain text", () => {
    expect(parseSentenceMarkup("a `b")).toEqual([
      { text: "a `b", kind: "text" },
    ]);
  });
});

describe("display metadata (task 3.3 fallback)", () => {
  const catalog: ToolCatalog = {
    read_file: { name: "read_file", displayName: "Read File", iconKey: "file" },
  };

  it("the catalog wins for a known name", () => {
    expect(toolDisplayInfo("read_file", catalog)).toEqual({
      displayName: "Read File",
      iconKey: "file",
    });
  });

  it("the raw name is the fallback for an unknown or MCP name", () => {
    expect(toolDisplayInfo("mcp__x__y", catalog)).toEqual({
      displayName: "mcp__x__y",
      iconKey: "plug",
    });
    expect(toolDisplayInfo("grep", null)).toEqual({
      displayName: "grep",
      iconKey: "plug",
    });
  });
});

describe("latency and clock formatting", () => {
  it("formats latencies without fabricating precision", () => {
    expect(formatLatency(340)).toBe("340ms");
    expect(formatLatency(1200)).toBe("1.2s");
    expect(formatLatency(15400)).toBe("15s");
    expect(formatLatency(64_000)).toBe("1m 04s");
  });

  it("formats the live elapsed clock", () => {
    const start = "2026-01-01T00:00:00Z";

    expect(formatElapsed(start, Date.parse(start) + 5_000)).toBe("0:05");
    expect(formatElapsed(start, Date.parse(start) + 65_000)).toBe("1:05");
  });

  it("counts lines the way the sentence table does", () => {
    expect(countLines("")).toBe(0);
    expect(countLines("a\n")).toBe(1);
    expect(countLines("a\nb\n")).toBe(2);
  });

  it("splits hosts from URLs", () => {
    expect(urlHost("https://example.com/x?y=1")).toBe("example.com");
    expect(urlHost("not a url")).toBe("not a url");
  });
});
