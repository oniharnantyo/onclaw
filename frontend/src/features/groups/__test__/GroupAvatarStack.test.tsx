/**
 * Group avatar stack tests (round 8): the header's stacked roster — one
 * circular face per member, each tucked under its predecessor (left over
 * right), separation rings in the surface tone, and a green (success) ring
 * on members whose task is in flight ("is working" in the tooltip). Queued
 * members keep the surface ring — waiting is not working.
 */
import { screen } from "@testing-library/dom";
import { describe, expect, it } from "vite-plus/test";

import type { Agent } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { renderUI } from "../../chat/__test__/test-utils";
import { GroupAvatarStack } from "../GroupAvatarStack";

function agent(id: string, name: string): Agent {
  return {
    ID: id,
    Name: name,
    ProviderID: "prov",
    Model: "m",
    Effort: "",
    Slug: id,
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
}

describe("GroupAvatarStack (round 8)", () => {
  const roster: Agent[] = [
    agent("a1", "Scout"),
    agent("a2", "Jono"),
    agent("a3", "Iris"),
  ];

  it("renders one circular face per member, overlapping left over right", async () => {
    await renderUI(<GroupAvatarStack roster={roster} runningIDs={new Set()} />);

    const a1 = screen.getByTestId("group-avatar-stack-item-a1");
    const a2 = screen.getByTestId("group-avatar-stack-item-a2");
    const a3 = screen.getByTestId("group-avatar-stack-item-a3");

    expect(a1.className).not.toContain("-ms-2");
    expect(a2.className).toContain("-ms-2");
    expect(a3.className).toContain("-ms-2");

    // Left-over-right: the z order descends along the roster.
    expect(a1.style.zIndex).toBe("3");
    expect(a2.style.zIndex).toBe("2");
    expect(a3.style.zIndex).toBe("1");
  });

  it("rings a running member green; idle members keep the surface ring", async () => {
    await renderUI(
      <GroupAvatarStack roster={roster} runningIDs={new Set(["a2"])} />
    );

    const a2 = screen.getByTestId("group-avatar-stack-item-a2");
    const a3 = screen.getByTestId("group-avatar-stack-item-a3");

    expect(a2.dataset.running).toBe("true");
    expect(a2.className).toContain("ring-success");
    expect(a2.title).toBe("Jono is working");

    expect(a3.dataset.running).toBe("false");
    expect(a3.className).toContain("ring-surface");
    expect(a3.title).toBe("Iris");
  });

  it("labels the stack for the roster it carries", async () => {
    await renderUI(<GroupAvatarStack roster={roster} runningIDs={new Set()} />);

    const stack = screen.getByTestId("group-avatar-stack");
    expect(stack.getAttribute("aria-label")).toBe(
      "Group members: Scout, Jono, Iris"
    );
  });
});
