/**
 * Group mention picker tests (add-agent-groups task 5.4, spec agent-groups
 * "Mention picker"): in a group conversation the composer's `@` menu offers
 * the group's members and the `@everyone` entry (never the plain agents
 * list); picking a member inserts the literal `@Name` text and records the
 * explicit recipient, and picking everyone records none — the send then
 * routes through the groups binding with that recipient (design D4).
 */
import { act } from "react";
import { waitFor } from "@testing-library/dom";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type { TriggerItem } from "@assistant-ui/react";

import type {
  Agent,
  AgentGroup,
  AgentGroupMember,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { Client } from "../../../lib/client";
import { mockedSurface } from "../../chat/__test__/mockedSurface";
// The client mock factory is imported FIRST on purpose: the hoisted
// `vi.mock` below runs the moment the chat store pulls the client module in.
import {
  makeClient,
  type MockedClient,
  type MockFn,
} from "../../chat/composer/__test__/clientMock";
import {
  renderComposer,
  composerInput,
  seedStore,
} from "../../chat/composer/__test__/harness";
import { useChatStore } from "../../chat/store";
import { groupMentionItems, isEveryoneMention } from "../MentionPicker";

const members: AgentGroupMember[] = [
  { GroupID: "g1", AgentID: "a1", AddedAt: "2026-01-01T00:00:00Z" },
  { GroupID: "g1", AgentID: "a2", AddedAt: "2026-01-01T00:00:00Z" },
];

const group: AgentGroup = {
  ID: "g1",
  Name: "research",
  Title: "Research",
  Instructions: "",
  LeadAgentID: "a1",
  Archived: false,
  Revision: 1,
  CreatedAt: "2026-01-01T00:00:00Z",
  UpdatedAt: "2026-01-01T00:00:00Z",
};

/** The harness client widened with the groups member this suite drives. */
type GroupClient = Client & { groups: { send: MockFn } };

function makeGroupClient(): GroupClient {
  // SAFETY: the widened literal carries the harness surface plus the groups
  // member this suite drives (the bridge relabels, the value is unchanged).
  const c = mockedSurface<GroupClient, MockedClient>(makeClient());

  c.groups = mockedSurface<GroupClient["groups"], { send: MockFn }>({
    send: vi.fn(async () => undefined),
  });

  return c;
}

vi.mock("../../../lib/client", () => ({ client: makeGroupClient() }));

import { client } from "../../../lib/client";

// SAFETY: the client module is mocked above, so its runtime shape is the
// widened mock.
const mockedClient = client as GroupClient;

/** The open trigger popover's item list. */
function items(): HTMLElement | null {
  return document.querySelector('[data-slot="composer-trigger-popover-items"]');
}

/** The item whose label matches inside the open popover. */
function item(label: string): HTMLElement {
  const list = items();

  if (list === null) throw new Error("no trigger popover is open");

  const found = Array.from(
    list.querySelectorAll<HTMLElement>('[role="option"]')
  ).find((element: HTMLElement): boolean =>
    (element.textContent ?? "").includes(label)
  );

  if (found === undefined) throw new Error(`trigger item "${label}" not found`);

  return found;
}

beforeEach(() => {
  vi.clearAllMocks();
  // The group conversation surface: the store holds the open group and its
  // roster, and no agent session is selected.
  seedStore({
    activeAgentID: null,
    activeSessionID: null,
    activeGroupID: "g1",
    groups: [group],
    groupMembers: members,
  });
});

describe("groupMentionItems (the @ menu's source)", () => {
  it("offers the roster's member names and the everyone entry", () => {
    const items = groupMentionItems(members, seedStoreAgents(), "");

    expect(items.map((i: TriggerItem): string => i.label)).toEqual([
      "Scout",
      "Runner",
      "everyone",
    ]);
    expect(items[items.length - 1].description).toContain("Every member");
  });

  it("prefix-filters with the composer's legacy rule", () => {
    expect(
      groupMentionItems(members, seedStoreAgents(), "ru").map(
        (i: TriggerItem): string => i.label
      )
    ).toEqual(["Runner"]);
    expect(
      groupMentionItems(members, seedStoreAgents(), "ev").map(
        (i: TriggerItem): string => i.label
      )
    ).toEqual(["everyone"]);
    expect(groupMentionItems(members, seedStoreAgents(), "zz")).toEqual([]);
  });

  it("marks the everyone entry so the pick handler can tell it apart", () => {
    const everyone = groupMentionItems(members, seedStoreAgents(), "")[2];

    expect(isEveryoneMention(everyone)).toBe(true);
    expect(
      isEveryoneMention(groupMentionItems(members, seedStoreAgents(), "")[0])
    ).toBe(false);
  });
});

/** The agents list the harness seeds (the member names resolve from it). */
function seedStoreAgents(): Agent[] {
  return useChatStore.getState().agents;
}

describe("the group composer's mention picker", () => {
  it("the @ menu lists the members and @everyone, not the plain agents list", async () => {
    const user = userEvent.setup();
    await renderComposer();

    await user.type(composerInput(), "@");

    await waitFor(() => expect(items()).not.toBeNull());
    expect(items()?.textContent).toContain("Scout");
    expect(items()?.textContent).toContain("Runner");
    expect(items()?.textContent).toContain("everyone");
    // Runner is a member here; the plain agents list would read the same —
    // the discriminator is the everyone entry, which only groups offer.
    expect(items()?.textContent).toContain("Every member");
  });

  it("a member pick inserts @Name and records the explicit recipient", async () => {
    const user = userEvent.setup();
    await renderComposer();

    await user.type(composerInput(), "@Ru");
    await waitFor(() => expect(items()).not.toBeNull());

    await act(async () => {
      item("Runner").click();
    });

    await waitFor(() => expect(composerInput().value).toContain("@Runner"));
    expect(useChatStore.getState().groupRecipient).toBe("a2");
  });

  it("the everyone pick inserts @everyone and records no recipient", async () => {
    const user = userEvent.setup();
    await renderComposer();

    await user.type(composerInput(), "@");
    await waitFor(() => expect(items()).not.toBeNull());

    await act(async () => {
      item("everyone").click();
    });

    await waitFor(() => expect(composerInput().value).toContain("@everyone"));
    expect(useChatStore.getState().groupRecipient).toBeNull();
  });

  it("a send after a member pick addresses the message through the groups binding", async () => {
    const user = userEvent.setup();
    await renderComposer();

    await user.type(composerInput(), "@Sco");
    await waitFor(() => expect(items()).not.toBeNull());

    await act(async () => {
      item("Scout").click();
    });

    await user.type(composerInput(), " summarize the corpus");
    await user.keyboard("{Enter}");

    await waitFor(() =>
      expect(mockedClient.groups.send).toHaveBeenCalledOnce()
    );

    const req = mockedClient.groups.send.mock.calls[0][0];

    expect(req.GroupID).toBe("g1");
    // The inserted token carries its own trailing space (the composer's
    // directive grammar); collapse the whitespace for the assertion.
    expect(req.Text.replace(/\s+/g, " ").trim()).toBe(
      "@Scout summarize the corpus"
    );
    expect(req.RecipientAgentID).toBe("a1");
    // The recipient rode exactly one send.
    expect(useChatStore.getState().groupRecipient).toBeNull();
  });

  it("a send without a pick stays untargeted", async () => {
    const user = userEvent.setup();
    await renderComposer();

    await user.type(composerInput(), "anyone free?");
    await user.keyboard("{Enter}");

    await waitFor(() =>
      expect(mockedClient.groups.send).toHaveBeenCalledOnce()
    );

    const req = mockedClient.groups.send.mock.calls[0][0];

    expect(req.RecipientAgentID).toBe("");
  });
});
