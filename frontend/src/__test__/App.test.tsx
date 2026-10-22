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
  // SAFETY: the shell's rail is the only glass-chrome aside (the sessions
  // rail renders without it); located by class because the collapse toggle
  // no longer lives inside the rail when it is collapsed.
  return document.querySelector("aside.glass-chrome") as HTMLElement;
}

function mobileBar(): HTMLElement {
  // SAFETY: the shell's mobile top bar hosts the only nav element in the
  // header island; the desktop rail is an aside, so the query cannot hit it.
  return document.querySelector("header nav") as HTMLElement;
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

  it("collapses the rail to icons and persists the preference", async () => {
    const view = await renderUI(<App />);
    expect(window.localStorage.getItem("buddies.railCollapsed")).toBeNull();

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
