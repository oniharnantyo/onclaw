/**
 * Assistant message tests (task 3.2, spec chat-view "Chat transcript
 * rendering" / "Code block presentation" / "Streaming-safe markdown" /
 * "Message copy action" / "Regenerate and branch variants" and chat-view
 * "Background task progress"): GFM constructs render on the completed path,
 * mermaid shows source while streaming and a diagram (or the quiet fallback)
 * when complete, streaming code stays plain and completed code highlights
 * under its language label, the copy action carries the refusal toast,
 * regenerate and the variant picker ride the store without requests, the
 * turn activity shows the typing dots before the first token and a status
 * line naming a running tool, and the error card classifies the failure
 * into its plain-language headline with the raw string behind the details
 * disclosure and Retry riding the regenerate path
 * (refine-chat-surface-identity tasks 3.1–3.3).
 *
 * The heavy renderers are mocked at their module boundaries
 * (beautiful-mermaid, the vendored shiki element, the Wails clipboard
 * binding) — the real engines are covered by shikiHighlight.test.ts and the
 * mermaid package's own suite.
 */
import React from "react";
import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/dom";
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vite-plus/test";

import type { RenderOptions } from "beautiful-mermaid";
import {
  Kind,
  ToolStatus,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import { dismissToast, getToasts } from "../../../../lib/toasts";
import { fold } from "./harness";
import { useChatStore } from "../../store";

// --- module-boundary mocks -------------------------------------------------

const { renderMermaidSVG } = vi.hoisted(() => ({
  renderMermaidSVG: vi.fn<(code: string, opts: RenderOptions) => string>(),
}));

vi.mock("beautiful-mermaid", () => ({ renderMermaidSVG }));

const { ShikiStub } = vi.hoisted(() => ({
  ShikiStub: vi.fn(
    (props: { code?: string; language?: string }): React.ReactElement =>
      // SAFETY: the stub renders the two fields the completed-code tests
      // assert on; the vendored element passes exactly this shape.
      React.createElement(
        "div",
        {
          "data-testid": "md-code-highlighted",
          "data-language": props.language,
        },
        props.code
      )
  ),
}));

vi.mock("@/components/assistant-ui/elements/shiki-highlighter", () => ({
  SyntaxHighlighter: ShikiStub,
}));

const { clipboardSetText } = vi.hoisted(() => ({
  clipboardSetText: vi.fn<(text: string) => Promise<void>>(),
}));

vi.mock(
  "../../../../../bindings/github.com/oniharnantyo/buddies/internal/services/clipboardservice",
  () => ({ SetText: clipboardSetText })
);

import { ev, renderThread, type ThreadHarness } from "./harness";

const SVG = '<svg data-test-mermaid="1"><rect fill="var(--background)"/></svg>';

/** A completed turn with the given answer text. */
function settledTurn(text: string): ReturnType<typeof fold> {
  return fold(
    ev(Kind.KindUserMessage, { text: "ask", turn_id: "t1" }),
    ev(Kind.KindAssistantMessage, { text, turn_id: "t1" }),
    ev(Kind.KindTurnCompleted, { turn_id: "t1" })
  );
}

/** A failed turn carrying the given raw error string. */
function failedTurn(error: string): ReturnType<typeof fold> {
  return fold(
    ev(Kind.KindUserMessage, { text: "ask", turn_id: "t1" }),
    ev(Kind.KindTurnError, { turn_id: "t1", error })
  );
}

/** jsdom may not define navigator.clipboard at all — saved for restore. */
const clipboardDescriptor: PropertyDescriptor | undefined =
  Object.getOwnPropertyDescriptor(window.navigator, "clipboard");

function stubClipboard(writeText?: (text: string) => Promise<void>): void {
  Object.defineProperty(window.navigator, "clipboard", {
    value: writeText === undefined ? undefined : { writeText },
    configurable: true,
  });
}

let h: ThreadHarness | null = null;

async function mount(entries: ReturnType<typeof fold>): Promise<ThreadHarness> {
  h = await renderThread(entries);

  return h;
}

beforeEach(() => {
  vi.clearAllMocks();
  renderMermaidSVG.mockReset();
  clipboardSetText.mockReset();
  clipboardSetText.mockRejectedValue(new Error("clipboard write refused"));
  useChatStore.setState({ activeSessionID: "s1", busy: false, entries: [] });

  for (const t of getToasts()) dismissToast(t.id);
});

afterEach(async () => {
  if (h !== null) {
    await act(async () => {
      await h?.unmount();
    });
    h = null;
  }

  if (clipboardDescriptor !== undefined) {
    Object.defineProperty(window.navigator, "clipboard", clipboardDescriptor);
  } else {
    // SAFETY: when jsdom had no clipboard descriptor the stub was installed
    // as an own configurable property, so the delete only removes the stub.
    delete (window.navigator as { clipboard?: unknown }).clipboard;
  }
});

describe("GFM rendering (task 3.2)", () => {
  it("renders a pipe table as a table with header and body rows", async () => {
    await mount(settledTurn("| a | b |\n| --- | --- |\n| 1 | 2 |\n| 3 | 4 |"));

    const table = document.querySelector(
      "[data-testid='entry-assistant'] table"
    );

    expect(table).not.toBeNull();
    expect(table?.querySelectorAll("th")).toHaveLength(2);
    expect(table?.querySelectorAll("td")).toHaveLength(4);
  });

  it("renders an ordered list with its numbers in sequence", async () => {
    await mount(settledTurn("1. first\n2. second\n3. third"));

    const list = document.querySelector("[data-testid='entry-assistant'] ol");

    expect(list).not.toBeNull();
    expect(list?.className).toContain("list-decimal");
    expect(list?.querySelectorAll("li")).toHaveLength(3);
  });

  it("renders task-list items with their checked state", async () => {
    await mount(settledTurn("- [x] done thing\n- [ ] open thing"));

    const boxes = document
      .querySelector("[data-testid='entry-assistant']")
      ?.querySelectorAll<HTMLInputElement>("input[type='checkbox']");

    expect(boxes).toHaveLength(2);
    expect(boxes?.[0]?.checked).toBe(true);
    expect(boxes?.[1]?.checked).toBe(false);
  });
});

describe("mermaid (task 3.2, design D5)", () => {
  const SOURCE = "graph TD; A-->B";

  it("shows the fenced source while streaming and no diagram is attempted", async () => {
    await mount(
      fold(
        ev(Kind.KindUserMessage, { text: "ask", turn_id: "t1" }),
        ev(Kind.KindTextDelta, {
          text: "diagram:\n\n```mermaid\ngraph TD; A-->B",
          turn_id: "t1",
        })
      )
    );

    expect(renderMermaidSVG).not.toHaveBeenCalled();

    const assistant = document.querySelector("[data-testid='entry-assistant']");

    expect(assistant?.textContent).toContain("graph TD; A-->B");
    expect(assistant?.querySelector("[data-streaming='true']")).not.toBeNull();
    expect(document.querySelector("[data-test-mermaid]")).toBeNull();
  });

  it("renders the diagram once the message completes", async () => {
    renderMermaidSVG.mockReturnValue(SVG);

    await mount(settledTurn(`look:\n\n\`\`\`mermaid\n${SOURCE}\n\`\`\``));

    expect(renderMermaidSVG).toHaveBeenCalled();
    expect(document.querySelector("[data-test-mermaid]")).not.toBeNull();
  });

  it("falls back to the source with a quiet note when the diagram fails to parse", async () => {
    renderMermaidSVG.mockImplementation((): string => {
      throw new Error("parse error");
    });

    await mount(settledTurn(`look:\n\n\`\`\`mermaid\n${SOURCE}\n\`\`\``));

    const fallback = document.querySelector("[data-slot='mermaid-fallback']");

    expect(fallback).not.toBeNull();
    expect(fallback?.textContent).toContain(SOURCE);
    expect(fallback?.textContent).toContain("diagram could not be rendered");
  });

  it("re-renders the diagram through the theme tokens when the theme flips", async () => {
    renderMermaidSVG.mockReturnValue(SVG);

    const mounted = await mount(
      settledTurn(`look:\n\n\`\`\`mermaid\n${SOURCE}\n\`\`\``)
    );

    expect(document.querySelector("[data-test-mermaid]")).not.toBeNull();

    document.documentElement.dataset.theme = "dark";
    await mounted.setEntries(
      settledTurn(`look:\n\n\`\`\`mermaid\n${SOURCE}\n\`\`\``)
    );

    // The palette rides CSS variables, so the same SVG restyles without a
    // re-render; the diagram survives the flip in place.
    expect(document.querySelector("[data-test-mermaid]")).not.toBeNull();
    expect(renderMermaidSVG.mock.calls[0]?.[1]).toHaveProperty(
      "bg",
      "var(--background)"
    );

    delete document.documentElement.dataset.theme;
  });
});

describe("code blocks (task 3.2, spec chat-view 'Code block presentation')", () => {
  it("keeps streaming code plain and highlights it once the message completes", async () => {
    const source = "before\n\n```go\nx := 1\n```";

    const mounted = await mount(
      fold(
        ev(Kind.KindUserMessage, { text: "ask", turn_id: "t1" }),
        ev(Kind.KindTextDelta, { text: source, turn_id: "t1" })
      )
    );

    // Streaming: the fenced text renders plain (no highlighter work).
    expect(ShikiStub).not.toHaveBeenCalled();

    const streamingPre = document.querySelector("[data-streaming='true'] pre");

    expect(streamingPre?.textContent).toContain("x := 1");
    expect(
      document.querySelector("[data-testid='md-code-highlighted']")
    ).toBeNull();

    // Completed: the same fence renders through the shiki element with its
    // language label and copy control. The completed path loads lazily (the
    // highlighter chunk must not weigh the first paint), so the render waits
    // for the chunk to land.
    await mounted.setEntries(settledTurn(source));
    await mounted.settle();

    const highlighted = document.querySelector(
      "[data-testid='md-code-highlighted']"
    );

    expect(highlighted).not.toBeNull();
    expect(highlighted?.getAttribute("data-language")).toBe("go");
    expect(highlighted?.textContent).toContain("x := 1");

    const header = document.querySelector(".aui-code-header-root");

    expect(header?.textContent).toContain("go");
    expect(header?.querySelector("button")).not.toBeNull();
  });
});

describe("message actions (task 3.2)", () => {
  it("copies the message source and confirms for 1.2s", async () => {
    const writeText = vi.fn<(text: string) => Promise<void>>();
    writeText.mockResolvedValue(undefined);
    stubClipboard(writeText);

    const mounted = await mount(settledTurn("**hello** world"));

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-copy-message"));
    });

    expect(writeText).toHaveBeenCalledTimes(1);

    const button = screen.getByTestId("btn-copy-message");

    expect(button.getAttribute("data-copied")).toBe("true");
    expect(getToasts()).toHaveLength(0);
    expect(clipboardSetText).not.toHaveBeenCalled();

    await mounted.unmount();
    h = null;
  });

  it("announces a refused copy with the danger toast", async () => {
    stubClipboard(
      vi.fn().mockRejectedValue(new DOMException("denied", "NotAllowedError"))
    );

    const mounted = await mount(settledTurn("hello"));

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-copy-message"));
    });

    const toasts = getToasts();

    expect(toasts).toHaveLength(1);
    expect(toasts[0]?.kind).toBe("danger");
    expect(toasts[0]?.text).toContain("Copy failed");
    expect(
      screen.getByTestId("btn-copy-message").getAttribute("data-copied")
    ).toBe("false");

    await mounted.unmount();
    h = null;
  });

  it("hides the copy and regenerate actions while the message streams", async () => {
    await mount(
      fold(
        ev(Kind.KindUserMessage, { text: "ask", turn_id: "t1" }),
        ev(Kind.KindTextDelta, { text: "partial ", turn_id: "t1" })
      )
    );

    expect(screen.queryByTestId("btn-copy-message")).toBeNull();
    expect(screen.queryByTestId("btn-regenerate")).toBeNull();
  });

  it("wires regenerate to the store's regenerate action", async () => {
    const regenerate = vi.fn<(turnID: string) => Promise<void>>();
    regenerate.mockResolvedValue(undefined);

    useChatStore.setState({ regenerate });

    const mounted = await mount(settledTurn("an answer"));

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-regenerate"));
    });

    expect(regenerate).toHaveBeenCalledWith("t1");

    await mounted.unmount();
    h = null;
  });
});

describe("variant picker (task 3.2, design D3)", () => {
  const HISTORY = [
    ev(Kind.KindUserMessage, { text: "explain", turn_id: "t1", id: "e1" }),
    ev(Kind.KindAssistantMessage, {
      text: "Original answer",
      turn_id: "t1",
      id: "e2",
    }),
    ev(Kind.KindTurnCompleted, { turn_id: "t1", id: "e3" }),
  ];

  const REGEN = [
    ev(Kind.KindUserMessage, { turn_id: "t2", variant_of: "t1", id: "e4" }),
    ev(Kind.KindAssistantMessage, {
      text: "Second answer",
      turn_id: "t2",
      variant_of: "t1",
      id: "e5",
    }),
    ev(Kind.KindTurnCompleted, { turn_id: "t2", variant_of: "t1", id: "e6" }),
  ];

  it("renders n / total and switches variants offline", async () => {
    const regenerate = vi.fn<(turnID: string) => Promise<void>>();
    regenerate.mockResolvedValue(undefined);

    useChatStore.setState({ regenerate });

    const mounted = await mount(fold(...HISTORY, ...REGEN));

    expect(screen.getByTestId("variant-count").textContent).toBe("2 / 2");
    expect(
      document.querySelector("[data-testid='transcript']")?.textContent
    ).toContain("Second answer");

    await act(async () => {
      fireEvent.click(screen.getByTestId("variant-prev"));
    });

    // Pure local state: the displayed content switched with no request.
    expect(regenerate).not.toHaveBeenCalled();
    expect(screen.getByTestId("variant-count").textContent).toBe("1 / 2");
    expect(
      document.querySelector("[data-testid='transcript']")?.textContent
    ).toContain("Original answer");

    await act(async () => {
      fireEvent.click(screen.getByTestId("variant-next"));
    });
    expect(screen.getByTestId("variant-count").textContent).toBe("2 / 2");

    await mounted.unmount();
    h = null;
  });

  it("shows no picker while the turn has a single answer", async () => {
    await mount(settledTurn("only answer"));

    expect(screen.queryByTestId("variant-picker")).toBeNull();
  });
});

describe("turn activity (task 3.2)", () => {
  it("shows the typing indicator before the first token arrives", async () => {
    useChatStore.setState({ busy: true });

    const mounted = await mount(
      fold(ev(Kind.KindUserMessage, { text: "hello", turn_id: "t1" }))
    );

    expect(
      document.querySelector("[aria-label='Assistant is typing']")
    ).not.toBeNull();

    await mounted.unmount();
    h = null;
  });

  it("names the running tool with a live elapsed time at the tail", async () => {
    useChatStore.setState({ busy: true });

    const mounted = await mount(
      fold(
        ev(Kind.KindUserMessage, { text: "go", turn_id: "t1" }),
        ev(Kind.KindToolActivity, {
          turn_id: "t1",
          occurred_at: "2026-01-01T00:00:00Z",
          tool: {
            call_id: "c1",
            name: "web_search",
            arguments: '{"query":"weather"}',
            status: ToolStatus.ToolStatusRunning,
          },
        })
      )
    );

    const line = document.querySelector("[data-testid='turn-status-line']");

    expect(line?.textContent).toContain("web_search");
    expect(line?.getAttribute("role")).toBe("status");
    expect(
      line?.querySelector("[data-testid='turn-status-elapsed']")?.textContent
    ).toMatch(/^\d+:\d{2}$/);

    // The line disappears when the turn reaches a terminal outcome.
    useChatStore.setState({ busy: false });

    await mounted.setEntries(
      fold(
        ev(Kind.KindUserMessage, { text: "go", turn_id: "t1" }),
        ev(Kind.KindToolActivity, {
          turn_id: "t1",
          tool: {
            call_id: "c1",
            name: "web_search",
            arguments: '{"query":"weather"}',
            status: ToolStatus.ToolStatusCompleted,
            result: "Sunny",
          },
        }),
        ev(Kind.KindTurnCompleted, { turn_id: "t1" })
      )
    );

    expect(
      document.querySelector("[data-testid='turn-status-line']")
    ).toBeNull();

    await mounted.unmount();
    h = null;
  });
});

describe("markdown links (chat-panels 'Preview sources')", () => {
  it("opens a workspace preview tab for a mount-scoped link", async () => {
    useChatStore.setState({ activeAgentID: "a1" });

    const mounted = await mount(
      settledTurn("See [notes.md](/workspace/notes.md) for details.")
    );

    await act(async () => {
      // SAFETY: the mount-scoped link renders the md-link button whenever
      // the completed message mounts; the click target always exists.
      fireEvent.click(
        document.querySelector(
          "[data-testid='md-link-/workspace/notes.md']"
        ) as Element
      );
    });

    const tabs = useChatStore.getState().panelTabs;

    expect(tabs).toHaveLength(1);
    expect(tabs[0]).toMatchObject({
      kind: "workspace-file",
      title: "notes.md",
    });

    await mounted.unmount();
    h = null;
  });
});

describe("markdown links and the reasoning disclosure (ported scenarios)", () => {
  it("leaves external URLs as plain links and opens no preview tab", async () => {
    // The panel tabs are store state the earlier suites left behind; the
    // assertion is about what THIS render opens.
    useChatStore.setState({ panelTabs: [] });

    await mount(
      settledTurn("see [the docs](https://example.com/docs) for more")
    );

    const link = document.querySelector(
      '[data-testid="entry-assistant"] a[href="https://example.com/docs"]'
    );

    expect(link).toBeTruthy();
    expect(link?.getAttribute("target")).toBe("_blank");
    // A foreign scheme is a navigation target, not a workspace path: no
    // preview tab is opened.
    expect(useChatStore.getState().panelTabs).toHaveLength(0);
  });

  it("keeps the reasoning collapsed until its disclosure is toggled", async () => {
    await mount(
      fold(
        ev(Kind.KindUserMessage, { text: "think", turn_id: "t1" }),
        ev(Kind.KindReasoning, {
          text: "weighing the options",
          turn_id: "t1",
        }),
        ev(Kind.KindAssistantMessage, {
          text: "the answer",
          turn_id: "t1",
        })
      )
    );

    const trigger = document.querySelector('[data-slot="reasoning-trigger"]');

    expect(trigger).toBeTruthy();

    // Collapsed: the reasoning text is not on the page yet.
    expect(document.body.textContent).not.toContain("weighing the options");

    await act(async () => {
      // SAFETY: the trigger is the reasoning disclosure button the element
      // rendered; the assertion only narrows it to a clickable element.
      (trigger as HTMLElement).click();
    });

    await waitFor(() =>
      expect(document.body.textContent).toContain("weighing the options")
    );
  });
});

describe("the code block's wrap toggle (spec chat-view 'Code block presentation')", () => {
  it("the header offers the wrap toggle and it flips the rendered code", async () => {
    await mount(settledTurn("before\n\n```go\nx := 1\n```"));

    const header = document.querySelector(".aui-code-header-root");

    expect(header).toBeTruthy();
    expect(header?.textContent).toContain("go");

    const toggle = screen.getByTestId("code-wrap-toggle");

    expect(toggle.getAttribute("aria-pressed")).toBe("false");
    expect(toggle.textContent).toBe("Wrap");

    // The highlighter wrapper carries the state the stylesheet reads.
    const wrapper = document.querySelector("[data-code-wrap]");

    expect(wrapper?.getAttribute("data-code-wrap")).toBe("false");

    await act(async () => {
      fireEvent.click(toggle);
    });

    expect(
      screen.getByTestId("code-wrap-toggle").getAttribute("aria-pressed")
    ).toBe("true");
    expect(screen.getByTestId("code-wrap-toggle").textContent).toBe("No wrap");
    expect(
      document.querySelector("[data-code-wrap]")?.getAttribute("data-code-wrap")
    ).toBe("true");
  });
});

describe("the error card (refine-chat-surface-identity tasks 3.1–3.3, design D3)", () => {
  const RAW = "dial tcp 10.0.0.1:443: connect: connection refused";

  it("renders the classified headline, the hint, and Retry; the details stay collapsed", async () => {
    await mount(failedTurn(RAW));

    const card = screen.getByTestId("entry-error");

    expect(card.getAttribute("role")).toBe("alert");
    expect(card.textContent).toContain(
      "The agent couldn't reach the model provider."
    );
    expect(card.textContent).toContain(
      "Check the network connection and the provider's status"
    );
    expect(screen.getByTestId("btn-retry-turn")).not.toBeNull();

    // Collapsed by default: the raw string is on no page until the reader
    // opens the disclosure.
    expect(screen.queryByTestId("entry-error-details")).toBeNull();
    expect(document.body.textContent).not.toContain(RAW);
  });

  it("keeps the raw string out of the headline and verbatim in the disclosure", async () => {
    await mount(failedTurn(RAW));

    const card = screen.getByTestId("entry-error");

    // The headline never carries the raw string (the card, collapsed, has
    // no trace of it — the classifier's voice only).
    expect(card.textContent).not.toContain(RAW);

    await act(async () => {
      fireEvent.click(screen.getByTestId("entry-error-details-toggle"));
    });

    expect(screen.getByTestId("entry-error-details").textContent).toBe(RAW);
  });

  it("wires Retry to the store's regenerate action", async () => {
    const regenerate = vi.fn<(turnID: string) => Promise<void>>();
    regenerate.mockResolvedValue(undefined);

    useChatStore.setState({ regenerate });

    const mounted = await mount(failedTurn(RAW));

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-retry-turn"));
    });

    expect(regenerate).toHaveBeenCalledWith("t1");

    await mounted.unmount();
    h = null;
  });

  it("stays honest on an unrecognized failure: the generic headline, no invented cause", async () => {
    await mount(failedTurn("eino: upstream exploded mid-stream"));

    const card = screen.getByTestId("entry-error");

    expect(card.textContent).toContain("This turn failed.");
    expect(card.textContent).not.toContain("provider");
    expect(card.textContent).not.toContain("credentials");
    expect(card.textContent).not.toContain("context window");

    // The raw string still survives in the disclosure.
    await act(async () => {
      fireEvent.click(screen.getByTestId("entry-error-details-toggle"));
    });

    expect(screen.getByTestId("entry-error-details").textContent).toBe(
      "eino: upstream exploded mid-stream"
    );
  });

  it("stands Retry down while the turn is running and restores it when the turn settles", async () => {
    useChatStore.setState({ busy: true });

    const mounted = await mount(failedTurn(RAW));

    const card = screen.getByTestId("entry-error");

    expect(card.getAttribute("role")).toBe("alert");
    expect(screen.queryByTestId("btn-retry-turn")).toBeNull();
    // The details disclosure stays available while the stand-down holds.
    expect(screen.getByTestId("entry-error-details-toggle")).not.toBeNull();

    await mounted.setStore({ busy: false });

    expect(screen.getByTestId("btn-retry-turn")).not.toBeNull();

    await mounted.unmount();
    h = null;
  });

  it("classifies a credentials failure and keeps the raw string out of the headline", async () => {
    const RAW_CREDENTIALS =
      "403 forbidden: the api key is unauthorized for this model";

    await mount(failedTurn(RAW_CREDENTIALS));

    const card = screen.getByTestId("entry-error");

    expect(card.getAttribute("role")).toBe("alert");
    expect(card.textContent).toContain(
      "The model provider rejected the credentials."
    );
    expect(card.textContent).toContain(
      "Check the provider API key in Settings, then retry."
    );

    // Collapsed by default, and the classified voice only: the raw string
    // is on no page until the reader opens the disclosure.
    expect(screen.queryByTestId("entry-error-details")).toBeNull();
    expect(document.body.textContent).not.toContain(RAW_CREDENTIALS);
    expect(card.textContent).not.toContain(RAW_CREDENTIALS);

    await act(async () => {
      fireEvent.click(screen.getByTestId("entry-error-details-toggle"));
    });

    expect(screen.getByTestId("entry-error-details").textContent).toBe(
      RAW_CREDENTIALS
    );
  });

  it("classifies a context-window failure and keeps the raw string out of the headline", async () => {
    const RAW_CONTEXT =
      "error: prompt exceeds the model's token limit (context length 131072 tokens)";

    await mount(failedTurn(RAW_CONTEXT));

    const card = screen.getByTestId("entry-error");

    expect(card.getAttribute("role")).toBe("alert");
    expect(card.textContent).toContain(
      "The conversation doesn't fit the model's context window."
    );
    expect(card.textContent).toContain("Start a new session, then retry.");

    // Collapsed by default, and the classified voice only.
    expect(screen.queryByTestId("entry-error-details")).toBeNull();
    expect(document.body.textContent).not.toContain(RAW_CONTEXT);
    expect(card.textContent).not.toContain(RAW_CONTEXT);

    await act(async () => {
      fireEvent.click(screen.getByTestId("entry-error-details-toggle"));
    });

    expect(screen.getByTestId("entry-error-details").textContent).toBe(
      RAW_CONTEXT
    );
  });
});
