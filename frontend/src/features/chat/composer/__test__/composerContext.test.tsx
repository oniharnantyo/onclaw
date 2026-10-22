/**
 * Context meter suite (task 5.4, spec chat-view "Chat right sidebar" → the
 * context indicator and the breakdown popover): nothing renders before the
 * first turn, the ring reads the reported used value against the limit (the
 * model override's window when one is set), and the popover shows the total,
 * the stacked bar, the legend with its estimate captions, the headroom and
 * the last turn's tokens. The opening animation is off under reduced motion.
 */
import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

// The client mock factory is imported FIRST on purpose: the hoisted
// `vi.mock` below runs the moment the chat store pulls the client module in,
// so the factory's module must already be evaluated by then.
import { makeClient } from "./clientMock";
import { useChatStore } from "../../store";
import { renderComposer, seedStore } from "./harness";

vi.mock("../../../../lib/client", () => ({ client: makeClient() }));

/** The reduced-motion media query stub: it reports the preference the hook
 * reads and swallows the listener changes. */
function reducedMotionQuery(query: string): MediaQueryList {
  return {
    matches: query.includes("prefers-reduced-motion"),
    media: query,
    onchange: null,
    addEventListener: (): void => undefined,
    removeEventListener: (): void => undefined,
    addListener: (): void => undefined,
    removeListener: (): void => undefined,
    dispatchEvent: (): boolean => false,
  };
}

/** The usage the backend reports: a quarter of a 200k window, with the last
 * turn's input and output counts. */
interface UsageOverrides {
  used?: number;
  limit?: number;
  last_input?: number;
  last_output?: number;
}

/** The reported usage record. */
interface ReportedUsage {
  used: number;
  limit: number;
  last_input: number;
  last_output: number;
}

function usage(overrides: UsageOverrides = {}): ReportedUsage {
  return {
    used: 50000,
    limit: 200000,
    last_input: 1200,
    last_output: 300,
    ...overrides,
  };
}

/** Opens the breakdown popover. */
async function openPopover(): Promise<void> {
  await act(async () => {
    fireEvent.click(screen.getByTestId("context-ring-trigger"));
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  seedStore();
});

describe("the context meter (task 5.4)", () => {
  it("renders nothing before the first turn", async () => {
    await renderComposer();

    expect(screen.queryByTestId("context-meter")).toBeNull();
  });

  it("reads the reported used value against the backend limit", async () => {
    useChatStore.setState({ usage: usage() });
    await renderComposer();

    const ring = screen.getByTestId("context-ring");

    expect(ring.getAttribute("title")).toBe("50k / 200k tokens");
    expect(screen.getByTestId("context-ring-pct").textContent).toBe("25%");
  });

  it("accounts a model override against the model's capability window", async () => {
    useChatStore.setState({ usage: usage(), modelOverride: "m-base" });
    await renderComposer();

    // m-base's catalog window is 8k, so the same usage reads as full.
    const ring = screen.getByTestId("context-ring");

    expect(ring.getAttribute("title")).toBe("50k / 8k tokens");
    expect(screen.getByTestId("context-ring-pct").textContent).toBe("100%");
  });

  it("opens the popover with the total, the bar, the legend and the headroom", async () => {
    useChatStore.setState({ usage: usage() });
    await renderComposer();

    await openPopover();

    const popover = screen.getByTestId("context-popover");

    expect(popover.getAttribute("aria-label")).toBe("Context breakdown");
    expect(screen.getByTestId("context-popover-total").textContent).toBe(
      "50k / 200k tokens"
    );
    expect(screen.getByTestId("context-popover-bar")).toBeTruthy();
    expect(screen.getByTestId("context-headroom").textContent).toContain(
      "150k"
    );
  });

  it("legends every segment and captions the client estimates", async () => {
    useChatStore.setState({ usage: usage() });
    await renderComposer();

    await openPopover();

    // The five segments: instructions, tools and skills, files, conversation
    // and the exact server remainder.
    expect(screen.getByTestId("context-segment-instructions")).toBeTruthy();
    expect(screen.getByTestId("context-segment-tools")).toBeTruthy();
    expect(screen.getByTestId("context-segment-files")).toBeTruthy();
    expect(screen.getByTestId("context-segment-conversation")).toBeTruthy();
    expect(screen.getByTestId("context-segment-server")).toBeTruthy();

    // The client estimates carry their caption; the server remainder is exact
    // by construction and carries none.
    expect(screen.getByTestId("context-estimated-instructions")).toBeTruthy();
    expect(screen.getByTestId("context-estimated-tools")).toBeTruthy();
    expect(screen.getByTestId("context-estimated-files")).toBeTruthy();
    expect(screen.getByTestId("context-estimated-conversation")).toBeTruthy();
    expect(screen.queryByTestId("context-estimated-server")).toBeNull();
  });

  it("the total is the reported used value, never the estimate sum", async () => {
    useChatStore.setState({ usage: usage({ used: 12345 }) });
    await renderComposer();

    await openPopover();

    expect(screen.getByTestId("context-popover-total").textContent).toContain(
      "12.3k"
    );
  });

  it("hides the last-turn rows until the backend reports them", async () => {
    useChatStore.setState({
      usage: usage({ last_input: 0, last_output: 0 }),
    });
    await renderComposer();

    await openPopover();

    expect(screen.queryByTestId("context-last-input")).toBeNull();
    expect(screen.queryByTestId("context-last-output")).toBeNull();
  });

  it("shows the last turn's input and output once they are reported", async () => {
    useChatStore.setState({ usage: usage() });
    await renderComposer();

    await openPopover();

    expect(screen.getByTestId("context-last-input").textContent).toContain(
      "1.2k"
    );
    expect(screen.getByTestId("context-last-output").textContent).toContain(
      "300"
    );
  });

  it("closes on Escape, on an outside click, and on a second activation", async () => {
    useChatStore.setState({ usage: usage() });
    await renderComposer();

    await openPopover();
    expect(screen.getByTestId("context-popover")).toBeTruthy();

    // A second activation closes it.
    await openPopover();
    await waitFor(() =>
      expect(screen.queryByTestId("context-popover")).toBeNull()
    );

    // Escape closes it too.
    await openPopover();
    await act(async () => {
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    });
    await waitFor(() =>
      expect(screen.queryByTestId("context-popover")).toBeNull()
    );

    // An outside click closes it.
    await openPopover();
    await act(async () => {
      document.body.dispatchEvent(
        new MouseEvent("mousedown", { bubbles: true })
      );
    });
    await waitFor(() =>
      expect(screen.queryByTestId("context-popover")).toBeNull()
    );
  });

  it("skips the opening animation under reduced motion", async () => {
    const original = window.matchMedia;

    // SAFETY: the descriptor is restored below; the stub only reports the
    // reduced-motion preference to the hook.
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      value: reducedMotionQuery,
    });

    try {
      useChatStore.setState({ usage: usage() });
      await renderComposer();

      await openPopover();

      const popover = screen.getByTestId("context-popover");

      expect(popover.getAttribute("class")).not.toContain("animate-in");
    } finally {
      if (original === undefined) {
        // SAFETY: when jsdom had no matchMedia descriptor the stub was
        // installed as an own configurable property, so the delete only
        // removes the stub.
        delete (window as { matchMedia?: unknown }).matchMedia;
      } else {
        Object.defineProperty(window, "matchMedia", {
          configurable: true,
          value: original,
        });
      }
    }
  });
});
