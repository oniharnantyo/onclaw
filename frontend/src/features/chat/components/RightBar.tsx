/**
 * Right bar (merge-right-bar-previews, designs D1–D2): ONE container beside
 * the transcript that swaps between two states in place — never two columns.
 * With no preview tabs open it shows the session identity (the old right
 * sidebar): the active agent's avatar + name and the session's EFFECTIVE
 * model — the session's override when set, else the agent's configured
 * model, effort appended, provider display name resolved from settings.
 * Opening a preview replaces the identity in the same aside with the tab
 * strip + active renderer (the old right panel's machinery; the per-kind
 * renderers live in panelRenderers.tsx), and closing the last tab returns
 * the identity. A third in-place state (add-knowledge-corpus): the identity
 * view's Knowledge opener swaps the island for the knowledge browsing panel
 * (KnowledgePanel.tsx); activating a document opens its preview tab, which
 * leaves the browsing state behind. The identity avatar is also the
 * session's live face (add-live-agent-avatar design D8): the useLiveAvatar
 * hook runs above the early returns so its outcome seen-ref outlives the
 * avatar instance across identity ↔ preview swaps, and the identity branch
 * forwards its props as `live`.
 *
 * The pinned identity tab (add-pinned-identity-tab): `activePanelTabID ===
 * null` no longer implies "no tabs open" — it means the identity IS the
 * active surface, which it already was at zero tabs. While tabs are open
 * the strip leads with a pinned, never-closable identity tab (a static 18
 * px avatar label — the strip must never hold a second live face), and
 * activating it shows the identity content inside the tab shell while every
 * preview tab stays listed. Both shells render one shared identity element
 * (design D3), so the zero-tab island keeps its exact markup and the tabbed
 * view cannot drift from it.
 *
 * State grammar:
 * - Collapse (`sidebarCollapsed`) folds the bar to zero inline size but keeps
 *   it mounted (refine-chat-surface-identity D8): minimize/restore animates
 *   on `--motion-base` + `--ease-standard` and the identity content survives
 *   the round trip in the same nodes, while nothing paints or intercepts
 *   pointers at zero size; the expand control lives in the chat header.
 *   Collapsing is pure view state on the store — the live subscription and
 *   turn lifecycle live outside this component, so collapsing can never
 *   interrupt an in-flight turn.
 * - Below xl the identity state is hidden outright (`hidden xl:flex`, the
 *   header toggle is its only affordance at every width); the tab state
 *   renders as a full-height overlay sheet capped to the view, docked back
 *   into the flex row as a column at xl (the old panel's classes).
 * - One width preference for both states (design D2): the docked column is
 *   drag-resizable by its left edge in either state (handle exists only at
 *   xl); the width persists under `buddies.chatSidebarWidth` and clamps to
 *   sane bounds. The width transition stands down while a drag is live (1:1
 *   pointer tracking) and re-arms on release (refine-chat-surface-identity
 *   D8).
 *
 * The tab state is pure view state over the store's `panelTabs` and
 * `activePanelTabID` — opening, raising, closing, or resizing it touches
 * nothing the turn lifecycle owns, so it can never interrupt a streaming
 * turn. Tabs are reset by the store on session switch, which returns the bar
 * to its identity state.
 */
import { useEffect, useRef, useState } from "react";
import type {
  CSSProperties,
  PointerEvent as ReactPointerEvent,
  ReactElement,
} from "react";
import { X } from "lucide-react";

import type {
  Agent,
  Session,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { ProviderView } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import { AgentAvatar } from "../../../components";
import { cn } from "../../../lib/cn";
import type { KnowledgeDocument } from "../../../lib/client";
import { useLocalNumberPreference } from "../../../lib/useLocalPreference";
import { shortModelName } from "../format";
import { useLiveAvatar } from "../run/useLiveAvatar";
import { useChatStore } from "../store";
import type { ChatStore, PanelTab } from "../types";
import { knowledgeDocTab } from "../types";
import { KnowledgePanel } from "./KnowledgePanel";
import { PANEL_RENDERERS } from "./panelRenderers";

const SIDEBAR_WIDTH_KEY = "buddies.chatSidebarWidth";

const DEFAULT_SIDEBAR_WIDTH = 280;

const MIN_SIDEBAR_WIDTH = 220;

const MAX_SIDEBAR_WIDTH = 560;

export function RightBar(): ReactElement | null {
  const collapsed = useChatStore((s: ChatStore) => s.sidebarCollapsed);
  const tabs = useChatStore((s: ChatStore) => s.panelTabs);
  const activeStoreID = useChatStore((s: ChatStore) => s.activePanelTabID);
  const activatePanelTab = useChatStore((s: ChatStore) => s.activatePanelTab);
  const closePanelTab = useChatStore((s: ChatStore) => s.closePanelTab);
  const openPanelTab = useChatStore((s: ChatStore) => s.openPanelTab);
  const agents = useChatStore((s: ChatStore) => s.agents);
  const providers = useChatStore((s: ChatStore) => s.providers);
  const sessions = useChatStore((s: ChatStore) => s.sessions);
  const activeAgentID = useChatStore((s: ChatStore) => s.activeAgentID);
  const activeSessionID = useChatStore((s: ChatStore) => s.activeSessionID);

  // The identity face's live props (add-live-agent-avatar design D8): it
  // MUST sit above the early returns below — the bar stays mounted across
  // identity ↔ preview swaps, so the hook's seen-ref persists while only
  // the avatar instance comes and goes.
  const live = useLiveAvatar();

  // The active agent, looked up above the early returns because the
  // pinned-identity flag below needs it before the browsing reset runs
  // (the lookup is pure, so hoisting it is safe).
  const agent = agents.find((a: Agent) => a.ID === activeAgentID) ?? null;

  // The pinned identity tab (add-pinned-identity-tab D4): one derived flag.
  // A null active id with an agent present means the identity IS the active
  // surface — the island at zero tabs, the pinned tab's view with tabs
  // open. The agent rides the flag (`identityAgent`) so the identity
  // branches below see it narrowed non-null.
  const identityAgent = activeStoreID === null ? agent : null;
  const identityActive = identityAgent !== null;

  // The knowledge browsing state (add-knowledge-corpus): local view state
  // of the identity view only. Any preview taking the active surface — a
  // document activation here or a file link elsewhere — yields the bar to
  // the tabs view, and returning to the identity view (pinned tab or last
  // close) never restores the browsing state (it does not persist across
  // that boundary).
  const [browsingKnowledge, setBrowsingKnowledge] = useState(false);

  useEffect((): void => {
    if (!identityActive) setBrowsingKnowledge(false);
  }, [identityActive]);

  // One width preference for both states (design D2).
  const [width, setWidth] = useLocalNumberPreference(
    SIDEBAR_WIDTH_KEY,
    DEFAULT_SIDEBAR_WIDTH
  );

  const asideRef = useRef<HTMLElement | null>(null);

  // The collapsed fold is inert (refine-chat-surface-identity D8): the bar
  // stays mounted at zero inline size, so focus could still tab into its
  // invisible controls — `inert` drops the whole subtree from the
  // interaction tree while the same nodes keep the restore animation alive
  // (no display:none, no unmount).
  useEffect((): void => {
    asideRef.current?.toggleAttribute("inert", collapsed);
  }, [collapsed]);

  // Drag state (refine-chat-surface-identity D8): while a drag is live the
  // width transition stands down for 1:1 pointer tracking and re-arms on
  // release (or cancellation).
  const [resizing, setResizing] = useState(false);

  // The handle rides the bar's left edge, so widening follows the pointer
  // leftward: width = right edge − pointer, clamped. Shared by both states.
  const onResizeStart = (e: ReactPointerEvent<HTMLDivElement>): void => {
    e.preventDefault();
    const aside = asideRef.current;

    if (!aside) return;

    setResizing(true);

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
      setResizing(false);
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", stop);
      window.removeEventListener("pointercancel", stop);
    };

    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", stop);
    window.addEventListener("pointercancel", stop);
  };

  // One inline style for every aside this component returns (the D2 width
  // var; D8's motion): the width transition drives minimize/restore AND the
  // drag width, so it stands down only while a drag is live. SAFETY: the bar
  // reads its width through the CSS custom property in the width class;
  // React's style typing does not model custom properties, so the record is
  // asserted once here.
  const shellStyle = {
    "--sidebar-width": `${width}px`,
    transition: resizing
      ? "none"
      : [
          "width var(--motion-base) var(--ease-standard)",
          "padding var(--motion-base) var(--ease-standard)",
          "margin var(--motion-base) var(--ease-standard)",
          "border-width var(--motion-base) var(--ease-standard)",
        ].join(", "),
  } as CSSProperties;

  // Collapsed (refine-chat-surface-identity D8): the bar folds to zero
  // inline size but stays mounted — nothing paints, nothing intercepts
  // pointers, and the restore animates from these same nodes.
  if (collapsed) {
    shellStyle.width = 0;
    shellStyle.minWidth = 0;
    shellStyle.padding = 0;
    shellStyle.margin = 0;
    shellStyle.borderWidth = 0;
    shellStyle.boxShadow = "none";
    shellStyle.overflow = "hidden";
    shellStyle.pointerEvents = "none";
  }

  const session =
    sessions.find((s: Session) => s.ID === activeSessionID) ?? null;

  // Effective config (design D1): the session's override when set, else the
  // agent's configured default. The line shows the provider display name +
  // model family — the last `/`-delimited segment (refine-chat-surface-
  // identity D4); the full id lives in the hover title below.
  const model = session?.Model || agent?.Model || "";
  const effort = session?.Effort || agent?.Effort || "";

  const provider = agent
    ? providers.find((p: ProviderView) => p.ID === agent.ProviderID)
    : undefined;

  const modelLine = agent
    ? `${provider ? provider.Name : agent.ProviderID}${model ? `/${shortModelName(model)}` : ""}${effort ? ` · ${effort}` : ""}`
    : "";

  // The identity content shared by both shells (add-pinned-identity-tab
  // D3): the island's inner nodes exactly — the bar's ONE live face (the
  // pinned tab's label avatar passes no `live`), the name + effective-model
  // lines, the Knowledge opener. Rendering the same elements from both
  // shells keeps them from drifting.
  const identityContent = (agent: Agent): ReactElement => (
    <>
      <AgentAvatar id={agent.ID} avatar={agent.Avatar} size={64} live={live} />
      {/* max-w-full lets the island's shrink-wrapped block truncate instead
          of overflowing the bar (refine-chat-surface-identity D4). */}
      <div className="max-w-full text-center">
        <p
          className="font-display text-base font-semibold"
          data-testid="sidebar-agent-name"
        >
          {agent.Name}
        </p>
        <p
          className="mt-0.5 truncate font-mono text-xs text-muted"
          data-testid="sidebar-model"
          title={model || undefined}
        >
          {modelLine || "No model selected"}
        </p>
      </div>
      <button
        type="button"
        className="mt-1 cursor-pointer rounded-md border border-line px-3 py-1.5 text-[12px] font-medium text-fg2 hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
        data-testid="knowledge-opener"
        onClick={(): void => setBrowsingKnowledge(true)}
      >
        Knowledge
      </button>
    </>
  );

  // Identity state (design D1): no tabs open — the old right sidebar's
  // island. It requires an agent and stays below-xl hidden (`hidden
  // xl:flex`), exactly as before the merge.
  if (tabs.length === 0) {
    if (!agent) return null;

    // Knowledge browsing state (add-knowledge-corpus): the same aside shell,
    // the identity island swapped for the panel in place. Activating a
    // document opens its preview tab, which leaves this branch entirely.
    if (browsingKnowledge) {
      return (
        <aside
          ref={asideRef}
          className="glass-panel relative m-2 hidden w-[var(--sidebar-width)] shrink-0 flex-col items-center gap-3 rounded-lg border border-line bg-surface p-4 xl:flex"
          style={shellStyle}
          data-testid="chat-right-bar"
        >
          <KnowledgePanel
            agentID={agent.ID}
            onOpenDoc={(doc: KnowledgeDocument): void =>
              openPanelTab(knowledgeDocTab(doc))
            }
            onBack={(): void => setBrowsingKnowledge(false)}
          />
          <div
            role="separator"
            aria-orientation="vertical"
            aria-label="Resize panel"
            onPointerDown={onResizeStart}
            className="absolute inset-y-0 left-0 z-10 hidden w-1.5 cursor-col-resize hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] xl:block"
            data-testid="sidebar-resize"
          />
        </aside>
      );
    }

    return (
      <aside
        ref={asideRef}
        className="glass-panel relative m-2 hidden w-[var(--sidebar-width)] shrink-0 flex-col items-center gap-3 rounded-lg border border-line bg-surface p-4 xl:flex"
        style={shellStyle}
        data-testid="chat-right-bar"
      >
        {identityContent(agent)}
        <div
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize panel"
          onPointerDown={onResizeStart}
          className="absolute inset-y-0 left-0 z-10 hidden w-1.5 cursor-col-resize hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] xl:block"
          data-testid="sidebar-resize"
        />
      </aside>
    );
  }

  // Tab state: the SAME aside now hosts the old right panel's machinery —
  // below xl a full-height overlay sheet, at xl the docked resizable column.
  // The active surface (add-pinned-identity-tab D4): the identity flag wins
  // (a null active id with an agent present), else the find ?? last
  // fallback keeps a stale id from emptying the bar.
  const activeTab =
    identityAgent === null
      ? (tabs.find((t: PanelTab) => t.id === activeStoreID) ??
        tabs[tabs.length - 1])
      : null;

  const Renderer = activeTab === null ? null : PANEL_RENDERERS[activeTab.kind];

  // Which surface owns the content area (refine-chat-surface-identity D8):
  // keying the wrapper on it remounts the node on every identity↔preview
  // swap, replaying the fade below.
  const surfaceKey =
    identityAgent !== null && activeTab === null
      ? browsingKnowledge
        ? "knowledge"
        : "identity"
      : (activeTab?.id ?? "empty");

  return (
    <aside
      ref={asideRef}
      className={cn(
        "absolute inset-y-0 right-0 z-30 flex w-[min(100%,var(--sidebar-width))] shrink-0 flex-col border-l border-line bg-surface",
        "shadow-[var(--elev-raised)] xl:relative xl:z-auto xl:w-[var(--sidebar-width)] xl:shadow-none"
      )}
      style={shellStyle}
      data-testid="chat-right-bar"
    >
      <div
        className="flex items-center gap-1 overflow-x-auto border-b border-line p-1"
        data-testid="panel-tabs"
      >
        {agent !== null && (
          <button
            type="button"
            className={cn(
              "flex shrink-0 cursor-pointer items-center rounded-md border px-1.5 py-1",
              identityActive
                ? "border-line bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] text-fg"
                : "border-transparent text-muted"
            )}
            data-testid="panel-identity-tab"
            aria-label={agent.Name}
            title={agent.Name}
            aria-current={identityActive ? "true" : undefined}
            onClick={(): void => activatePanelTab(null)}
          >
            {/* Static label (add-pinned-identity-tab D6): the strip must
                  never hold a second live face. */}
            <AgentAvatar id={agent.ID} avatar={agent.Avatar} size={18} />
          </button>
        )}
        {tabs.map((tab: PanelTab) => (
          <div
            key={tab.id}
            className={cn(
              "flex min-w-0 items-center gap-1 rounded-md border px-1.5 py-1",
              tab.id === activeTab?.id
                ? "border-line bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] text-fg"
                : "border-transparent text-muted"
            )}
          >
            <button
              type="button"
              className="min-w-0 cursor-pointer truncate text-xs"
              data-testid={`panel-tab-${tab.id}`}
              onClick={() => activatePanelTab(tab.id)}
            >
              {tab.title}
            </button>
            <button
              type="button"
              className="flex shrink-0 cursor-pointer items-center rounded-full p-0.5 text-muted hover:text-fg"
              aria-label={`Close ${tab.title}`}
              title={`Close ${tab.title}`}
              data-testid={`panel-tab-close-${tab.id}`}
              onClick={() => closePanelTab(tab.id)}
            >
              <X aria-hidden="true" className="size-3" />
            </button>
          </div>
        ))}
      </div>
      <div
        className="od-scroll min-h-0 flex-1 overflow-y-auto p-3 text-sm leading-relaxed text-fg"
        data-testid="panel-content"
      >
        <div key={surfaceKey} className="od-fade" data-testid="panel-surface">
          {identityAgent !== null && activeTab === null ? (
            browsingKnowledge ? (
              <KnowledgePanel
                agentID={identityAgent.ID}
                onOpenDoc={(doc: KnowledgeDocument): void =>
                  openPanelTab(knowledgeDocTab(doc))
                }
                onBack={(): void => setBrowsingKnowledge(false)}
              />
            ) : (
              <div className="flex flex-col items-center gap-3">
                {identityContent(identityAgent)}
              </div>
            )
          ) : null}
          {Renderer !== null && activeTab !== null && (
            <Renderer key={activeTab.id} tab={activeTab} />
          )}
        </div>
      </div>
      <div
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize panel"
        onPointerDown={onResizeStart}
        className="absolute inset-y-0 left-0 z-10 hidden w-1.5 cursor-col-resize hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] xl:block"
        data-testid="sidebar-resize"
      />
    </aside>
  );
}
