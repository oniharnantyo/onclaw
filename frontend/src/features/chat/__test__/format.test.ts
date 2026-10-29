import { describe, expect, it } from "vite-plus/test";

import { shortModelName } from "../format";

describe("shortModelName (design D4)", () => {
  it("keeps the last /-delimited segment of a provider-qualified id", () => {
    expect(shortModelName("anthropic/claude-sonnet-4-5")).toBe(
      "claude-sonnet-4-5"
    );
    expect(shortModelName("openrouter/deepseek/deepseek-chat")).toBe(
      "deepseek-chat"
    );
  });

  it("returns a bare id whole", () => {
    expect(shortModelName("m-pro")).toBe("m-pro");
  });

  it("maps an empty id to an empty name", () => {
    expect(shortModelName("")).toBe("");
  });
});
