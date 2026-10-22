/**
 * Shared harness for the thread suites (tasks 3.1–3.5): renders ChatThread
 * inside ChatRuntimeProvider over a seeded chat store, with the Wails client
 * mocked at the module seam (nothing reaches the network) and a
 * ResizeObserver stub installed before any mount — jsdom ships none, and the
 * vendored reasoning preview and conversation map construct one
 * unconditionally in their effects.
 *
 * jsdom has no layout: the suites pin the viewport's scrollHeight per step
 * (the useFollowBottom.test pattern) and stub the viewport element's
 * scrollTo, which the thread's auto-scroll controller drives.
 */
import { act } from "react";
import type { ReactElement } from "react";
import { mockedSurface } from "../../__test__/mockedSurface";
import { renderUI } from "../../__test__/test-utils";
import { vi } from "vite-plus/test";

import {
  Kind,
  ToolStatus,
  type Event as SessionEvent,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type { Client } from "../../../../lib/client";
import { applyEvent, type Entry } from "../../entries";
import { useChatStore, type ExtendedChatStore } from "../../store";
import { ChatRuntimeProvider } from "../../runtime/ChatRuntimeProvider";
import { ChatThread } from "../ChatThread";
import { useAui } from "@assistant-ui/react";

// ---------------------------------------------------------------------------
// Environment stubs (module scope: installed before any component mounts).
// ---------------------------------------------------------------------------

/** The recorded scroll calls of the current render. The stub is installed on
 * the PROTOTYPE (not on the mounted element) so the controller's very first
 * scroll — the initialize scroll-to-bottom, which lands on an animation
 * frame right after the mount — is recorded too. A stub installed on the
 * element afterwards misses it, and the reader's own scroll then races the
 * controller's pending intent. */
const scrollCalls: { top: number; behavior: string }[] = [];

/** The prototype stub: it applies the scroll and dispatches the event a
 * browser fires asynchronously after one. */
const originalScrollTo = Element.prototype.scrollTo;

/** The legacy numeric overload of the DOM's scrollTo (the x coordinate);
 * the options object is the modern form. */
function isLegacyScroll(options: ScrollToOptions | number): options is number {
  return typeof options === "number";
}

/** The options the stub applies: the options object, or the legacy numeric
 * form read as the vertical offset. */
function scrollOptions(
  options: ScrollToOptions | number | undefined
): ScrollToOptions {
  if (options === undefined) return {};

  if (isLegacyScroll(options)) return { top: options, left: 0 };

  return options;
}

Element.prototype.scrollTo = function scrollToStub(
  this: Element,
  options?: ScrollToOptions | number
): void {
  const resolved: ScrollToOptions = scrollOptions(options);

  scrollCalls.push({
    top: resolved.top ?? 0,
    behavior: String(resolved.behavior ?? "auto"),
  });

  this.scrollTop = resolved.top ?? 0;

  window.setTimeout((): void => {
    this.dispatchEvent(new Event("scroll"));
  }, 0);
};

/** Restores the platform's scroll implementation. */
export function restoreScrollTo(): void {
  Element.prototype.scrollTo = originalScrollTo;
}

/** No-op stand-in: the observed elements just never report a resize; the
 * MutationObserver fallback inside the auto-scroll controller still fires. */
class ResizeObserverStub implements ResizeObserver {
  observe(_target: Element): void {}

  unobserve(_target: Element): void {}

  disconnect(): void {}
}

window.ResizeObserver = ResizeObserverStub;

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: only the members the adapter can reach. */
type MockedClient = Client & {
  chat: {
    send: MockFn;
    cancel: MockFn;
    regenerate: MockFn;
    transcriptPage: MockFn;
    resolveApproval: MockFn;
    subscribe: MockFn;
  };
};

function makeClient(): MockedClient {
  const c = {
    chat: {
      send: vi.fn(async (): Promise<string> => "turn-mock"),
      cancel: vi.fn(async (): Promise<void> => undefined),
      regenerate: vi.fn(async (): Promise<string> => "turn-mock"),
      transcriptPage: vi.fn(async (): Promise<SessionEvent[]> => []),
      resolveApproval: vi.fn(async (): Promise<void> => undefined),
      subscribe: vi.fn(async (): Promise<() => void> => (): void => undefined),
    },
  };

  // SAFETY: the literal covers exactly the surface this harness drives; the
  // Client-typed intersection defeats the structural check, so the
  // conversion funnels through the shared mockedSurface bridge.
  return mockedSurface<MockedClient, object>(c);
}

vi.mock("../../../../../lib/client", () => ({ client: makeClient() }));

// ---------------------------------------------------------------------------
// Runtime probe: the suites that drive the reader's own send reach the
// thread's runtime through the provider context.
// ---------------------------------------------------------------------------

interface AuiClient {
  thread: {
    append: (text: string) => Promise<void> | void;
  };
}

/** The runtime probe's capture slot (the suites drive the thread's client
 * through it): a named contract rather than a widening anonymous object. */
interface AuiCapture {
  aui: AuiClient | null;
}

const captured: AuiCapture = { aui: null };

function AuiProbe(): ReactElement {
  const aui = useAui();

  // SAFETY: the probe only exposes the append method the suites drive; the
  // client's scope object is exactly what useAui returned.
  captured.aui = aui as AuiClient;

  return <span data-testid="aui-probe" />;
}

/** The thread's runtime client (captured by the mounted probe). */
export function auiOf(): AuiClient {
  if (captured.aui === null)
    throw new Error("the thread runtime is not mounted");

  return captured.aui;
}

// ---------------------------------------------------------------------------
// Fixtures.
// ---------------------------------------------------------------------------

export function ev(
  kind: SessionEvent["kind"],
  extra: Partial<SessionEvent> = {}
): SessionEvent {
  return { kind, occurred_at: "2026-01-01T00:00:00Z", ...extra };
}

export function fold(...events: SessionEvent[]): Entry[] {
  return events.reduce<Entry[]>(
    (entries: Entry[], e: SessionEvent) => applyEvent(entries, e),
    []
  );
}

/** A turn mid-flight: the prompt plus a streaming answer. */
export function streamingTurn(): Entry[] {
  return fold(
    ev(Kind.KindUserMessage, { text: "hello", turn_id: "t1" }),
    ev(Kind.KindTextDelta, { text: "streamed answer ", turn_id: "t1" })
  );
}

/** One completed tool activity record. */
export function toolEvent(callID: string, file: string): SessionEvent {
  return ev(Kind.KindToolActivity, {
    turn_id: "t1",
    tool: {
      call_id: callID,
      name: "read_file",
      arguments: `{"file_path":"${file}"}`,
      status: ToolStatus.ToolStatusCompleted,
      result: "ok",
    },
  });
}

// ---------------------------------------------------------------------------
// The harness.
// ---------------------------------------------------------------------------

export interface ThreadHarness {
  /** The scroll surface (the viewport). */
  viewport: HTMLElement;
  /** The scrollTo calls the auto-scroll controller made. */
  scrollToCalls: { top: number; behavior: string }[];
  /** Pins the viewport's scrollHeight (jsdom has none). */
  pinLayout: (height: number) => void;
  setEntries: (entries: Entry[]) => Promise<void>;
  setStore: (patch: Partial<ExtendedChatStore>) => Promise<void>;
  /** One coalesced frame plus a hair more (rAF + MutationObserver land). */
  settle: () => Promise<void>;
  /** Waits until the viewport has scrolled to the given offset. The
   * auto-scroll controller lands on an animation frame, so a fixed sleep
   * races it under load — the poll waits for the real scroll instead. */
  waitForScroll: (top: number, timeoutMs?: number) => Promise<void>;
  /** Waits until the controller has issued at least `count` scrolls. The
   * controller's INITIAL scroll-to-bottom must land before the reader
   * scrolls up: a scroll-up while its intent is still pending is swallowed
   * (the controller treats it as its own scroll), and the jump pill then
   * never appears. */
  waitForScrollCall: (count?: number, timeoutMs?: number) => Promise<void>;
  unmount: () => Promise<void>;
}

export async function renderThread(entries: Entry[]): Promise<ThreadHarness> {
  await act(async () => {
    useChatStore.setState({ entries });
  });

  const { container, unmount } = await renderUI(
    <ChatRuntimeProvider>
      <AuiProbe />
      <ChatThread />
    </ChatRuntimeProvider>
  );

  // SAFETY: the transcript testid renders the viewport div by contract, and
  // the suites drive scroll geometry on that element.
  const viewport = container.querySelector(
    '[data-testid="transcript"]'
  ) as HTMLElement;

  return {
    viewport,
    scrollToCalls: scrollCalls,
    pinLayout: (height: number): void => {
      Object.defineProperty(viewport, "scrollHeight", {
        configurable: true,
        value: height,
      });
    },
    setEntries: async (next: Entry[]): Promise<void> => {
      await act(async () => {
        useChatStore.setState({ entries: next });
      });
    },
    setStore: async (patch: Partial<ExtendedChatStore>): Promise<void> => {
      await act(async () => {
        useChatStore.setState(patch);
      });
    },
    settle: async (): Promise<void> => {
      await act(async () => {
        await new Promise<void>((resolve: () => void) =>
          setTimeout(resolve, 50)
        );
      });
    },
    waitForScrollCall: async (
      count: number = 1,
      timeoutMs: number = 2000
    ): Promise<void> => {
      const deadline = Date.now() + timeoutMs;

      while (Date.now() < deadline) {
        if (scrollCalls.length >= count) return;

        await act(async () => {
          await new Promise<void>((resolve: () => void) => {
            setTimeout(resolve, 25);
          });
        });
      }
    },
    waitForScroll: async (
      top: number,
      timeoutMs: number = 2000
    ): Promise<void> => {
      const deadline = Date.now() + timeoutMs;

      while (Date.now() < deadline) {
        if (viewport.scrollTop === top) return;

        await act(async () => {
          await new Promise<void>((resolve: () => void) => {
            setTimeout(resolve, 25);
          });
        });
      }
    },
    unmount,
  };
}

export function scrollToBottomOf(harness: ThreadHarness, height: number): void {
  harness.pinLayout(height);
  harness.viewport.scrollTop = height;
  harness.settle();
}
