import { genConfig } from "@oniharnantyo/react-nice-avatar";
import { describe, expect, it } from "vite-plus/test";

import { avatarProps } from "../avatar";

/**
 * Regression pins for the audit finding "avatar identity re-rolls per
 * mount": the kit Avatar runs configs through react-nice-avatar's
 * genConfig, which random-fills every key the config leaves unset. A full
 * derived config is the only way the same id renders the same face
 * everywhere (cards, rail, sidebar, wizard preview).
 */
describe("avatarProps", () => {
  it("derives every genConfig key — nothing left to random-fill", () => {
    const props = avatarProps("a1");

    for (const key of Object.keys(genConfig({}))) {
      expect(props).toHaveProperty(key);
    }
  });

  it("is deterministic for the same id and distinct across ids", () => {
    expect(avatarProps("scout")).toEqual(avatarProps("scout"));
    expect(avatarProps("scout")).not.toEqual(avatarProps("scout2"));
  });

  it("keeps genConfig output byte-stable across calls", () => {
    const first = JSON.stringify(genConfig(avatarProps("scout")));
    const second = JSON.stringify(genConfig(avatarProps("scout")));
    expect(first).toBe(second);
  });
});
