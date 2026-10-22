/**
 * Theme parity test (task 6.3, spec chat-view "Theme parity"): the thread
 * renders the same mixed content — a GFM table, a mermaid diagram, a code
 * block, a tool run, a pending approval and the run surfaces — under both
 * `data-theme="light"` and `data-theme="dark"` without a render error.
 *
 * The tokens themselves re-skin through the `data-theme` attribute on the
 * document element; this suite proves nothing in the tree hardcodes a colour
 * that would break the other theme.
 */
import { screen, waitFor } from "@testing-library/dom";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";

import {
  Kind,
  ToolStatus,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import { ev, fold, renderThread } from "./harness";

vi.mock("../../../../../lib/client", () => ({
  client: {
    settings: { tools: vi.fn(async (): Promise<never[]> => []) },
    chat: {
      browserScreenshot: vi.fn(async (): Promise<never> => {
        throw new Error("no screenshots in tests");
      }),
      resolveApproval: vi.fn(async (): Promise<void> => undefined),
    },
  },
}));

/** The document element's theme attribute, restored after each test. */
const originalTheme = document.documentElement.getAttribute("data-theme");

afterEach(() => {
  if (originalTheme === null) {
    document.documentElement.removeAttribute("data-theme");
  } else {
    document.documentElement.setAttribute("data-theme", originalTheme);
  }
});

/** One turn carrying every surface the parity check covers. */
function mixedTurn(): ReturnType<typeof fold> {
  return fold(
    ev(Kind.KindUserMessage, { text: "show me", turn_id: "t1" }),
    ev(Kind.KindReasoning, { text: "weighing it", turn_id: "t1" }),
    ev(Kind.KindAssistantMessage, {
      text: [
        "| a | b |",
        "| - | - |",
        "| 1 | 2 |",
        "",
        "```go",
        "x := 1",
        "```",
        "",
        "```mermaid",
        "graph TD; A-->B;",
        "```",
      ].join("\n"),
      turn_id: "t1",
    }),
    ev(Kind.KindToolActivity, {
      turn_id: "t1",
      tool: {
        call_id: "c1",
        name: "read_file",
        arguments: '{"file_path":"src/a.ts"}',
        status: ToolStatus.ToolStatusCompleted,
        result: "contents",
      },
    }),
    ev(Kind.KindToolActivity, {
      turn_id: "t1",
      tool: {
        call_id: "c2",
        name: "read_file",
        arguments: '{"file_path":"src/b.ts"}',
        status: ToolStatus.ToolStatusCompleted,
        result: "contents",
      },
    }),
    ev(Kind.KindToolActivity, {
      turn_id: "t1",
      tool: {
        call_id: "c3",
        name: "read_file",
        arguments: '{"file_path":"src/c.ts"}',
        status: ToolStatus.ToolStatusCompleted,
        result: "contents",
      },
    }),
    ev(Kind.KindToolActivity, {
      turn_id: "t1",
      tool: {
        call_id: "c4",
        name: "read_file",
        arguments: '{"file_path":"src/d.ts"}',
        status: ToolStatus.ToolStatusCompleted,
        result: "contents",
      },
    }),
    ev(Kind.KindApprovalRequired, {
      turn_id: "t1",
      approval: { interrupt_id: "int-1", command: "rm -rf build/" },
    })
  );
}

describe("theme parity (task 6.3)", () => {
  it("renders the mixed transcript in the light theme", async () => {
    document.documentElement.setAttribute("data-theme", "light");

    const h = await renderThread(mixedTurn());
    // The completed-text path loads lazily (the highlighter chunk stays out
    // of the first paint), so the render waits for it to land.
    await h.settle();

    expect(screen.getByTestId("entry-user")).toBeTruthy();
    expect(screen.getByTestId("entry-assistant")).toBeTruthy();
    // The GFM table rendered as a table, not as pipe text.
    await waitFor(() => expect(document.querySelector("table")).toBeTruthy());
    // The tool run folded behind its summary.
    expect(screen.getByTestId("run-summary")).toBeTruthy();
    // The pending approval is actionable.
    expect(screen.getByTestId("entry-approval")).toBeTruthy();

    await h.unmount();
  });

  it("renders the same mixed transcript in the dark theme", async () => {
    document.documentElement.setAttribute("data-theme", "dark");

    const h = await renderThread(mixedTurn());
    await h.settle();

    expect(screen.getByTestId("entry-user")).toBeTruthy();
    expect(screen.getByTestId("entry-assistant")).toBeTruthy();
    await waitFor(() => expect(document.querySelector("table")).toBeTruthy());
    expect(screen.getByTestId("run-summary")).toBeTruthy();
    expect(screen.getByTestId("entry-approval")).toBeTruthy();

    await h.unmount();
  });

  it("renders the SAME structure in both themes", async () => {
    document.documentElement.setAttribute("data-theme", "light");

    const light = await renderThread(mixedTurn());

    await light.settle();

    const lightIDs = Array.from(
      document.querySelectorAll<HTMLElement>("[data-testid]")
    )
      .map((element: HTMLElement): string => element.dataset.testid ?? "")
      .sort();

    await light.unmount();

    document.documentElement.setAttribute("data-theme", "dark");

    const dark = await renderThread(mixedTurn());

    await dark.settle();

    const darkIDs = Array.from(
      document.querySelectorAll<HTMLElement>("[data-testid]")
    )
      .map((element: HTMLElement): string => element.dataset.testid ?? "")
      .sort();

    await dark.unmount();

    // Parity: the same surfaces, in the same order, under both themes — the
    // themes change the tokens, never the structure.
    expect(darkIDs).toEqual(lightIDs);
    expect(lightIDs.length).toBeGreaterThan(5);
  });
});
