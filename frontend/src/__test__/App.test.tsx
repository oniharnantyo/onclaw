/**
 * Shell tests (redesign-shell-layout tasks 1.1–1.3): the quiet textless
 * strip, the Settings full-island swap of the Sessions island, and the
 * persisted rail collapse. The heavy view modules are mocked wholesale; the
 * real SessionRail renders against a seeded chat store.
 */
import { act, createElement, type ReactElement } from "react";
import { fireEvent, screen, within } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type {
  Agent,
  Session,
} from "../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import App from "../App";
import { useChatStore } from "../features/chat/store";
import { useNavStore } from "../stores/nav";
import { usePlatformStore } from "../stores/platform";
import { renderUI } from "./test-utils";

// The shell subscribes to the settings signal, and the theme store's platform
// sync touches windowTheme; both route through the mocked client.
vi.mock("../lib/client", () => ({
  client: {
    events: {
      onSettingsOpen: vi.fn((_handler: () => void) => (): void => undefined),
    },
    windowTheme: {
      setLight: vi.fn((): Promise<void> => Promise.resolve()),
    },
  },
}));

// Heavy views are stubbed: the shell tests assert the chrome around them,
// not their contents.
vi.mock("../features/chat", () => ({
  default: (): ReactElement =>
    createElement("div", { "data-testid": "chat-view-mock" }),
}));

vi.mock("../features/agents", () => ({
  default: (): ReactElement =>
    createElement("div", { "data-testid": "agents-view-mock" }),
}));

vi.mock("../features/settings", () => ({
  default: (): ReactElement =>
    createElement("div", { "data-testid": "settings-view-mock" }),
}));

vi.mock("../features/scheduler", () => ({
  default: (): ReactElement =>
    createElement("div", { "data-testid": "scheduler-view-mock" }),
}));

vi.mock("../features/knowledge", () => ({
  default: (): ReactElement =>
    createElement("div", { "data-testid": "knowledge-view-mock" }),
}));

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

const sessions: Session[] = [
  {
    ID: "s1",
    AgentID: "a1",
    Title: "First",
    Model: "",
    Effort: "",
    CreatedAt: "2026-01-01T00:00:00Z",
    LastActivityAt: "2026-01-01T00:00:00Z",
  },
  {
    ID: "s2",
    AgentID: "a1",
    Title: "Second",
    Model: "",
    Effort: "",
    CreatedAt: "2026-01-02T00:00:00Z",
    LastActivityAt: "2026-01-02T00:00:00Z",
  },
  {
    ID: "s3",
    AgentID: "a1",
    Title: "Third",
    Model: "",
    Effort: "",
    CreatedAt: "2026-01-03T00:00:00Z",
    LastActivityAt: "2026-01-03T00:00:00Z",
  },
];

function railAside(): HTMLElement {
  // SAFETY: the shell's nav rail is the aside carrying data-testid nav-rail
  // (design D7: the rail is a flat canvas column without a glass-chrome
  // card, so class-based lookup no longer applies).
  return document.querySelector('[data-testid="nav-rail"]') as HTMLElement;
}

function mobileBar(): HTMLElement {
  // SAFETY: the shell's mobile top bar hosts the only nav element in the
  // header island; the desktop rail is an aside, so the query cannot hit it.
  return document.querySelector("header nav") as HTMLElement;
}

function railNav(): HTMLElement {
  // SAFETY: the rail aside hosts the only nav descendant (the bar's nav
  // lives in the header), so the query cannot hit the mobile bar.
  return railAside().querySelector("nav") as HTMLElement;
}

async function clickToggle(): Promise<void> {
  await act(async () => {
    fireEvent.click(screen.getByTestId("btn-toggle-rail"));
  });
}

beforeEach(() => {
  window.localStorage.clear();
  useChatStore.setState({
    agents: [agent],
    activeAgentID: "a1",
    activeSessionID: "s1",
    sessions,
    busy: false,
  });
  useNavStore.setState({
    menu: "chat",
    chatTarget: null,
    settingsPaneLabel: "Providers",
  });
  usePlatformStore.setState({ isDesktopApp: false, isMac: false });
});

describe("App shell", () => {
  it("renders a textless chat strip with no interactive elements", async () => {
    const view = await renderUI(<App />);
    const strip = screen.getByTestId("titlebar-strip");
    // The strip is a quiet drag-zone band on chat: no copy — the header
    // below and the sessions rail carry the live state.
    expect(strip.textContent).not.toContain("Chat");
    expect(strip.textContent).not.toContain("sessions");
    expect(strip.querySelector("button")).toBeNull();
    expect(strip.querySelector("input")).toBeNull();
    await view.unmount();
  });

  it("keeps the view title on the agents strip", async () => {
    const view = await renderUI(<App />);
    await act(async () => {
      useNavStore.setState({ menu: "agents" });
    });
    const strip = screen.getByTestId("titlebar-strip");
    expect(strip.textContent).toContain("Agents");
    expect(strip.textContent).not.toContain("sessions");
    await view.unmount();
  });

  it("renders the schedules rail item and swaps the pane when activated", async () => {
    const view = await renderUI(<App />);
    expect(railAside().textContent).toContain("Schedules");

    await act(async () => {
      useNavStore.setState({ menu: "schedules" });
    });

    const main = document.querySelector("main");
    expect(main?.getAttribute("data-active-menu")).toBe("schedules");
    expect(screen.getByTestId("scheduler-view-mock")).toBeTruthy();
    expect(screen.getByTestId("titlebar-strip").textContent).toContain(
      "Schedules"
    );
    await view.unmount();
  });

  it("renders the knowledge rail item and swaps the pane when activated", async () => {
    const view = await renderUI(<App />);
    expect(railAside().textContent).toContain("Knowledge");

    await act(async () => {
      useNavStore.setState({ menu: "knowledge" });
    });

    const main = document.querySelector("main");
    expect(main?.getAttribute("data-active-menu")).toBe("knowledge");
    expect(screen.getByTestId("knowledge-view-mock")).toBeTruthy();
    expect(screen.getByTestId("titlebar-strip").textContent).toContain(
      "Knowledge"
    );
    await view.unmount();
  });

  it("keeps the schedules item clickable in the collapsed rail", async () => {
    const view = await renderUI(<App />);
    await clickToggle();

    // Collapsed rows are icon-only: the name moves onto the button.
    expect(railAside().textContent).not.toContain("Schedules");

    await act(async () => {
      fireEvent.click(
        within(railAside()).getByRole("button", { name: "Schedules" })
      );
    });

    expect(
      document.querySelector("main")?.getAttribute("data-active-menu")
    ).toBe("schedules");
    expect(screen.getByTestId("scheduler-view-mock")).toBeTruthy();
    await view.unmount();
  });

  it("carries the schedules destination in the mobile bar", async () => {
    const view = await renderUI(<App />);

    await act(async () => {
      fireEvent.click(
        within(mobileBar()).getByRole("button", { name: "Schedules" })
      );
    });

    expect(
      document.querySelector("main")?.getAttribute("data-active-menu")
    ).toBe("schedules");
    await view.unmount();
  });

  it("mounts settings inside the island: rail visible, strip title shown, chat replaced and restored on return", async () => {
    const view = await renderUI(<App />);
    expect(screen.getByTestId("chat-session-rail")).toBeTruthy();
    expect(railAside()).toBeTruthy();

    await act(async () => {
      useNavStore.setState({ menu: "settings" });
    });

    // Rail remains visible when viewing settings
    expect(railAside()).toBeTruthy();

    // Strip shows "Settings" title
    const strip = screen.getByTestId("titlebar-strip");
    expect(strip.textContent).toContain("Settings");
    expect(strip.textContent).not.toContain("Chat");

    // Chat layer is absent, replaced by settings view
    expect(screen.queryByTestId("chat-session-rail")).toBeNull();
    expect(screen.getByTestId("settings-view-mock")).toBeTruthy();

    // Switching back to chat restores chat layer and active session
    await act(async () => {
      useNavStore.setState({ menu: "chat" });
    });
    expect(screen.getByTestId("chat-session-rail")).toBeTruthy();
    expect(useChatStore.getState().activeSessionID).toBe("s1");
    await view.unmount();
  });

  it("reveals the session column on the shared motion grammar without gating the rail", async () => {
    const view = await renderUI(<App />);

    // ui-conventions (structural chrome): the session column rides the
    // existing od-fade onset — the shared grammar (--motion-base +
    // --ease-standard) — as the chat view mounts.
    const reveal = screen.getByTestId("session-column-reveal");
    expect(reveal.className).toContain("od-fade");
    // Opacity only: no transition on inline-size that could fight the
    // rail's own 1:1 drag resize.
    expect(reveal.className).not.toContain("transition-[width]");
    // Structural, not lazy: the rail's content is queryable immediately —
    // no delayed mount behind the reveal.
    expect(screen.getByTestId("chat-session-rail")).toBeTruthy();
    expect(screen.getByTestId("session-search")).toBeTruthy();

    await view.unmount();
  });

  it("collapses the rail to icons and persists the preference", async () => {
    const view = await renderUI(<App />);
    expect(window.localStorage.getItem("buddies.railCollapsed")).toBeNull();

    // Design D8: the w-20 ↔ w-56 collapse animates the rail's inline width
    // on the shared motion grammar (--motion-base 200ms, --ease-standard)
    // instead of snapping; reduced motion collapses it globally, so no
    // branch is asserted here.
    expect(railAside().className).toContain("transition-[width]");
    expect(railAside().className).toContain("duration-[var(--motion-base)]");
    expect(railAside().className).toContain("ease-[var(--ease-standard)]");

    await clickToggle();
    // Collapsed width fits the macOS traffic lights that overlay the rail's
    // top-left corner (80px island).
    expect(railAside().className).toContain("w-20");
    // Scoped to the rail: the below-lg bar island keeps its (CSS-hidden)
    // labels by contract.
    expect(railAside().textContent).not.toContain("Agents");
    // The expand control moved above the sessions rail's label (the real
    // SessionRail renders here), so the nav rail keeps no toggle of its own.
    expect(
      railAside().querySelector('[data-testid="btn-toggle-rail"]')
    ).toBeNull();
    expect(
      screen
        .getByTestId("chat-session-rail")
        .querySelector('[data-testid="btn-toggle-rail"]')
    ).toBeTruthy();
    expect(window.localStorage.getItem("buddies.railCollapsed")).toBe("1");

    await act(async () => {
      useNavStore.getState().toggleRail();
    });
    expect(railAside().className).not.toContain("w-20");
    expect(railAside().textContent).toContain("Agents");
    expect(window.localStorage.getItem("buddies.railCollapsed")).toBe("0");
    await view.unmount();
  });

  it("restores the collapsed rail from storage on a fresh mount", async () => {
    window.localStorage.setItem("buddies.railCollapsed", "1");
    const view = await renderUI(<App />);
    expect(railAside().className).toContain("w-20");
    await view.unmount();
  });

  // Task 2.5: the rail theme control reads as a control, not a status — its
  // visible label and accessible name both carry the CURRENT preference
  // (ThemeCycleButton's contract, onclaw parity), and cycling from the rail
  // keeps them in agreement while walking light → dark → system.
  it("rail theme control: label and aria-label agree and cycle together", async () => {
    const view = await renderUI(<App />);
    const button = screen.getByTestId("rail-theme");

    expect(button.textContent).toContain("Theme: system");
    expect(button.getAttribute("aria-label")).toContain("Theme: system");

    await act(async () => {
      fireEvent.click(button);
    });
    expect(button.textContent).toContain("Theme: light");
    expect(button.getAttribute("aria-label")).toContain("Theme: light");

    await act(async () => {
      fireEvent.click(button);
    });
    expect(button.textContent).toContain("Theme: dark");
    expect(button.getAttribute("aria-label")).toContain("Theme: dark");
    await view.unmount();
  });

  it("shows the web wordmark on the strip and omits it on desktop", async () => {
    const view = await renderUI(<App />);
    expect(screen.getByTestId("titlebar-strip").textContent).toContain(
      "buddies"
    );
    await view.unmount();

    usePlatformStore.setState({ isDesktopApp: true, isMac: true });
    const desktop = await renderUI(<App />);
    expect(screen.getByTestId("titlebar-strip").textContent).not.toContain(
      "buddies"
    );
    await desktop.unmount();
  });
});

// The expanded rail's island pins the SideRow grammar (openspec
// replace-menu-island-git-helper, app-shell): a flat menu where selection is
// an accent-tint pill + aria-current — not a text-color/weight change — and
// the bottom cluster reads as menu rows, not a footer.
describe("App shell nav grammar", () => {
  it("renders the rail as a flat canvas column with no card of its own", async () => {
    const view = await renderUI(<App />);
    const rail = railAside();

    // Design D7 (git-helper's sidebar): the island is flush on the window
    // canvas — the content island stays the carded surface, not the rail.
    expect(rail.className).not.toContain("glass-chrome");
    expect(rail.className).not.toContain("border-line");
    expect(rail.className).not.toContain("bg-surface");
    expect(rail.className).not.toContain("rounded-lg");

    await view.unmount();
  });

  it("renders the expanded island as four flat icon rows with no headings", async () => {
    const view = await renderUI(<App />);
    const rows = within(railNav()).getAllByRole("button");

    // Exactly the four destinations: a group heading would be a non-button
    // child, so the exact count pins the flat grammar structurally.
    expect(rows).toHaveLength(4);
    expect(rows.map((row: HTMLElement) => row.textContent)).toEqual([
      "Chat",
      "Agents",
      "Schedules",
      "Knowledge",
    ]);

    for (const row of rows) {
      // SideRow geometry: 30px row, leading icon, truncated label span.
      expect(row.className).toContain("h-[30px]");
      expect(row.querySelector("svg")).toBeTruthy();
      const label = row.querySelector("span.truncate");
      expect(label).toBeTruthy();
      expect(label?.textContent?.length ?? 0).toBeGreaterThan(0);
    }

    await view.unmount();
  });

  it("marks the active destination with the accent pill, not ink changes", async () => {
    const view = await renderUI(<App />);
    const chatRow = within(railNav()).getByRole("button", { name: "Chat" });

    expect(chatRow.getAttribute("aria-current")).toBe("page");
    // The 15% accent pill carries selection; \b guards against the
    // text-fg/text-fg2 substring trap.
    expect(chatRow.className).toContain("color-mix(in_oklab,var(--accent)_15%");
    expect(chatRow.className).toMatch(/\btext-fg\b/);
    expect(chatRow.className).not.toContain("text-accenttext");
    expect(chatRow.className).not.toContain("font-semibold");

    const agentsRow = within(railNav()).getByRole("button", {
      name: "Agents",
    });

    expect(agentsRow.getAttribute("aria-current")).toBeNull();
    expect(agentsRow.className).not.toContain("var(--accent)_15%");
    // Idle rows only tint on hover.
    expect(agentsRow.className).toContain(
      "hover:bg-[color-mix(in_oklab,var(--fg)_6%"
    );
    await view.unmount();
  });

  it("styles the bottom cluster as menu rows and swaps the pane on Settings", async () => {
    const view = await renderUI(<App />);
    const theme = screen.getByTestId("rail-theme");
    const settings = screen.getByTestId("rail-settings");

    // Same 30px row geometry as the destination rows above.
    expect(theme.className).toContain("h-[30px]");
    expect(settings.className).toContain("h-[30px]");
    expect(settings.className).toContain("rounded-md");

    await act(async () => {
      fireEvent.click(settings);
    });

    expect(
      document.querySelector('main[data-active-menu="settings"]')
    ).toBeTruthy();
    expect(screen.getByTestId("settings-view-mock")).toBeTruthy();
    await view.unmount();
  });

  it("keeps knowledge and settings operable in the collapsed rail", async () => {
    const view = await renderUI(<App />);
    await clickToggle();

    // Collapsed rows are icon-only: the names move onto the buttons.
    expect(railAside().textContent).not.toContain("Knowledge");
    expect(railAside().textContent).not.toContain("Settings");

    const knowledge = within(railAside()).getByRole("button", {
      name: "Knowledge",
    });

    expect(
      within(railAside()).getByRole("button", { name: "Settings" })
    ).toBeTruthy();

    await act(async () => {
      fireEvent.click(knowledge);
    });

    expect(
      document.querySelector("main")?.getAttribute("data-active-menu")
    ).toBe("knowledge");
    await view.unmount();
  });
});
