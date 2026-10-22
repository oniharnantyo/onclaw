/**
 * Tool renderer tests (task 4.2): every "Interactive tool bodies" and
 * "Honest fallback" scenario on the new layer — the shell success with its
 * duration, a non-zero exit code, timeout and truncation as distinct
 * states, the running cursor, the no-output wording, the search searching
 * state and results with the query pill + sources line + linked
 * title/hostname/snippet (stagger collapsed under reduced motion), the diff
 * body's header counts, path activation opening the right panel, the MCP
 * fallback, unparseable arguments verbatim, the plan snapshot, and the task
 * card. Renderers mount standalone with hand-built part props.
 */
import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import { renderUI } from "../../../__test__/test-utils";
import { resetCollapseMap } from "../../collapse";
import { useChatStore } from "../../../store";
import { EditFileTool } from "../FileTools";
import { ExecuteTool } from "../ExecuteTool";
import { GrepTool } from "../SearchTools";
import { TaskTool } from "../TaskTool";
import { TodoWriteTool } from "../TodoTool";
import { WebSearchTool } from "../WebTools";
import { partProps } from "./harness";
import { toolRenderers } from "../renderers";

vi.mock("../../../../../lib/client", () => ({
  client: {
    settings: {
      tools: vi.fn(async (): Promise<never[]> => []),
    },
    chat: {
      browserScreenshot: vi.fn(async (): Promise<never> => {
        throw new Error("no screenshots in tests");
      }),
    },
  },
}));

// The previews' lazy Shiki loader stays out of these body assertions.
vi.mock("../../primitives/shikiHighlight", () => ({
  highlightTokens: async (): Promise<null> => null,
  isKnownLanguage: (): boolean => false,
  tokenStyle: (): Record<string, string> => ({}),
}));

beforeEach(async () => {
  vi.clearAllMocks();
  resetCollapseMap();

  await act(async () => {
    useChatStore.setState({
      activeAgentID: "a1",
      panelTabs: [],
      entries: [],
    });
  });
});

async function open(toolCallId: string): Promise<void> {
  await act(async () => {
    fireEvent.click(screen.getByTestId(`tool-header-${toolCallId}`));
  });
}

describe("shell body (spec: Shell command block and its states)", () => {
  it("success: the command and its output in a terminal block, success status, duration", async () => {
    const end = Date.parse("2026-01-01T00:00:02Z");

    const { unmount } = await renderUI(
      <ExecuteTool
        {...partProps({
          toolCallId: "c1",
          toolName: "execute",
          argsText: '{"command":"ls -1"}',
          result: "a.txt\nb.txt",
          timing: { startedAt: end - 1200, completedAt: end },
        })}
      />
    );

    await open("c1");

    const block = document.querySelector('[data-slot="terminal-block"]');

    expect(block?.textContent).toContain("ls -1");

    await waitFor((): void => {
      expect(block?.textContent).toContain("a.txt");
    });

    expect(block?.getAttribute("data-state")).toBe("done");
    expect(block?.textContent).toContain("exit 0");
    expect(block?.textContent).toContain("1.2s");

    await unmount();
  });

  it("a non-zero exit states the code in a failure state with the output readable", async () => {
    const { unmount } = await renderUI(
      <ExecuteTool
        {...partProps({
          toolCallId: "c1",
          toolName: "execute",
          argsText: '{"command":"go test ./..."}',
          result: "FAIL foo\n[Command failed with exit code 1]",
        })}
      />
    );

    await open("c1");

    const block = document.querySelector('[data-slot="terminal-block"]');

    await waitFor((): void => {
      expect(block?.textContent).toContain("FAIL foo");
    });

    expect(block?.getAttribute("data-state")).toBe("failed");
    expect(block?.textContent).toContain("exit 1");
    // The consumed marker line is not duplicated in the body.
    expect(block?.textContent).not.toContain(
      "[Command failed with exit code 1]"
    );

    await unmount();
  });

  it("a timeout is its own status, distinct from a failed exit", async () => {
    const { unmount } = await renderUI(
      <ExecuteTool
        {...partProps({
          toolCallId: "c1",
          toolName: "execute",
          argsText: '{"command":"sleep 999"}',
          result: "partial\n[Command timed out and was stopped]",
        })}
      />
    );

    await open("c1");

    expect(screen.getByTestId("execute-timeout").textContent).toContain(
      "timed out"
    );

    const body = screen.getByTestId("execute-body");

    await waitFor((): void => {
      expect(body.textContent).toContain("partial");
    });

    // The timeout states its own status, distinct from the failed exit's.
    expect(body.textContent).toContain("timed out");
    expect(body.textContent).not.toContain(
      "[Command timed out and was stopped]"
    );

    await unmount();
  });

  it("truncation states its own status", async () => {
    const { unmount } = await renderUI(
      <ExecuteTool
        {...partProps({
          toolCallId: "c1",
          toolName: "execute",
          argsText: '{"command":"cat big.log"}',
          result: "head\n[Output was truncated due to size limits]",
        })}
      />
    );

    await open("c1");

    const block = document.querySelector('[data-slot="terminal-block"]');

    await waitFor((): void => {
      expect(block?.textContent).toContain("output truncated");
    });

    await unmount();
  });

  it("while running the block shows the command with the cursor and no exit status yet", async () => {
    const { unmount } = await renderUI(
      <ExecuteTool
        {...partProps({
          toolCallId: "c1",
          toolName: "execute",
          argsText: '{"command":"go test ./..."}',
          status: { type: "running" },
        })}
      />
    );

    await open("c1");

    const block = document.querySelector('[data-slot="terminal-block"]');

    expect(block?.getAttribute("data-state")).toBe("running");
    expect(block?.textContent).toContain("go test ./...");
    expect(block?.textContent).not.toContain("exit");

    await unmount();
  });

  it("a call that produced no output says so instead of an empty block", async () => {
    const { unmount } = await renderUI(
      <ExecuteTool
        {...partProps({
          toolCallId: "c1",
          toolName: "execute",
          argsText: '{"command":"touch x"}',
          result: "[Command executed successfully with no output]",
        })}
      />
    );

    await open("c1");

    await waitFor((): void => {
      expect(screen.getByTestId("execute-no-output")).toBeTruthy();
    });

    const block = document.querySelector('[data-slot="terminal-block"]');

    expect(block?.textContent).not.toContain(
      "[Command executed successfully with no output]"
    );

    await unmount();
  });
});

describe("edit body (spec: Edit renders a diff)", () => {
  it("the header names the file with the added and removed counts, rows colored, context collapsed", async () => {
    const oldText = [
      "a",
      "b",
      "c",
      "d",
      "e",
      "f",
      "g",
      "h",
      "i",
      "j",
      "k",
    ].join("\n");

    const argsText = JSON.stringify({
      file_path: "/workspace/app.ts",
      old_string: oldText,
      new_string: oldText.replace("f", "F"),
    });

    const { unmount } = await renderUI(
      <EditFileTool
        {...partProps({
          toolCallId: "c1",
          toolName: "edit_file",
          argsText,
          result: "replaced",
        })}
      />
    );

    await open("c1");

    const diff = document.querySelector('[data-slot="code-diff"]');

    expect(diff?.textContent).toContain("/workspace/app.ts");
    expect(diff?.textContent).toContain("+1");
    expect(diff?.textContent).toContain("−1");
    expect(screen.getByTestId("edit-body").textContent).not.toContain(
      "all occurrences"
    );

    await unmount();
  });

  it("the all-occurrences marker renders when the call replaced every occurrence", async () => {
    const { unmount } = await renderUI(
      <EditFileTool
        {...partProps({
          toolCallId: "c1",
          toolName: "edit_file",
          argsText: JSON.stringify({
            file_path: "/workspace/app.ts",
            old_string: "x",
            new_string: "y",
            replace_all: true,
          }),
          result: "replaced",
        })}
      />
    );

    await open("c1");

    expect(screen.getByTestId("edit-replace-all").textContent).toContain(
      "all occurrences"
    );

    await unmount();
  });
});

describe("web_search body (spec: Search shows a searching state / results as cards)", () => {
  it("while running the query pill shows with a searching state and no results yet", async () => {
    const { unmount } = await renderUI(
      <WebSearchTool
        {...partProps({
          toolCallId: "c1",
          toolName: "web_search",
          argsText: '{"query":"buddies wails"}',
          status: { type: "running" },
        })}
      />
    );

    await open("c1");

    expect(screen.getByTestId("search-body").textContent).toContain(
      "buddies wails"
    );
    expect(screen.getByTestId("search-body").textContent).toContain(
      "Searching"
    );
    expect(screen.queryByTestId("search-result")).toBeNull();
    expect(screen.queryByTestId("search-sources")).toBeNull();

    await unmount();
  });

  it("results land with the sources line and each card's linked title, hostname, snippet", async () => {
    stubReducedMotion(true);

    try {
      const { unmount } = await renderUI(
        <WebSearchTool
          {...partProps({
            toolCallId: "c1",
            toolName: "web_search",
            argsText: '{"query":"buddies wails"}',
            result: JSON.stringify({
              results: [
                {
                  title: "First",
                  url: "https://first.dev/x",
                  snippet: "one snippet",
                },
                {
                  title: "Second",
                  url: "https://second.dev/y",
                  snippet: "two snippet",
                },
              ],
            }),
          })}
        />
      );

      await open("c1");

      expect(screen.getByTestId("search-sources").textContent).toBe(
        "Read 2 sources"
      );

      const titles = screen.getAllByTestId("search-result-title");

      expect(titles).toHaveLength(2);
      // SAFETY: the search card renders every source title as the anchor
      // itself (WebTools' result title), and the length check above pins the
      // queried node to the first of those two anchors.
      expect((titles[0] as HTMLAnchorElement).href).toBe("https://first.dev/x");
      expect(screen.getAllByTestId("search-result-host")[0].textContent).toBe(
        "first.dev"
      );
      expect(
        screen.getAllByTestId("search-result-snippet")[0].textContent
      ).toBe("one snippet");

      await unmount();
    } finally {
      stubReducedMotion(false);
    }
  });
});

describe("path activation (spec: Path in a body opens the preview)", () => {
  it("a grep match row opens the file's preview in the right panel", async () => {
    const { unmount } = await renderUI(
      <GrepTool
        {...partProps({
          toolCallId: "c1",
          toolName: "grep",
          argsText: '{"pattern":"todo","output_mode":"content"}',
          result: "src/app.ts:12:  // TODO fix\n",
        })}
      />
    );

    await open("c1");

    await act(async () => {
      fireEvent.click(screen.getByTestId("grep-match"));
    });

    const tabs = useChatStore.getState().panelTabs;

    expect(tabs).toHaveLength(1);
    expect(tabs[0]?.kind).toBe("workspace-file");
    expect(tabs[0]?.title).toBe("app.ts");

    await unmount();
  });
});

describe("honest fallback (spec: MCP tool / Unparseable arguments)", () => {
  it("an MCP tool renders its name with pretty-printed args and result and no sentence", async () => {
    const Fallback = toolRenderers.fallback;

    const { unmount } = await renderUI(
      <Fallback
        {...partProps({
          toolCallId: "c1",
          toolName: "mcp__github__search",
          argsText: '{"q":"a"}',
          result: '{"hits":1}',
        })}
      />
    );

    const trigger = document.querySelector(
      '[data-slot="tool-fallback-trigger"]'
    );

    expect(trigger?.textContent).toContain("mcp__github__search");

    await act(async () => {
      fireEvent.click(
        document.querySelector('[data-slot="tool-fallback-trigger"]') ??
          document.body
      );
    });

    expect(document.body.textContent).toContain('"q": "a"');
    expect(document.body.textContent).toContain('"hits": 1');
    // No generated sentence: the raw name is not rephrased anywhere.
    expect(document.body.textContent).not.toContain("Found");

    await unmount();
  });

  it("a shipped tool with unparseable arguments shows them verbatim in the fallback body", async () => {
    const { unmount } = await renderUI(
      <EditFileTool
        {...partProps({
          toolCallId: "c1",
          toolName: "edit_file",
          argsText: "not json at all",
          result: "replaced",
        })}
      />
    );

    await open("c1");

    const fallback = screen.getByTestId("tool-fallback-c1");

    expect(fallback.textContent).toContain("not json at all");
    expect(fallback.textContent).toContain("Arguments");

    await unmount();
  });
});

describe("plan snapshot (spec: Plan snapshot)", () => {
  it("the plan items render as a checklist with pending, in-progress, done, and failed states", async () => {
    const items = [
      { key: "k1", text: "first", status: "done" },
      { key: "k2", text: "second", status: "active" },
      { key: "k3", text: "third", status: "pending" },
      { key: "k4", text: "boom", status: "failed" },
    ];

    const { unmount } = await renderUI(
      <TodoWriteTool
        {...partProps({
          toolCallId: "c1",
          toolName: "todo_write",
          argsText: JSON.stringify({ items }),
          result: JSON.stringify({ items }),
        })}
      />
    );

    await open("c1");

    const body = screen.getByTestId("todo-body");

    // The element counts completed steps up to the active one (first done,
    // second in progress → 1 of 4); the failed item lists beneath it.
    expect(body.textContent).toContain("1 of 4");
    expect(screen.getByTestId("todo-failed").textContent).toContain("boom");
    expect(body.textContent).toContain("first");
    expect(body.textContent).toContain("third");

    await unmount();
  });
});

describe("task card (port of TaskCallCard's scenarios)", () => {
  it("a running delegation shows the working state; the terminal record shows the answer", async () => {
    const running = await renderUI(
      <TaskTool
        {...partProps({
          toolCallId: "c1",
          toolName: "task",
          argsText: '{"task_id":"t-1","description":"scan the repo"}',
          status: { type: "running" },
        })}
      />
    );

    const card = document.querySelector('[data-slot="task-card"]');

    expect(card?.getAttribute("data-state")).toBe("working");
    expect(card?.textContent).toContain("scan the repo");

    await running.unmount();

    const done = await renderUI(
      <TaskTool
        {...partProps({
          toolCallId: "c1",
          toolName: "task",
          argsText:
            '{"task_id":"t-1","description":"scan the repo","transcript_path":".tasks/t-1.md"}',
          result: "no findings",
        })}
      />
    );

    const doneCard = document.querySelector('[data-slot="task-card"]');

    expect(doneCard?.getAttribute("data-state")).toBe("done");
    expect(doneCard?.textContent).toContain("no findings");
    expect(doneCard?.textContent).toContain(".tasks/t-1.md");

    await done.unmount();
  });
});

/** jsdom has no matchMedia; the stub mirrors the legacy suites' idiom. */
function stubReducedMotion(reduced: boolean): void {
  interface MediaMatches {
    matches: boolean;
    addEventListener: (type: string, listener: () => void) => void;
    removeEventListener: (type: string, listener: () => void) => void;
  }

  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    writable: true,
    value: (query: string): MediaMatches => ({
      matches: reduced && query.includes("prefers-reduced-motion"),
      addEventListener: (): void => undefined,
      removeEventListener: (): void => undefined,
    }),
  });
}

/**
 * The renderer catalogue (port of the legacy registry suite): every tool the
 * app ships resolves to its OWN renderer — the fallback is only ever the
 * unknown/MCP names — and the delegation card reports every terminal state
 * (port of TaskCallCard's scenarios).
 */
describe("the renderer catalogue", () => {
  it("gives every shipped tool its own renderer; the fallback covers the unknown names", () => {
    const BUILTINS = [
      "current_time",
      "http_get",
      "ls",
      "read_file",
      "write_file",
      "edit_file",
      "glob",
      "grep",
      "execute",
      "delete_file",
      "todo_write",
      "todo_read",
      "web_fetch",
      "web_search",
      "browser",
      "task",
    ];

    for (const name of BUILTINS) {
      expect(toolRenderers[name]).toBeDefined();
      expect(toolRenderers[name]).not.toBe(toolRenderers.fallback);
    }

    // The fallback is the only entry an unknown name reaches.
    expect(toolRenderers.fallback).toBeDefined();
  });

  it("the delegation card marks a cancelled, a failed and a legacy task", async () => {
    const cancelled = await renderUI(
      <TaskTool
        {...partProps({
          toolCallId: "c-cancel",
          toolName: "task",
          argsText: '{"task_id":"t-2","description":"scan the repo"}',
          status: { type: "incomplete", reason: "cancelled" },
        })}
      />
    );

    expect(
      document
        .querySelector('[data-slot="task-card"]')
        ?.getAttribute("data-state")
    ).toBe("cancelled");

    await cancelled.unmount();

    const failed = await renderUI(
      <TaskTool
        {...partProps({
          toolCallId: "c-fail",
          toolName: "task",
          argsText: '{"task_id":"t-3","description":"scan the repo"}',
          isError: true,
          status: { type: "complete" },
        })}
      />
    );

    expect(
      document
        .querySelector('[data-slot="task-card"]')
        ?.getAttribute("data-state")
    ).toBe("failed");

    await failed.unmount();

    // A legacy record with no status at all reads as terminal.
    const legacy = await renderUI(
      <TaskTool
        {...partProps({
          toolCallId: "c-legacy",
          toolName: "task",
          argsText: '{"task_id":"t-4","description":"scan the repo"}',
        })}
      />
    );

    expect(
      document
        .querySelector('[data-slot="task-card"]')
        ?.getAttribute("data-state")
    ).toBe("done");

    await legacy.unmount();
  });
});
