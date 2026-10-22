import { beforeEach, describe, expect, it, vi } from "vite-plus/test";
import userEvent from "@testing-library/user-event";
import type { Skill } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { Client } from "../../../lib/client";

import SkillsPane from "../components/SkillsPane";
import type { SettingsPaneConfig } from "../types";
import { renderUI } from "./test-utils";

/** Header config passed to the pane; mirrors the static nav entry. */
const pane: SettingsPaneConfig = {
  key: "skills",
  label: "Skills",
  icon: "spark",
  description: "Directory-backed skills agents can use, with global toggles.",
};

const skills: Skill[] = [];

vi.mock("../../../lib/client", () => ({
  client: {
    settings: {
      skills: vi.fn(async () => [...skills]),
      setSkillEnabled: vi.fn(async (_name: string, enabled: boolean) => {
        for (const s of skills) if (s.Name === _name) s.Enabled = enabled;
      }),
    },
  },
}));

import { client } from "../../../lib/client";

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  settings: {
    skills: MockFn;
    setSkillEnabled: MockFn;
  };
};

// SAFETY: the client module is mocked with vi.fn members above.
const mockedClient = client as MockedClient;

describe("SkillsPane", () => {
  beforeEach(() => {
    skills.length = 0;
    vi.clearAllMocks();
  });

  it("lists directory-backed skills with name, description, and source badge", async () => {
    skills.push({
      Name: "summarize",
      Description: "Summarize text",
      Version: "",
      Source: "directory",
      Enabled: true,
      CreatedAt: "",
      UpdatedAt: "",
    });
    const { findByTestId } = await renderUI(<SkillsPane pane={pane} />);

    const row = await findByTestId("skill-summarize");
    expect(row.textContent).toContain("Summarize text");
    expect((await findByTestId("skill-source-summarize")).textContent).toBe(
      "directory"
    );
  });

  it("offers Rescan directory as the empty state's next-step action", async () => {
    const user = userEvent.setup();

    const { getByTestId, findByTestId } = await renderUI(
      <SkillsPane pane={pane} />
    );

    expect(await findByTestId("skills-empty")).toBeTruthy();

    await user.click(getByTestId("skills-reload"));
    expect(mockedClient.settings.skills).toHaveBeenCalledTimes(2);
  });

  it("toggles a skill off and on globally", async () => {
    skills.push({
      Name: "summarize",
      Description: "",
      Version: "",
      Source: "directory",
      Enabled: true,
      CreatedAt: "",
      UpdatedAt: "",
    });
    const user = userEvent.setup();
    const { findByTestId } = await renderUI(<SkillsPane pane={pane} />);

    // The kit Toggle is a role=switch button, not a checkbox input.
    const toggle = await findByTestId("skill-toggle-summarize");
    expect(toggle.getAttribute("role")).toBe("switch");
    expect(toggle.getAttribute("aria-label")).toBe("Enable summarize");
    expect(toggle.getAttribute("aria-checked")).toBe("true");

    await user.click(toggle);
    expect(mockedClient.settings.setSkillEnabled).toHaveBeenCalledWith(
      "summarize",
      false
    );

    const toggledOff = await findByTestId("skill-toggle-summarize");
    expect(toggledOff.getAttribute("aria-checked")).toBe("false");

    await user.click(toggledOff);
    expect(mockedClient.settings.setSkillEnabled).toHaveBeenCalledWith(
      "summarize",
      true
    );
  });
});
