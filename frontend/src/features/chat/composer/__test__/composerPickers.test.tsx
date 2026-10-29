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
import { renderComposer, seedStore, capabilities } from "./harness";

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

/** The RailPicker container around a trigger (its layout parent). */
function pickerOf(testId: string): HTMLElement {
  const trigger = screen.getByTestId(testId);

  // SAFETY: every rail trigger renders inside its RailPicker container.
  return trigger.parentElement as HTMLElement;
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

  it("the model trigger shows the family name and keeps the full id as its title (design D4)", async () => {
    useChatStore.setState({
      models: [
        ...capabilities,
        {
          ID: "anthropic/claude-sonnet-4-5",
          ContextWindow: 200000,
          Efforts: [],
          ImageInput: true,
          PDFInput: false,
          VideoInput: false,
          PDFParts: true,
          VideoParts: false,
        },
      ],
      modelOverride: "anthropic/claude-sonnet-4-5",
    });
    await renderComposer();

    // The family name is the trigger's whole label; the full id is the tooltip.
    const trigger = screen.getByTestId("composer-model-picker");

    expect(trigger.textContent).toBe("claude-sonnet-4-5");
    expect(trigger.getAttribute("title")).toBe("anthropic/claude-sonnet-4-5");

    // The picker menu keeps the full ids.
    await openMenu("composer-model-picker");

    expect(
      screen.getByTestId("composer-model-picker-menu").textContent
    ).toContain("anthropic/claude-sonnet-4-5");
  });

  it("squeezes by truncating the triggers, never wrapping their labels (live 1100px: pickers scrollWidth must fit clientWidth)", async () => {
    await renderComposer();

    // jsdom cannot measure layout, so this pins the classes the live check
    // depends on. Live measurement (smoke 2026-10-28, 1100×900): the pickers
    // rail was squeezed to 261px while its content needed 473px, and the
    // model label `laguna-s-2.1` wrapped mid-token onto two lines. The
    // shrink chain (min-w-0) must reach every trigger, and each trigger
    // must carry the no-wrap + ellipsis contract: `block` overrides the
    // vendored flex (a flex box cannot ellipsize its bare text node),
    // `truncate` = overflow-hidden + text-ellipsis + whitespace-nowrap.
    const root = screen.getByTestId("composer-pickers");

    expect(root.classList.contains("min-w-0")).toBe(true);

    for (const testId of [
      "composer-agent-picker",
      "composer-model-picker",
      "composer-effort-picker",
    ]) {
      const trigger = screen.getByTestId(testId);

      // `block` overrides the vendored flex (ellipsis needs a block box),
      // `truncate` is the no-wrap + ellipsis contract, `min-w-0` lets the
      // trigger shrink with the rail, and the chevron sits absolute inside
      // a pe-6 reserve — the ellipsis can never eat it.
      for (const cls of [
        "block",
        "relative",
        "min-w-0",
        "max-w-full",
        "truncate",
        "pe-6",
      ]) {
        expect(trigger.classList.contains(cls)).toBe(true);
      }
    }

    // The squeeze degrades longest-label-first: only the model picker
    // absorbs width (its container keeps the default shrink weight); the
    // agent and effort containers carry a near-zero shrink weight so their
    // short labels stay whole until the extreme. (Live 1100px: the first
    // cut truncated "Scout" and "high" too — three ellipses in a row.)
    expect(
      pickerOf("composer-model-picker").classList.contains("shrink-[0.01]")
    ).toBe(false);
    expect(
      pickerOf("composer-agent-picker").classList.contains("shrink-[0.01]")
    ).toBe(true);
    expect(
      pickerOf("composer-effort-picker").classList.contains("shrink-[0.01]")
    ).toBe(true);

    // The inherit/override markers carry state: they must not be squeezed
    // away by the shrinking triggers beside them.
    expect(
      screen
        .getByTestId("composer-model-picker-default")
        .classList.contains("shrink-0")
    ).toBe(true);
    expect(
      screen
        .getByTestId("composer-effort-picker-default")
        .classList.contains("shrink-0")
    ).toBe(true);
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
