/**
 * Composer picker suite (task 5.3, spec chat-view "Composer affordances"):
 * the agent picker switches the conversation context without rebinding the
 * viewed session, the model picker lists the session provider's models, and
 * the effort picker offers only the selected model's supported levels and
 * hides entirely when the model supports none. A pick persists on the session
 * (the override write) and the inherited value is marked "default".
 */
import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

// The client mock factory is imported FIRST on purpose: the hoisted
// `vi.mock` below runs the moment the chat store pulls the client module in,
// so the factory's module must already be evaluated by then.
import { makeClient, type MockedClient } from "./clientMock";
import type { Session } from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { useChatStore } from "../../store";
import { useNavStore } from "../../../../stores/nav";
import { renderComposer, seedStore } from "./harness";

vi.mock("../../../../lib/client", () => ({ client: makeClient() }));

import { client } from "../../../../lib/client";

// SAFETY: the client module is mocked with makeClient(), so its runtime shape
// is exactly that mock object.
const mockedClient = client as MockedClient;

/** Opens a rail menu by its trigger test id. */
async function openMenu(testId: string): Promise<void> {
  await act(async () => {
    fireEvent.click(screen.getByTestId(testId));
  });
}

/** Clicks a rail row and lets the async store action settle. */
async function pickOption(testId: string): Promise<void> {
  await act(async () => {
    fireEvent.mouseDown(screen.getByTestId(testId));
    await Promise.resolve();
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  window.localStorage.clear();
  useNavStore.setState({ menu: "chat", chatTarget: null });
  seedStore();
});

describe("the composer pickers (task 5.3)", () => {
  it("shows the effective model and effort marked default while inherited", async () => {
    await renderComposer();

    const model = screen.getByTestId("composer-model-picker");

    expect(model.textContent).toContain("m-pro");
    expect(screen.queryByTestId("composer-model-picker-default")).toBeTruthy();

    const effort = screen.getByTestId("composer-effort-picker");

    expect(effort.textContent).toContain("high");
    expect(screen.queryByTestId("composer-effort-picker-default")).toBeTruthy();
  });

  it("hides the effort picker entirely when the model supports none", async () => {
    useChatStore.setState({ modelOverride: "m-base" });
    await renderComposer();

    expect(screen.queryByTestId("composer-effort-picker")).toBeNull();
  });

  it("offers only the selected model's effort options", async () => {
    await renderComposer();
    await openMenu("composer-effort-picker");

    const menu = screen.getByTestId("composer-effort-picker-menu");

    expect(menu.textContent).toContain("low");
    expect(menu.textContent).toContain("medium");
    expect(menu.textContent).toContain("high");
    // The agent-default row plus exactly the capability set — nothing else.
    expect(
      menu.querySelectorAll("[data-testid^='composer-effort-option-']")
    ).toHaveLength(1);
  });

  it("lists the session provider's models", async () => {
    await renderComposer();
    await openMenu("composer-model-picker");

    const menu = screen.getByTestId("composer-model-picker-menu");

    expect(menu.textContent).toContain("m-pro");
    expect(menu.textContent).toContain("m-base");
  });

  it("persists a model pick through the store and drops the default marker", async () => {
    await renderComposer();
    await openMenu("composer-model-picker");

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /m-base/ }));
    });

    await waitFor(() =>
      expect(mockedClient.chat.setSessionOverrides).toHaveBeenCalledWith(
        "s1",
        "m-base",
        ""
      )
    );
    expect(useChatStore.getState().modelOverride).toBe("m-base");
    // The pick landed on the session row too (design D1).
    expect(
      useChatStore
        .getState()
        .sessions.find((s: Session): boolean => s.ID === "s1")?.Model
    ).toBe("m-base");

    // Switching to a model without efforts cleared the inherited effort
    // (design D2), the marker is gone, and the effort control hides.
    expect(useChatStore.getState().effortOverride).toBe("");
    await waitFor(() =>
      expect(screen.queryByTestId("composer-effort-picker")).toBeNull()
    );
    expect(screen.getByTestId("composer-model-picker").textContent).toContain(
      "m-base"
    );
    expect(screen.queryByTestId("composer-model-picker-default")).toBeNull();
  });

  it("persists an effort pick alongside the current model override", async () => {
    await renderComposer();
    await openMenu("composer-effort-picker");

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "low" }));
    });

    await waitFor(() =>
      expect(mockedClient.chat.setSessionOverrides).toHaveBeenCalledWith(
        "s1",
        "",
        "low"
      )
    );
    expect(useChatStore.getState().effortOverride).toBe("low");
    expect(
      useChatStore
        .getState()
        .sessions.find((s: Session): boolean => s.ID === "s1")?.Effort
    ).toBe("low");
    expect(screen.queryByTestId("composer-effort-picker-default")).toBeNull();
  });

  it("the agent-default row clears the override back to inherit", async () => {
    useChatStore.setState({ modelOverride: "m-base" });
    await renderComposer();

    await openMenu("composer-model-picker");
    await pickOption("composer-model-option-default");

    await waitFor(() =>
      expect(mockedClient.chat.setSessionOverrides).toHaveBeenCalledWith(
        "s1",
        "",
        ""
      )
    );
    expect(useChatStore.getState().modelOverride).toBe("");
    // The effective model reverts to the agent's configured default, marked
    // inherited again.
    await waitFor(() =>
      expect(screen.getByTestId("composer-model-picker").textContent).toContain(
        "m-pro"
      )
    );
    expect(screen.queryByTestId("composer-model-picker-default")).toBeTruthy();
  });

  it("the agent picker switches the chat context; the viewed session stays bound", async () => {
    await renderComposer();

    await openMenu("composer-agent-picker");
    await pickOption("composer-agent-option-a2");

    // The chat target carries the switch; the viewed session is untouched.
    await waitFor(() =>
      expect(useNavStore.getState().chatTarget?.agentID).toBe("a2")
    );
    expect(mockedClient.chat.setSessionOverrides).not.toHaveBeenCalled();
    expect(
      useChatStore
        .getState()
        .sessions.find((s: Session): boolean => s.ID === "s1")?.AgentID
    ).toBe("a1");
  });
});
