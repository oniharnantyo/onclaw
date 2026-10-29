/**
 * Avatar tests (ported from onclaw-v2's Avatar.test.tsx, minus the
 * AvatarPicker case — the picker is not part of this kit yet): initials
 * fallback, image path with error fallback, and the generated-face path,
 * plus the live pass-through (design D2): `live` present arms the fork
 * element and drops the wrapper clip, `live` absent leaves the fork element
 * exactly as today, and a missing `live.fx` normalises to null.
 */
import type { ReactElement } from "react";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type {
  AvatarRefHandle,
  NiceAvatarProps,
} from "@oniharnantyo/react-nice-avatar";

import { Avatar } from "../Avatar";
import { renderUI } from "../../__test__/test-utils";
import { avatarProps } from "../../../lib/avatar";

/** Props captured from every mock fork render, latest last. */
const mockRenderedProps: NiceAvatarProps[] = [];

// The fork is replaced wholesale: a class component standing in for the real
// one (whose default export implements the imperative handle), so a callback
// ref observes the same mount/unmount lifecycle while recording props for
// the pass-through assertions. genConfig stays real, so the recorded spread
// is what the app actually feeds the fork. react is imported inside the
// factory: hoisted factories run before the file's own imports are
// initialised.
vi.mock(
  "@oniharnantyo/react-nice-avatar",
  async (
    importOriginal: () => Promise<
      typeof import("@oniharnantyo/react-nice-avatar")
    >
  ) => {
    const actual = await importOriginal();

    const { Component, createElement } = await import("react");

    class MockNiceAvatar extends Component<NiceAvatarProps> {
      render(): ReactElement {
        mockRenderedProps.push(this.props);

        return createElement("svg", { "data-testid": "mock-nice-avatar" });
      }
    }

    // The real class implements AvatarRefHandle; stamp the same no-op members
    // on the prototype so the ref delivers a usable handle on mount.
    const noop = (): void => undefined;

    Object.assign(MockNiceAvatar.prototype, {
      nod: noop,
      shake: noop,
      sneeze: noop,
      squash: noop,
      doubleTake: noop,
      talk: noop,
      stopTalking: noop,
      turn: noop,
      showFx: noop,
      clearFx: noop,
      wake: noop,
      sleep: noop,
    });

    return { ...actual, default: MockNiceAvatar };
  }
);

describe("components/ui/Avatar", () => {
  beforeEach(() => {
    mockRenderedProps.length = 0;
  });

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

    // NiceAvatar renders svg shapes (the mock stands in for the fork here).
    expect(container.querySelector("svg")).not.toBeNull();

    await unmount();
  });

  it("adds no live keys to the fork and keeps the clip when live is absent", async () => {
    const { container, unmount } = await renderUI(
      <Avatar name="Agent" avatar={avatarProps("a1")} size={40} />
    );

    const props = mockRenderedProps.at(-1);
    // SAFETY: the generated-face branch always renders the fork element.
    expect(props).toBeDefined();
    expect("animated" in props!).toBe(false);
    expect("secondaryMotion" in props!).toBe(false);
    expect("agentState" in props!).toBe(false);
    expect("fx" in props!).toBe(false);
    expect("lookAt" in props!).toBe(false);
    expect("sleepAfter" in props!).toBe(false);

    // Static wrapper still clips (design D2: absent means today's render),
    // and carries no state attributes.
    const wrapper = container.firstElementChild;
    expect(wrapper?.className).toContain("overflow-hidden");
    expect(wrapper?.hasAttribute("data-avatar-state")).toBe(false);
    expect(wrapper?.hasAttribute("data-avatar-fx")).toBe(false);

    await unmount();
  });

  it("arms the fork and drops the clip when live is present", async () => {
    const onHandle = vi.fn();

    const { container, unmount } = await renderUI(
      <Avatar
        name="Agent"
        avatar={avatarProps("a1")}
        size={64}
        live={{
          agentState: "thinking",
          fx: "sparkle",
          lookAt: "global",
          sleepAfter: 120000,
          onHandle,
        }}
      />
    );

    const props = mockRenderedProps.at(-1);
    // SAFETY: the generated-face branch always renders the fork element.
    expect(props).toBeDefined();
    expect(props?.animated).toBe(true);
    expect(props?.secondaryMotion).toBe(true);
    expect(props?.agentState).toBe("thinking");
    expect(props?.fx).toBe("sparkle");
    expect(props?.lookAt).toBe("global");
    expect(props?.sleepAfter).toBe(120000);
    // The silhouette knob stays pinned under the live keys too.
    expect(props?.shape).toBe("rounded");

    // The wrapper keeps the rounded grammar but lets badges and head sway
    // leave the box (design D2).
    const wrapper = container.firstElementChild;
    expect(wrapper?.className).toContain("rounded-md");
    expect(wrapper?.className).not.toContain("overflow-hidden");
    // The level state and badge are mirrored onto the wrapper so a live
    // smoke (and the DOM) can read them without reaching into the fork.
    expect(wrapper?.getAttribute("data-avatar-state")).toBe("thinking");
    expect(wrapper?.getAttribute("data-avatar-fx")).toBe("sparkle");

    // The mock fork's instance arrives through the callback ref on mount,
    // carrying the imperative contract.
    expect(onHandle).toHaveBeenCalledTimes(1);
    // SAFETY: React calls the ref with the mock class instance on mount.
    const handle = onHandle.mock.calls[0]?.[0] as AvatarRefHandle | null;
    expect(handle).not.toBeNull();
    expect(handle?.nod).toBeInstanceOf(Function);

    await unmount();

    // ...and the ref fires with null on unmount (design D2).
    expect(onHandle).toHaveBeenCalledTimes(2);
    expect(onHandle.mock.calls[1]?.[0]).toBeNull();
  });

  it("normalises a missing live fx to null", async () => {
    const { container, unmount } = await renderUI(
      <Avatar
        name="Agent"
        avatar={avatarProps("a1")}
        size={40}
        live={{ agentState: "listening" }}
      />
    );

    const props = mockRenderedProps.at(-1);
    // SAFETY: the generated-face branch always renders the fork element.
    expect(props).toBeDefined();
    expect(props?.fx).toBeNull();
    // sleepAfter stays off the element when not given.
    expect("sleepAfter" in props!).toBe(false);
    // No badge → no fx attribute on the wrapper; the state still shows.
    const wrapper = container.firstElementChild;
    expect(wrapper?.getAttribute("data-avatar-state")).toBe("listening");
    expect(wrapper?.hasAttribute("data-avatar-fx")).toBe(false);

    await unmount();
  });
});
