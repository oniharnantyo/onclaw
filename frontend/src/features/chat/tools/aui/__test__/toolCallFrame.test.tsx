/**
 * ToolCallFrame tests (task 4.3): ports the ToolCallCard and toolCollapse
 * scenarios onto the new card — collapsed by default with disclosure, the
 * raw toggle's verbatim panes, the >10-line clamp, the running state as a
 * shimmer label with a live elapsed counter (no spinner; static under
 * reduced motion), in-place running→completed updates, failure styling, the
 * latency rules (none without a timing, formatted with one), edit_file
 * auto-open once with explicit-close precedence, body-failure degradation
 * that leaves siblings intact, and the same flat grammar under both themes.
 * The card is a quiet line, not a panel: no card surface, a receding tool
 * name behind the sentence that carries the payload, and a body that opens
 * under a left hairline rail. Renderers mount standalone with hand-built
 * part props.
 */
import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import { renderUI } from "../../../__test__/test-utils";
import { resetCollapseMap } from "../../collapse";
import { useChatStore } from "../../../store";
import { ExecuteTool } from "../ExecuteTool";
import { EditFileTool, ReadFileTool } from "../FileTools";
import { WebSearchTool } from "../WebTools";
import { partProps } from "./harness";
import type { ToolActivity } from "../../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";

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

// The previews' lazy Shiki loader stays out of these chrome assertions.
vi.mock("../../primitives/shikiHighlight", () => ({
  highlightTokens: async (): Promise<null> => null,
  isKnownLanguage: (): boolean => false,
  tokenStyle: (): Record<string, string> => ({}),
}));

beforeEach(async () => {
  vi.clearAllMocks();
  resetCollapseMap();

  await act(async () => {
    useChatStore.setState({ activeAgentID: "a1", panelTabs: [], entries: [] });
  });
});

describe("ToolCallFrame chrome (task 4.3)", () => {
  it("starts collapsed and opens the body on disclosure", async () => {
    const { unmount } = await renderUI(
      <ReadFileTool
        {...partProps({
          toolCallId: "c1",
          toolName: "read_file",
          argsText: '{"file_path":"/workspace/a.md"}',
          result: "     1\tcontent\n",
        })}
      />
    );

    expect(screen.getByTestId("entry-tool-c1")).toBeTruthy();
    expect(screen.queryByTestId("tool-body-c1")).toBeNull();

    await act(async () => {
      fireEvent.click(screen.getByTestId("tool-header-c1"));
    });

    expect(screen.getByTestId("tool-body-c1")).toBeTruthy();
    expect(
      screen.getByTestId("tool-header-c1").getAttribute("aria-expanded")
    ).toBe("true");

    await act(async () => {
      fireEvent.click(screen.getByTestId("tool-header-c1"));
    });

    expect(screen.queryByTestId("tool-body-c1")).toBeNull();

    await unmount();
  });

  it("the toggle survives a re-render (module collapse map, not local state)", async () => {
    const props = partProps({
      toolCallId: "c1",
      toolName: "read_file",
      argsText: '{"file_path":"/workspace/a.md"}',
      result: "     1\tcontent\n",
    });

    const h = await renderUI(<ReadFileTool {...props} />);

    await act(async () => {
      fireEvent.click(screen.getByTestId("tool-header-c1"));
    });

    expect(screen.getByTestId("tool-body-c1")).toBeTruthy();

    // A streaming re-render mounts a fresh element tree for the same call.
    await h.rerender(<ReadFileTool key="second" {...props} />);

    expect(screen.getByTestId("tool-body-c1")).toBeTruthy();

    await h.unmount();
  });

  it("the raw toggle shows the delivered strings and replaces the formatted body", async () => {
    const { unmount } = await renderUI(
      <ReadFileTool
        {...partProps({
          toolCallId: "c1",
          toolName: "read_file",
          argsText: '{"file_path":"/workspace/a.md"}',
          result: "plain result text",
        })}
      />
    );

    await act(async () => {
      fireEvent.click(screen.getByTestId("tool-header-c1"));
    });

    expect(screen.queryByTestId("tool-raw-c1-panes")).toBeNull();

    await act(async () => {
      fireEvent.click(screen.getByTestId("tool-raw-c1-toggle"));
    });

    const panes = screen.getByTestId("tool-raw-c1-panes");

    expect(panes.textContent).toContain('{"file_path":"/workspace/a.md"}');
    expect(panes.textContent).toContain("plain result text");

    await act(async () => {
      fireEvent.click(screen.getByTestId("tool-raw-c1-toggle"));
    });

    expect(screen.queryByTestId("tool-raw-c1-panes")).toBeNull();

    await unmount();
  });

  it("clamps long output behind a show-all control", async () => {
    const longResult = Array.from(
      { length: 15 },
      (_line: undefined, i: number): string => `line ${i}`
    ).join("\n");

    const { unmount } = await renderUI(
      <ReadFileTool
        {...partProps({
          toolCallId: "c1",
          toolName: "read_file",
          argsText: '{"file_path":"/workspace/a.md"}',
          result: longResult,
        })}
      />
    );

    await act(async () => {
      fireEvent.click(screen.getByTestId("tool-header-c1"));
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId("tool-raw-c1-toggle"));
    });

    const clamp = screen.getByTestId("tool-raw-c1-result-text");

    expect(clamp.getAttribute("data-clamped")).toBe("true");
    expect(clamp.textContent).not.toContain("line 14");

    await act(async () => {
      fireEvent.click(screen.getByTestId("tool-raw-c1-result-show-all"));
    });

    expect(screen.getByTestId("tool-raw-c1-result-text").textContent).toContain(
      "line 14"
    );

    await unmount();
  });

  it("shows the running state as a shimmer label with a live elapsed counter — no spinner", async () => {
    const call: ToolActivity = {
      call_id: "c1",
      name: "execute",
      arguments: '{"command":"go test ./..."}',
    };

    await act(async () => {
      useChatStore.setState({
        entries: [{ kind: "tool", id: "c1", call, at: recentClock() }],
      });
    });

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

    const status = screen.getByTestId("tool-status-c1");

    expect(status.textContent).toContain("running");
    expect(status.textContent).toMatch(/running · \d+:\d\d/);
    // No spinning icon anywhere in the card (the body stays collapsed).
    expect(document.querySelector(".animate-spin")).toBeNull();
    // The command is presented while running (the sentence carries it).
    expect(screen.getByTestId("entry-tool-c1").textContent).toContain(
      "go test ./..."
    );
    expect(screen.queryByTestId("tool-body-c1")).toBeNull();

    await unmount();
  });

  it("under reduced motion the running label is static text", async () => {
    stubReducedMotion(true);

    try {
      const { unmount } = await renderUI(
        <ExecuteTool
          {...partProps({
            toolCallId: "c1",
            toolName: "execute",
            argsText: '{"command":"ls"}',
            status: { type: "running" },
          })}
        />
      );

      const status = screen.getByTestId("tool-status-c1");

      expect(status.getAttribute("data-reduced-motion")).toBe("true");
      expect(status.querySelector(".shimmer")).toBeNull();

      await unmount();
    } finally {
      stubReducedMotion(false);
    }
  });

  it("completes in place: one card, no second, outcome sentence replaces the running label", async () => {
    const running = partProps({
      toolCallId: "c1",
      toolName: "web_search",
      argsText: '{"query":"weather"}',
      status: { type: "running" },
    });

    const completed = partProps({
      toolCallId: "c1",
      toolName: "web_search",
      argsText: '{"query":"weather"}',
      result: '{"results":[{"title":"a","url":"https://a.dev","snippet":"s"}]}',
    });

    const h = await renderUI(<WebSearchTool {...running} />);

    expect(screen.getByTestId("tool-status-c1")).toBeTruthy();

    // The terminal record re-reports the same call id.
    await h.rerender(<WebSearchTool {...completed} />);

    expect(screen.getAllByTestId("entry-tool-c1")).toHaveLength(1);
    expect(screen.queryByTestId("tool-status-c1")).toBeNull();
    expect(screen.queryByTestId("tool-error-c1")).toBeNull();
    expect(screen.getByTestId("entry-tool-c1").textContent).toContain(
      "Found 1 result"
    );

    await h.unmount();
  });

  it("a failed call carries failure styling with the error readable in the body", async () => {
    const { unmount } = await renderUI(
      <ExecuteTool
        {...partProps({
          toolCallId: "c1",
          toolName: "execute",
          argsText: '{"command":"boom"}',
          result: "exit status 1: command not found",
          isError: true,
        })}
      />
    );

    const card = screen.getByTestId("entry-tool-c1");

    expect(screen.getByTestId("tool-error-c1").textContent).toBe("failed");
    // The failure rides the line — the card itself carries no bordered
    // surface (design D3).
    expect(card.className).toContain("text-dangertext");
    expect(card.className).not.toContain("border");
    expect(card.getAttribute("data-tool-state")).toBe("failed");

    await act(async () => {
      fireEvent.click(screen.getByTestId("tool-header-c1"));
    });

    await waitFor((): void => {
      expect(screen.getByTestId("tool-body-c1").textContent).toContain(
        "exit status 1: command not found"
      );
    });

    await unmount();
  });

  it("a part without a timing shows no latency; a timing renders formatted", async () => {
    const first = await renderUI(
      <ReadFileTool
        {...partProps({
          toolCallId: "legacy",
          toolName: "read_file",
          argsText: "{}",
          result: "old",
        })}
      />
    );

    expect(screen.queryByTestId("tool-duration-legacy")).toBeNull();

    await first.unmount();

    const end = Date.parse("2026-01-01T00:00:02Z");

    const second = await renderUI(
      <ReadFileTool
        {...partProps({
          toolCallId: "timed",
          toolName: "read_file",
          argsText: "{}",
          result: "ok",
          timing: { startedAt: end - 1500, completedAt: end },
        })}
      />
    );

    expect(screen.getByTestId("tool-duration-timed").textContent).toBe("1.5s");

    await second.unmount();
  });

  it("edit_file auto-opens once on the live running→completed edge; explicit close wins", async () => {
    const running = partProps({
      toolCallId: "c-edit",
      toolName: "edit_file",
      argsText:
        '{"file_path":"/workspace/app.ts","old_string":"const a = 1;","new_string":"const a = 2;"}',
      status: { type: "running" },
    });

    const completed = partProps({
      toolCallId: "c-edit",
      toolName: "edit_file",
      argsText:
        '{"file_path":"/workspace/app.ts","old_string":"const a = 1;","new_string":"const a = 2;"}',
      result: "replaced",
    });

    const h = await renderUI(<EditFileTool {...running} />);

    // Running: collapsed (no body yet).
    expect(screen.queryByTestId("tool-body-c-edit")).toBeNull();

    await h.rerender(<EditFileTool {...completed} />);

    // The edge auto-opened the card to show the diff.
    expect(screen.getByTestId("tool-body-c-edit")).toBeTruthy();

    // The reader closes it by hand…
    await act(async () => {
      fireEvent.click(screen.getByTestId("tool-header-c-edit"));
    });

    expect(screen.queryByTestId("tool-body-c-edit")).toBeNull();

    // …and the same call re-reporting its terminal state stays closed.
    await h.rerender(<EditFileTool {...completed} />);

    expect(screen.queryByTestId("tool-body-c-edit")).toBeNull();

    await h.unmount();
  });

  it("a reloaded completed edit (no live edge) starts collapsed", async () => {
    const { unmount } = await renderUI(
      <EditFileTool
        {...partProps({
          toolCallId: "c-edit",
          toolName: "edit_file",
          argsText:
            '{"file_path":"/workspace/app.ts","old_string":"a","new_string":"b"}',
          result: "replaced",
        })}
      />
    );

    expect(screen.queryByTestId("tool-body-c-edit")).toBeNull();

    await unmount();
  });

  it("a body throwing on a malformed payload degrades to the fallback without touching siblings", async () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => undefined);

    try {
      const { unmount } = await renderUI(
        <>
          <EditFileTool
            {...partProps({
              toolCallId: "c-bad",
              toolName: "edit_file",
              // Parses as an object but carries no usable fields: the
              // shaped body throws and the boundary degrades the card.
              argsText: "{}",
              result: "replaced",
            })}
          />
          <ReadFileTool
            {...partProps({
              toolCallId: "c-good",
              toolName: "read_file",
              argsText: '{"file_path":"/workspace/a.md"}',
              result: "     1\tcontent\n",
            })}
          />
        </>
      );

      await act(async () => {
        fireEvent.click(screen.getByTestId("tool-header-c-bad"));
      });

      expect(screen.getByTestId("tool-fallback-c-bad")).toBeTruthy();

      await act(async () => {
        fireEvent.click(screen.getByTestId("tool-header-c-good"));
      });

      expect(screen.getByTestId("read-body")).toBeTruthy();

      await unmount();
    } finally {
      spy.mockRestore();
    }
  });

  it("renders the header as a quiet line, not a card", async () => {
    const { unmount } = await renderUI(
      <ReadFileTool
        {...partProps({
          toolCallId: "c1",
          toolName: "read_file",
          argsText: '{"file_path":"/workspace/a.md"}',
          result: "     1\tcontent\n",
        })}
      />
    );

    const header = screen.getByTestId("tool-header-c1");

    // The header is a line: no fill, no border, no panel.
    expect(header.className).not.toContain("bg-card");
    expect(header.className).not.toContain("border");
    expect(header.className).toContain("text-muted-foreground");

    // The tool name recedes; the sentence carries the payload.
    expect(screen.getByTestId("tool-name-c1").className).toContain(
      "text-muted-foreground"
    );
    expect(screen.getByTestId("tool-sentence-c1").textContent).toContain(
      "a.md"
    );

    // The body is absent while the line is collapsed…
    expect(screen.queryByTestId("tool-body-c1")).toBeNull();

    await act(async () => {
      fireEvent.click(header);
    });

    // …and opens beneath the header under a left hairline rail.
    const body = screen.getByTestId("tool-body-c1");

    expect(body.className).toContain("border-s");
    expect(body.className).toContain("ps-3");
    expect(body.className).not.toContain("border-t");

    await unmount();
  });

  it("renders the same flat grammar under light and dark themes", async () => {
    for (const theme of ["light", "dark"] as const) {
      document.documentElement.dataset.theme = theme;

      const { unmount } = await renderUI(
        <ReadFileTool
          {...partProps({
            toolCallId: "c1",
            toolName: "read_file",
            argsText: '{"file_path":"/workspace/a.md"}',
            result: "     1\tcontent\n",
          })}
        />
      );

      const card = screen.getByTestId("entry-tool-c1");

      // No card surface in either theme: the call is a line.
      expect(card.className).not.toContain("bg-card");
      expect(card.className).not.toContain("border");
      expect(card.className).not.toContain("rounded");

      // The name recedes behind the sentence in either theme.
      expect(screen.getByTestId("tool-name-c1").className).toContain(
        "text-muted-foreground"
      );
      expect(screen.getByTestId("tool-sentence-c1").className).toContain(
        "text-foreground/90"
      );

      await unmount();
    }

    delete document.documentElement.dataset.theme;
  });
});

/** An ISO timestamp a few seconds ago — the live clock's start. */
function recentClock(): string {
  return new Date(Date.now() - 4_000).toISOString();
}

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
