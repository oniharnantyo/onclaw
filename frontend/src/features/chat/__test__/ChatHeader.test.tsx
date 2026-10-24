/**
 * Chat header tests (task 6.3): the header is a title bar — it leads with
 * the active session's title (the rail's "Untitled session" convention; the
 * agent's name only before a session exists) and follows the store's
 * selection, and nothing else: no status vocabulary (the sessions rail's
 * running dots and the transcript's approvals own lifecycle) and no model
 * chip. The right sidebar's toggle stays hosted here — the sidebar hides
 * entirely when minimized, so the header is its only affordance — and the
 * token-grammar classes both themes ride are pinned.
 */
import { act } from "react";
import { fireEvent, screen } from "@testing-library/dom";
import { beforeEach, describe, expect, it } from "vite-plus/test";

import type {
  Agent,
  Session,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { ChatHeader } from "../components/ChatHeader";
import { useChatStore } from "../store";
import { renderUI } from "./test-utils";

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

const session: Session = {
  ID: "s1",
  AgentID: "a1",
  Title: "Weekly notes",
  Model: "",
  Effort: "",
  CreatedAt: "2026-01-01T00:00:00Z",
  LastActivityAt: "2026-01-01T00:00:00Z",
};

function seedStore(): void {
  useChatStore.setState({
    agents: [agent],
    sessions: [session],
    activeAgentID: "a1",
    activeSessionID: "s1",
    busy: false,
    entries: [],
  });
}

beforeEach(() => {
  useChatStore.setState({
    agents: [],
    sessions: [],
    activeAgentID: null,
    activeSessionID: null,
    busy: false,
    entries: [],
  });
  seedStore();
});

describe("ChatHeader", () => {
  it("leads with the active session's title and nothing else", async () => {
    await renderUI(<ChatHeader />);
    expect(screen.getByTestId("chat-header-session-title").textContent).toBe(
      "Weekly notes"
    );
    // Title bar only: no status vocabulary, no model chip.
    expect(screen.queryByTestId("chat-header-status")).toBeNull();
    expect(screen.queryByTestId("chat-header-model")).toBeNull();
  });

  it("follows the store's session selection", async () => {
    await renderUI(<ChatHeader />);

    await act(async () => {
      useChatStore.setState({
        sessions: [
          ...useChatStore.getState().sessions,
          {
            ...session,
            ID: "s2",
            Title: "Retro planning",
          },
        ],
        activeSessionID: "s2",
      });
    });

    expect(screen.getByTestId("chat-header-session-title").textContent).toBe(
      "Retro planning"
    );
  });

  it("falls back to the agent's name before any session exists", async () => {
    useChatStore.setState({ activeSessionID: null, sessions: [] });
    await renderUI(<ChatHeader />);
    expect(screen.getByTestId("chat-header-session-title").textContent).toBe(
      "Scout"
    );
  });

  it("names an untitled session after the rail's convention", async () => {
    useChatStore.setState({ sessions: [{ ...session, Title: "" }] });
    await renderUI(<ChatHeader />);
    expect(screen.getByTestId("chat-header-session-title").textContent).toBe(
      "Untitled session"
    );
  });

  it("rides the token grammar so both themes re-skin it", async () => {
    await renderUI(<ChatHeader />);
    const el = screen.getByTestId("chat-header");

    expect(el.className).toContain("bg-surface");
    expect(el.className).toContain("border-line");
  });

  // The right bar hides entirely when minimized, so the header hosts its
  // toggle: the accessible name states the CURRENT state and clicking flips
  // the store flag the RightBar renders from.
  it("hosts the sidebar toggle and flips the store state", async () => {
    await renderUI(<ChatHeader />);
    const button = screen.getByTestId("btn-toggle-sidebar");

    expect(button.getAttribute("aria-label")).toBe("Collapse sidebar");

    await act(async () => {
      fireEvent.click(button);
    });
    expect(useChatStore.getState().sidebarCollapsed).toBe(true);
    expect(button.getAttribute("aria-label")).toBe("Expand sidebar");

    await act(async () => {
      fireEvent.click(button);
    });
    expect(useChatStore.getState().sidebarCollapsed).toBe(false);
  });
});
