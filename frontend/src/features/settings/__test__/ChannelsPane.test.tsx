import { act } from "react";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";
import userEvent from "@testing-library/user-event";
import type {
  Agent,
  ChannelPairing,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type {
  ChannelChatView,
  ChannelStateEvent,
  ChannelView,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { Client, ChannelsStateHandler } from "../../../lib/client";

import ChannelsPane from "../components/ChannelsPane";
import type { SettingsPaneConfig } from "../types";
import { renderUI, setInput } from "./test-utils";

/** Header config passed to the pane; mirrors the static nav entry. */
const pane: SettingsPaneConfig = {
  key: "channels",
  label: "Channels",
  icon: "message",
  description: "Connect the Telegram bot.",
};

let channel: ChannelView;

let chats: ChannelChatView[];

let pairings: ChannelPairing[];

let agents: Agent[];

/** Handlers captured from the event subscriptions; a test fires them to
 * simulate the backend's channels:changed / channels:state pushes. */
const changedHandlers: (() => void)[] = [];

const stateHandlers: ChannelsStateHandler[] = [];

vi.mock("../../../lib/client", () => ({
  client: {
    agents: {
      list: vi.fn(async (): Promise<Agent[]> => [...agents]),
    },
    channels: {
      list: vi.fn(async (): Promise<ChannelView[]> => [{ ...channel }]),
      saveChannel: vi.fn(
        async (
          _id: string,
          input: { BotToken: string }
        ): Promise<ChannelView> => setTokenOn(input.BotToken)
      ),
      setEnabled: vi.fn(
        async (_id: string, enabled: boolean): Promise<ChannelView> => {
          channel = { ...channel, enabled };

          return { ...channel };
        }
      ),
      chats: vi.fn(async (): Promise<ChannelChatView[]> => [...chats]),
      pendingPairings: vi.fn(async (): Promise<ChannelPairing[]> => [
        ...pairings,
      ]),
      approvePairing: vi.fn(async (_id: number): Promise<void> => {
        pairings = [];
      }),
      dismissPairing: vi.fn(async (_id: number): Promise<void> => {
        pairings = [];
      }),
      setChatPinnedAgent: vi.fn(
        async (chatID: number, agentID: string): Promise<ChannelChatView> => {
          // SAFETY: the fixture list the test set always holds the chat id.
          const found = chats.find(
            (c: ChannelChatView): boolean => c.chatId === chatID
          );

          const next: ChannelChatView = { ...found!, pinnedAgentId: agentID };

          chats = chats.map((c: ChannelChatView) =>
            c.chatId === chatID ? next : c
          );

          return next;
        }
      ),
      setHomeChat: vi.fn(async (chatID: number): Promise<ChannelChatView> => {
        chats = chats.map((c: ChannelChatView) => ({
          ...c,
          isHome: c.chatId === chatID,
        }));

        // SAFETY: the fixture just mapped the row; the id is present.
        return { ...chats.find((c: ChannelChatView) => c.chatId === chatID)! };
      }),
      setChatFreeResponse: vi.fn(
        async (chatID: number, free: boolean): Promise<ChannelChatView> => {
          chats = chats.map((c: ChannelChatView) =>
            c.chatId === chatID ? { ...c, freeResponse: free } : c
          );

          // SAFETY: the fixture just mapped the row; the id is present.
          return {
            ...chats.find((c: ChannelChatView) => c.chatId === chatID)!,
          };
        }
      ),
    },
    events: {
      onChannelsChanged: vi.fn((handler: () => void) => {
        changedHandlers.push(handler);

        return (): void => undefined;
      }),
      onChannelsState: vi.fn((handler: ChannelsStateHandler) => {
        stateHandlers.push(handler);

        return (): void => undefined;
      }),
    },
  },
}));

import { client } from "../../../lib/client";

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  agents: { list: MockFn };
  channels: {
    list: MockFn;
    saveChannel: MockFn;
    setEnabled: MockFn;
    chats: MockFn;
    pendingPairings: MockFn;
    approvePairing: MockFn;
    dismissPairing: MockFn;
    setChatPinnedAgent: MockFn;
    setHomeChat: MockFn;
    setChatFreeResponse: MockFn;
  };
  events: {
    onChannelsChanged: MockFn;
    onChannelsState: MockFn;
  };
};

// SAFETY: the client module is mocked with vi.fn members above.
const mockedClient = client as MockedClient;

function setTokenOn(token: string): ChannelView {
  // The backend's blank-token rule: "" keeps the stored token (task 1.4);
  // only a non-blank input replaces it.
  if (token !== "") {
    channel = { ...channel, tokenSet: true, tokenHint: token.slice(-4) };
  }

  return { ...channel };
}

function makeAgent(patch: Partial<Agent> = {}): Agent {
  return {
    ID: "a1",
    Name: "Scout",
    Slug: "scout",
    Description: "",
    Brief: "",
    Avatar: null,
    PromptsStatus: "",
    PromptsError: "",
    ProviderID: "p1",
    Model: "m1",
    Effort: "",
    Temperature: 0,
    MaxTokens: 0,
    ContextWindow: 0,
    Tools: null,
    Skills: null,
    MCPServers: null,
    SubagentsEnabled: true,
    CreatedAt: "",
    UpdatedAt: "",
    ...patch,
  };
}

function makeChannel(patch: Partial<ChannelView> = {}): ChannelView {
  return {
    id: "telegram",
    enabled: true,
    tokenSet: false,
    tokenHint: "",
    proxyUrl: "",
    createdAt: "",
    updatedAt: "",
    ...patch,
  };
}

function makeChat(patch: Partial<ChannelChatView>): ChannelChatView {
  return {
    chatId: 101,
    kind: "dm",
    title: "Alice",
    sessionId: "s1",
    pinnedAgentId: "",
    isHome: false,
    freeResponse: false,
    ...patch,
  };
}

/** Fires the backend's channels:changed push (every mutation announces). */
async function emitChanged(): Promise<void> {
  await act(async () => {
    for (const handler of changedHandlers) handler();
  });
}

/** Fires one channels:state push through the captured subscription. */
async function emitState(event: ChannelStateEvent): Promise<void> {
  await act(async () => {
    for (const handler of stateHandlers) handler(event);
  });
}

describe("ChannelsPane", () => {
  beforeEach(() => {
    channel = makeChannel();
    chats = [];
    pairings = [];
    agents = [];
    changedHandlers.length = 0;
    stateHandlers.length = 0;
    vi.clearAllMocks();
  });

  it("renders the channel card with its brand mark, name, and enable toggle", async () => {
    const { findByTestId } = await renderUI(<ChannelsPane pane={pane} />);

    expect(await findByTestId("channel-card-telegram")).toBeTruthy();
    expect((await findByTestId("channel-name-telegram")).textContent).toBe(
      "Telegram"
    );
    expect(await findByTestId("channel-enabled-telegram")).toBeTruthy();
  });

  it("shows the disabled chip for a disabled channel before any state event", async () => {
    channel = makeChannel({ enabled: false });

    const { findByTestId } = await renderUI(<ChannelsPane pane={pane} />);

    expect((await findByTestId("channel-state-chip")).textContent).toBe(
      "disabled"
    );
  });

  it("renders the write-only token field blank with the stored-token hint line", async () => {
    channel = makeChannel({ tokenSet: true, tokenHint: "1234" });

    const { findByTestId } = await renderUI(<ChannelsPane pane={pane} />);

    // SAFETY: the testid names the pane's kit Input, an HTMLInputElement.
    const token = (await findByTestId("channel-token")) as HTMLInputElement;
    expect(token.value).toBe("");
    expect(token.getAttribute("type")).toBe("password");
    expect((await findByTestId("channel-token-hint")).textContent).toContain(
      "••••1234"
    );
  });

  it("keeps the stored token when saved with the field left blank", async () => {
    channel = makeChannel({ tokenSet: true, tokenHint: "1234" });
    const user = userEvent.setup();

    const { findByTestId, getByTestId } = await renderUI(
      <ChannelsPane pane={pane} />
    );

    await findByTestId("channel-token-hint");
    await user.click(getByTestId("channel-token-save"));

    expect(mockedClient.channels.saveChannel).toHaveBeenCalledWith("telegram", {
      Enabled: true,
      BotToken: "",
      ProxyURL: "",
    });
    // The hint survives: a blank save keeps the stored token.
    expect((await findByTestId("channel-token-hint")).textContent).toContain(
      "••••1234"
    );
  });

  it("submits a typed token and clears the field after the save", async () => {
    const user = userEvent.setup();

    const { findByTestId, getByTestId } = await renderUI(
      <ChannelsPane pane={pane} />
    );

    const token = await findByTestId("channel-token");
    await setInput(token, "12:ABC_new");
    await user.click(getByTestId("channel-token-save"));

    expect(mockedClient.channels.saveChannel).toHaveBeenCalledWith("telegram", {
      Enabled: true,
      BotToken: "12:ABC_new",
      ProxyURL: "",
    });

    // Write-only: the field never echoes the secret back.
    // SAFETY: the testid names the pane's kit Input, an HTMLInputElement.
    const cleared = (await findByTestId("channel-token")) as HTMLInputElement;
    expect(cleared.value).toBe("");
  });

  it("toggles the channel through setEnabled", async () => {
    const user = userEvent.setup();

    const { findByTestId } = await renderUI(<ChannelsPane pane={pane} />);

    const toggle = await findByTestId("channel-enabled-telegram");
    await user.click(toggle);

    expect(mockedClient.channels.setEnabled).toHaveBeenCalledWith(
      "telegram",
      false
    );
  });

  it("shows the connection-state chip from the channels:state event", async () => {
    const { findByTestId, queryByTestId } = await renderUI(
      <ChannelsPane pane={pane} />
    );

    // Push-only: no chip before the backend's first state report.
    expect(queryByTestId("channel-state-chip")).toBeNull();

    await emitState({
      channelId: "telegram",
      state: "error",
      detail: "token revoked",
    });

    expect((await findByTestId("channel-state-chip")).textContent).toBe(
      "error"
    );
    expect((await findByTestId("channel-state-detail")).textContent).toBe(
      "token revoked"
    );
  });

  it("allows a pending pairing and refetches on the changed event", async () => {
    pairings = [{ TelegramUserID: 42, DisplayName: "Dana", RequestedAt: "" }];
    const user = userEvent.setup();

    const { findByTestId, getByTestId } = await renderUI(
      <ChannelsPane pane={pane} />
    );

    const card = await findByTestId("pairing-card-42");
    expect(card.textContent).toContain("Dana");
    expect((await findByTestId("pairing-id-42")).textContent).toBe("42");

    await user.click(getByTestId("pairing-allow-42"));

    expect(mockedClient.channels.approvePairing).toHaveBeenCalledWith(42);
    // The refresh rides channels:changed: the list read runs again.
    await emitChanged();
    expect(mockedClient.channels.list).toHaveBeenCalledTimes(2);
    expect(await findByTestId("channels-pairings-empty")).toBeTruthy();
  });

  it("dismisses a pending pairing without allowlisting", async () => {
    pairings = [{ TelegramUserID: 42, DisplayName: "Dana", RequestedAt: "" }];
    const user = userEvent.setup();

    const { findByTestId, getByTestId } = await renderUI(
      <ChannelsPane pane={pane} />
    );

    await findByTestId("pairing-card-42");
    await user.click(getByTestId("pairing-dismiss-42"));

    expect(mockedClient.channels.dismissPairing).toHaveBeenCalledWith(42);
    expect(mockedClient.channels.approvePairing).not.toHaveBeenCalled();

    await emitChanged();
    expect(await findByTestId("channels-pairings-empty")).toBeTruthy();
  });

  it("renders kind badges and the free-response toggle for groups only", async () => {
    chats = [
      makeChat({ chatId: 101, kind: "dm", title: "Alice" }),
      makeChat({ chatId: 202, kind: "group", title: "Ops" }),
    ];

    const { findByTestId, queryByTestId } = await renderUI(
      <ChannelsPane pane={pane} />
    );

    expect((await findByTestId("chat-kind-101")).textContent).toBe("dm");
    expect((await findByTestId("chat-kind-202")).textContent).toBe("group");

    // The unaddressed-message gate is a group concern only.
    expect(await findByTestId("chat-free-202")).toBeTruthy();
    expect(queryByTestId("chat-free-101")).toBeNull();
  });

  it("pins an agent from the per-chat select", async () => {
    agents = [makeAgent()];
    chats = [makeChat({ chatId: 101 })];
    const user = userEvent.setup();

    const { findByTestId } = await renderUI(<ChannelsPane pane={pane} />);

    await user.selectOptions(await findByTestId("chat-pin-101"), "a1");

    expect(mockedClient.channels.setChatPinnedAgent).toHaveBeenCalledWith(
      101,
      "a1"
    );
  });

  it("keeps the home radio exclusive: marking one chat clears the other", async () => {
    chats = [
      makeChat({ chatId: 101, isHome: true }),
      makeChat({ chatId: 202, isHome: false }),
    ];
    const user = userEvent.setup();

    const { findByTestId } = await renderUI(<ChannelsPane pane={pane} />);

    // SAFETY: the testid names the pane's native radio input.
    const home101 = (await findByTestId("chat-home-101")) as HTMLInputElement;
    // SAFETY: the testid names the pane's native radio input.
    const home202 = (await findByTestId("chat-home-202")) as HTMLInputElement;
    expect(home101.checked).toBe(true);
    expect(home202.checked).toBe(false);

    await user.click(home202);

    // Exactly one call: the store clears every other row (design D13).
    expect(mockedClient.channels.setHomeChat).toHaveBeenCalledTimes(1);
    expect(mockedClient.channels.setHomeChat).toHaveBeenCalledWith(202);

    // SAFETY: the testid names the pane's native radio input.
    const flipped101 = (await findByTestId(
      "chat-home-101"
    )) as HTMLInputElement;

    // SAFETY: the testid names the pane's native radio input.
    const flipped202 = (await findByTestId(
      "chat-home-202"
    )) as HTMLInputElement;

    expect(flipped101.checked).toBe(false);
    expect(flipped202.checked).toBe(true);
  });
});
