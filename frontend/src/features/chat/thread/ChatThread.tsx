/**
 * ChatThread (tasks 3.1, 3.3, 3.6, design D9): the transcript on the
 * assistant-ui thread primitives — Root / Viewport / Messages composing
 * buddies' own message components (the vendored thread.aui.tsx reference,
 * never mounted wholesale). The viewport's auto-scroll plus the jump pill
 * replace useFollowBottom: following while at bottom, pause on the reader's
 * own upward scroll, the pill (visible only while not at the bottom) jumps
 * back and re-arms, the reader's own send returns to the bottom, and a
 * disclosure toggle inside a message holds the viewport. Load-earlier rides
 * hasEarlier/onLoadEarlier at the top; the viewport's prepend anchor keeps
 * the reader's position when a page lands above. The viewport wrapper is
 * the relative anchor for the floating run surfaces (top-right, never over
 * the composer) and the conversation-map rail stands beside it.
 */
import { ArrowDown } from "lucide-react";
import { useEffect, useState } from "react";
import type { ReactElement } from "react";
import { createPortal } from "react-dom";

import {
  ThreadPrimitive,
  useAuiState,
  useThreadViewport,
  type AssistantState,
  type ThreadViewportState,
} from "@assistant-ui/react";

import { cn } from "../../../lib/cn";
import { RunSurfaces } from "../run/RunSurfaces";
import { useChatStore, type ExtendedChatStore } from "../store";
import { ChatAssistantMessage } from "./ChatAssistantMessage";
import { ChatUserMessage } from "./ChatUserMessage";
import { ConversationMapRail } from "./ConversationMapRail";
import { TurnActivity } from "./TurnActivity";

// The vendored element set constructs ResizeObserver unconditionally (the
// viewport's size handle, the reasoning preview, the conversation map).
// Hosts without it — the jsdom test environment among them — get a no-op
// stand-in so the thread renders; a real webview defines it and this is a
// no-op. The auto-scroll controller independently observes content growth
// through its MutationObserver fallback, so nothing depends on the stub
// reporting resizes.
if (window.ResizeObserver === undefined) {
  class ResizeObserverStub implements ResizeObserver {
    observe(_target: Element): void {}

    unobserve(_target: Element): void {}

    disconnect(): void {}
  }

  window.ResizeObserver = ResizeObserverStub;
}

/** The message components the thread renders — module-level so the
 * components prop identity is stable. */
const MESSAGE_COMPONENTS = {
  UserMessage: ChatUserMessage,
  AssistantMessage: ChatAssistantMessage,
};

/** The "load earlier" control at the window's top (hasEarlier/onLoadEarlier
 * ride the runtime adapter). The label shimmers while a page is in flight. */
function LoadEarlierSlot(): ReactElement | null {
  const visible = useAuiState(
    (s: AssistantState) => s.thread.hasEarlier || s.thread.isLoadingEarlier
  );

  const loading = useAuiState((s: AssistantState) => s.thread.isLoadingEarlier);

  if (!visible) return null;

  return (
    <ThreadPrimitive.LoadEarlier asChild>
      <button
        type="button"
        className="mx-auto rounded-full border border-line bg-surface px-3 py-1 text-xs text-muted transition-colors hover:text-fg"
        data-testid="btn-load-earlier"
      >
        <span className={cn(loading && "shimmer motion-reduce:animate-none")}>
          {loading ? "Loading earlier messages" : "Load earlier messages"}
        </span>
      </button>
    </ThreadPrimitive.LoadEarlier>
  );
}

/** The empty/loading stand-ins: skeleton rows while a session loads, the
 * quiet hint on a fresh one. */
function ThreadIntro(): ReactElement | null {
  const loadingSession = useChatStore(
    (s: ExtendedChatStore) => s.loadingSession
  );

  const empty = useAuiState(
    (s: AssistantState) => s.thread.messages.length === 0
  );

  if (loadingSession) {
    return (
      <div
        aria-label="Loading session"
        className="mx-auto flex w-full flex-col gap-3"
        data-testid="transcript-skeleton"
      >
        <div className="h-12 w-2/3 animate-pulse self-end rounded-md bg-[color-mix(in_oklab,var(--fg)_8%,transparent)]" />
        <div className="h-16 w-3/4 animate-pulse rounded-md bg-[color-mix(in_oklab,var(--fg)_8%,transparent)]" />
        <div className="h-10 w-1/2 animate-pulse rounded-md bg-[color-mix(in_oklab,var(--fg)_8%,transparent)]" />
      </div>
    );
  }

  if (!empty) return null;

  return (
    <p className="py-16 text-center text-sm text-muted">
      No messages yet — say hello below.
    </p>
  );
}

/** The jump-to-latest pill: visible only while the reader is not at the
 * bottom; activation scrolls back and re-arms following. */
function JumpToLatest(): ReactElement | null {
  const isAtBottom = useThreadViewport(
    (s: ThreadViewportState) => s.isAtBottom
  );

  if (isAtBottom) return null;

  return (
    <ThreadPrimitive.ScrollToBottom asChild>
      <button
        type="button"
        aria-label="Jump to latest"
        className="absolute bottom-4 right-6 z-10 flex size-9 items-center justify-center rounded-full border border-line bg-surface text-muted shadow-sm hover:text-fg disabled:invisible"
        data-testid="btn-jump-latest"
        title="Jump to latest"
      >
        <ArrowDown aria-hidden="true" className="size-4" />
      </button>
    </ThreadPrimitive.ScrollToBottom>
  );
}

/** The transcript. The viewport subtree owns the viewport context, so the
 * map rail (the composition's sticky overlay form, pinned to the viewport's
 * right edge) and the jump pill mount inside it; the run card needs no
 * context and anchors to the wrapper instead. */
export function ChatThread(): ReactElement {
  // The overlay layer the conversation map portals into: a sibling of the
  // viewport inside the relative wrapper, so the map floats over the
  // scrollport without touching the scroll height.
  const [overlay, setOverlay] = useState<HTMLElement | null>(null);

  // Warm the completed-text chunk while the transcript is still empty. The
  // assistant message loads the vendored markdown element lazily (the
  // highlighter weighs megabytes, and the first paint of an empty chat must
  // not pay for them), but a reader who watches a turn stream would then see
  // its completed body flash blank while the chunk lands. Fetching it here —
  // after the first paint — keeps the bundle lean AND the first completed
  // message whole.
  useEffect((): void => {
    void import("@/components/assistant-ui/elements/markdown-text");
    void import("@/components/assistant-ui/elements/shiki-highlighter.aui");
  }, []);

  return (
    <ThreadPrimitive.Root className="flex h-full min-h-0 min-w-0 flex-1 flex-col bg-background">
      <div className="relative flex min-h-0 min-w-0 flex-1">
        <div
          ref={setOverlay}
          className="pointer-events-none absolute inset-0 z-10"
        />
        <ThreadPrimitive.Viewport
          className="od-scroll relative min-h-0 min-w-0 flex-1 overflow-y-auto scroll-smooth"
          data-testid="transcript"
        >
          <div className="mx-auto flex w-full max-w-3xl flex-col gap-3 px-4 py-4">
            <LoadEarlierSlot />
            <ThreadIntro />
            <div className="flex flex-col gap-4">
              <ThreadPrimitive.Messages components={MESSAGE_COMPONENTS} />
              <TurnActivity />
            </div>
          </div>
          {/* The map renders into the overlay layer, OUTSIDE the scrollport:
           * its zero-height sticky rail inside the scrollport inflates the
           * scroll height by one viewport, which parks the auto-scroll in
           * empty space below the transcript — the reader then sees a blank
           * thread. The portal keeps the viewport context (a portal
           * preserves it) and leaves the scroll height equal to the
           * content's. */}
          {overlay !== null && createPortal(<ConversationMapRail />, overlay)}
          {/* Zero-height sticky slot: the pill rides the scrollport's bottom
           * edge whether or not the reader is scrolled up. */}
          <div className="sticky bottom-0 h-0 w-full">
            <JumpToLatest />
          </div>
        </ThreadPrimitive.Viewport>
        <RunSurfaces />
      </div>
    </ThreadPrimitive.Root>
  );
}
