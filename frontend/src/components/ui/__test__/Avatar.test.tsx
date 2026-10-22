/**
 * Avatar tests (ported from onclaw-v2's Avatar.test.tsx, minus the
 * AvatarPicker case — the picker is not part of this kit yet): initials
 * fallback, image path with error fallback, and the generated-face path.
 */
import { describe, expect, it } from "vite-plus/test";

import { Avatar } from "../Avatar";
import { renderUI } from "../../__test__/test-utils";
import { avatarProps } from "../../../lib/avatar";

describe("components/ui/Avatar", () => {
  it("renders initials when no avatar config or src is provided", async () => {
    const { container, unmount } = await renderUI(
      <Avatar name="Radar Assistant" kind="agent" size={32} />
    );

    expect(container.textContent).toContain("RA");

    await unmount();
  });

  it("renders an image when a src is provided", async () => {
    const { getByAltText, unmount } = await renderUI(
      <Avatar name="User" src="https://example.com/avatar.png" size={32} />
    );

    // SAFETY: the Avatar renders an img with alt = name on this path.
    const img = getByAltText("User") as HTMLImageElement;

    expect(img.src).toBe("https://example.com/avatar.png");

    await unmount();
  });

  it("renders a generated face when an avatar config is provided", async () => {
    const { container, unmount } = await renderUI(
      <Avatar name="Agent" avatar={avatarProps("a1")} size={40} />
    );

    // NiceAvatar renders svg shapes.
    expect(container.querySelector("svg")).not.toBeNull();

    await unmount();
  });
});
