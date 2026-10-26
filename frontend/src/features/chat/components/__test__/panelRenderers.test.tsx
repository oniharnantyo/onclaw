/**
 * panelRenderers tests (designs D3–D6, spec merge-right-bar-previews): the
 * tab builders pin their identity formats, the markdown file preview
 * defaults to Raw and toggles to the standalone markdown body only for
 * `.md`/`.markdown` names, the trajectory tab renders each parsed step as
 * its transcript element through the jailed reader, the refused and failed
 * reads surface their own outcomes, a running task's open tab polls the
 * growing transcript file every ~2s — stopping for good once the task turns
 * terminal — and the knowledge document preview (add-knowledge-corpus)
 * decodes its base64 bytes unicode-safely, toggles markdown, embeds PDFs,
 * and surfaces read failures.
 */
import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/dom";
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vite-plus/test";

import { ToolStatus } from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type { ContextUsage } from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/agents/models";
import type {
  PromptDocuments,
  WorkspaceEntry,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { Client, KnowledgeDocument } from "../../../../lib/client";
import { knowledgeDocTab } from "../../types";
import {
  PANEL_RENDERERS,
  trajectoryTab,
  workspaceFileTab,
} from "../panelRenderers";
import { useChatStore } from "../../store";
import type { Entry, PanelTab } from "../../types";
import { renderUI } from "../../__test__/test-utils";

/** The jail's refusal wording (backend "workspaces: refused …"). */
const REFUSED = 'workspaces: refused "/etc/passwd": outside the workspace';

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  agents: {
    getPromptDocuments: MockFn;
  };
  chat: {
    sessions: MockFn;
    subscribe: MockFn;
    transcript: MockFn;
    contextUsage: MockFn;
    workspaceFile: MockFn;
    listWorkspace: MockFn;
  };
  knowledge: {
    content: MockFn;
  };
};

const NO_DOCS: PromptDocuments = {
  Identity: "",
  IdentityExists: false,
  Soul: "",
  SoulExists: false,
  Bootstrap: "",
  BootstrapExists: false,
};

vi.mock("../../../../lib/client", () => ({
  client: {
    agents: {
      getPromptDocuments: vi.fn(async (): Promise<PromptDocuments> => NO_DOCS),
    },
    chat: {
      sessions: vi.fn(async (): Promise<never[]> => []),
      subscribe: vi.fn(async (): Promise<() => void> => () => undefined),
      transcript: vi.fn(async (): Promise<never[]> => []),
      transcriptPage: vi.fn(async (): Promise<never[]> => []),
      contextUsage: vi.fn(async (): Promise<ContextUsage> => ({
        used: 0,
        limit: 0,
        last_input: 0,
        last_output: 0,
      })),
      workspaceFile: vi.fn(async (): Promise<string> => "file body"),
      listWorkspace: vi.fn(async (): Promise<WorkspaceEntry[]> => []),
    },
    knowledge: {
      content: vi.fn(async (): Promise<string> => ""),
    },
  },
}));

import { client } from "../../../../lib/client";

// SAFETY: the client module is mocked with vi.fn members above.
const mockedClient = client as MockedClient;

/** Renders one tab through the registry, exactly as the panel mounts it. */
function renderTab(tab: PanelTab): ReturnType<typeof renderUI> {
  const Renderer = PANEL_RENDERERS[tab.kind];

  return renderUI(<Renderer tab={tab} />);
}

/** A workspace-file tab fixture with a stable id (ids are minted by a
 * module-level counter; the renderers never read it). Built as the literal
 * member — the builders' `Omit<PanelTab, "id">` spreads flatten the union —
 * mirroring workspaceFileTab's identity format. */
function fileTab(relPath: string): PanelTab {
  const name = relPath.split("/").filter(Boolean).pop() ?? "Workspace";

  return {
    id: "t1",
    kind: "workspace-file",
    identity: `workspace-file:a1:${relPath}`,
    title: name,
    data: { agentID: "a1", relPath, name },
  };
}

/** A trajectory tab fixture for task-1's transcript, mirroring the design
 * D3 builder's identity format (pinned by the builder test below). */
function trajTab(): PanelTab {
  return {
    id: "t1",
    kind: "trajectory",
    identity: "trajectory:a1:task-1",
    title: "Research",
    data: {
      agentID: "a1",
      taskID: "task-1",
      transcriptPath: ".tasks/t1.output",
    },
  };
}

/** One task store entry for task-1 with the given lifecycle status and
 * optional live progress line. */
function taskEntry(status: ToolStatus, progress: string = ""): Entry {
  const task: Extract<Entry, { kind: "task" }>["task"] = {
    task_id: "task-1",
    description: "Research the retry policy",
    status,
  };

  if (progress !== "") task.progress = progress;

  return {
    kind: "task",
    id: "entry-task-1",
    task,
    at: "2026-01-01T00:00:00Z",
  };
}

beforeEach(() => {
  // clearAllMocks resets the call history; implementations come back to the
  // factory defaults. Per-test overrides below use mockImplementation —
  // never Once variants, so an unconsumed queue entry can never answer a
  // later test's fetch with the previous test's fixture.
  vi.clearAllMocks();

  return act(async () => {
    useChatStore.setState({
      activeAgentID: "a1",
      activeSessionID: "s1",
      entries: [],
      panelTabs: [],
      loadingSession: false,
      error: null,
      busy: false,
    });
  });
});

afterEach(() => {
  vi.useRealTimers();
});

describe("panelRenderers — tab builders (design D3)", () => {
  it("pins the trajectory tab's identity to agent + task", () => {
    expect(
      trajectoryTab("a1", "task-1", ".tasks/t1.output", "Research")
    ).toEqual({
      kind: "trajectory",
      identity: "trajectory:a1:task-1",
      title: "Research",
      data: {
        agentID: "a1",
        taskID: "task-1",
        transcriptPath: ".tasks/t1.output",
      },
    });
  });

  it("keeps the workspace-file tab's identity on the path", () => {
    expect(workspaceFileTab("a1", "notes.md")).toEqual({
      kind: "workspace-file",
      identity: "workspace-file:a1:notes.md",
      title: "notes.md",
      data: { agentID: "a1", relPath: "notes.md", name: "notes.md" },
    });
  });
});

describe("panelRenderers — markdown preview toggle (design D6)", () => {
  it("defaults a .md file to Raw and toggles to the markdown body", async () => {
    mockedClient.chat.workspaceFile.mockImplementation(
      async (): Promise<string> => "# Title\n\nbody text"
    );

    const { unmount } = await renderTab(fileTab("notes.md"));

    // Raw is the default: the mono pre block, no markdown body.
    const raw = await waitFor(() =>
      screen.getByTestId("panel-preview-content")
    );

    expect(raw.textContent).toBe("# Title\n\nbody text");
    expect(screen.queryByTestId("panel-preview-markdown")).toBeNull();
    expect(screen.getByTestId("panel-preview-modes")).toBeTruthy();
    expect(mockedClient.chat.workspaceFile).toHaveBeenCalledWith(
      "a1",
      "notes.md"
    );

    await act(async () => {
      fireEvent.click(screen.getByTestId("panel-preview-mode-preview"));
    });

    const body = await waitFor(() =>
      screen.getByTestId("panel-preview-markdown")
    );

    expect(body.querySelector("h1")?.textContent).toBe("Title");
    expect(body.textContent).toContain("body text");
    expect(screen.queryByTestId("panel-preview-content")).toBeNull();

    // And back.
    await act(async () => {
      fireEvent.click(screen.getByTestId("panel-preview-mode-raw"));
    });

    await waitFor(() => screen.getByTestId("panel-preview-content"));
    expect(screen.queryByTestId("panel-preview-markdown")).toBeNull();

    await unmount();
  });

  it("shows no toggle for a non-markdown file name", async () => {
    const { unmount } = await renderTab(fileTab("notes.txt"));

    await waitFor(() => screen.getByTestId("panel-preview-content"));

    expect(screen.queryByTestId("panel-preview-modes")).toBeNull();

    await unmount();
  });
});

/** UTF-8 base64 of a text fixture: TextEncoder → binary string → atob's
 * inverse. The renderer's decode is the reverse path, so unicode survives. */
function b64Of(text: string): string {
  const bytes = new TextEncoder().encode(text);

  let binary = "";

  bytes.forEach((b: number): void => {
    binary += String.fromCharCode(b);
  });

  return btoa(binary);
}

/** A knowledge-doc tab fixture with a stable id, mirroring the knowledgeDocTab
 * builder's identity format (pinned by the builder test below). */
function knowledgeTab(filename: string, mime: string): PanelTab {
  return {
    id: "t1",
    kind: "knowledge-doc",
    identity: "knowledge-doc:d1",
    title: filename,
    data: { docId: "d1", filename, mime },
  };
}

describe("panelRenderers — knowledge document preview (add-knowledge-corpus)", () => {
  it("pins the knowledge-doc tab's identity to the catalog id", () => {
    const doc: KnowledgeDocument = {
      id: "d1",
      filename: "runbook.md",
      mime: "text/markdown",
      size: 8,
      scope: "agent",
      agentId: "a1",
      agentSlug: "scout",
      description: "",
      source: "upload",
      createdAt: "2026-01-01T00:00:00Z",
    };

    expect(knowledgeDocTab(doc)).toEqual({
      kind: "knowledge-doc",
      identity: "knowledge-doc:d1",
      title: "runbook.md",
      data: { docId: "d1", filename: "runbook.md", mime: "text/markdown" },
    });
  });

  it("renders text content decoded from base64, unicode-safe", async () => {
    mockedClient.knowledge.content.mockImplementation(
      async (): Promise<string> => b64Of("héllo ✓ — multi-byte")
    );

    const { unmount } = await renderTab(
      knowledgeTab("notes.txt", "text/plain")
    );

    const raw = await waitFor(() =>
      screen.getByTestId("panel-preview-content")
    );

    expect(raw.textContent).toBe("héllo ✓ — multi-byte");
    expect(mockedClient.knowledge.content).toHaveBeenCalledWith("d1");
    expect(screen.queryByTestId("panel-preview-modes")).toBeNull();

    await unmount();
  });

  it("defaults a .md document to Raw and toggles to the markdown body", async () => {
    mockedClient.knowledge.content.mockImplementation(
      async (): Promise<string> => b64Of("# Title\n\nbody text")
    );

    const { unmount } = await renderTab(
      knowledgeTab("runbook.md", "text/markdown")
    );

    // Raw is the default: the mono pre block, no markdown body.
    const raw = await waitFor(() =>
      screen.getByTestId("panel-preview-content")
    );

    expect(raw.textContent).toBe("# Title\n\nbody text");
    expect(screen.queryByTestId("panel-preview-markdown")).toBeNull();
    expect(screen.getByTestId("panel-preview-modes")).toBeTruthy();

    await act(async () => {
      fireEvent.click(screen.getByTestId("panel-preview-mode-preview"));
    });

    const body = await waitFor(() =>
      screen.getByTestId("panel-preview-markdown")
    );

    expect(body.querySelector("h1")?.textContent).toBe("Title");
    expect(body.textContent).toContain("body text");

    // And back.
    await act(async () => {
      fireEvent.click(screen.getByTestId("panel-preview-mode-raw"));
    });

    await waitFor(() => screen.getByTestId("panel-preview-content"));
    expect(screen.queryByTestId("panel-preview-markdown")).toBeNull();

    await unmount();
  });

  it("renders a PDF mime as the embedded document with its data URL", async () => {
    const pdf = b64Of("%PDF-1.4 bytes");

    mockedClient.knowledge.content.mockImplementation(
      async (): Promise<string> => pdf
    );

    const { unmount } = await renderTab(
      knowledgeTab("spec.pdf", "application/pdf")
    );

    const embed = await waitFor(() =>
      screen.getByTestId("panel-knowledge-pdf")
    );

    expect(embed.getAttribute("type")).toBe("application/pdf");
    expect(embed.getAttribute("data")).toBe(
      `data:application/pdf;base64,${pdf}`
    );
    expect(screen.queryByTestId("panel-preview-content")).toBeNull();

    await unmount();
  });

  it("surfaces a failed content read as an error", async () => {
    mockedClient.knowledge.content.mockImplementation(
      async (): Promise<string> => {
        throw new Error('not found: "d1"');
      }
    );

    const { unmount } = await renderTab(
      knowledgeTab("notes.txt", "text/plain")
    );

    await waitFor(() =>
      expect(screen.getByTestId("panel-preview-error").textContent).toBe(
        'not found: "d1"'
      )
    );
    expect(screen.queryByTestId("panel-preview-content")).toBeNull();

    await unmount();
  });

  it("renders an empty document as empty", async () => {
    mockedClient.knowledge.content.mockImplementation(
      async (): Promise<string> => b64Of("")
    );

    const { unmount } = await renderTab(
      knowledgeTab("notes.txt", "text/plain")
    );

    await waitFor(() => screen.getByTestId("panel-preview-empty"));

    expect(screen.queryByTestId("panel-preview-content")).toBeNull();

    await unmount();
  });
});

describe("panelRenderers — trajectory preview (designs D3–D4)", () => {
  it("renders each step kind as its transcript element", async () => {
    const records: { step: string; content: string }[] = [
      { step: "task", content: "Research the retry policy" },
      { step: "reasoning", content: "Start with the docs" },
      { step: "tool", content: "call read_file docs/retry.md" },
      { step: "text", content: "The policy retries **three** times." },
      { step: "result", content: "Retries are capped at three." },
    ];

    const transcript = records
      .map((record: { step: string; content: string }): string =>
        JSON.stringify(record)
      )
      .join("\n");

    mockedClient.chat.workspaceFile.mockImplementation(
      async (): Promise<string> => transcript
    );

    const { unmount } = await renderTab(trajTab());

    await waitFor(() => screen.getByTestId("panel-trajectory"));
    expect(mockedClient.chat.workspaceFile).toHaveBeenCalledWith(
      "a1",
      ".tasks/t1.output"
    );

    // The task prompt as a user-style message.
    expect(screen.getByTestId("trajectory-step-task").textContent).toContain(
      "Research the retry policy"
    );

    // The reasoning disclosure starts collapsed.
    const reasoning = screen.getByTestId("trajectory-step-reasoning");

    expect(reasoning.textContent).not.toContain("Start with the docs");

    await act(async () => {
      fireEvent.click(reasoning.querySelector("button")!);
    });

    await waitFor(() =>
      expect(reasoning.textContent).toContain("Start with the docs")
    );

    // The tool call renders as the transcript's real tool card (design D4):
    // an unpaired call of an idle task reads completed. The card starts
    // collapsed like the transcript's; the payload assertions live in the
    // paired-card test below.
    const toolCard = screen
      .getByTestId("trajectory-step-tool")
      .querySelector('[data-testid^="entry-tool-"]');

    expect(toolCard?.getAttribute("data-tool-state")).toBe("completed");

    // The text step renders as markdown.
    expect(
      screen.getByTestId("trajectory-step-text").querySelector("strong")
        ?.textContent
    ).toBe("three");

    // The final result record is not rendered — the parent transcript
    // relays the child's answer (design D4).
    expect(screen.queryByTestId("trajectory-step-result")).toBeNull();
    expect(screen.queryByText("Retries are capped at three.")).toBeNull();

    await unmount();
  });

  it("renders the refused outcome for a path the jail refuses", async () => {
    mockedClient.chat.workspaceFile.mockImplementation(
      async (): Promise<string> => {
        throw new Error(REFUSED);
      }
    );

    const { unmount } = await renderTab(trajTab());

    await waitFor(() => screen.getByTestId("panel-preview-refused"));

    expect(screen.queryByTestId("trajectory-step-task")).toBeNull();
    expect(screen.queryByTestId("panel-preview-error")).toBeNull();

    await unmount();
  });

  it("surfaces a non-refused read failure as an error", async () => {
    mockedClient.chat.workspaceFile.mockImplementation(
      async (): Promise<string> => {
        throw new Error("not found: .tasks/t1.output");
      }
    );

    const { unmount } = await renderTab(trajTab());

    await waitFor(() =>
      expect(screen.getByTestId("panel-preview-error").textContent).toBe(
        "not found: .tasks/t1.output"
      )
    );
    expect(screen.queryByTestId("panel-preview-refused")).toBeNull();

    await unmount();
  });
});

describe("panelRenderers — trajectory live growth (design D5)", () => {
  it("polls while the task runs and stops once it turns terminal", async () => {
    vi.useFakeTimers();

    try {
      await act(async () => {
        useChatStore.setState({
          entries: [taskEntry(ToolStatus.ToolStatusRunning)],
        });
      });

      const { unmount } = await renderTab(trajTab());

      // One fetch on open.
      expect(mockedClient.chat.workspaceFile).toHaveBeenCalledTimes(1);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(500);
      });
      expect(mockedClient.chat.workspaceFile).toHaveBeenCalledTimes(2);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(500);
      });
      expect(mockedClient.chat.workspaceFile).toHaveBeenCalledTimes(3);

      // The task turns terminal: the interval stops for good (one last
      // fetch lands the final state, then advancing never calls again) and
      // the live tail drops.
      await act(async () => {
        useChatStore.setState({
          entries: [taskEntry(ToolStatus.ToolStatusCompleted)],
        });
      });

      const calls = mockedClient.chat.workspaceFile.mock.calls.length;

      await act(async () => {
        await vi.advanceTimersByTimeAsync(8000);
      });

      expect(mockedClient.chat.workspaceFile.mock.calls.length).toBe(calls);
      expect(screen.queryByTestId("trajectory-tail")).toBeNull();

      await unmount();
    } finally {
      vi.useRealTimers();
    }
  });

  it("fetches a terminal task's transcript once, with no polling", async () => {
    vi.useFakeTimers();

    try {
      await act(async () => {
        useChatStore.setState({
          entries: [taskEntry(ToolStatus.ToolStatusCompleted)],
        });
      });

      const { unmount } = await renderTab(trajTab());

      expect(mockedClient.chat.workspaceFile).toHaveBeenCalledTimes(1);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(6000);
      });

      expect(mockedClient.chat.workspaceFile).toHaveBeenCalledTimes(1);

      await unmount();
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("panelRenderers — trajectory tool cards (design D4)", () => {
  it("folds a call and its result into one completed card", async () => {
    mockedClient.chat.workspaceFile.mockImplementation(
      async (): Promise<string> =>
        [
          JSON.stringify({
            step: "tool",
            content: 'call read_file {"file_path": "docs/retry.md"}',
          }),
          JSON.stringify({ step: "tool", content: "result read_file: done" }),
        ].join("\n")
    );

    const { unmount } = await renderTab(trajTab());

    await waitFor(() => screen.getByTestId("entry-tool-traj-s0"));

    const card = screen.getByTestId("entry-tool-traj-s0");

    expect(card.getAttribute("data-tool-state")).toBe("completed");
    expect(screen.getAllByTestId(/^entry-tool-/)).toHaveLength(1);
    expect(card.textContent).toContain("docs/retry.md");

    await unmount();
  });

  it("renders an open call of a running task in its running state with the live tail", async () => {
    await act(async () => {
      useChatStore.setState({
        entries: [
          taskEntry(ToolStatus.ToolStatusRunning, "Reading docs/retry.md"),
        ],
      });
    });

    mockedClient.chat.workspaceFile.mockImplementation(
      async (): Promise<string> =>
        JSON.stringify({
          step: "tool",
          content: 'call read_file {"file_path": "docs/retry.md"}',
        })
    );

    const { unmount } = await renderTab(trajTab());

    await waitFor(() => screen.getByTestId("entry-tool-traj-s0"));

    expect(
      screen.getByTestId("entry-tool-traj-s0").getAttribute("data-tool-state")
    ).toBe("running");
    expect(screen.getByTestId("trajectory-tail").textContent).toContain(
      "Reading docs/retry.md"
    );

    await unmount();
  });

  it("renders an unknown tool through the card shell's fallback body", async () => {
    mockedClient.chat.workspaceFile.mockImplementation(
      async (): Promise<string> =>
        JSON.stringify({
          step: "tool",
          content: 'call mcp_unknown {"query": "x"}',
        })
    );

    const { unmount } = await renderTab(trajTab());

    await waitFor(() => screen.getByTestId("entry-tool-traj-s0"));

    const card = screen.getByTestId("entry-tool-traj-s0");

    expect(card.getAttribute("data-tool-state")).toBe("completed");
    expect(card.textContent).toContain("mcp_unknown");

    await unmount();
  });
});
