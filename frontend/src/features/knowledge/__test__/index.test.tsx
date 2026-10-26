/**
 * KnowledgeView component tests (add-knowledge-corpus, "Knowledge management
 * menu" + "Live catalog updates"): rows render with scope markers, size, and
 * creation date; the scope filter narrows to all/global/one agent; the
 * upload modal calls upload with base64 + the picked scope and pre-gates a
 * .docx client-side with the named-kinds reason; delete promotes through the
 * confirm dialog; promote/demote call setScope with the right args; the
 * description editor calls setDescription; a markdown document grows the
 * Raw|Preview toggle; and `knowledge:changed` triggers a coalesced refetch.
 * The lib client is mocked wholesale (the scheduler suite's pattern).
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

import type { Agent } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { Client, KnowledgeDocument } from "../../../lib/client";
import type { KnowledgeChangedEvent } from "../../../lib/events";
import { renderUI } from "../../../__test__/test-utils";
import { useNavStore } from "../../../stores/nav";

vi.mock("../../../lib/client", () => ({ client: makeClient() }));

// Import after the mock is registered.
import { client } from "../../../lib/client";
import KnowledgeView from "../index";

// SAFETY: the client module is mocked with makeClient(), so its runtime
// shape is exactly this mock object.
const mockedClient = client as MockedClient;

let docs: KnowledgeDocument[];

let knowledgeChangedHandler: ((e: KnowledgeChangedEvent) => void) | null = null;

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with vi.fn mock members. */
type MockedClient = Client & {
  agents: {
    list: MockFn;
  };
  knowledge: {
    list: MockFn;
    upload: MockFn;
    delete: MockFn;
    setScope: MockFn;
    setDescription: MockFn;
    content: MockFn;
  };
  events: {
    onKnowledgeChanged: MockFn;
    onSettingsOpen: MockFn;
  };
};

/** base64("hello") — the content read's payload for every preview fetch. */
const HELLO_B64 = "aGVsbG8=";

function makeDoc(
  overrides: Partial<KnowledgeDocument> = {}
): KnowledgeDocument {
  return {
    id: "d1",
    filename: "notes.md",
    mime: "text/markdown",
    size: 2048,
    scope: "global",
    agentId: "",
    agentSlug: "",
    description: "Meeting notes",
    source: "user",
    createdAt: "2026-01-05T00:00:00Z",
    ...overrides,
  };
}

function makeClient(): MockedClient {
  const c = {
    agents: {
      list: vi.fn(async (): Promise<Agent[]> => agents),
    },
    knowledge: {
      list: vi.fn(async (): Promise<KnowledgeDocument[]> => docs),
      upload: vi.fn(async (): Promise<KnowledgeDocument> =>
        makeDoc({ id: "d-new", filename: "uploaded.txt" })
      ),
      delete: vi.fn(async (): Promise<void> => undefined),
      setScope: vi.fn(async (): Promise<KnowledgeDocument> => makeDoc()),
      setDescription: vi.fn(async (): Promise<KnowledgeDocument> => makeDoc()),
      content: vi.fn(async (): Promise<string> => HELLO_B64),
    },
    events: {
      onKnowledgeChanged: vi.fn(
        (handler: (e: KnowledgeChangedEvent) => void) => {
          knowledgeChangedHandler = handler;

          return (): void => {
            knowledgeChangedHandler = null;
          };
        }
      ),
      onSettingsOpen: vi.fn(() => (): void => undefined),
    },
  };

  // SAFETY: makeClient() builds exactly this mock shape; the relabel
  // funnels through the same audited conversion the other suites use.
  return c as MockedClient;
}

const agents: Agent[] = [
  {
    ID: "a1",
    Name: "Scout",
    Slug: "scout",
    Description: "",
    Brief: "",
    Avatar: null,
    SubagentsEnabled: true,
    PromptsStatus: "ready",
    PromptsError: "",
    ProviderID: "prov",
    Model: "gpt-test",
    Effort: "",
    Temperature: 0.7,
    MaxTokens: 0,
    ContextWindow: 0,
    Tools: null,
    Skills: null,
    MCPServers: null,
    CreatedAt: "2026-01-01T00:00:00Z",
    UpdatedAt: "2026-01-01T00:00:00Z",
  },
  {
    ID: "a2",
    Name: "Sentinel",
    Slug: "sentinel",
    Description: "",
    Brief: "",
    Avatar: null,
    SubagentsEnabled: true,
    PromptsStatus: "ready",
    PromptsError: "",
    ProviderID: "prov",
    Model: "gpt-test",
    Effort: "",
    Temperature: 0.7,
    MaxTokens: 0,
    ContextWindow: 0,
    Tools: null,
    Skills: null,
    MCPServers: null,
    CreatedAt: "2026-01-01T00:00:00Z",
    UpdatedAt: "2026-01-01T00:00:00Z",
  },
];

const unmounts: Array<() => Promise<void>> = [];

async function renderView(): Promise<void> {
  const view = await renderUI(<KnowledgeView />);

  unmounts.push(view.unmount);
}

/** The rendered kit Select: an HTMLSelectElement by construction. */
function selectOf(testId: string): HTMLSelectElement {
  const el = screen.getByTestId(testId);

  if (!(el instanceof HTMLSelectElement))
    throw new Error(`${testId} is not a select`);

  return el;
}

/** The rendered kit Input: an HTMLInputElement by construction. */
function inputOf(testId: string): HTMLInputElement {
  const el = screen.getByTestId(testId);

  if (!(el instanceof HTMLInputElement))
    throw new Error(`${testId} is not an input`);

  return el;
}

/** Drives the upload modal's hidden file input with one file (the
 * attachments suite's pattern). */
async function chooseFile(file: File): Promise<void> {
  const input = document.querySelector<HTMLInputElement>(
    'input[data-testid="knowledge-upload-file"]'
  );

  if (input === null) throw new Error("the upload modal opened no file input");

  await act(async () => {
    fireEvent.change(input, { target: { files: [file] } });
  });
}

/** Fires the captured subscription once (payload-free event). */
function fireKnowledgeChanged(): void {
  knowledgeChangedHandler?.(
    // SAFETY: the backend emits this event with an empty payload — the view
    // re-lists without reading it, so the empty event object is exact.
    {} as KnowledgeChangedEvent
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  docs = [
    makeDoc(),
    makeDoc({
      id: "d2",
      filename: "runbook.txt",
      mime: "text/plain",
      scope: "agent",
      agentId: "a1",
      agentSlug: "scout",
      description: "Ops runbook",
      size: 512,
    }),
  ];
  useNavStore.setState({ menu: "knowledge", chatTarget: null });
});

afterEach(async (): Promise<void> => {
  while (unmounts.length > 0) {
    const unmount = unmounts.pop();

    if (unmount) await unmount();
  }

  document.body.innerHTML = "";
});

describe("KnowledgeView", () => {
  it("lists documents with names, scope markers, sizes, and creation dates", async () => {
    await renderView();

    const globalRow = await waitFor(() =>
      screen.getByTestId("knowledge-row-d1")
    );

    expect(globalRow.textContent).toContain("notes.md");
    expect(globalRow.textContent).toContain("Meeting notes");
    expect(screen.getByTestId("knowledge-scope-d1").textContent).toBe("Global");
    expect(screen.getByTestId("knowledge-size-d1").textContent).toBe("2 KB");
    expect(screen.getByTestId("knowledge-created-d1").textContent).toContain(
      "2026"
    );

    const agentRow = screen.getByTestId("knowledge-row-d2");

    expect(agentRow.textContent).toContain("runbook.txt");
    // The agent scope shows the agent's display name, not the raw scope.
    expect(screen.getByTestId("knowledge-scope-d2").textContent).toBe("Scout");
    expect(screen.getByTestId("knowledge-size-d2").textContent).toBe("512 B");
  });

  it("narrows the list through the scope filter (all / global / one agent)", async () => {
    await renderView();
    await waitFor(() => screen.getByTestId("knowledge-row-d1"));

    const filter = selectOf("knowledge-scope-filter");
    expect(filter.value).toBe("all");
    expect(screen.getByTestId("knowledge-row-d1")).toBeTruthy();
    expect(screen.getByTestId("knowledge-row-d2")).toBeTruthy();

    await act(async () => {
      fireEvent.change(filter, { target: { value: "global" } });
    });
    expect(screen.getByTestId("knowledge-row-d1")).toBeTruthy();
    expect(screen.queryByTestId("knowledge-row-d2")).toBeNull();

    await act(async () => {
      fireEvent.change(filter, { target: { value: "a1" } });
    });
    expect(screen.queryByTestId("knowledge-row-d1")).toBeNull();
    expect(screen.getByTestId("knowledge-row-d2")).toBeTruthy();

    await act(async () => {
      fireEvent.change(filter, { target: { value: "a2" } });
    });
    expect(screen.getByTestId("knowledge-filter-empty")).toBeTruthy();
  });

  it("renders the selected document's decoded text preview", async () => {
    await renderView();
    await waitFor(() => screen.getByTestId("knowledge-row-d2"));

    await act(async () => {
      fireEvent.click(screen.getByTestId("knowledge-row-d2"));
    });

    const content = await waitFor(() =>
      screen.getByTestId("knowledge-preview-content")
    );

    expect(mockedClient.knowledge.content).toHaveBeenCalledWith("d2");
    expect(content.textContent).toBe("hello");
  });

  it("grows the Raw|Preview toggle for markdown, Raw the default", async () => {
    await renderView();
    await waitFor(() => screen.getByTestId("knowledge-row-d1"));

    await act(async () => {
      fireEvent.click(screen.getByTestId("knowledge-row-d1"));
    });

    await waitFor(() => screen.getByTestId("knowledge-preview-modes"));
    // Raw default: the mono body renders, the markdown pipeline does not.
    expect(screen.getByTestId("knowledge-preview-content")).toBeTruthy();
    expect(screen.queryByTestId("knowledge-preview-markdown")).toBeNull();

    await act(async () => {
      fireEvent.click(screen.getByTestId("knowledge-preview-mode-preview"));
    });
    expect(screen.getByTestId("knowledge-preview-markdown")).toBeTruthy();

    await act(async () => {
      fireEvent.click(screen.getByTestId("knowledge-preview-mode-raw"));
    });
    expect(screen.getByTestId("knowledge-preview-content")).toBeTruthy();
  });

  it("uploads through the modal with base64, scope, and description", async () => {
    await renderView();
    await waitFor(() => screen.getByTestId("knowledge-row-d1"));

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-upload-knowledge"));
    });
    expect(screen.getByTestId("knowledge-upload-modal")).toBeTruthy();
    // Global is the default scope in the menu.
    expect(selectOf("knowledge-upload-scope").value).toBe("");

    await chooseFile(
      new File(["hello"], "runbook.txt", { type: "text/plain" })
    );
    expect(screen.getByTestId("knowledge-upload-filename").textContent).toBe(
      "runbook.txt · 5 bytes"
    );

    await act(async () => {
      fireEvent.change(selectOf("knowledge-upload-scope"), {
        target: { value: "a1" },
      });
    });
    await act(async () => {
      fireEvent.change(inputOf("knowledge-upload-description"), {
        target: { value: "ops doc" },
      });
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-confirm-upload"));
    });

    await waitFor(() =>
      expect(mockedClient.knowledge.upload).toHaveBeenCalledWith(
        "runbook.txt",
        "text/plain",
        HELLO_B64,
        "agent",
        "a1",
        "ops doc"
      )
    );
    await waitFor(() =>
      expect(screen.queryByTestId("knowledge-upload-modal")).toBeNull()
    );
  });

  it("rejects a .docx client-side with the named-kinds reason", async () => {
    await renderView();
    await waitFor(() => screen.getByTestId("knowledge-row-d1"));

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-upload-knowledge"));
    });
    await chooseFile(
      new File(["zz"], "report.docx", {
        type: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
      })
    );
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-confirm-upload"));
    });

    const error = screen.getByTestId("knowledge-upload-error");

    expect(error.textContent).toContain(
      'Unsupported file type "application/vnd.openxmlformats-officedocument.wordprocessingml.document"'
    );
    expect(error.textContent).toContain(
      "only text and PDF documents are accepted"
    );
    expect(mockedClient.knowledge.upload).not.toHaveBeenCalled();
  });

  it("rejects an oversize text file client-side with the size reason", async () => {
    await renderView();
    await waitFor(() => screen.getByTestId("knowledge-row-d1"));

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-upload-knowledge"));
    });
    await chooseFile(
      new File([new ArrayBuffer((1 << 20) + 1)], "big.txt", {
        type: "text/plain",
      })
    );
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-confirm-upload"));
    });

    expect(screen.getByTestId("knowledge-upload-error").textContent).toBe(
      "Exceeds the 1 MB text-file limit"
    );
    expect(mockedClient.knowledge.upload).not.toHaveBeenCalled();
  });

  it("gates delete behind the confirm dialog, then deletes and refetches", async () => {
    await renderView();
    await waitFor(() => screen.getByTestId("knowledge-row-d2"));

    await act(async () => {
      fireEvent.click(screen.getByTestId("knowledge-row-d2"));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-delete-doc"));
    });
    expect(screen.getByTestId("delete-knowledge-dialog")).toBeTruthy();
    expect(mockedClient.knowledge.delete).not.toHaveBeenCalled();

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-cancel-delete-doc"));
    });
    await waitFor(() =>
      expect(screen.queryByTestId("delete-knowledge-dialog")).toBeNull()
    );
    expect(mockedClient.knowledge.delete).not.toHaveBeenCalled();

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-delete-doc"));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-confirm-delete-doc"));
    });

    await waitFor(() =>
      expect(mockedClient.knowledge.delete).toHaveBeenCalledWith("d2")
    );
  });

  it("promotes an agent-scoped document to global", async () => {
    await renderView();
    await waitFor(() => screen.getByTestId("knowledge-row-d2"));

    await act(async () => {
      fireEvent.click(screen.getByTestId("knowledge-row-d2"));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-promote-doc"));
    });

    await waitFor(() =>
      expect(mockedClient.knowledge.setScope).toHaveBeenCalledWith(
        "d2",
        "global",
        ""
      )
    );
  });

  it("demotes a global document through the agent picker", async () => {
    await renderView();
    await waitFor(() => screen.getByTestId("knowledge-row-d1"));

    await act(async () => {
      fireEvent.click(screen.getByTestId("knowledge-row-d1"));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-demote-doc"));
    });
    expect(screen.getByTestId("knowledge-demote-modal")).toBeTruthy();
    expect(mockedClient.knowledge.setScope).not.toHaveBeenCalled();

    await act(async () => {
      fireEvent.change(selectOf("knowledge-demote-agent"), {
        target: { value: "a1" },
      });
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-confirm-demote"));
    });

    await waitFor(() =>
      expect(mockedClient.knowledge.setScope).toHaveBeenCalledWith(
        "d1",
        "agent",
        "a1"
      )
    );
  });

  it("edits the description through setDescription and refetches", async () => {
    await renderView();
    await waitFor(() => screen.getByTestId("knowledge-row-d1"));

    await act(async () => {
      fireEvent.click(screen.getByTestId("knowledge-row-d1"));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("knowledge-desc-edit-d1"));
    });

    const input = inputOf("knowledge-desc-input");

    expect(input.value).toBe("Meeting notes");

    await act(async () => {
      fireEvent.change(input, { target: { value: "Updated notes" } });
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("knowledge-desc-save"));
    });

    await waitFor(() =>
      expect(mockedClient.knowledge.setDescription).toHaveBeenCalledWith(
        "d1",
        "Updated notes"
      )
    );
    await waitFor(() =>
      expect(mockedClient.knowledge.list.mock.calls.length).toBeGreaterThan(1)
    );
  });

  it("refetches when knowledge:changed fires", async () => {
    await renderView();
    await waitFor(() => screen.getByTestId("knowledge-row-d1"));
    expect(knowledgeChangedHandler).not.toBeNull();
    const callsBefore = mockedClient.knowledge.list.mock.calls.length;

    await act(async () => {
      fireKnowledgeChanged();
    });

    await waitFor(() =>
      expect(mockedClient.knowledge.list.mock.calls.length).toBeGreaterThan(
        callsBefore
      )
    );
  });

  it("coalesces a burst of events into one refetch", async () => {
    await renderView();
    await waitFor(() => screen.getByTestId("knowledge-row-d1"));
    const callsBefore = mockedClient.knowledge.list.mock.calls.length;

    await act(async () => {
      fireKnowledgeChanged();
      fireKnowledgeChanged();
      fireKnowledgeChanged();
    });

    await waitFor(() =>
      expect(mockedClient.knowledge.list.mock.calls.length).toBe(
        callsBefore + 1
      )
    );
  });

  it("shows the error state when the list fetch fails", async () => {
    mockedClient.knowledge.list.mockRejectedValueOnce(
      new Error("store unavailable")
    );
    await renderView();

    const error = await waitFor(() => screen.getByTestId("knowledge-error"));

    expect(error.textContent).toContain("store unavailable");
  });
});
