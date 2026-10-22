/**
 * Component tests for AgentAvatar's config precedence (avatar parity): a
 * stored config (the wizard's AvatarPicker output on the agent row) wins
 * over the id-derived face; absent or empty falls back to the derivation so
 * pre-picker agents keep their established face. faceColor is the probe:
 * nice-avatar paints it as an SVG fill verbatim.
 */
import { describe, expect, it } from "vite-plus/test";

import { AgentAvatar } from "../AgentAvatar";
import { avatarProps } from "../../lib/avatar";
import { renderUI } from "./test-utils";

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
});
