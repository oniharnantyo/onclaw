/**
 * Tool-body scenario ports (spec chat-tool-cards "Interactive tool bodies"):
 * the scenarios the legacy `features/chat/__test__` suites proved on the
 * retired renderer layer, re-proven here against the wave-2B layer through
 * its registry seam (`toolRenderers`) and the shared harness's part props.
 * Wave 4 deletes the legacy suites with their renderers, so these assertions
 * are what keeps the written-file preview, the read window, grep's three
 * output modes, the file list, the web-fetch chip and clamp, the browser
 * page/action/snapshot bodies, the screenshot thumbnail's lightbox round
 * trip, the key-value bodies, the plan under a running write, and the edit
 * diff's context collapse and line-number gutter anchored to the spec.
 *
 * Each renderer mounts standalone through the registry map (a completed or
 * running part with inert runtime callbacks); a card body only mounts once
 * its header is opened (cards start collapsed), so every scenario opens its
 * card first. The Wails client is mocked at the module seam — the sanctioned
 * pattern from harness.tsx/the legacy suites — so the settings catalog
 * returns empty and the screenshot read serves a 1×1 PNG from memory.
 */
import { act } from "react";
import { fireEvent, screen, waitFor, within } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import { mockedSurface } from "../../../__test__/mockedSurface";
import { renderUI } from "../../../__test__/test-utils";
import type {
  BrowserScreenshotContent,
  Client,
} from "../../../../../lib/client";
import { resetCollapseMap } from "../../collapse";
import { useChatStore } from "../../../store";
import { partProps } from "./harness";
import { toolRenderers } from "../renderers";

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client surface this suite drives: the catalog fetch every card
 * triggers on mount and the screenshot bytes read. */
type MockedClient = Client & {
  settings: { tools: MockFn };
  chat: { browserScreenshot: MockFn };
};

/** A tiny valid PNG (1×1 pixel, base64). */
const PNG_B64 =
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";

/** The read's served content: the tool's own name shape plus dimensions. */
const SCREENSHOT_CONTENT = {
  name: "1.png",
  width: 800,
  height: 600,
  data: PNG_B64,
};

/** The client the hoisted mock factory installs. */
function makeClient(): MockedClient {
  // SAFETY: the literal covers the surface this suite drives; the Client
  // intersection defeats the structural check, so the conversion funnels
  // through the shared mockedSurface bridge (the audited conversion seam).
  return mockedSurface<MockedClient, object>({
    settings: { tools: vi.fn(async (): Promise<never[]> => []) },
    chat: {
      browserScreenshot: vi.fn(async (): Promise<BrowserScreenshotContent> => ({
        ...SCREENSHOT_CONTENT,
      })),
    },
  });
}

vi.mock("../../../../../lib/client", () => ({ client: makeClient() }));

// The previews' lazy Shiki loader stays out of these body assertions: the
// bodies are asserted plain, and the real engine has its own suite.
vi.mock("../../primitives/shikiHighlight", () => ({
  highlightTokens: async (): Promise<null> => null,
  isKnownLanguage: (): boolean => false,
  tokenStyle: (): Record<string, string> => ({}),
}));

import { client } from "../../../../../lib/client";

// SAFETY: the client module is mocked with makeClient() above, so its
// runtime shape is exactly the mock object this suite drives.
const mockScreenshot = (client as MockedClient).chat.browserScreenshot;

beforeEach(async () => {
  vi.clearAllMocks();
  resetCollapseMap();

  mockScreenshot.mockImplementation(
    async (): Promise<BrowserScreenshotContent> => ({
      ...SCREENSHOT_CONTENT,
    })
  );

  // jsdom lacks the object-URL factory; the thumb needs it for the Blob.
  Object.defineProperty(URL, "createObjectURL", {
    value: vi.fn((): string => "blob:mock-url"),
    configurable: true,
  });

  Object.defineProperty(URL, "revokeObjectURL", {
    value: vi.fn(),
    configurable: true,
  });

  await act(async () => {
    useChatStore.setState({
      activeAgentID: "a1",
      panelTabs: [],
      entries: [],
    });
  });
});

/** Opens one tool card: the body mounts only while the card is open. */
async function open(toolCallId: string): Promise<void> {
  await act(async () => {
    fireEvent.click(screen.getByTestId(`tool-header-${toolCallId}`));
  });
}

/** The text of a set of matched nodes, in document order. */
function textsOf(nodes: HTMLElement[]): string[] {
  return nodes.map((node: HTMLElement): string => node.textContent ?? "");
}

describe("written file preview (spec: Written file preview)", () => {
  it("renders the written content as a code preview with its line count", async () => {
    const Write = toolRenderers.write_file;

    const { unmount } = await renderUI(
      <Write
        {...partProps({
          toolCallId: "write-1",
          toolName: "write_file",
          argsText: JSON.stringify({
            file_path: "/workspace/main.go",
            content: "package main\n\nfunc main() {}\n",
          }),
        })}
      />
    );

    await open("write-1");

    expect(screen.getByTestId("write-line-count").textContent).toBe("3 lines");
    expect(screen.getByTestId("write-code").textContent).toContain(
      "func main()"
    );
    // File previews show gutter numbers from the first line.
    expect(textsOf(screen.getAllByTestId("write-code-lineno"))).toEqual([
      "1",
      "2",
      "3",
    ]);
    expect(screen.getByTestId("file-chip-/workspace/main.go")).toBeTruthy();

    await unmount();
  });
});

describe("read window (spec: read-file body)", () => {
  it("renders the lines that were read as a numbered preview keeping their file line numbers", async () => {
    const Read = toolRenderers.read_file;

    const { unmount } = await renderUI(
      <Read
        {...partProps({
          toolCallId: "read-1",
          toolName: "read_file",
          argsText: JSON.stringify({
            file_path: "/workspace/big.md",
            offset: 41,
          }),
          result: "    41\toffset line\n    42\tanother\n",
        })}
      />
    );

    await open("read-1");

    // The read window starts at the middleware's real line numbers, not 1.
    expect(textsOf(screen.getAllByTestId("read-code-lineno"))).toEqual([
      "41",
      "42",
    ]);
    expect(screen.getByTestId("read-line-count").textContent).toBe("2 lines");
    expect(screen.getByTestId("read-code").textContent).toContain(
      "offset line"
    );

    await unmount();
  });

  it("a read refusal message renders as plain text, not a code block", async () => {
    const Read = toolRenderers.read_file;

    const { unmount } = await renderUI(
      <Read
        {...partProps({
          toolCallId: "read-2",
          toolName: "read_file",
          argsText: JSON.stringify({ file_path: "/workspace/gone.md" }),
          result:
            "No content read from /workspace/gone.md: the file is empty, or offset 5 is past its last line",
        })}
      />
    );

    await open("read-2");

    expect(screen.getByTestId("read-plain-text").textContent).toContain(
      "No content read from /workspace/gone.md"
    );
    expect(screen.queryByTestId("read-code")).toBeNull();

    await unmount();
  });
});

describe("grep matches grouped by file (spec: Grep matches grouped by file)", () => {
  it("groups content matches by file and highlights the pattern in each line", async () => {
    const Grep = toolRenderers.grep;

    const { unmount } = await renderUI(
      <Grep
        {...partProps({
          toolCallId: "grep-1",
          toolName: "grep",
          argsText: JSON.stringify({
            pattern: "TODO",
            output_mode: "content",
            path: "/workspace",
          }),
          result:
            "/workspace/a.go:4: handle TODO here\n/workspace/a.go:9: another TODO\n/workspace/b.go:2: TODO in b\n",
        })}
      />
    );

    await open("grep-1");

    const body = screen.getByTestId("grep-body");

    expect(within(body).getAllByTestId("grep-match")).toHaveLength(3);
    // Two file groups, in delivery order: a.go then b.go.
    expect(textsOf(screen.getAllByTestId(/^grep-file-/))).toEqual([
      "/workspace/a.go",
      "/workspace/b.go",
    ]);

    const marks = Array.from(
      body.querySelectorAll("mark"),
      (node: Element): string => node.textContent ?? ""
    );

    expect(marks).toEqual(["TODO", "TODO", "TODO"]);

    await unmount();
  });

  it("the files_with_matches mode renders the paths as a file list", async () => {
    const Grep = toolRenderers.grep;

    const { unmount } = await renderUI(
      <Grep
        {...partProps({
          toolCallId: "grep-2",
          toolName: "grep",
          argsText: JSON.stringify({ pattern: "TODO" }),
          result: "Found 2 files\n/workspace/a.go\n/workspace/b.go",
        })}
      />
    );

    await open("grep-2");

    expect(screen.getByTestId("grep-files")).toBeTruthy();
    expect(screen.getAllByTestId(/^grep-files-path-/)).toHaveLength(2);

    await unmount();
  });

  it("the count mode renders count rows and reports no matches cleanly", async () => {
    const Grep = toolRenderers.grep;

    const counted = await renderUI(
      <Grep
        {...partProps({
          toolCallId: "grep-3",
          toolName: "grep",
          argsText: JSON.stringify({ pattern: "x", output_mode: "count" }),
          result: "/workspace/a.go:3",
        })}
      />
    );

    await open("grep-3");

    expect(screen.getByTestId("grep-count-/workspace/a.go").textContent).toBe(
      "3"
    );

    await counted.unmount();

    const empty = await renderUI(
      <Grep
        {...partProps({
          toolCallId: "grep-4",
          toolName: "grep",
          argsText: JSON.stringify({ pattern: "x" }),
          result: "No files found",
        })}
      />
    );

    await open("grep-4");

    expect(screen.getByTestId("grep-empty").textContent).toBe(
      "No matches found"
    );

    await empty.unmount();
  });

  it("an unparsable result degrades to the verbatim pane", async () => {
    const Grep = toolRenderers.grep;

    const { unmount } = await renderUI(
      <Grep
        {...partProps({
          toolCallId: "grep-5",
          toolName: "grep",
          argsText: JSON.stringify({ pattern: "x", output_mode: "content" }),
          result: "grep: internal error",
        })}
      />
    );

    await open("grep-5");

    expect(screen.getByTestId("grep-plain-text").textContent).toContain(
      "grep: internal error"
    );

    await unmount();
  });
});

describe("file list (spec: file-list body)", () => {
  it("glob and ls render the returned paths as a file list", async () => {
    const Glob = toolRenderers.glob;

    const globbed = await renderUI(
      <Glob
        {...partProps({
          toolCallId: "glob-1",
          toolName: "glob",
          argsText: JSON.stringify({ pattern: "**/*.ts" }),
          result: "src/a.ts\nsrc/b.ts\n",
        })}
      />
    );

    await open("glob-1");

    expect(
      within(globbed.container).getAllByTestId(/^file-list-path-/)
    ).toHaveLength(2);

    await globbed.unmount();

    const Ls = toolRenderers.ls;

    const listed = await renderUI(
      <Ls
        {...partProps({
          toolCallId: "ls-1",
          toolName: "ls",
          argsText: JSON.stringify({ path: "/workspace/src" }),
          result: "src/a.ts\n",
        })}
      />
    );

    await open("ls-1");

    expect(
      within(listed.container).getAllByTestId(/^file-list-path-/)
    ).toHaveLength(1);

    await listed.unmount();
  });

  it("an empty listing says so instead of rendering an empty box", async () => {
    const Ls = toolRenderers.ls;

    const { unmount } = await renderUI(
      <Ls
        {...partProps({
          toolCallId: "ls-2",
          toolName: "ls",
          argsText: JSON.stringify({ path: "/workspace/empty" }),
          result: "No files found",
        })}
      />
    );

    await open("ls-2");

    expect(screen.getByTestId("file-list-empty").textContent).toBe(
      "The directory is empty"
    );
    expect(screen.queryByTestId("file-list-body")).toBeNull();

    await unmount();
  });
});

describe("web fetch (spec: web-fetch body)", () => {
  it("shows the address chip and the clamped text", async () => {
    const Fetch = toolRenderers.web_fetch;

    const long = Array.from(
      { length: 20 },
      (_line: undefined, i: number): string => `para ${i}`
    ).join("\n");

    const { unmount } = await renderUI(
      <Fetch
        {...partProps({
          toolCallId: "fetch-1",
          toolName: "web_fetch",
          argsText: JSON.stringify({ url: "https://example.com/readme" }),
          result: long,
        })}
      />
    );

    await open("fetch-1");

    expect(screen.getByTestId("webfetch-url").textContent).toBe(
      "https://example.com/readme"
    );

    const text = screen.getByTestId("webfetch-text-text");

    expect(text.getAttribute("data-clamped")).toBe("true");
    expect(text.textContent).toContain("para 0");
    expect(text.textContent).not.toContain("para 19");
    expect(screen.getByTestId("webfetch-text-show-all")).toBeTruthy();

    await unmount();
  });
});

describe("browser bodies (spec: browser navigation / snapshot table)", () => {
  it("a navigation result shows the address and title", async () => {
    const Browser = toolRenderers.browser;

    const { unmount } = await renderUI(
      <Browser
        {...partProps({
          toolCallId: "browser-1",
          toolName: "browser",
          argsText: JSON.stringify({
            action: "navigate",
            url: "https://example.com",
          }),
          result: '{"url":"https://example.com","title":"Example Domain"}',
        })}
      />
    );

    await open("browser-1");

    expect(screen.getByTestId("browser-url").textContent).toBe(
      "https://example.com"
    );
    expect(screen.getByTestId("browser-title").textContent).toBe(
      "Example Domain"
    );

    await unmount();
  });

  it("an action result carries the ok badge and the snapshot's element table", async () => {
    const Browser = toolRenderers.browser;

    const result = JSON.stringify({
      ok: true,
      url: "https://example.com",
      title: "Example",
      elements: [
        { ref: "e1", role: "button", name: "Submit" },
        { ref: "e2", role: "textbox", name: "Email" },
      ],
    });

    const { unmount } = await renderUI(
      <Browser
        {...partProps({
          toolCallId: "browser-2",
          toolName: "browser",
          argsText: JSON.stringify({ action: "click", ref: "e1" }),
          result,
        })}
      />
    );

    await open("browser-2");

    expect(screen.getByTestId("browser-ok").textContent).toBe("ok");
    expect(textsOf(screen.getAllByTestId("browser-element-ref"))).toEqual([
      "e1",
      "e2",
    ]);

    await unmount();
  });

  it("a snapshot renders the page title and address and the ref/role/name table", async () => {
    const Browser = toolRenderers.browser;

    const result = JSON.stringify({
      url: "https://example.com",
      title: "Example",
      elements: [{ ref: "e1", role: "heading", name: "Hello" }],
    });

    const { unmount } = await renderUI(
      <Browser
        {...partProps({
          toolCallId: "browser-3",
          toolName: "browser",
          argsText: JSON.stringify({ action: "snapshot" }),
          result,
        })}
      />
    );

    await open("browser-3");

    expect(screen.getByTestId("browser-url").textContent).toBe(
      "https://example.com"
    );
    expect(screen.getByTestId("browser-title").textContent).toBe("Example");

    const table = screen.getByTestId("browser-elements");

    expect(textsOf(within(table).getAllByRole("columnheader"))).toEqual([
      "Ref",
      "Role",
      "Name",
    ]);
    expect(textsOf(within(table).getAllByRole("cell"))).toEqual([
      "e1",
      "heading",
      "Hello",
    ]);

    await unmount();
  });
});

describe("screenshot thumbnail enlarges (spec: Screenshot thumbnail enlarges)", () => {
  it("fetches by basename and renders a Blob-URL thumbnail at the reported dimensions", async () => {
    const Browser = toolRenderers.browser;

    const { unmount } = await renderUI(
      <Browser
        {...partProps({
          toolCallId: "shot-1",
          toolName: "browser",
          argsText: JSON.stringify({ action: "screenshot" }),
          result: JSON.stringify({
            path: ".browsers/screenshots/1.png",
            width: 800,
            height: 600,
          }),
        })}
      />
    );

    await open("shot-1");

    await waitFor((): void => {
      expect(screen.getByTestId("screenshot-image")).toBeTruthy();
    });

    // The bound read received the basename only — never a path.
    expect(mockScreenshot).toHaveBeenCalledWith("1.png");

    const image = screen.getByTestId("screenshot-image");

    expect(image.getAttribute("width")).toBe("800");
    expect(image.getAttribute("height")).toBe("600");
    expect(image.getAttribute("src")).toBe("blob:mock-url");

    await unmount();
  });

  it("the thumbnail enlarges into the lightbox and closes back with Escape", async () => {
    const Browser = toolRenderers.browser;

    const { unmount } = await renderUI(
      <Browser
        {...partProps({
          toolCallId: "shot-2",
          toolName: "browser",
          argsText: JSON.stringify({ action: "screenshot" }),
          result: JSON.stringify({
            path: ".browsers/screenshots/1.png",
            width: 800,
            height: 600,
          }),
        })}
      />
    );

    await open("shot-2");

    await waitFor((): void => {
      expect(screen.getByTestId("screenshot-thumb")).toBeTruthy();
    });

    const thumb = screen.getByTestId("screenshot-thumb");

    await act(async () => {
      thumb.focus();
      fireEvent.click(thumb);
    });

    expect(screen.getByTestId("lightbox")).toBeTruthy();
    expect(screen.getByTestId("lightbox-image").getAttribute("src")).toBe(
      "blob:mock-url"
    );

    await act(async () => {
      fireEvent.keyDown(document, { key: "Escape" });
    });

    expect(screen.queryByTestId("lightbox")).toBeNull();
    // Focus returns to the thumbnail that opened the overlay.
    expect(document.activeElement).toBe(thumb);

    await unmount();
  });

  it("a refused or missing read renders the unavailable state", async () => {
    mockScreenshot.mockRejectedValueOnce(
      new Error("browser: screenshot name refused")
    );

    const Browser = toolRenderers.browser;

    const { unmount } = await renderUI(
      <Browser
        {...partProps({
          toolCallId: "shot-3",
          toolName: "browser",
          argsText: JSON.stringify({ action: "screenshot" }),
          result: JSON.stringify({
            path: ".browsers/screenshots/2.png",
            width: 800,
            height: 600,
          }),
        })}
      />
    );

    await open("shot-3");

    await waitFor((): void => {
      expect(mockScreenshot).toHaveBeenCalledWith("2.png");
    });

    await waitFor((): void => {
      expect(screen.getByTestId("screenshot-unavailable")).toBeTruthy();
    });

    expect(screen.queryByTestId("screenshot-thumb")).toBeNull();

    await unmount();
  });

  it("a path whose basename breaks the name shape is refused before any call", async () => {
    const Browser = toolRenderers.browser;

    const { unmount } = await renderUI(
      <Browser
        {...partProps({
          toolCallId: "shot-4",
          toolName: "browser",
          argsText: JSON.stringify({ action: "screenshot" }),
          result: JSON.stringify({
            path: "../secrets/evil.png",
            width: 800,
            height: 600,
          }),
        })}
      />
    );

    await open("shot-4");

    // No bound read and no thumbnail — the unavailable state only.
    expect(mockScreenshot).not.toHaveBeenCalled();
    expect(screen.getByTestId("screenshot-unavailable")).toBeTruthy();
    expect(screen.queryByTestId("screenshot-thumb")).toBeNull();

    await unmount();
  });
});

describe("key-value bodies (spec: other shipped tools)", () => {
  it("http_get and current_time render structured results as key-value pairs", async () => {
    const HTTPGet = toolRenderers.http_get;

    const http = await renderUI(
      <HTTPGet
        {...partProps({
          toolCallId: "http-1",
          toolName: "http_get",
          argsText: JSON.stringify({ url: "https://api.example.com" }),
          result: '{"status":"ok","ttl":30,"tags":["a","b"]}',
        })}
      />
    );

    await open("http-1");

    expect(within(http.container).getByTestId("httpget-url").textContent).toBe(
      "https://api.example.com"
    );

    const httpRows = within(http.container).getAllByTestId("kv-row");

    expect(httpRows).toHaveLength(3);
    expect(httpRows[2].textContent).toContain("a, b");

    await http.unmount();

    const CurrentTime = toolRenderers.current_time;

    const time = await renderUI(
      <CurrentTime
        {...partProps({
          toolCallId: "time-1",
          toolName: "current_time",
          argsText: "{}",
          result: '{"utc":"2026-01-01T00:00:00Z","zone":"UTC"}',
        })}
      />
    );

    await open("time-1");

    expect(within(time.container).getAllByTestId("kv-row")).toHaveLength(2);

    await time.unmount();
  });

  it("an unstructured result renders as text", async () => {
    const CurrentTime = toolRenderers.current_time;

    const { unmount } = await renderUI(
      <CurrentTime
        {...partProps({
          toolCallId: "time-2",
          toolName: "current_time",
          argsText: "{}",
          result: "plain text result",
        })}
      />
    );

    await open("time-2");

    expect(screen.getByTestId("kv-text-text").textContent).toBe(
      "plain text result"
    );
    expect(screen.queryByTestId("kv-pairs")).toBeNull();

    await unmount();
  });
});

describe("plan under write (spec: Plan snapshot)", () => {
  it("while running the plan under write renders from the arguments", async () => {
    const Todo = toolRenderers.todo_write;

    const { unmount } = await renderUI(
      <Todo
        {...partProps({
          toolCallId: "todo-1",
          toolName: "todo_write",
          argsText: JSON.stringify({
            items: [{ key: "1", text: "only starting", status: "pending" }],
          }),
          status: { type: "running" },
        })}
      />
    );

    await open("todo-1");

    // No result yet: the snapshot comes from the arguments being written.
    expect(screen.getByTestId("todo-body").textContent).toContain(
      "only starting"
    );

    await unmount();
  });
});

describe("edit diff (spec: Edit renders a diff)", () => {
  it("shows every removed and added line with its old/new file line number and tint", async () => {
    const Edit = toolRenderers.edit_file;

    const { unmount, container } = await renderUI(
      <Edit
        {...partProps({
          toolCallId: "edit-nums",
          toolName: "edit_file",
          argsText: JSON.stringify({
            file_path: "/workspace/nums.txt",
            old_string: "a\nb\nc",
            new_string: "a\nB\nc",
          }),
        })}
      />
    );

    await open("edit-nums");

    // Rows: context(a), removal(b), addition(B), context(c). The removal keeps
    // the old file's number on the old gutter and none on the new; the
    // addition is the mirror — that is what makes the numbers the files' own.
    expect(textsOf(screen.getAllByTestId("diff-old-lineno"))).toEqual([
      "1",
      "2",
      "",
      "3",
    ]);
    expect(textsOf(screen.getAllByTestId("diff-new-lineno"))).toEqual([
      "1",
      "",
      "2",
      "3",
    ]);
    // One gutter row per rendered row: the numbers cannot drift off their row.
    expect(screen.getAllByTestId("diff-gutter-row")).toHaveLength(4);

    // The vendored element tints each row by kind (its own two-tone
    // vocabulary); the gutter rides beside those rows.
    const rows = Array.from(
      container.querySelectorAll('[data-slot="code-diff"] div[style]'),
      (node: Element): string => node.className
    );

    expect(rows).toHaveLength(4);
    expect(rows[1]).toContain("bg-red-500/10");
    expect(rows[2]).toContain("bg-emerald-500/10");

    await unmount();
  });
});

describe("edit diff context collapse (spec: Edit renders a diff)", () => {
  // The vendored CodeDiff mints no per-row test ids; its row nodes are the
  // only ones on the diff surface carrying an inline animation delay, so the
  // visible row count reads through that.
  function renderedRows(container: HTMLElement): number {
    return container.querySelectorAll('[data-slot="code-diff"] div[style]')
      .length;
  }

  it("collapses a long unchanged run to three context lines each side behind an expand seam", async () => {
    const Edit = toolRenderers.edit_file;

    // 30 shared lines after the change → 3 + seam + 3 instead of 30.
    const shared = Array.from(
      { length: 30 },
      (_line: undefined, i: number): string => `same ${i}\n`
    ).join("");

    const { unmount, container } = await renderUI(
      <Edit
        {...partProps({
          toolCallId: "edit-1",
          toolName: "edit_file",
          argsText: JSON.stringify({
            file_path: "/workspace/big.txt",
            old_string: `x\n${shared}`,
            new_string: `y\n${shared}`,
          }),
        })}
      />
    );

    await open("edit-1");

    // One removed row, one added row, and three context lines each side.
    expect(renderedRows(container)).toBe(8);
    // The gutter follows the same visible slice.
    expect(screen.getAllByTestId("diff-gutter-row")).toHaveLength(8);

    const seams = screen.getAllByTestId("diff-expand");

    expect(seams).toHaveLength(1);
    expect(seams[0].textContent).toBe("··· Expand 24 unchanged lines");
    // Well under the cap: no show-all control.
    expect(screen.queryByTestId("diff-show-all")).toBeNull();

    await act(async () => {
      fireEvent.click(seams[0]);
    });

    // The hidden run lands at its true position; the seam is gone.
    expect(renderedRows(container)).toBe(32);
    expect(screen.getAllByTestId("diff-gutter-row")).toHaveLength(32);
    expect(screen.queryByTestId("diff-expand")).toBeNull();

    await unmount();
  });

  it("caps a huge diff at 400 rows behind a show-all control", async () => {
    const Edit = toolRenderers.edit_file;

    const bigOld = Array.from(
      { length: 500 },
      (_line: undefined, i: number): string => `old ${i}\n`
    ).join("");

    const bigNew = Array.from(
      { length: 500 },
      (_line: undefined, i: number): string => `new ${i}\n`
    ).join("");

    const { unmount, container } = await renderUI(
      <Edit
        {...partProps({
          toolCallId: "edit-2",
          toolName: "edit_file",
          argsText: JSON.stringify({
            file_path: "/workspace/huge.txt",
            old_string: bigOld,
            new_string: bigNew,
          }),
        })}
      />
    );

    await open("edit-2");

    // 500 removed + 500 added rows: the cap renders 400, holds back 600.
    expect(renderedRows(container)).toBe(400);
    expect(screen.getAllByTestId("diff-gutter-row")).toHaveLength(400);

    const showAll = screen.getByTestId("diff-show-all");

    expect(showAll.textContent).toBe("Show all (600 more rows)");

    await act(async () => {
      fireEvent.click(showAll);
    });

    expect(renderedRows(container)).toBe(1000);
    expect(screen.getAllByTestId("diff-gutter-row")).toHaveLength(1000);
    expect(screen.queryByTestId("diff-show-all")).toBeNull();

    await unmount();
  });
});
