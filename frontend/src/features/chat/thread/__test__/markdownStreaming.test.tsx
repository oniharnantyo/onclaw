/**
 * Streaming markdown on the thread layer (tasks 3.2, spec chat-view
 * "Streaming-safe markdown" and "GFM rendering" — the old-layer scenarios
 * from features/chat/__test__/markdownStreaming.test.tsx ported to the new
 * assistant-ui composition): an unclosed fence mid-stream renders as one
 * stable streaming block with no runaway, unclosed inline code and emphasis
 * markers are closed by the stabilizer so the remainder paints stable, a
 * pipe table renders as a real table WHILE STREAMING (remark-gfm rides the
 * streaming path, matching the completed MarkdownText), and the final
 * render of a streamed message equals a one-pass render of the same
 * complete text — both sides rendered by the vendored MarkdownText.
 *
 * The streaming assertions are DOM-level: the streaming path is plain
 * react-markdown behind `stabilizeMarkdown`, so the constructs read straight
 * off the document. The completed path's highlighter is stubbed at the
 * module boundary (the vendored shiki element) so the render-equivalence
 * comparison is deterministic — the real engine is covered by
 * shikiHighlight.test.ts and the mermaid package's own suite.
 */
import React from "react";
import { act } from "react";
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vite-plus/test";

import { Kind } from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import { useChatStore } from "../../store";

// --- module-boundary mock (the completed path's highlighter) ---------------

const { ShikiStub } = vi.hoisted(() => ({
  ShikiStub: vi.fn(
    (props: { code?: string; language?: string }): React.ReactElement =>
      // SAFETY: the stub renders the two fields the render-equivalence test
      // pins; the vendored element passes exactly this shape.
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

import { ev, fold, renderThread, type ThreadHarness } from "./harness";

/** A turn mid-flight with the given partial answer text. */
function streamingTurnWith(text: string): ReturnType<typeof fold> {
  return fold(
    ev(Kind.KindUserMessage, { text: "ask", turn_id: "t1" }),
    ev(Kind.KindTextDelta, { text, turn_id: "t1" })
  );
}

/** A completed turn with the given answer text. */
function settledTurn(text: string): ReturnType<typeof fold> {
  return fold(
    ev(Kind.KindUserMessage, { text: "ask", turn_id: "t1" }),
    ev(Kind.KindAssistantMessage, { text, turn_id: "t1" }),
    ev(Kind.KindTurnCompleted, { turn_id: "t1" })
  );
}

const streamedHTML = (): string | undefined =>
  document.querySelector("[data-testid='entry-assistant']")?.innerHTML;

let h: ThreadHarness | null = null;

async function mount(entries: ReturnType<typeof fold>): Promise<ThreadHarness> {
  h = await renderThread(entries);

  return h;
}

beforeEach(() => {
  vi.clearAllMocks();
  useChatStore.setState({ activeSessionID: "s1", busy: false, entries: [] });
});

afterEach(async () => {
  if (h !== null) {
    await act(async () => {
      await h?.unmount();
    });
    h = null;
  }
});

describe("streaming-safe markdown (ported to the thread layer)", () => {
  it("renders an unclosed fence mid-stream as one stable block, no runaway", async () => {
    await mount(streamingTurnWith("before\n\n```go\nx := 1"));

    // The prose before the fence renders as prose — once.
    const ps = document.querySelectorAll("[data-testid='entry-assistant'] p");

    expect(ps).toHaveLength(1);
    expect(ps[0]?.textContent).toBe("before");

    // Exactly one streaming code block carries the fenced text so far, and
    // the prose above it never leaks into the fence.
    const pres = document.querySelectorAll("[data-streaming='true'] pre");

    expect(pres).toHaveLength(1);
    expect(pres[0]?.getAttribute("data-streaming")).toBe("true");
    expect(pres[0]?.textContent).toContain("x := 1");
    expect(pres[0]?.textContent).not.toContain("before");
  });

  it("closes unclosed inline constructs so the remainder paints stable", async () => {
    // The stabilizer closed the span: it renders as inline code, not as a
    // literal backtick left to leak into the next streamed chunk.
    const first = await mount(streamingTurnWith("use `fmt to print"));

    expect(
      document.querySelector("[data-testid='entry-assistant'] p code")
        ?.textContent
    ).toBe("fmt to print");

    await first.unmount();
    h = null;

    // Same for the dangling bold marker.
    const second = await mount(streamingTurnWith("this is **bold"));

    const strong = document.querySelector(
      "[data-testid='entry-assistant'] strong"
    );

    expect(strong?.textContent).toBe("bold");
    expect(strong?.parentElement?.textContent).toBe("this is bold");

    await second.unmount();
    h = null;
  });

  it("renders a pipe table as a real table while streaming (remark-gfm)", async () => {
    await mount(
      streamingTurnWith("| a | b |\n| --- | --- |\n| 1 | 2 |\n| 3 | 4 |")
    );

    // The streaming path parses GFM identically to the completed path: the
    // table is structure, not a paragraph of pipes.
    const table = document.querySelector(
      "[data-testid='entry-assistant'] [data-streaming='true'] table"
    );

    expect(table).not.toBeNull();
    expect(table?.querySelectorAll("th")).toHaveLength(2);
    expect(table?.querySelectorAll("td")).toHaveLength(4);
    expect(table?.querySelector("td")?.textContent).toBe("1");
  });

  it("the final render of a streamed message equals a one-pass render", async () => {
    const source = "before\n\n```go\nx := 1\n```\n\ndone *here*";

    // Stream the message in two frames, then complete the turn — the last
    // frame sits on the completed path (the vendored MarkdownText).
    const streamed = await mount(streamingTurnWith("before"));

    await streamed.setEntries(streamingTurnWith(source));
    await streamed.setEntries(settledTurn(source));
    // The completed path loads lazily (the highlighter chunk stays out of the
    // first paint), so both renders wait for the chunk to land before their
    // HTML is compared.
    await streamed.settle();

    const afterStream = streamedHTML();

    await streamed.unmount();
    h = null;

    // One-pass render of the same complete text, from the start.
    const onePass = await mount(settledTurn(source));
    await onePass.settle();

    const staticHTML = streamedHTML();

    expect(staticHTML).toBe(afterStream);

    // The equality is not vacuous: the completed render carries the
    // highlighted fence and the emphasis the source holds.
    const em = document.querySelector("[data-testid='entry-assistant'] em");

    expect(
      document.querySelector("[data-testid='md-code-highlighted']")
    ).not.toBeNull();
    expect(em?.textContent).toBe("here");

    await onePass.unmount();
    h = null;
  });
});
