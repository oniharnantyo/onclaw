/**
 * KnowledgePanel tests (add-knowledge-corpus, spec chat-panels "Knowledge
 * panel"): the list shows the session agent's scoped documents and the
 * global ones with scope markers while other agents' documents stay out,
 * activation hands the document to the bar's opener, upload defaults to the
 * agent's scope (global as the explicit choice) and passes base64 + agentID
 * through the binding, the corpus pre-gates reject unsupported and oversize
 * files before any round trip, backend upload rejections surface inline with
 * their reason, promote moves an agent document to global and re-lists it as
 * global, and knowledge:changed refetches coalesced while the panel is open.
 */
import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type { Client, KnowledgeDocument } from "../../../../lib/client";
import { KnowledgePanel, knowledgeRejection } from "../KnowledgePanel";
import { renderUI } from "../../__test__/test-utils";

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  knowledge: {
    list: MockFn;
    upload: MockFn;
    setScope: MockFn;
  };
  events: {
    onKnowledgeChanged: MockFn;
  };
};

/** The subscribed knowledge:changed handler, captured by the mock so tests
 * drive the event exactly as the transport would deliver it. */
let knowledgeChangedHandler: ((event: Record<string, never>) => void) | null =
  null;

vi.mock("../../../../lib/client", () => ({
  client: {
    knowledge: {
      list: vi.fn(async (): Promise<never[]> => []),
      upload: vi.fn(async (): Promise<KnowledgeDocument> => ({
        id: "d-new",
        filename: "uploaded.txt",
        mime: "text/plain",
        size: 5,
        scope: "agent",
        agentId: "a1",
        agentSlug: "scout",
        description: "",
        source: "upload",
        createdAt: "2026-01-01T00:00:00Z",
      })),
      setScope: vi.fn(async (): Promise<KnowledgeDocument> => ({
        id: "d1",
        filename: "runbook.md",
        mime: "text/markdown",
        size: 12,
        scope: "global",
        agentId: "",
        agentSlug: "",
        description: "How to run things",
        source: "upload",
        createdAt: "2026-01-01T00:00:00Z",
      })),
    },
    events: {
      onKnowledgeChanged: vi.fn((handler: () => void): (() => void) => {
        knowledgeChangedHandler = handler;

        return (): void => {
          knowledgeChangedHandler = null;
        };
      }),
    },
  },
}));

import { client } from "../../../../lib/client";

// SAFETY: the client module is mocked with vi.fn members above.
const mockedClient = client as MockedClient;

/** One catalog row; `scope`/`agentId` shape which panel lists it. */
function doc(overrides: Partial<KnowledgeDocument> = {}): KnowledgeDocument {
  return {
    id: "d1",
    filename: "runbook.md",
    mime: "text/markdown",
    size: 12,
    scope: "agent",
    agentId: "a1",
    agentSlug: "scout",
    description: "How to run things",
    source: "upload",
    createdAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  knowledgeChangedHandler = null;
});

describe("KnowledgePanel — listing", () => {
  it("lists the agent's and global documents with scope markers, excluding other agents' docs", async () => {
    mockedClient.knowledge.list.mockImplementation(async () => [
      doc(),
      doc({ id: "d2", scope: "global", agentId: "", agentSlug: "" }),
      doc({ id: "d3", scope: "agent", agentId: "a2", agentSlug: "other" }),
    ]);

    const onOpenDoc = vi.fn();

    const { unmount } = await renderUI(
      <KnowledgePanel agentID="a1" onOpenDoc={onOpenDoc} onBack={vi.fn()} />
    );

    await waitFor(() => screen.getByTestId("knowledge-doc-d1"));

    expect(screen.getByTestId("knowledge-doc-d2")).toBeTruthy();
    // Another agent's scoped document is not this panel's business.
    expect(screen.queryByTestId("knowledge-doc-d3")).toBeNull();

    expect(screen.getByTestId("knowledge-scope-d1").textContent).toBe("agent");
    expect(screen.getByTestId("knowledge-scope-d2").textContent).toBe("global");

    // Name and description ride the row.
    const row = screen.getByTestId("knowledge-doc-d1");

    expect(row.textContent).toContain("runbook.md");
    expect(row.textContent).toContain("How to run things");

    await unmount();
  });

  it("shows the empty state when the visible scopes hold no documents", async () => {
    mockedClient.knowledge.list.mockImplementation(async () => [
      doc({ id: "d3", scope: "agent", agentId: "a2", agentSlug: "other" }),
    ]);

    const { unmount } = await renderUI(
      <KnowledgePanel agentID="a1" onOpenDoc={vi.fn()} onBack={vi.fn()} />
    );

    await waitFor(() => screen.getByTestId("knowledge-empty"));

    expect(screen.queryByTestId("knowledge-list")).toBeNull();

    await unmount();
  });

  it("surfaces a failed list load as an error", async () => {
    mockedClient.knowledge.list.mockImplementation(async () => {
      throw new Error("store closed");
    });

    const { unmount } = await renderUI(
      <KnowledgePanel agentID="a1" onOpenDoc={vi.fn()} onBack={vi.fn()} />
    );

    await waitFor(() =>
      expect(screen.getByTestId("knowledge-error").textContent).toBe(
        "store closed"
      )
    );

    await unmount();
  });
});

describe("KnowledgePanel — activation", () => {
  it("hands the activated document to the bar's opener", async () => {
    const d = doc();

    mockedClient.knowledge.list.mockImplementation(async () => [d]);

    const onOpenDoc = vi.fn();

    const { unmount } = await renderUI(
      <KnowledgePanel agentID="a1" onOpenDoc={onOpenDoc} onBack={vi.fn()} />
    );

    await waitFor(() => screen.getByTestId("knowledge-doc-d1"));

    await act(async () => {
      fireEvent.click(screen.getByTestId("knowledge-doc-d1"));
    });

    expect(onOpenDoc).toHaveBeenCalledWith(d);

    await unmount();
  });
});

describe("KnowledgePanel — upload", () => {
  it("defaults to the agent's scope and passes base64 + agentID", async () => {
    const { unmount } = await renderUI(
      <KnowledgePanel agentID="a1" onOpenDoc={vi.fn()} onBack={vi.fn()} />
    );

    await waitFor(() => screen.getByTestId("knowledge-upload-input"));

    const file = new File(["hello"], "notes.txt", { type: "text/plain" });

    await act(async () => {
      fireEvent.change(screen.getByTestId("knowledge-upload-input"), {
        target: { files: [file] },
      });
    });

    await waitFor(() =>
      expect(mockedClient.knowledge.upload).toHaveBeenCalledWith(
        "notes.txt",
        "text/plain",
        btoa("hello"),
        "agent",
        "a1",
        ""
      )
    );

    await unmount();
  });

  it("passes the typed description and the explicit Global choice", async () => {
    const { unmount } = await renderUI(
      <KnowledgePanel agentID="a1" onOpenDoc={vi.fn()} onBack={vi.fn()} />
    );

    await waitFor(() => screen.getByTestId("knowledge-upload-input"));

    await act(async () => {
      fireEvent.change(screen.getByTestId("knowledge-upload-description"), {
        target: { value: "incident notes" },
      });
      fireEvent.change(screen.getByTestId("knowledge-upload-scope"), {
        target: { value: "global" },
      });
    });

    const file = new File(["body"], "notes.txt", { type: "text/plain" });

    await act(async () => {
      fireEvent.change(screen.getByTestId("knowledge-upload-input"), {
        target: { files: [file] },
      });
    });

    await waitFor(() =>
      expect(mockedClient.knowledge.upload).toHaveBeenCalledWith(
        "notes.txt",
        "text/plain",
        btoa("body"),
        "global",
        "",
        "incident notes"
      )
    );

    await unmount();
  });

  it("rejects an unsupported file client-side with the reason", async () => {
    const { unmount } = await renderUI(
      <KnowledgePanel agentID="a1" onOpenDoc={vi.fn()} onBack={vi.fn()} />
    );

    await waitFor(() => screen.getByTestId("knowledge-upload-input"));

    const file = new File(["x"], "report.docx", {
      type: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
    });

    await act(async () => {
      fireEvent.change(screen.getByTestId("knowledge-upload-input"), {
        target: { files: [file] },
      });
    });

    await waitFor(() =>
      expect(
        screen.getByTestId("knowledge-action-error").textContent
      ).toContain("Unsupported file type")
    );
    expect(mockedClient.knowledge.upload).not.toHaveBeenCalled();

    await unmount();
  });

  it("rejects an oversize text file client-side before the round trip", async () => {
    const { unmount } = await renderUI(
      <KnowledgePanel agentID="a1" onOpenDoc={vi.fn()} onBack={vi.fn()} />
    );

    await waitFor(() => screen.getByTestId("knowledge-upload-input"));

    const file = new File([new Uint8Array((1 << 20) + 1)], "big.txt", {
      type: "text/plain",
    });

    await act(async () => {
      fireEvent.change(screen.getByTestId("knowledge-upload-input"), {
        target: { files: [file] },
      });
    });

    await waitFor(() =>
      expect(screen.getByTestId("knowledge-action-error").textContent).toBe(
        "Exceeds the 1 MB text-file limit"
      )
    );
    expect(mockedClient.knowledge.upload).not.toHaveBeenCalled();

    await unmount();
  });

  it("surfaces a backend upload rejection inline with its reason", async () => {
    mockedClient.knowledge.upload.mockImplementation(
      async (): Promise<KnowledgeDocument> => {
        throw new Error('duplicate filename "notes.txt" in this scope');
      }
    );

    const { unmount } = await renderUI(
      <KnowledgePanel agentID="a1" onOpenDoc={vi.fn()} onBack={vi.fn()} />
    );

    await waitFor(() => screen.getByTestId("knowledge-upload-input"));

    const file = new File(["hello"], "notes.txt", { type: "text/plain" });

    await act(async () => {
      fireEvent.change(screen.getByTestId("knowledge-upload-input"), {
        target: { files: [file] },
      });
    });

    await waitFor(() =>
      expect(
        screen.getByTestId("knowledge-action-error").textContent
      ).toContain('duplicate filename "notes.txt" in this scope')
    );

    await unmount();
  });
});

describe("KnowledgePanel — promote", () => {
  it("calls setScope to global and re-lists the document as global", async () => {
    let listing = [doc()];

    mockedClient.knowledge.list.mockImplementation(
      async (): Promise<KnowledgeDocument[]> => listing
    );

    const { unmount } = await renderUI(
      <KnowledgePanel agentID="a1" onOpenDoc={vi.fn()} onBack={vi.fn()} />
    );

    await waitFor(() => screen.getByTestId("knowledge-doc-d1"));
    expect(screen.getByTestId("knowledge-promote-d1")).toBeTruthy();

    listing = [doc({ scope: "global", agentId: "", agentSlug: "" })];

    await act(async () => {
      fireEvent.click(screen.getByTestId("knowledge-promote-d1"));
    });

    await waitFor(() =>
      expect(mockedClient.knowledge.setScope).toHaveBeenCalledWith(
        "d1",
        "global",
        ""
      )
    );

    // The refetched list shows the global marker and drops the promote
    // action (spec "Promote from the panel").
    await waitFor(() =>
      expect(screen.getByTestId("knowledge-scope-d1").textContent).toBe(
        "global"
      )
    );
    expect(screen.queryByTestId("knowledge-promote-d1")).toBeNull();

    await unmount();
  });

  it("surfaces a promote rejection inline with its reason", async () => {
    mockedClient.knowledge.list.mockImplementation(
      async (): Promise<KnowledgeDocument[]> => [doc()]
    );
    mockedClient.knowledge.setScope.mockImplementation(
      async (): Promise<KnowledgeDocument> => {
        throw new Error('name "runbook.md" already exists in global');
      }
    );

    const { unmount } = await renderUI(
      <KnowledgePanel agentID="a1" onOpenDoc={vi.fn()} onBack={vi.fn()} />
    );

    await waitFor(() => screen.getByTestId("knowledge-promote-d1"));

    await act(async () => {
      fireEvent.click(screen.getByTestId("knowledge-promote-d1"));
    });

    await waitFor(() =>
      expect(
        screen.getByTestId("knowledge-action-error").textContent
      ).toContain('name "runbook.md" already exists in global')
    );

    await unmount();
  });
});

describe("KnowledgePanel — live updates", () => {
  it("refetches when knowledge:changed fires while open", async () => {
    const { unmount } = await renderUI(
      <KnowledgePanel agentID="a1" onOpenDoc={vi.fn()} onBack={vi.fn()} />
    );

    await waitFor(() => expect(knowledgeChangedHandler).not.toBeNull());

    const callsBefore = mockedClient.knowledge.list.mock.calls.length;

    await act(async () => {
      knowledgeChangedHandler!({});
    });

    await waitFor(() =>
      expect(mockedClient.knowledge.list.mock.calls.length).toBe(
        callsBefore + 1
      )
    );

    await unmount();
  });

  it("coalesces a burst of knowledge:changed events into one refetch", async () => {
    const { unmount } = await renderUI(
      <KnowledgePanel agentID="a1" onOpenDoc={vi.fn()} onBack={vi.fn()} />
    );

    await waitFor(() => expect(knowledgeChangedHandler).not.toBeNull());

    const callsBefore = mockedClient.knowledge.list.mock.calls.length;

    await act(async () => {
      knowledgeChangedHandler!({});
      knowledgeChangedHandler!({});
      knowledgeChangedHandler!({});
    });

    await waitFor(() =>
      expect(mockedClient.knowledge.list.mock.calls.length).toBe(
        callsBefore + 1
      )
    );

    await unmount();
  });
});

describe("knowledgeRejection — the corpus pre-gate", () => {
  it("admits text under 1 MiB and PDFs under 20 MiB", () => {
    expect(knowledgeRejection("text/plain", 512)).toBeNull();
    expect(knowledgeRejection("text/markdown", 1 << 20)).toBeNull();
    expect(knowledgeRejection("application/pdf", 19 << 20)).toBeNull();
  });

  it("rejects oversize and unsupported files with the reason", () => {
    expect(knowledgeRejection("text/plain", (1 << 20) + 1)).toBe(
      "Exceeds the 1 MB text-file limit"
    );
    expect(knowledgeRejection("application/pdf", (20 << 20) + 1)).toBe(
      "Exceeds the 20 MB PDF limit"
    );
    expect(knowledgeRejection("application/zip", 10)).toContain(
      "Unsupported file type"
    );
  });
});
