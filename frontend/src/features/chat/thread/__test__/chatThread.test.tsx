/**
 * ChatThread tests (task 3.1, spec chat-view "Chat transcript rendering" and
 * "Transcript windowing"): the viewport follows a streaming reply while the
 * reader is at the bottom, a reader's own upward scroll pauses following and
 * reveals the jump pill, the pill jumps back and re-arms, the reader's own
 * send returns to the bottom, a disclosure toggle inside a message holds the
 * viewport until the reader returns to the bottom, and a prepended history
 * page keeps the reader's position (the viewport's prepend anchor — no
 * hand-rolled height-delta effect needed). jsdom has no layout: scrollHeight
 * is pinned per step and message geometry is stubbed at
 * getBoundingClientRect, exactly like the useFollowBottom / Minimap suites
 * do for the layer this replaces.
 */
import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/dom";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";

import {
  ArtifactChange,
  Kind,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type { Agent } from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { applyEvent } from "../../entries";
import { useChatStore } from "../../store";
import {
  auiOf,
  ev,
  fold,
  renderThread,
  streamingTurn,
  toolEvent,
  type ThreadHarness,
} from "./harness";

/** Message-row geometry for the prepend-anchor step: each message row a
 * 100px box starting every 300px, shifted up by the closure scroll position.
 * Non-message elements read the container rect. Everything carries
 * [data-message-id] in document order (MessagePrimitive.Root's seam). */
let scrollPos = 0;

const rectSpy = vi
  .spyOn(Element.prototype, "getBoundingClientRect")
  .mockImplementation(function (this: Element): DOMRect {
    // SAFETY: jsdom has no layout engine; this fabricates the DOMRect shape
    // carrying exactly the fields (top/height) the controller reads.
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

function messageIDs(): string[] {
  const ids: string[] = [];

  for (const el of document.querySelectorAll("[data-message-id]")) {
    const id = el.getAttribute("data-message-id");

    if (id !== null) ids.push(id);
  }

  return ids;
}

/** The session agent the attribution suite selects (the SessionRail
 * fixture's shape). */
const agent: Agent = {
  ID: "a1",
  Name: "Scout",
  ProviderID: "prov",
  Model: "gpt-test",
  Effort: "",
  Slug: "scout",
  Description: "",
  Brief: "",
  Avatar: null,
  SubagentsEnabled: true,
  PromptsStatus: "ready",
  PromptsError: "",
  ContextWindow: 0,
  Temperature: 0.7,
  MaxTokens: 0,
  Tools: null,
  Skills: null,
  MCPServers: null,
  CreatedAt: "2026-01-01T00:00:00Z",
  UpdatedAt: "2026-01-01T00:00:00Z",
};

async function bottomed(harness: ThreadHarness, height: number): Promise<void> {
  harness.pinLayout(height);
  harness.viewport.scrollTop = height;
  await harness.settle();
  // The controller's initialize scroll must land before the reader moves: it
  // is the position the controller compares the reader's scroll against, and
  // it lands on an animation frame (late under load).
  await harness.waitForScrollCall();
}

afterEach(() => {
  rectSpy.mockClear();
  scrollPos = 0;
  useChatStore.setState({
    busy: false,
    activeSessionID: "s1",
    entries: [],
    agents: [],
    activeAgentID: null,
  });
});

describe("ChatThread (task 3.1)", () => {
  it("follows a streaming reply while the reader is at the bottom", async () => {
    useChatStore.setState({ busy: true, activeSessionID: "s1" });
    const entries = streamingTurn();
    const h = await renderThread(entries);

    await bottomed(h, 1000);

    // The next delta grows the tail; the pinned viewport tracks it.
    const grown = applyEvent(
      entries,
      ev(Kind.KindTextDelta, {
        text: "and much more streamed text arrives ",
        turn_id: "t1",
      })
    );

    h.pinLayout(2400);
    await h.setEntries(grown);
    await h.settle();
    expect(h.viewport.scrollTop).toBe(2400);

    await h.unmount();
  });

  it("a scrolled-up reader stays put and the jump control appears", async () => {
    useChatStore.setState({ busy: true, activeSessionID: "s1" });
    const entries = streamingTurn();
    const h = await renderThread(entries);

    await bottomed(h, 1000);

    await act(async () => {
      h.viewport.scrollTop = 400;
      fireEvent.scroll(h.viewport);
    });
    // The controller's scroll bookkeeping lands on an animation frame: settle
    // it, then wait for the pill.
    await h.settle();
    await waitFor(() =>
      expect(screen.getByTestId("btn-jump-latest")).toBeTruthy()
    );

    const grown = applyEvent(
      entries,
      ev(Kind.KindTextDelta, {
        text: "growth the reader is not watching ",
        turn_id: "t1",
      })
    );

    h.pinLayout(2400);
    await h.setEntries(grown);
    await h.settle();
    // The viewport stays where the reader left it; the pill stays visible.
    expect(h.viewport.scrollTop).toBe(400);
    // The controller's scroll bookkeeping lands on an animation frame: settle
    // it, then wait for the pill.
    await h.settle();
    await waitFor(() =>
      expect(screen.getByTestId("btn-jump-latest")).toBeTruthy()
    );

    await h.unmount();
  });

  it("jump to latest scrolls back, hides the pill, and re-arms following", async () => {
    useChatStore.setState({ busy: true, activeSessionID: "s1" });
    const entries = streamingTurn();
    const h = await renderThread(entries);

    await bottomed(h, 1000);

    await act(async () => {
      h.viewport.scrollTop = 400;
      fireEvent.scroll(h.viewport);
    });

    h.pinLayout(2400);
    await h.setEntries(
      applyEvent(
        entries,
        ev(Kind.KindTextDelta, {
          text: "growth while scrolled up ",
          turn_id: "t1",
        })
      )
    );
    await h.settle();

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-jump-latest"));
    });
    await h.settle();

    // The reader lands at the bottom and the control hides.
    expect(h.viewport.scrollTop).toBe(2400);
    await waitFor(() =>
      expect(screen.queryByTestId("btn-jump-latest")).toBeNull()
    );

    // Following resumed: further growth is tracked again.
    h.pinLayout(3000);
    await h.setEntries(
      applyEvent(
        fold(
          ev(Kind.KindUserMessage, { text: "hello", turn_id: "t1" }),
          ev(Kind.KindTextDelta, {
            text: "streamed answer growth while scrolled up more ",
            turn_id: "t1",
          })
        ),
        ev(Kind.KindTextDelta, { text: "after the jump ", turn_id: "t1" })
      )
    );
    await h.settle();
    expect(h.viewport.scrollTop).toBe(3000);

    await h.unmount();
  });

  it("the reader's own send returns the transcript to the bottom", async () => {
    useChatStore.setState({ activeSessionID: "s1" });

    const entries = fold(
      ev(Kind.KindUserMessage, { text: "hello", turn_id: "t1" }),
      ev(Kind.KindTextDelta, { text: "a settled answer ", turn_id: "t1" }),
      ev(Kind.KindTurnCompleted, { turn_id: "t1" })
    );

    const h = await renderThread(entries);

    // The controller's initialize scroll must land FIRST, and with the
    // pinned layout: a reader scroll while its scroll intent is still
    // pending is swallowed as the controller's own scroll, and the jump pill
    // then never appears.
    await bottomed(h, 1000);
    await act(async () => {
      h.viewport.scrollTop = 400;
      fireEvent.scroll(h.viewport);
    });
    // The controller's scroll bookkeeping lands on an animation frame: settle
    // it, then wait for the pill.
    await h.settle();
    await waitFor(() =>
      expect(screen.getByTestId("btn-jump-latest")).toBeTruthy()
    );

    // The reader sends: the run starts (isRunning flips), which returns the
    // viewport to the bottom so the reply starts in view.
    await act(async () => {
      h.pinLayout(1200);
      await auiOf().thread.append("second prompt");
    });
    // The auto-scroll controller lands on an animation frame: wait for the
    // scroll instead of racing it with a fixed sleep.
    await h.waitForScroll(1200);

    expect(h.viewport.scrollTop).toBe(1200);
    await waitFor(() =>
      expect(screen.queryByTestId("btn-jump-latest")).toBeNull()
    );

    await h.unmount();
  });

  it("a disclosure toggle holds the viewport until the reader returns to the bottom", async () => {
    useChatStore.setState({ busy: true, activeSessionID: "s1" });

    const run = fold(
      ev(Kind.KindUserMessage, { text: "go", turn_id: "t1" }),
      toolEvent("c1", "a.ts"),
      toolEvent("c2", "b.ts"),
      toolEvent("c3", "c.ts"),
      toolEvent("c4", "d.ts"),
      ev(Kind.KindTextDelta, { text: "answering ", turn_id: "t1" })
    );

    const h = await renderThread(run);
    await bottomed(h, 1000);

    // A streaming delta pins to the new bottom…
    const grown = applyEvent(
      run,
      ev(Kind.KindTextDelta, { text: "more ", turn_id: "t1" })
    );

    h.pinLayout(2000);
    await h.setEntries(grown);
    await h.settle();
    expect(h.viewport.scrollTop).toBe(2000);

    // …the reader expands the folded tool run, and the expansion (plus any
    // delta landing in the same stretch) must not yank the viewport.
    await act(async () => {
      // SAFETY: the run summary renders its expand button with the folded
      // tool run; the click target always exists on this mount.
      fireEvent.click(
        document.querySelector('[data-testid="run-summary"] button') as Element
      );
    });
    expect(
      document
        .querySelector('[data-testid="run-summary"] button')
        ?.getAttribute("aria-expanded")
    ).toBe("true");

    h.pinLayout(2600);
    await h.setEntries(
      applyEvent(grown, ev(Kind.KindTextDelta, { text: "mid ", turn_id: "t1" }))
    );
    await h.settle();
    expect(h.viewport.scrollTop).toBe(2000);

    // The reader scrolls back to the bottom; following re-arms.
    await act(async () => {
      h.viewport.scrollTop = 2600;
      fireEvent.wheel(h.viewport, { deltaY: 120 });
      fireEvent.scroll(h.viewport);
    });

    h.pinLayout(3200);
    await h.setEntries(
      applyEvent(
        grown,
        ev(Kind.KindTextDelta, { text: "even more ", turn_id: "t1" })
      )
    );
    await h.settle();
    expect(h.viewport.scrollTop).toBe(3200);

    await h.unmount();
  });

  it("loading an earlier page keeps the reader's position (prepend anchor)", async () => {
    useChatStore.setState({ busy: true, activeSessionID: "s1" });
    const entries = streamingTurn();
    const h = await renderThread(entries);

    await bottomed(h, 1000);

    // The reader withdraws consent and settles scrolled up; the controller
    // measures the first message as its prepend anchor.
    await act(async () => {
      fireEvent.wheel(h.viewport, { deltaY: -120 });
    });
    h.viewport.scrollTop = 400;
    scrollPos = 400;
    await act(async () => {
      fireEvent.scroll(h.viewport);
    });
    await h.setEntries(entries);
    await h.settle();

    // "Load earlier" prepends an older turn before the anchored one.
    const older = fold(
      ev(Kind.KindUserMessage, { text: "old prompt", turn_id: "t0" })
    );

    expect(messageIDs().length).toBeGreaterThanOrEqual(2);

    h.pinLayout(1250);
    await h.setEntries([...older, ...entries]);
    await h.settle();

    // The anchored message stays under the cursor: scrollTop moved by the
    // grown height (400 + 300), not to the bottom.
    expect(h.viewport.scrollTop).toBe(700);
    // The controller's scroll bookkeeping lands on an animation frame: settle
    // it, then wait for the pill.
    await h.settle();
    await waitFor(() =>
      expect(screen.getByTestId("btn-jump-latest")).toBeTruthy()
    );

    await h.unmount();
  });

  it("renders the compaction divider between turns", async () => {
    const entries = fold(
      ev(Kind.KindUserMessage, { text: "before", turn_id: "t1" }),
      {
        kind: Kind.KindCompaction,
        occurred_at: "2026-01-01T00:01:00Z",
        compaction: {
          checkpoint_id: "cp1",
          tokens_before: 154000,
          tokens_after: 9200,
        },
      },
      ev(Kind.KindUserMessage, { text: "after", turn_id: "t2" })
    );

    const h = await renderThread(entries);

    const el = screen.getByTestId("entry-compaction");

    expect(el.textContent).toContain("Context compacted");
    expect(el.textContent).toContain("154k → 9.2k tokens");

    await h.unmount();
  });

  it("renders the inline error entry for a failed turn", async () => {
    const entries = fold(
      ev(Kind.KindUserMessage, { text: "go", turn_id: "t1" }),
      ev(Kind.KindTurnError, {
        turn_id: "t1",
        error: "provider exploded",
      })
    );

    const h = await renderThread(entries);

    const el = screen.getByTestId("entry-error");

    // The card announces itself with alert semantics and speaks the
    // honest generic for an unrecognized failure — the raw string is NOT
    // the headline.
    expect(el.getAttribute("role")).toBe("alert");
    expect(el.textContent).toContain("This turn failed.");
    expect(el.textContent).not.toContain("provider exploded");

    // The raw string survives verbatim behind the collapsed disclosure.
    expect(screen.queryByTestId("entry-error-details")).toBeNull();

    await act(async () => {
      fireEvent.click(screen.getByTestId("entry-error-details-toggle"));
    });

    expect(screen.getByTestId("entry-error-details").textContent).toBe(
      "provider exploded"
    );

    await h.unmount();
  });

  it("renders the artifacts chips row after the reply, outside the tool-call fold (add-turn-artifacts design D7)", async () => {
    useChatStore.setState({ activeAgentID: "a1", panelTabs: [] });

    // Five tool calls: the cards fold, the chips row must not fold with them.
    const entries = fold(
      ev(Kind.KindUserMessage, { text: "go", turn_id: "t1" }),
      toolEvent("c1", "a.md"),
      toolEvent("c2", "b.md"),
      toolEvent("c3", "c.md"),
      toolEvent("c4", "d.md"),
      toolEvent("c5", "e.md"),
      ev(Kind.KindAssistantMessage, {
        text: "the report is ready",
        turn_id: "t1",
      }),
      ev(Kind.KindTurnCompleted, {
        turn_id: "t1",
        artifacts: [
          {
            path: "artifacts/report.md",
            size: 2048,
            change: ArtifactChange.ArtifactCreated,
          },
        ],
      })
    );

    const h = await renderThread(entries);
    await h.settle();

    const row = screen.getByTestId("entry-artifacts");

    expect(row.textContent).toContain("report.md");

    // In message order: the row renders after the reply text, not inside it.
    const answer = screen.getByText("the report is ready");

    expect(
      answer.compareDocumentPosition(row) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy();

    // Activation opens the workspace-file preview tab — the same call the
    // markdown links make (spec "Chip opens the preview").
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "report.md" }));
    });

    expect(useChatStore.getState().panelTabs[0]).toMatchObject({
      kind: "workspace-file",
      data: { agentID: "a1", relPath: "artifacts/report.md" },
    });

    useChatStore.setState({ activeAgentID: null, panelTabs: [] });

    await h.unmount();
  });

  it("shows the empty-state hint on a fresh session", async () => {
    const h = await renderThread([]);

    expect(screen.getByText("No messages yet — say hello below.")).toBeTruthy();

    await h.unmount();
  });
});

describe("the user prompt (port of the legacy Transcript suite)", () => {
  it("renders the prompt as a right-aligned flat surface", async () => {
    const h = await renderThread(
      fold(
        ev(Kind.KindUserMessage, { text: "hello there", turn_id: "t1" }),
        ev(Kind.KindAssistantMessage, { text: "hi", turn_id: "t1" })
      )
    );

    const prompt = screen.getByTestId("entry-user");

    // Right-aligned (the row ends with the prompt) and flat (no bubble
    // border) — the transcript's prompt grammar.
    const row = prompt.parentElement;

    expect(row?.className).toContain("justify-end");
    expect(prompt.getAttribute("data-role")).toBe("user");
    expect(prompt.textContent).toContain("hello there");

    await h.unmount();
  });
});

describe("turn attribution (refine-chat-surface-identity task 2.2, design D2)", () => {
  it("opens an assistant turn with the session agent's static stub, and user turns carry none", async () => {
    useChatStore.setState({ agents: [agent], activeAgentID: "a1" });

    const h = await renderThread(
      fold(
        ev(Kind.KindUserMessage, { text: "hello", turn_id: "t1" }),
        ev(Kind.KindAssistantMessage, { text: "hi there", turn_id: "t1" }),
        ev(Kind.KindTurnCompleted, { turn_id: "t1" })
      )
    );

    // Exactly one stub in the transcript: the assistant turn's.
    const stubs = screen.getAllByTestId("turn-attribution");

    expect(stubs).toHaveLength(1);
    expect(stubs[0]?.textContent).toContain("Scout");

    // The face renders small — the 16px square the stub passes AgentAvatar.
    // SAFETY: the stub's first child is AgentAvatar's wrapper div; the
    // assertion reads the inline size style off it.
    const face = stubs[0]?.firstElementChild as HTMLElement | null;

    expect(face?.style.width).toBe("16px");
    expect(face?.style.height).toBe("16px");

    // The stub's avatar is the STATIC render: no live-animation state
    // attribute anywhere inside it (the live face is the right bar's only).
    expect(stubs[0]?.querySelector("[data-avatar-state]")).toBeNull();

    await h.unmount();
  });

  it("renders no attribution on a compaction marker", async () => {
    useChatStore.setState({ agents: [agent], activeAgentID: "a1" });

    const entries = fold(
      ev(Kind.KindUserMessage, { text: "before", turn_id: "t1" }),
      {
        kind: Kind.KindCompaction,
        occurred_at: "2026-01-01T00:01:00Z",
        compaction: {
          checkpoint_id: "cp1",
          tokens_before: 154000,
          tokens_after: 9200,
        },
      },
      ev(Kind.KindUserMessage, { text: "after", turn_id: "t2" })
    );

    const h = await renderThread(entries);

    expect(screen.getByTestId("entry-compaction")).not.toBeNull();
    expect(screen.queryByTestId("turn-attribution")).toBeNull();

    await h.unmount();
  });
});
