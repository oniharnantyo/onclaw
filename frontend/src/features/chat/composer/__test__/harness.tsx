/**
 * Shared harness for the composer suites (task 5.x): a seeded chat store and
 * the composer mounted inside the runtime provider — the same wiring the chat
 * view uses (the view mounts `ChatRuntimeProvider` around the chat column,
 * the composer inside it).
 *
 * The mocked client lives in `./clientMock` (a leaf module) so the hoisted
 * `vi.mock` factory in each suite can call it before the store is evaluated.
 */
import { screen } from "@testing-library/dom";

import type { Skill } from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { renderUI } from "../../__test__/test-utils";
import { ChatRuntimeProvider } from "../../runtime/ChatRuntimeProvider";
import { useChatStore } from "../../store";
import { ChatComposer } from "../ChatComposer";
import { capabilities, runner, scout, session, skills } from "./clientMock";

export { capabilities, runner, scout, session, skills };

/** Seeds the store the way the chat view finds it after a load. */
export function seedStore(
  overrides: Partial<ReturnType<typeof useChatStore.getState>> = {}
): void {
  useChatStore.setState({
    agents: [scout, runner],
    providers: [],
    skills: skills.filter((s: Skill) => s.Enabled),
    activeAgentID: "a1",
    sessions: [session],
    activeSessionID: "s1",
    models: capabilities,
    modelOverride: "",
    effortOverride: "",
    entries: [],
    usage: null,
    loadingSession: false,
    error: null,
    busy: false,
    dispatching: false,
    activeTurnID: null,
    finishedTurnIDs: new Set<string>(),
    queue: [],
    sidebarCollapsed: false,
    panelTabs: [],
    ...overrides,
  });
}

/** Mounts the composer inside the runtime provider — the chat view's wiring. */
export async function renderComposer(): Promise<{
  unmount: () => Promise<void>;
}> {
  const { unmount } = await renderUI(
    <ChatRuntimeProvider>
      <ChatComposer />
    </ChatRuntimeProvider>
  );

  return { unmount };
}

/** The composer's textarea. */
export function composerInput(): HTMLTextAreaElement {
  // SAFETY: the chat composer renders as a textarea by contract.
  return screen.getByTestId("chat-composer") as HTMLTextAreaElement;
}
