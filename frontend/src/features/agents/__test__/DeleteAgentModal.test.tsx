/**
 * Delete-confirmation tests (task 6.2): the copy must keep promising that
 * past sessions are preserved — deleting an agent keeps its session history,
 * and the server-side workspace-directory cleanup is invisible to the user —
 * and the type-the-name gate still arms the destructive call.
 */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
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

vi.mock("../../../lib/client", () => ({ client: makeClient() }));

// Import after the mock is registered.
import { DeleteAgentModal } from "../components/DeleteAgentModal";
import { client } from "../../../lib/client";

// SAFETY: the client module is mocked with makeClient(), so its runtime shape
// is exactly this mock object.
const mockedClient = client as MockedClient;

type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the agent delete call plus the knowledge list
 * the modal reads to state the scoped-documents consequence. */
type MockedClient = Client & {
  agents: {
    delete: MockFn;
  };
  knowledge: {
    list: MockFn;
  };
};

function makeClient(): MockedClient {
  const c = {
    agents: {
      delete: vi.fn(async (): Promise<void> => undefined),
    },
    knowledge: {
      list: vi.fn(async (): Promise<KnowledgeDocument[]> => knowledgeDocs),
    },
  };

  // SAFETY: the object literal above satisfies the Client surface with its
  // vi.fn members matching the MockedClient contract.
  return c as MockedClient;
}

/** One knowledge document row override for the list mock. */
function makeDoc(
  overrides: Partial<KnowledgeDocument> = {}
): KnowledgeDocument {
  return {
    id: "d1",
    filename: "runbook.txt",
    mime: "text/plain",
    size: 128,
    scope: "agent",
    agentId: "a1",
    agentSlug: "scout",
    description: "",
    source: "user",
    createdAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

let knowledgeDocs: KnowledgeDocument[];

function makeAgent(): Agent {
  return {
    ID: "a1",
    Name: "Scout",
    Slug: "scout",
    Description: "",
    Brief: "",
    Avatar: null,
    SubagentsEnabled: true,
    PromptsStatus: "ready",
    PromptsError: "",
    ProviderID: "openai",
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
  };
}

// --- tiny mount helper over react-dom/client (@testing-library/react absent)
const roots: Root[] = [];

async function renderDelete(): Promise<void> {
  // SAFETY: the flag is an optional boolean on the global object.
  (
    globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
  ).IS_REACT_ACT_ENVIRONMENT = true;
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  roots.push(root);
  await act(async () => {
    root.render(
      <DeleteAgentModal
        agent={makeAgent()}
        onClose={() => undefined}
        onDeleted={() => undefined}
      />
    );
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  knowledgeDocs = [];
});

afterEach(() => {
  while (roots.length) {
    const root = roots.pop()!;
    act(() => {
      root.unmount();
    });
  }

  document.body.innerHTML = "";
});

describe("DeleteAgentModal", () => {
  it("promises that past sessions are kept", async () => {
    await renderDelete();

    const copy = screen
      .getByTestId("delete-agent-modal")
      .querySelector("p:last-of-type");

    expect(copy?.textContent).toContain("Past sessions are kept");
    expect(copy?.textContent).toContain("cannot be undone");
  });

  it("fires the delete only after the exact name is typed", async () => {
    await renderDelete();

    // SAFETY: this test id renders a button element.
    const confirm = screen.getByTestId(
      "btn-confirm-delete"
    ) as HTMLButtonElement;

    expect(confirm.disabled).toBe(true);

    await act(async () => {
      fireEvent.click(confirm);
    });
    expect(mockedClient.agents.delete).not.toHaveBeenCalled();

    // SAFETY: this test id renders an input element.
    const nameInput = screen.getByTestId(
      "delete-agent-name-input"
    ) as HTMLInputElement;

    await act(async () => {
      fireEvent.change(nameInput, { target: { value: "Scout" } });
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-confirm-delete"));
    });

    await waitFor(() =>
      expect(mockedClient.agents.delete).toHaveBeenCalledWith("a1")
    );
  });

  // add-knowledge-corpus D9: the confirmation states the knowledge
  // consequence when the agent's scope holds documents.
  it("names the scoped knowledge documents when the agent has some", async () => {
    knowledgeDocs = [makeDoc(), makeDoc({ id: "d2", filename: "spec.md" })];
    await renderDelete();

    const note = await waitFor(() =>
      screen.getByTestId("delete-agent-knowledge-note")
    );

    expect(note.textContent).toContain(
      "2 knowledge documents saved to this agent will be deleted"
    );
    expect(mockedClient.knowledge.list).toHaveBeenCalled();
  });

  it("uses the singular when exactly one scoped document exists", async () => {
    knowledgeDocs = [makeDoc()];
    await renderDelete();

    const note = await waitFor(() =>
      screen.getByTestId("delete-agent-knowledge-note")
    );

    expect(note.textContent).toContain(
      "1 knowledge document saved to this agent will be deleted"
    );
  });

  it("stays silent when no documents are scoped to the agent", async () => {
    knowledgeDocs = [
      makeDoc({ scope: "global", agentId: "", agentSlug: "" }),
      // Another agent's scoped document must not count either.
      makeDoc({ id: "d2", agentId: "a2", agentSlug: "sentinel" }),
    ];
    await renderDelete();

    await waitFor(() => expect(mockedClient.knowledge.list).toHaveBeenCalled());
    expect(screen.queryByTestId("delete-agent-knowledge-note")).toBeNull();
  });
});
