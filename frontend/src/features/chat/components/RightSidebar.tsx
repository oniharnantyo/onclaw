/**
 * Right sidebar: the active agent (avatar + name) and the session's
 * EFFECTIVE model — the session's override when set, else the agent's
 * configured model (design D1) — with the provider's display name resolved
 * from settings. The context ring lives on the composer's rail now
 * (ContextMeter), so this panel is identity only.
 *
 * Collapsing hides the island entirely — nothing remains mounted; the
 * expand control lives in the chat header, so the panel is reachable from
 * there. Collapsing is pure view state on the store — the live subscription
 * and turn lifecycle live outside this component, so collapsing can never
 * interrupt an in-flight turn. Below xl the panel is hidden outright (the
 * header toggle is its only affordance at every width). The docked column is
 * drag-resizable by its left edge (the preview panel's pattern): the width
 * persists under `buddies.chatSidebarWidth` and clamps to sane bounds.
 */
import { useRef } from "react";
import type {
  CSSProperties,
  PointerEvent as ReactPointerEvent,
  ReactElement,
} from "react";

import type {
  Agent,
  Session,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { ProviderView } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import { AgentAvatar } from "../../../components";
import { useLocalNumberPreference } from "../../../lib/useLocalPreference";
import { useChatStore } from "../store";
import type { ChatStore } from "../types";

const SIDEBAR_WIDTH_KEY = "buddies.chatSidebarWidth";

const DEFAULT_SIDEBAR_WIDTH = 240;

const MIN_SIDEBAR_WIDTH = 200;

const MAX_SIDEBAR_WIDTH = 400;

export function RightSidebar(): ReactElement | null {
  const collapsed = useChatStore((s: ChatStore) => s.sidebarCollapsed);
  const agents = useChatStore((s: ChatStore) => s.agents);
  const providers = useChatStore((s: ChatStore) => s.providers);
  const sessions = useChatStore((s: ChatStore) => s.sessions);
  const activeAgentID = useChatStore((s: ChatStore) => s.activeAgentID);
  const activeSessionID = useChatStore((s: ChatStore) => s.activeSessionID);

  const asideRef = useRef<HTMLElement | null>(null);

  const [width, setWidth] = useLocalNumberPreference(
    SIDEBAR_WIDTH_KEY,
    DEFAULT_SIDEBAR_WIDTH
  );

  const onResizeStart = (e: ReactPointerEvent<HTMLDivElement>): void => {
    e.preventDefault();
    const aside = asideRef.current;

    if (!aside) return;

    // The handle rides the panel's left edge, so widening follows the
    // pointer leftward: width = right edge − pointer, clamped.
    const move = (ev: PointerEvent): void => {
      const right = aside.getBoundingClientRect().right;

      setWidth(
        Math.min(
          MAX_SIDEBAR_WIDTH,
          Math.max(MIN_SIDEBAR_WIDTH, Math.round(right - ev.clientX))
        )
      );
    };

    const stop = (): void => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", stop);
    };

    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", stop);
  };

  const agent = agents.find((a: Agent) => a.ID === activeAgentID) ?? null;

  const session =
    sessions.find((s: Session) => s.ID === activeSessionID) ?? null;

  // Effective config (design D1): the session's override when set, else the
  // agent's configured default.
  const model = session?.Model || agent?.Model || "";
  const effort = session?.Effort || agent?.Effort || "";

  const provider = agent
    ? providers.find((p: ProviderView) => p.ID === agent.ProviderID)
    : undefined;

  const modelLine = agent
    ? `${provider ? provider.Name : agent.ProviderID}${model ? `/${model}` : ""}${effort ? ` · ${effort}` : ""}`
    : "";

  // Minimized hides the island entirely — not even a collapsed sliver stays.
  if (collapsed || !agent) return null;

  return (
    <aside
      ref={asideRef}
      className="glass-panel relative m-2 hidden w-[var(--sidebar-width)] shrink-0 flex-col items-center gap-3 rounded-lg border border-line bg-surface p-4 xl:flex"
      style={
        // SAFETY: the panel reads its width through the CSS custom property
        // in the width class; React's style typing does not model custom
        // properties, so the record is asserted once here.
        { "--sidebar-width": `${width}px` } as CSSProperties
      }
      data-testid="chat-sidebar"
    >
      <AgentAvatar id={agent.ID} avatar={agent.Avatar} size={64} />
      <div className="text-center">
        <p
          className="font-display text-base font-semibold"
          data-testid="sidebar-agent-name"
        >
          {agent.Name}
        </p>
        <p
          className="mt-0.5 font-mono text-xs break-all text-muted"
          data-testid="sidebar-model"
        >
          {modelLine || "No model selected"}
        </p>
      </div>
      <div
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize agent panel"
        onPointerDown={onResizeStart}
        className="absolute inset-y-0 left-0 z-10 hidden w-1.5 cursor-col-resize hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] xl:block"
        data-testid="sidebar-resize"
      />
    </aside>
  );
}
