/**
 * Component tests for AgentAvatar's config precedence (avatar parity): a
 * stored config (the wizard's AvatarPicker output on the agent row) wins
 * over the id-derived face; absent or empty falls back to the derivation so
 * pre-picker agents keep their established face. faceColor is the probe:
 * nice-avatar paints it as an SVG fill verbatim. The live pass-through
 * (design D2) is asserted on the kit-Avatar boundary: `live` reaches it
 * unchanged, absence forwards nothing.
 */
import { createElement, type ReactElement } from "react";
import { describe, expect, it, vi } from "vite-plus/test";

import { AgentAvatar } from "../AgentAvatar";
import { avatarProps } from "../../lib/avatar";
import { renderUI } from "./test-utils";
import type { AvatarProps } from "../ui/Avatar";

/** Props captured from every wrapped kit-Avatar render, latest last. */
const mockAvatarProps: AvatarProps[] = [];

// The kit Avatar is wrapped, not replaced: the spy records what AgentAvatar
// forwards, then delegates to the real Avatar so the face-probing tests keep
// exercising the real render chain.
vi.mock(
  "../ui/Avatar",
  async (importOriginal: () => Promise<typeof import("../ui/Avatar")>) => {
    const actual = await importOriginal();

    return {
      ...actual,
      Avatar: (props: AvatarProps): ReactElement => {
        mockAvatarProps.push(props);

        return createElement(actual.Avatar, props);
      },
    };
  }
);

/** The face actually painted inside the avatar box: the nice-avatar SVG. */
async function renderFace(
  props: React.ComponentProps<typeof AgentAvatar>
): Promise<{ svgText: string; unmount: () => Promise<void> }> {
  const { container, unmount } = await renderUI(<AgentAvatar {...props} />);

  // SAFETY: AgentAvatar always renders exactly one nice-avatar SVG.
  const face = container.querySelector("svg");

  // SAFETY: Avatar's nice-avatar carries the config as fill attributes.
  const svgText = face?.outerHTML ?? "";

  return { svgText, unmount };
}

describe("AgentAvatar", () => {
  it("renders the stored config when the agent has one", async () => {
    // A face color the id derivation can never produce (#AC6651 is the
    // picker's palette, not the derivation's FACE_COLORS), so its presence
    // proves the stored config won.
    const stored = { ...avatarProps("a1"), faceColor: "#AC6651" };

    const { svgText, unmount } = await renderFace({
      id: "a1",
      avatar: stored,
    });

    expect(svgText).toContain("#AC6651");

    await unmount();
  });

  it("falls back to the id-derived face when no config is stored", async () => {
    const derived = avatarProps("scout");

    const { svgText, unmount } = await renderFace({ id: "scout" });

    expect(svgText).toContain(derived.faceColor);

    await unmount();
  });

  it("treats an empty stored config as absent", async () => {
    const derived = avatarProps("scout");

    const { svgText, unmount } = await renderFace({ id: "scout", avatar: {} });

    expect(svgText).toContain(derived.faceColor);

    await unmount();
  });

  it("forwards live to the kit Avatar unchanged", async () => {
    // The identity view's payload (design D2); the reference must survive
    // the hop, so the kit Avatar sees exactly what the caller passed.
    const live = {
      agentState: "thinking" as const,
      fx: null,
      lookAt: "global" as const,
      sleepAfter: 120000,
    };

    const { unmount } = await renderFace({ id: "scout", live });

    const forwarded = mockAvatarProps.at(-1);
    // SAFETY: AgentAvatar always renders exactly one kit Avatar.
    expect(forwarded).toBeDefined();
    expect(forwarded?.live).toBe(live);

    await unmount();
  });

  it("forwards no live props when live is absent", async () => {
    const { unmount } = await renderFace({ id: "scout" });

    const forwarded = mockAvatarProps.at(-1);
    // SAFETY: AgentAvatar always renders exactly one kit Avatar.
    expect(forwarded).toBeDefined();
    expect(forwarded?.live).toBeUndefined();

    await unmount();
  });
});
