/**
 * Conversation map tests (task 3.4, spec chat-view "Conversation minimap"):
 * one tick per turn, the tick of the turn being read marked, on-screen ticks
 * deepened, click scrolls the transcript to the turn, hover previews the
 * turn's name, and the rail hides below two rendered turns. Message geometry
 * is stubbed at getBoundingClientRect (jsdom has no layout) exactly like the
 * Minimap suite of the layer this replaces; the tracking measure lands on a
 * requestAnimationFrame frame.
 */
import { act } from "react";
import { fireEvent, screen } from "@testing-library/dom";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";

import { Kind } from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import { ev, fold, renderThread } from "./harness";

/** Each message row a 100px box starting every 300px, shifted up by the
 * closure scroll position; non-message elements read the container rect. */
let scrollPos = 0;

const rectSpy = vi
  .spyOn(Element.prototype, "getBoundingClientRect")
  .mockImplementation(function (this: Element): DOMRect {
    // SAFETY: jsdom has no layout engine; this fabricates the DOMRect shape
    // carrying exactly the fields (top/height) the rail's measure reads.
    const rect = (top: number, height: number): DOMRect =>
      ({
        top,
        height,
        bottom: top + height,
        left: 0,
        right: 0,
        width: 0,
        x: 0,
        y: top,
        toJSON: (): Record<string, number> => ({}),
      }) as DOMRect;

    if (this.getAttribute("data-message-id") === null) return rect(0, 600);

    const messages = document.querySelectorAll("[data-message-id]");
    const index = Array.prototype.indexOf.call(messages, this);

    return rect(index * 300 - scrollPos, 100);
  });

function twoTurns(): ReturnType<typeof fold> {
  return fold(
    ev(Kind.KindUserMessage, { text: "first prompt words", turn_id: "t1" }),
    ev(Kind.KindAssistantMessage, {
      text: "first answer",
      turn_id: "t1",
    }),
    ev(Kind.KindTurnCompleted, { turn_id: "t1" }),
    ev(Kind.KindUserMessage, { text: "second prompt words", turn_id: "t2" }),
    ev(Kind.KindAssistantMessage, {
      text: "second answer",
      turn_id: "t2",
    }),
    ev(Kind.KindTurnCompleted, { turn_id: "t2" })
  );
}

function ticks(): HTMLElement[] {
  return Array.from(
    document.querySelectorAll<HTMLElement>(
      '[data-slot="conversation-map-tick"]'
    )
  );
}

afterEach(() => {
  rectSpy.mockClear();
  scrollPos = 0;
});

describe("ConversationMapRail (task 3.4)", () => {
  it("hides below two rendered turns", async () => {
    const h = await renderThread(
      fold(
        ev(Kind.KindUserMessage, { text: "only", turn_id: "t1" }),
        ev(Kind.KindAssistantMessage, { text: "answer", turn_id: "t1" }),
        ev(Kind.KindTurnCompleted, { turn_id: "t1" })
      )
    );

    await h.settle();

    expect(screen.queryByTestId("chat-minimap")).toBeNull();

    await h.unmount();
  });

  it("renders one tick per turn from two turns on", async () => {
    const h = await renderThread(twoTurns());
    await h.settle();

    expect(screen.getByTestId("chat-minimap")).toBeTruthy();
    expect(ticks()).toHaveLength(2);

    // Wide widths only: the lg grammar is the mechanism the stylesheet keys
    // on (the narrow-width hide); `contents` keeps the composition's own
    // sticky overlay reading the viewport as its containing block.
    expect(screen.getByTestId("chat-minimap").className).toContain("hidden");
    expect(screen.getByTestId("chat-minimap").className).toContain(
      "lg:contents"
    );

    await h.unmount();
  });

  it("scrolls to the tick's turn on click", async () => {
    const h = await renderThread(twoTurns());
    await h.settle();

    h.viewport.scrollTop = 500;
    scrollPos = 500;

    await act(async () => {
      fireEvent.click(ticks()[1]);
    });

    // The scroll lands the turn-2 head (row index 2 → top 600 - 500 at the
    // click) at the viewport's top: 100 + 500.
    expect(h.scrollToCalls.at(-1)).toEqual({
      top: 600,
      behavior: "smooth",
    });

    await h.unmount();
  });

  it("marks the tick being read and deepens on-screen ticks while scrolling", async () => {
    const h = await renderThread(twoTurns());
    await h.settle();

    // At the top, the first turn's tick is the marked one.
    let marked = ticks().map((t: HTMLElement): boolean =>
      t.hasAttribute("data-active")
    );

    expect(marked).toEqual([true, false]);

    // Scroll so the second turn fills the viewport; the measure lands on a
    // frame — rows sit at index*300, so scrollTop 700 puts turn two's rows
    // across the reading line and on screen.
    h.viewport.scrollTop = 700;
    scrollPos = 700;

    await act(async () => {
      fireEvent.scroll(h.viewport);
    });
    await h.settle();

    marked = ticks().map((t: HTMLElement): boolean =>
      t.hasAttribute("data-active")
    );

    expect(marked).toEqual([false, true]);
    expect(ticks()[1]?.hasAttribute("data-in-view")).toBe(true);
    expect(ticks()[0]?.hasAttribute("data-in-view")).toBe(false);

    await h.unmount();
  });

  it("previews the turn's name on hover", async () => {
    const h = await renderThread(twoTurns());
    await h.settle();

    await act(async () => {
      fireEvent.mouseEnter(ticks()[0]);
    });

    // The preview card opens after its 120ms intent delay.
    await vi.waitFor(
      (): void => {
        expect(document.body.textContent).toContain("first prompt words");
      },
      { timeout: 2000 }
    );

    await h.unmount();
  });
});

describe("the map's responsive grammar (port of the legacy Minimap suite)", () => {
  it("hides at narrow widths through the lg: grammar", async () => {
    const h = await renderThread(twoTurns());

    const rail = screen.getByTestId("chat-minimap");

    // Hidden by default, shown from the lg breakpoint up — the same grammar
    // the legacy minimap used.
    expect(rail.getAttribute("class")).toContain("hidden");
    expect(rail.getAttribute("class")).toContain("lg:contents");

    await h.unmount();
  });
});
