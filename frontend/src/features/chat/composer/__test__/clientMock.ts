/**
 * The composer's mocked Wails client (task 5.x): one factory every composer
 * suite hands to its `vi.mock` call. It lives in its own leaf module — it
 * imports nothing that pulls the chat store in — so the hoisted mock factory
 * can call it before the store (and the store's own client import) is
 * evaluated.
 */
import { vi } from "vite-plus/test";

import type { Event as SessionEvent } from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type {
  Agent,
  Session,
  Skill,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { ModelCapability } from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { Client } from "../../../../lib/client";
import { mockedSurface } from "../../__test__/mockedSurface";

export type MockFn = ReturnType<typeof vi.fn>;

/** The mocked client shape: the Client surface with its vi.fn members. */
export type MockedClient = Client & {
  agents: { list: MockFn };
  chat: {
    sessions: MockFn;
    allSessions: MockFn;
    runningSessions: MockFn;
    createSession: MockFn;
    send: MockFn;
    subscribe: MockFn;
    transcript: MockFn;
    transcriptPage: MockFn;
    contextUsage: MockFn;
    setSessionOverrides: MockFn;
    cancel: MockFn;
    resolveApproval: MockFn;
    attachmentContent: MockFn;
    saveAttachment: MockFn;
  };
  settings: {
    providers: MockFn;
    skills: MockFn;
    providerModels: MockFn;
    tools: MockFn;
  };
};

/** The agent the composer suites chat with. */
export const scout: Agent = {
  ID: "a1",
  Name: "Scout",
  ProviderID: "prov",
  Model: "m-pro",
  Effort: "high",
  Slug: "scout",
  Description: "",
  Brief: "",
  Avatar: null,
  SubagentsEnabled: true,
  PromptsStatus: "ready",
  PromptsError: "",
  ContextWindow: 0,
  Temperature: 0.7,
  MaxTokens: 0,
  Tools: null,
  Skills: null,
  MCPServers: null,
  CreatedAt: "2026-01-01T00:00:00Z",
  UpdatedAt: "2026-01-01T00:00:00Z",
};

/** A second agent, for the agent picker. */
export const runner: Agent = {
  ...scout,
  ID: "a2",
  Name: "Runner",
  Model: "m-base",
  Effort: "",
};

/** The open session. */
export const session: Session = {
  ID: "s1",
  AgentID: "a1",
  Title: "Scout chat",
  Model: "",
  Effort: "",
  CreatedAt: "2026-01-01T00:00:00Z",
  LastActivityAt: "2026-01-01T00:00:00Z",
};

/** The skills the composer's `$` menu lists; the store keeps the enabled ones
 * only, so the disabled one never reaches the composer. */
export const skills: Skill[] = [
  {
    Name: "search",
    Description: "Web search",
    Version: "",
    Source: "",
    Enabled: true,
    CreatedAt: "",
    UpdatedAt: "",
  },
  {
    Name: "secret",
    Description: "Disabled",
    Version: "",
    Source: "",
    Enabled: false,
    CreatedAt: "",
    UpdatedAt: "",
  },
];

/** The provider's model capabilities: one model with efforts, one without. */
export const capabilities: ModelCapability[] = [
  {
    ID: "m-base",
    ContextWindow: 8000,
    Efforts: [],
    ImageInput: false,
    PDFInput: false,
    VideoInput: false,
    PDFParts: true,
    VideoParts: true,
  },
  {
    ID: "m-pro",
    ContextWindow: 200000,
    Efforts: ["low", "medium", "high"],
    ImageInput: true,
    PDFInput: false,
    VideoInput: false,
    PDFParts: true,
    VideoParts: true,
  },
];

/** The live session event handler the mocked subscribe captured, and the turn
 * counter the mocked send advances. */
export interface LiveEvents {
  handler: ((event: SessionEvent) => void) | null;
  turnSeq: number;
}

export const live: LiveEvents = {
  handler: null,
  turnSeq: 0,
};

/** The mocked client: every member the composer (or its rail) can reach. */
export function makeClient(): MockedClient {
  const c = {
    agents: {
      list: vi.fn(async (): Promise<Agent[]> => [scout, runner]),
    },
    chat: {
      sessions: vi.fn(async (agentID: string): Promise<Session[]> =>
        agentID === "a1" ? [session] : [{ ...session, ID: "s2", AgentID: "a2" }]
      ),
      // selectAgent reads the all-agents listing (add-grouped-session-rail
      // design D1); the mock serves the same sessions the per-agent member
      // does, both agents together.
      allSessions: vi.fn(async (): Promise<Session[]> => [
        session,
        { ...session, ID: "s2", AgentID: "a2" },
      ]),
      // No turn in flight in these suites (add-grouped-session-rail D2).
      runningSessions: vi.fn(async (): Promise<string[]> => []),
      createSession: vi.fn(async (): Promise<Session> => ({
        ...session,
        ID: "s-new",
      })),
      send: vi.fn(async (): Promise<string> => {
        live.turnSeq += 1;

        return `turn-${live.turnSeq}`;
      }),
      subscribe: vi.fn(
        async (
          _id: string,
          handler: (event: SessionEvent) => void
        ): Promise<() => void> => {
          live.handler = handler;

          return (): void => {
            live.handler = null;
          };
        }
      ),
      transcript: vi.fn(async (): Promise<never[]> => []),
      transcriptPage: vi.fn(async (): Promise<never[]> => []),
      contextUsage: vi.fn(async () => ({
        used: 0,
        limit: 200000,
        last_input: 0,
        last_output: 0,
      })),
      setSessionOverrides: vi.fn(async (): Promise<void> => undefined),
      cancel: vi.fn(async (): Promise<void> => undefined),
      resolveApproval: vi.fn(async (): Promise<void> => undefined),
      attachmentContent: vi.fn(async (): Promise<never> => {
        throw new Error("no attachment content in tests");
      }),
      saveAttachment: vi.fn(async (): Promise<{ key: string }> => ({
        key: "cap-key-1",
      })),
    },
    settings: {
      providers: vi.fn(async () => [
        { ID: "prov", Name: "Prov Co", Enabled: true },
      ]),
      skills: vi.fn(async (): Promise<Skill[]> => skills),
      providerModels: vi.fn(
        async (): Promise<ModelCapability[]> => capabilities
      ),
      tools: vi.fn(async (): Promise<never[]> => []),
    },
  };

  // SAFETY: the literal covers exactly the surface the composer drives; the
  // Client-typed intersection defeats the structural check, so the conversion
  // funnels through the shared mockedSurface bridge.
  return mockedSurface<MockedClient, object>(c);
}
