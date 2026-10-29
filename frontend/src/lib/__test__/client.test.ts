import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import {
  Kind,
  type Event as SessionEvent,
} from "../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type {
  Preview,
  ScheduleInput,
  ScheduleView,
} from "../../../bindings/github.com/oniharnantyo/buddies/internal/scheduler/models";
import type {
  GatewaySettings,
  Hook,
  HookExecution,
  MCPServer,
  Session,
  Skill,
} from "../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type {
  AgentHookAssignment,
  BrowserConfig,
  BrowserProfile,
  DocumentPresetEntry,
  GatewayStateEvent,
  GeneratedDocumentContent,
  IntegrationConnectInput,
  KnowledgeDocument,
  ModelCapability,
  ProviderInput,
  ProviderModelView,
  ProviderView,
  SchedulesChangedEvent,
  ToolEntry,
  VerifyResult,
  WebSearchConfig,
  WebSearchProviderInfo,
} from "../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import { createClient, type Client } from "../client";
import {
  MCP_OAUTH_COMPLETED_EVENT,
  SCHEDULES_CHANGED_EVENT,
  SETTINGS_OPEN_EVENT,
  sessionEventName,
  type MCPOAuthCompletedEvent,
} from "../events";
import type {
  EventHandler,
  EventPayload,
  Transport,
  Unsubscribe,
} from "../transport";

function makeSessionEvent(
  kind: SessionEvent["kind"],
  text?: string
): SessionEvent {
  return { kind, occurred_at: new Date().toISOString(), text };
}

/** A write-input provider (ProviderInput: only client-chosen fields). */
function makeProviderInput(): ProviderInput {
  return {
    Type: "anthropic",
    Name: "anthropic",
    BaseURL: "",
    APIKey: "secret-key",
    Enabled: true,
  };
}

/** The secret-free read shape the backend returns for one provider. */
function makeProviderView(): ProviderView {
  return {
    ID: "p1",
    Type: "anthropic",
    Name: "anthropic",
    BaseURL: "",
    Enabled: true,
    KeySet: true,
    KeyHint: "-key",
    CreatedAt: "",
    UpdatedAt: "",
  };
}

/** A fresh-draft hook: empty timestamps, as the settings panes build them. */
function makeHook(): Hook {
  return {
    ID: "",
    Name: "notify",
    Event: "turn_completed",
    Matcher: "",
    HandlerType: "command",
    Config: "{}",
    TimeoutMS: 0,
    OnFailure: "",
    Enabled: true,
    Position: 0,
    Status: "",
    StatusError: "",
    CreatedAt: "",
    UpdatedAt: "",
  };
}

/** A fresh-draft MCP server: empty timestamps, as the settings panes build them. */
function makeMCPServer(): MCPServer {
  return {
    ID: "",
    Name: "fetch",
    Transport: "stdio",
    Command: "npx",
    Args: ["-y", "fetch"],
    Env: {},
    URL: "",
    Headers: {},
    Auth: {},
    Service: "",
    CredentialSet: false,
    CredentialHint: "",
    Enabled: true,
    Status: "",
    StatusError: "",
    ToolCount: 0,
    CreatedAt: "",
    UpdatedAt: "",
  };
}

/** A write-input schedule: only the five client-chosen fields (design D13). */
function makeScheduleInput(): ScheduleInput {
  return {
    name: "Morning digest",
    agentId: "a1",
    prompt: "Summarize my inbox",
    cron: "0 9 * * *",
    enabled: true,
  };
}

/** The read shape the backend returns for one schedule; the pointer times
 * bound as `string | null` (add-scheduler D13). */
function makeScheduleView(): ScheduleView {
  return {
    id: "sch-1",
    name: "Morning digest",
    agentId: "a1",
    prompt: "Summarize my inbox",
    cron: "0 9 * * *",
    description: "Every day at 9:00",
    enabled: true,
    nextFireAt: "2026-10-22T09:00:00+07:00",
    lastRunAt: null,
    lastRunStatus: "",
    lastRunError: "",
    lastRunSessionId: "",
    createdAt: "2026-10-21T00:00:00Z",
    updatedAt: "2026-10-21T00:00:00Z",
  };
}

type MockFn = ReturnType<typeof vi.fn>;

/** The read shape the backend returns for one knowledge document
 * (add-knowledge-corpus D8). */
function makeKnowledgeDocument(): KnowledgeDocument {
  return {
    id: "doc-1",
    filename: "notes.md",
    mime: "text/markdown",
    size: 128,
    scope: "agent",
    agentId: "a1",
    agentSlug: "scout",
    description: "Scout's field notes",
    source: "user",
    createdAt: "2026-10-25T00:00:00Z",
  };
}

/** The read shape the backend returns for one document preset
 * (add-create-document D4). */
function makeDocumentPreset(): DocumentPresetEntry {
  return {
    id: "preset-1",
    name: "Client Report",
    format: "pdf",
    config: '{"schema_version":1,"theme":"corporate"}',
    hasTemplate: false,
    createdAt: "2026-10-26T00:00:00Z",
    updatedAt: "2026-10-26T00:00:00Z",
  };
}

/** The read shape the backend returns for one generated document
 * (add-create-document D7). */
function makeGeneratedDocument(): GeneratedDocumentContent {
  return { name: "report.pdf", sizeBytes: 9, data: "cG5mLWJ5dGVz" };
}

/** Guard for mock-transport members the cases under test never reach. */
function unreachable(what: string): never {
  throw new Error(`${what} is not stubbed in this mock transport`);
}

/** The mock transport surface returned by makeTransport(). */
interface MockTransport {
  transport: Transport;
  calls: string[];
  emit(name: string, data?: EventPayload): void;
  active(name: string): number;
  chat: {
    CreateSession: MockFn;
    Send: MockFn;
    Cancel: MockFn;
    Regenerate: MockFn;
    Sessions: MockFn;
    AllSessions: MockFn;
    RunningSessions: MockFn;
    SetSessionOverrides: MockFn;
    ResolveApproval: MockFn;
    Subscribe: MockFn;
    Transcript: MockFn;
    TranscriptPage: MockFn;
    DeleteSession: MockFn;
    SaveAttachment: MockFn;
    AttachmentContent: MockFn;
    WorkspaceFile: MockFn;
    WorkspaceFileBinary: MockFn;
    ListWorkspace: MockFn;
    Unsubscribe: MockFn;
    TurnState: MockFn;
    ContextUsage: MockFn;
    BrowserScreenshot: MockFn;
    GeneratedDocument: MockFn;
  };
  appInfo: { Get: MockFn };
  settings: Transport["settings"];
  gateway: { State: MockFn; Start: MockFn; Pause: MockFn; Resume: MockFn };
  schedules: {
    List: MockFn;
    Get: MockFn;
    Create: MockFn;
    Update: MockFn;
    SetEnabled: MockFn;
    Delete: MockFn;
    RunNow: MockFn;
    Preview: MockFn;
  };
  knowledge: {
    List: MockFn;
    Upload: MockFn;
    Delete: MockFn;
    SetScope: MockFn;
    SetDescription: MockFn;
    Content: MockFn;
  };
  unsubscribe: Unsubscribe;
}

/** Minimal mock transport: recordable calls and manual event dispatch. */
function makeTransport(): MockTransport {
  const subscriptions = new Map<string, Set<EventHandler>>();
  const calls: string[] = [];

  const chat = {
    CreateSession: vi.fn(async (agentID: string) => {
      calls.push(`chat.CreateSession:${agentID}`);

      const session: Session = {
        ID: "s1",
        AgentID: agentID,
        Title: "",
        Model: "",
        Effort: "",
        CreatedAt: new Date().toISOString(),
        LastActivityAt: new Date().toISOString(),
      };

      return session;
    }),
    Send: vi.fn(async (sessionID: string, message: string) => {
      calls.push(`chat.Send:${sessionID}:${message}`);

      return "turn-1";
    }),
    Cancel: vi.fn(async (sessionID: string) => {
      calls.push(`chat.Cancel:${sessionID}`);
    }),
    Regenerate: vi.fn(async (sessionID: string, turnID: string) => {
      calls.push(`chat.Regenerate:${sessionID}:${turnID}`);

      return "turn-2";
    }),
    Sessions: vi.fn(async () => []),
    AllSessions: vi.fn(async () => []),
    RunningSessions: vi.fn(async () => []),
    SetSessionOverrides: vi.fn(
      async (sessionID: string, model: string, effort: string) => {
        calls.push(`chat.SetSessionOverrides:${sessionID}:${model}:${effort}`);
      }
    ),
    ResolveApproval: vi.fn(
      async (sessionID: string, interruptID: string, approved: boolean) => {
        calls.push(
          `chat.ResolveApproval:${sessionID}:${interruptID}:${approved}`
        );
      }
    ),
    Subscribe: vi.fn(async (sessionID: string) => {
      calls.push(`chat.Subscribe:${sessionID}`);
    }),
    Transcript: vi.fn(async () => []),
    TranscriptPage: vi.fn(async () => []),
    DeleteSession: vi.fn(async (sessionID: string) => {
      calls.push(`chat.DeleteSession:${sessionID}`);
    }),
    SaveAttachment: vi.fn(async (name: string, mime: string, data: string) => {
      calls.push(`chat.SaveAttachment:${name}:${mime}`);

      return {
        key: "0123456789abcdef0123456789abcdef",
        name,
        mime,
        size: data.length,
      };
    }),
    AttachmentContent: vi.fn(async (key: string) => {
      calls.push(`chat.AttachmentContent:${key}`);

      return {
        key,
        name: "shot.png",
        mime: "image/png",
        size: 9,
        data: "cG5nLWJ5dGVz",
      };
    }),
    WorkspaceFile: vi.fn(async (agentID: string, relPath: string) => {
      calls.push(`chat.WorkspaceFile:${agentID}:${relPath}`);

      return "workspace notes";
    }),
    WorkspaceFileBinary: vi.fn(async (agentID: string, relPath: string) => {
      calls.push(`chat.WorkspaceFileBinary:${agentID}:${relPath}`);

      return {
        mime: "application/pdf",
        sizeBytes: 9,
        data: "cG5mLWJ5dGVz",
      };
    }),
    ListWorkspace: vi.fn(async () => []),
    Unsubscribe: vi.fn(async (sessionID: string) => {
      calls.push(`chat.Unsubscribe:${sessionID}`);
    }),
    TurnState: vi.fn(async (sessionID: string) => {
      calls.push(`chat.TurnState:${sessionID}`);

      return {
        busy: false,
        turn_id: "",
      };
    }),
    ContextUsage: vi.fn(async () => ({
      used: 0,
      limit: 200000,
      last_input: 0,
      last_output: 0,
    })),
    BrowserScreenshot: vi.fn(async (name: string) => ({
      name,
      width: 1,
      height: 1,
      data: "cG5nLWJ5dGVz",
    })),
    GeneratedDocument: vi.fn(async (agentID: string, name: string) => ({
      name,
      sizeBytes: 9,
      data: "cG5mLWJ5dGVz",
    })),
  };

  const appInfo = {
    Get: vi.fn(async () => {
      calls.push("appInfo.Get");

      return { name: "buddies", version: "0.0.0" };
    }),
  };

  // SAFETY: only the provider read and verify methods are stubbed with
  // recording mocks; the rest of the settings surface is unreachable here.
  const settings: Transport["settings"] = {
    Providers: vi.fn(async (): Promise<ProviderView[] | null> => []),
    Provider: (): Promise<ProviderView> => unreachable("settings.Provider"),
    CreateProvider: (): Promise<ProviderView> =>
      unreachable("settings.CreateProvider"),
    UpdateProvider: (): Promise<ProviderView> =>
      unreachable("settings.UpdateProvider"),
    DeleteProvider: (): Promise<void> => unreachable("settings.DeleteProvider"),
    ProviderModels: (): Promise<ModelCapability[] | null> =>
      unreachable("settings.ProviderModels"),
    CreateModel: (): Promise<ProviderModelView | null> =>
      unreachable("settings.CreateModel"),
    UpdateModel: (): Promise<ProviderModelView | null> =>
      unreachable("settings.UpdateModel"),
    DeleteModel: (): Promise<void> => unreachable("settings.DeleteModel"),
    ToggleModel: (): Promise<ProviderModelView | null> =>
      unreachable("settings.ToggleModel"),
    ListProviderModels: (): Promise<(ProviderModelView | null)[] | null> =>
      unreachable("settings.ListProviderModels"),
    VerifyProviderDraft: vi.fn(async (): Promise<VerifyResult> => ({
      OK: true,
      Detail: "",
    })),
    VerifyProvider: vi.fn(async (): Promise<VerifyResult> => ({
      OK: true,
      Detail: "",
    })),
    Skills: (): Promise<Skill[]> => unreachable("settings.Skills"),
    SetSkillEnabled: (name: string, enabled: boolean): Promise<void> =>
      unreachable(`settings.SetSkillEnabled:${name}:${enabled}`),
    Tools: (): Promise<ToolEntry[]> => unreachable("settings.Tools"),
    SetToolEnabled: (name: string, enabled: boolean): Promise<void> =>
      unreachable(`settings.SetToolEnabled:${name}:${enabled}`),
    WebSearchProviders: (): Promise<WebSearchProviderInfo[] | null> =>
      unreachable("settings.WebSearchProviders"),
    WebSearchConfig: (): Promise<WebSearchConfig> =>
      unreachable("settings.WebSearchConfig"),
    SaveWebSearchConfig: (_config: WebSearchConfig): Promise<void> =>
      unreachable("settings.SaveWebSearchConfig"),
    BrowserConfig: (): Promise<BrowserConfig> =>
      unreachable("settings.BrowserConfig"),
    SaveBrowserConfig: (_config: BrowserConfig): Promise<void> =>
      unreachable("settings.SaveBrowserConfig"),
    BrowserProfiles: (): Promise<BrowserProfile[] | null> =>
      unreachable("settings.BrowserProfiles"),
    CreateBrowserProfile: (_name: string): Promise<void> =>
      unreachable("settings.CreateBrowserProfile"),
    DeleteBrowserProfile: (_name: string): Promise<void> =>
      unreachable("settings.DeleteBrowserProfile"),
    ListDocumentPresets: (
      _format: string
    ): Promise<DocumentPresetEntry[] | null> =>
      unreachable("settings.ListDocumentPresets"),
    CreateDocumentPreset: (
      _format: string,
      _name: string,
      _configJSON: string
    ): Promise<DocumentPresetEntry> =>
      unreachable("settings.CreateDocumentPreset"),
    UpdateDocumentPreset: (
      _id: string,
      _name: string,
      _configJSON: string
    ): Promise<DocumentPresetEntry> =>
      unreachable("settings.UpdateDocumentPreset"),
    DuplicateDocumentPreset: (
      _id: string,
      _newName: string
    ): Promise<DocumentPresetEntry> =>
      unreachable("settings.DuplicateDocumentPreset"),
    DeleteDocumentPreset: (_id: string): Promise<void> =>
      unreachable("settings.DeleteDocumentPreset"),
    DefaultDocumentPreset: (_format: string): Promise<string> =>
      unreachable("settings.DefaultDocumentPreset"),
    SetDefaultDocumentPreset: (_format: string, _name: string): Promise<void> =>
      unreachable("settings.SetDefaultDocumentPreset"),
    SetDocumentPresetTemplate: (
      _presetID: string,
      _filename: string,
      _base64Data: string
    ): Promise<void> => unreachable("settings.SetDocumentPresetTemplate"),
    Hooks: (): Promise<Hook[]> => unreachable("settings.Hooks"),
    CreateHook: (hook: Hook): Promise<Hook> =>
      unreachable(`settings.CreateHook:${hook.Name}`),
    UpdateHook: (hook: Hook): Promise<Hook> =>
      unreachable(`settings.UpdateHook:${hook.Name}`),
    DeleteHook: (id: string): Promise<void> =>
      unreachable(`settings.DeleteHook:${id}`),
    AgentHooks: (): Promise<AgentHookAssignment[] | null> =>
      unreachable("settings.AgentHooks"),
    AssignHook: (): Promise<void> => unreachable("settings.AssignHook"),
    UnassignHook: (): Promise<void> => unreachable("settings.UnassignHook"),
    HookExecutions: (): Promise<HookExecution[] | null> =>
      unreachable("settings.HookExecutions"),
    MCPServers: (): Promise<MCPServer[]> => unreachable("settings.MCPServers"),
    MCPServer: (id: string): Promise<MCPServer> =>
      unreachable(`settings.MCPServer:${id}`),
    CreateMCPServer: (server: MCPServer): Promise<MCPServer> =>
      unreachable(`settings.CreateMCPServer:${server.Name}`),
    UpdateMCPServer: (server: MCPServer): Promise<MCPServer> =>
      unreachable(`settings.UpdateMCPServer:${server.Name}`),
    DeleteMCPServer: (id: string): Promise<void> =>
      unreachable(`settings.DeleteMCPServer:${id}`),
    ProbeMCPServer: (id: string): Promise<MCPServer> =>
      unreachable(`settings.ProbeMCPServer:${id}`),
    AuthorizeMCPServer: (id: string): Promise<void> =>
      unreachable(`settings.AuthorizeMCPServer:${id}`),
    DisconnectMCPServer: (id: string): Promise<MCPServer> =>
      unreachable(`settings.DisconnectMCPServer:${id}`),
    ConnectIntegration: (_input: IntegrationConnectInput): Promise<MCPServer> =>
      unreachable("settings.ConnectIntegration"),
    DisconnectIntegration: (id: string): Promise<void> =>
      unreachable(`settings.DisconnectIntegration:${id}`),
    Gateway: (): Promise<GatewaySettings> => unreachable("settings.Gateway"),
    SaveGateway: (_settings: GatewaySettings): Promise<void> =>
      unreachable("settings.SaveGateway"),
  };

  const gateway = {
    State: vi.fn(async () => "running"),
    Start: vi.fn(async () => undefined),
    Pause: vi.fn(async () => undefined),
    Resume: vi.fn(async () => undefined),
  };

  // SAFETY: the schedules surface is fully stubbed with recording mocks; the
  // cases below drive every method, so no unreachable members exist here.
  const schedules = {
    List: vi.fn(async (): Promise<ScheduleView[] | null> => [
      makeScheduleView(),
    ]),
    Get: vi.fn(async (_id: string): Promise<ScheduleView> =>
      makeScheduleView()
    ),
    Create: vi.fn(async (_input: ScheduleInput): Promise<ScheduleView> =>
      makeScheduleView()
    ),
    Update: vi.fn(
      async (_id: string, _input: ScheduleInput): Promise<ScheduleView> =>
        makeScheduleView()
    ),
    SetEnabled: vi.fn(
      async (_id: string, _enabled: boolean): Promise<ScheduleView> =>
        makeScheduleView()
    ),
    Delete: vi.fn(async (_id: string): Promise<void> => undefined),
    RunNow: vi.fn(async (_id: string): Promise<ScheduleView> =>
      makeScheduleView()
    ),
    Preview: vi.fn(async (_expr: string): Promise<Preview> => ({
      ok: true,
      description: "Every day at 9:00",
      next: ["2026-10-22T09:00:00+07:00"],
    })),
  };

  const windowTheme = {
    SetLight: vi.fn(async () => undefined),
  };

  const knowledge = {
    List: vi.fn(async (): Promise<KnowledgeDocument[] | null> => [
      makeKnowledgeDocument(),
    ]),
    Upload: vi.fn(
      async (
        _filename: string,
        _mime: string,
        _data: string,
        _scope: string,
        _agentID: string,
        _description: string
      ): Promise<KnowledgeDocument> => makeKnowledgeDocument()
    ),
    Delete: vi.fn(async (_id: string): Promise<void> => undefined),
    SetScope: vi.fn(
      async (
        _id: string,
        _scope: string,
        _agentID: string
      ): Promise<KnowledgeDocument> => makeKnowledgeDocument()
    ),
    SetDescription: vi.fn(
      async (_id: string, _description: string): Promise<KnowledgeDocument> =>
        makeKnowledgeDocument()
    ),
    Content: vi.fn(async (_id: string): Promise<string> => "aGVsbG8="),
  };

  const unsubscribe: Unsubscribe = () => {
    calls.push("off");
  };

  const transport: Transport = {
    appInfo,
    // SAFETY: the client wrapper under test never calls agent methods in
    // these cases, so empty stubs satisfy the transport contract.
    agents: {} as Transport["agents"],
    chat,
    settings,
    gateway,
    schedules,
    knowledge,
    windowTheme,
    onEvent: (name: string, handler: EventHandler) => {
      calls.push(`onEvent:${name}`);
      let set = subscriptions.get(name);

      if (!set) {
        set = new Set();
        subscriptions.set(name, set);
      }

      set.add(handler);

      return (): void => {
        set.delete(handler);
      };
    },
  };

  function emit(name: string, data?: EventPayload): void {
    for (const handler of subscriptions.get(name) ?? []) {
      // SAFETY: each channel's client wrapper narrows the payload; the
      // settings:open channel carries no payload and ignores the argument.
      handler(data as EventPayload);
    }
  }

  function active(name: string): number {
    return subscriptions.get(name)?.size ?? 0;
  }

  return {
    transport,
    calls,
    emit,
    active,
    chat,
    appInfo,
    settings,
    gateway,
    schedules,
    knowledge,
    unsubscribe,
  };
}

describe("client wrapper", () => {
  let t: ReturnType<typeof makeTransport>;
  let client: Client;

  beforeEach(() => {
    t = makeTransport();
    client = createClient(t.transport);
  });

  it("routes app info calls through the transport", async () => {
    const meta = await client.appInfo.get();
    expect(meta).toEqual({ name: "buddies", version: "0.0.0" });
    expect(t.calls).toContain("appInfo.Get");
  });

  it("routes chat.send and returns the turn id", async () => {
    const turnID = await client.chat.send("sess-1", "hello");
    expect(turnID).toBe("turn-1");
    expect(t.chat.Send).toHaveBeenCalledWith("sess-1", "hello", null);
  });

  it("routes chat.send with attachment capability keys", async () => {
    const keys = ["0123456789abcdef0123456789abcdef"];
    await client.chat.send("sess-1", "look", keys);
    expect(t.chat.Send).toHaveBeenCalledWith("sess-1", "look", keys);
  });

  it("routes chat.regenerate and returns the new turn id", async () => {
    const turnID = await client.chat.regenerate("sess-1", "turn-1");
    expect(turnID).toBe("turn-2");
    expect(t.chat.Regenerate).toHaveBeenCalledWith("sess-1", "turn-1");
  });

  it("routes chat.transcriptPage with the paging window", async () => {
    await client.chat.transcriptPage("sess-1", 42, 60);
    expect(t.chat.TranscriptPage).toHaveBeenCalledWith("sess-1", 42, 60);
  });

  it("routes chat.deleteSession verbatim", async () => {
    await client.chat.deleteSession("sess-1");
    expect(t.chat.DeleteSession).toHaveBeenCalledWith("sess-1");
  });

  it("routes chat.saveAttachment and chat.attachmentContent", async () => {
    const meta = await client.chat.saveAttachment(
      "shot.png",
      "image/png",
      "cG5nLWJ5dGVz"
    );

    expect(meta.key).toBe("0123456789abcdef0123456789abcdef");
    expect(t.chat.SaveAttachment).toHaveBeenCalledWith(
      "shot.png",
      "image/png",
      "cG5nLWJ5dGVz"
    );

    const content = await client.chat.attachmentContent(meta.key);
    expect(content.name).toBe("shot.png");
    expect(t.chat.AttachmentContent).toHaveBeenCalledWith(meta.key);
  });

  it("routes chat.cancel verbatim", async () => {
    await client.chat.cancel("sess-1");
    expect(t.chat.Cancel).toHaveBeenCalledWith("sess-1");
  });

  it("routes chat.resolveApproval with session, interrupt id, and decision", async () => {
    await client.chat.resolveApproval("sess-1", "int-1", true);
    expect(t.chat.ResolveApproval).toHaveBeenCalledWith(
      "sess-1",
      "int-1",
      true
    );

    await client.chat.resolveApproval("sess-1", "int-2", false);
    expect(t.chat.ResolveApproval).toHaveBeenCalledWith(
      "sess-1",
      "int-2",
      false
    );
  });

  it("routes chat.createSession", async () => {
    const session = await client.chat.createSession("agent-1");
    expect(session.ID).toBe("s1");
    expect(t.chat.CreateSession).toHaveBeenCalledWith("agent-1");
  });

  it("routes chat.setSessionOverrides verbatim (empty clears to inherit)", async () => {
    await client.chat.setSessionOverrides("sess-1", "gpt-5", "high");
    expect(t.chat.SetSessionOverrides).toHaveBeenCalledWith(
      "sess-1",
      "gpt-5",
      "high"
    );

    await client.chat.setSessionOverrides("sess-1", "", "");
    expect(t.chat.SetSessionOverrides).toHaveBeenCalledWith("sess-1", "", "");
  });

  it("routes chat.turnState and returns in-flight turn status", async () => {
    t.chat.TurnState.mockResolvedValueOnce({
      busy: true,
      turn_id: "turn-abc",
    });

    const status = await client.chat.turnState("sess-1");
    expect(status).toEqual({ busy: true, turn_id: "turn-abc" });
    expect(t.chat.TurnState).toHaveBeenCalledWith("sess-1");
  });

  it("routes chat.unsubscribe verbatim", async () => {
    await client.chat.unsubscribe("sess-1");
    expect(t.chat.Unsubscribe).toHaveBeenCalledWith("sess-1");
  });

  it("routes chat.generatedDocument and returns the served content", async () => {
    const content = makeGeneratedDocument();

    const read = vi
      .spyOn(t.transport.chat, "GeneratedDocument")
      .mockResolvedValue(content);

    await expect(
      client.chat.generatedDocument("agent-1", "report.pdf")
    ).resolves.toBe(content);
    expect(read).toHaveBeenCalledWith("agent-1", "report.pdf");
  });

  it("routes gateway state calls", async () => {
    await expect(client.gateway.state()).resolves.toBe("running");
    await client.gateway.pause();
    await client.gateway.resume();
    expect(t.gateway.Pause).toHaveBeenCalledOnce();
    expect(t.gateway.Resume).toHaveBeenCalledOnce();
  });

  describe("session event subscription", () => {
    it("subscribes on the backend, then listens on the session channel", async () => {
      const events: SessionEvent[] = [];

      const off = await client.chat.subscribe("sess-9", (e: SessionEvent) =>
        events.push(e)
      );

      expect(t.chat.Subscribe).toHaveBeenCalledWith("sess-9");
      expect(t.calls).toContain(`onEvent:${sessionEventName("sess-9")}`);

      t.emit(
        sessionEventName("sess-9"),
        makeSessionEvent(Kind.KindTextDelta, "he")
      );
      t.emit(
        sessionEventName("sess-9"),
        makeSessionEvent(Kind.KindTextDelta, "llo")
      );
      expect(events.map((e: SessionEvent) => e.text)).toEqual(["he", "llo"]);

      off();
      expect(t.calls).not.toContain("chat.Unsubscribe:sess-9");
      expect(t.active(sessionEventName("sess-9"))).toBe(0);
    });

    it("subscribe cleanup detaches locally without calling chat.Unsubscribe", async () => {
      const off = await client.chat.subscribe("sess-cleanup", () => undefined);
      expect(t.chat.Subscribe).toHaveBeenCalledWith("sess-cleanup");
      expect(t.active(sessionEventName("sess-cleanup"))).toBe(1);

      off();
      expect(t.active(sessionEventName("sess-cleanup"))).toBe(0);
      expect(t.chat.Unsubscribe).not.toHaveBeenCalled();
    });

    it("scopes subscriptions per session", async () => {
      const got: string[] = [];
      await client.chat.subscribe("a", (e: SessionEvent) => got.push(e.kind));
      await client.chat.subscribe("b", () => undefined);

      t.emit(sessionEventName("a"), makeSessionEvent(Kind.KindTurnCompleted));
      expect(got).toEqual(["turn_completed"]);
      expect(t.active(sessionEventName("b"))).toBe(1);
    });

    it("unsubscribing twice removes the handler only once", async () => {
      const off = await client.chat.subscribe("x", () => undefined);
      off();
      off();
      expect(t.active(sessionEventName("x"))).toBe(0);
    });
  });

  describe("gateway state subscription", () => {
    it("listens on the gateway:state channel and passes the typed payload", () => {
      const got: string[] = [];

      const off = client.gateway.subscribe((e: GatewayStateEvent) =>
        got.push(e.state)
      );

      expect(t.calls).toContain("onEvent:gateway:state");

      t.emit("gateway:state", { state: "paused" });
      expect(got).toEqual(["paused"]);

      off();
      expect(t.active("gateway:state")).toBe(0);
    });
  });

  describe("schedules surface", () => {
    it("routes schedules.list and returns the views", async () => {
      const views = [makeScheduleView()];

      const list = vi
        .spyOn(t.transport.schedules, "List")
        .mockResolvedValue(views);

      await expect(client.schedules.list()).resolves.toEqual(views);
      expect(list).toHaveBeenCalledOnce();
    });

    it("routes schedules.get verbatim", async () => {
      const view = makeScheduleView();

      const get = vi
        .spyOn(t.transport.schedules, "Get")
        .mockResolvedValue(view);

      await expect(client.schedules.get("sch-1")).resolves.toBe(view);
      expect(get).toHaveBeenCalledWith("sch-1");
    });

    it("routes schedules.create and schedules.update with the input shape", async () => {
      const input = makeScheduleInput();
      const view = makeScheduleView();

      const create = vi
        .spyOn(t.transport.schedules, "Create")
        .mockResolvedValue(view);

      const update = vi
        .spyOn(t.transport.schedules, "Update")
        .mockResolvedValue(view);

      await expect(client.schedules.create(input)).resolves.toBe(view);
      expect(create).toHaveBeenCalledWith(input);

      await expect(client.schedules.update("sch-1", input)).resolves.toBe(view);
      expect(update).toHaveBeenCalledWith("sch-1", input);
    });

    it("routes schedules.setEnabled with the toggle", async () => {
      const view = { ...makeScheduleView(), enabled: false };

      const setEnabled = vi
        .spyOn(t.transport.schedules, "SetEnabled")
        .mockResolvedValue(view);

      await expect(client.schedules.setEnabled("sch-1", false)).resolves.toBe(
        view
      );
      expect(setEnabled).toHaveBeenCalledWith("sch-1", false);
    });

    it("routes schedules.delete verbatim", async () => {
      const del = vi
        .spyOn(t.transport.schedules, "Delete")
        .mockResolvedValue(undefined);

      await client.schedules.delete("sch-1");
      expect(del).toHaveBeenCalledWith("sch-1");
    });

    it("routes schedules.runNow and returns the fired view", async () => {
      const view = {
        ...makeScheduleView(),
        lastRunAt: "2026-10-21T12:00:00+07:00",
        lastRunStatus: "ran",
        lastRunSessionId: "s9",
      };

      const runNow = vi
        .spyOn(t.transport.schedules, "RunNow")
        .mockResolvedValue(view);

      await expect(client.schedules.runNow("sch-1")).resolves.toBe(view);
      expect(runNow).toHaveBeenCalledWith("sch-1");
    });

    it("routes schedules.preview and returns the recurrence preview", async () => {
      const preview: Preview = {
        ok: true,
        description: "Every day at 9:00",
        next: ["2026-10-22T09:00:00+07:00"],
      };

      const previewFn = vi
        .spyOn(t.transport.schedules, "Preview")
        .mockResolvedValue(preview);

      await expect(client.schedules.preview("0 9 * * *")).resolves.toBe(
        preview
      );
      expect(previewFn).toHaveBeenCalledWith("0 9 * * *");
    });
  });

  describe("settings provider surface", () => {
    it("routes provider reads and returns the secret-free view", async () => {
      const view = makeProviderView();

      const providers = vi
        .spyOn(t.transport.settings, "Providers")
        .mockResolvedValue([view]);

      expect(await client.settings.providers()).toEqual([view]);
      expect(providers).toHaveBeenCalledOnce();
    });

    it("normalizes a null model catalog (nil Go slice) to an empty list", async () => {
      const wireNull: null = null;

      // SAFETY: the bridge maps a nil Go slice to null on the wire; this
      // cast recovers the transport's declared payload for the regression.
      const nilCatalog = wireNull as never;

      const models = vi
        .spyOn(t.transport.settings, "ProviderModels")
        .mockResolvedValue(nilCatalog);

      await expect(client.settings.providerModels("p1")).resolves.toEqual([]);
      expect(models).toHaveBeenCalledWith("p1");
    });

    it("routes verifyProviderDraft with the draft and provider id", async () => {
      const draft = makeProviderInput();
      const result: VerifyResult = { OK: true, Detail: "" };

      const verify = vi
        .spyOn(t.transport.settings, "VerifyProviderDraft")
        .mockResolvedValue(result);

      await expect(
        client.settings.verifyProviderDraft(draft, "p1")
      ).resolves.toBe(result);
      expect(verify).toHaveBeenCalledWith(draft, "p1");
    });

    it("routes verifyProvider with the saved provider id", async () => {
      const result: VerifyResult = { OK: false, Detail: "401: invalid key" };

      const verify = vi
        .spyOn(t.transport.settings, "VerifyProvider")
        .mockResolvedValue(result);

      await expect(client.settings.verifyProvider("p1")).resolves.toBe(result);
      expect(verify).toHaveBeenCalledWith("p1");
    });
  });

  describe("settings hook and mcp writes", () => {
    it("routes settings.createHook without the store-assigned timestamps", async () => {
      const hook = makeHook();

      const create = vi
        .spyOn(t.transport.settings, "CreateHook")
        .mockResolvedValue(hook);

      await client.settings.createHook(hook);

      const sent = create.mock.calls[0][0];
      expect(sent.Name).toBe("notify");
      expect(sent).not.toHaveProperty("CreatedAt");
      expect(sent).not.toHaveProperty("UpdatedAt");
    });

    it("routes settings.createMCPServer without the store-assigned timestamps", async () => {
      const server = makeMCPServer();

      const create = vi
        .spyOn(t.transport.settings, "CreateMCPServer")
        .mockResolvedValue(server);

      await client.settings.createMCPServer(server);

      const sent = create.mock.calls[0][0];
      expect(sent.Name).toBe("fetch");
      expect(sent).not.toHaveProperty("CreatedAt");
      expect(sent).not.toHaveProperty("UpdatedAt");
    });

    it("routes the integration connect/disconnect calls verbatim (add-integrations)", async () => {
      const row = makeMCPServer();

      const input: IntegrationConnectInput = {
        Service: "gitlab",
        Endpoint: "https://gitlab.com",
        AuthMethod: "token",
        Credential: "tok",
        ClientID: "",
        ClientSecret: "",
        ExtraAuthParams: null,
        CredentialHeaderStyle: "bearer",
        Products: ["issues"],
        ProductEndpoints: null,
      };

      const connect = vi
        .spyOn(t.transport.settings, "ConnectIntegration")
        .mockResolvedValue(row);

      const disconnect = vi
        .spyOn(t.transport.settings, "DisconnectIntegration")
        .mockResolvedValue(undefined);

      await expect(client.settings.connectIntegration(input)).resolves.toBe(
        row
      );
      await client.settings.disconnectIntegration("m1");

      expect(connect).toHaveBeenCalledWith(input);
      expect(disconnect).toHaveBeenCalledWith("m1");
    });

    it("routes the mcp oauth actions verbatim (design D2/D9)", async () => {
      const row = makeMCPServer();

      const authorize = vi
        .spyOn(t.transport.settings, "AuthorizeMCPServer")
        .mockResolvedValue(undefined);

      const disconnect = vi
        .spyOn(t.transport.settings, "DisconnectMCPServer")
        .mockResolvedValue(row);

      await client.settings.authorizeMCPServer("m1");
      await expect(client.settings.disconnectMCPServer("m1")).resolves.toBe(
        row
      );
      expect(authorize).toHaveBeenCalledWith("m1");
      expect(disconnect).toHaveBeenCalledWith("m1");
    });
  });

  describe("document preset surface", () => {
    it("routes settings.documentPresets and normalizes a null list", async () => {
      const presets = [makeDocumentPreset()];

      const list = vi
        .spyOn(t.transport.settings, "ListDocumentPresets")
        .mockResolvedValue(presets);

      await expect(client.settings.documentPresets("pdf")).resolves.toEqual(
        presets
      );
      expect(list).toHaveBeenCalledWith("pdf");

      // SAFETY: the bridge maps a nil Go slice to null on the wire; this
      // cast recovers the transport's declared payload for the regression.
      const nilList = null as never;
      vi.spyOn(t.transport.settings, "ListDocumentPresets").mockResolvedValue(
        nilList
      );
      await expect(client.settings.documentPresets("pdf")).resolves.toEqual([]);
    });

    it("routes the preset CRUD calls verbatim", async () => {
      const preset = makeDocumentPreset();

      const create = vi
        .spyOn(t.transport.settings, "CreateDocumentPreset")
        .mockResolvedValue(preset);

      const update = vi
        .spyOn(t.transport.settings, "UpdateDocumentPreset")
        .mockResolvedValue(preset);

      const duplicate = vi
        .spyOn(t.transport.settings, "DuplicateDocumentPreset")
        .mockResolvedValue(preset);

      const del = vi
        .spyOn(t.transport.settings, "DeleteDocumentPreset")
        .mockResolvedValue(undefined);

      await expect(
        client.settings.createDocumentPreset(
          "pdf",
          "Client Report",
          preset.config
        )
      ).resolves.toBe(preset);
      expect(create).toHaveBeenCalledWith(
        "pdf",
        "Client Report",
        preset.config
      );

      await expect(
        client.settings.updateDocumentPreset(
          "preset-1",
          "Client Report v2",
          "{}"
        )
      ).resolves.toBe(preset);
      expect(update).toHaveBeenCalledWith("preset-1", "Client Report v2", "{}");

      await expect(
        client.settings.duplicateDocumentPreset("preset-1", "Copy")
      ).resolves.toBe(preset);
      expect(duplicate).toHaveBeenCalledWith("preset-1", "Copy");

      await client.settings.deleteDocumentPreset("preset-1");
      expect(del).toHaveBeenCalledWith("preset-1");
    });

    it("routes the default preset selector (empty clears)", async () => {
      const get = vi
        .spyOn(t.transport.settings, "DefaultDocumentPreset")
        .mockResolvedValue("Client Report");

      const set = vi
        .spyOn(t.transport.settings, "SetDefaultDocumentPreset")
        .mockResolvedValue(undefined);

      await expect(client.settings.defaultDocumentPreset("pdf")).resolves.toBe(
        "Client Report"
      );
      expect(get).toHaveBeenCalledWith("pdf");

      await client.settings.setDefaultDocumentPreset("pdf", "Client Report");
      expect(set).toHaveBeenCalledWith("pdf", "Client Report");

      await client.settings.setDefaultDocumentPreset("pdf", "");
      expect(set).toHaveBeenLastCalledWith("pdf", "");
    });
  });

  describe("mcp:oauth:completed subscription", () => {
    it("listens on the mcp:oauth:completed channel and passes the typed payload", () => {
      const got: MCPOAuthCompletedEvent[] = [];

      const off = client.events.onMcpOAuthCompleted(
        (ev: MCPOAuthCompletedEvent): void => {
          got.push(ev);
        }
      );

      expect(t.calls).toContain(`onEvent:${MCP_OAUTH_COMPLETED_EVENT}`);

      t.emit(MCP_OAUTH_COMPLETED_EVENT, { serverId: "m1", ok: true });
      t.emit(MCP_OAUTH_COMPLETED_EVENT, {
        serverId: "m1",
        ok: false,
        error: "denied",
      });
      expect(got).toEqual([
        { serverId: "m1", ok: true },
        { serverId: "m1", ok: false, error: "denied" },
      ]);

      off();
      expect(t.active(MCP_OAUTH_COMPLETED_EVENT)).toBe(0);
    });
  });

  describe("schedules:changed subscription", () => {
    it("listens on the schedules:changed channel and passes the typed payload", () => {
      const got: SchedulesChangedEvent[] = [];

      const off = client.events.onSchedulesChanged(
        (ev: SchedulesChangedEvent): void => {
          got.push(ev);
        }
      );

      expect(t.calls).toContain(`onEvent:${SCHEDULES_CHANGED_EVENT}`);

      t.emit(SCHEDULES_CHANGED_EVENT, {
        scheduleId: "sch-1",
        sessionId: "",
        agentId: "",
      });
      t.emit(SCHEDULES_CHANGED_EVENT, {
        scheduleId: "sch-1",
        sessionId: "s9",
        agentId: "a1",
      });
      expect(got).toEqual([
        { scheduleId: "sch-1", sessionId: "", agentId: "" },
        { scheduleId: "sch-1", sessionId: "s9", agentId: "a1" },
      ]);

      off();
      expect(t.active(SCHEDULES_CHANGED_EVENT)).toBe(0);
    });
  });

  describe("settings:open subscription", () => {
    it("listens on the settings:open channel and ignores its empty payload", () => {
      let opened = 0;

      const off = client.events.onSettingsOpen((): void => {
        opened += 1;
      });

      expect(t.calls).toContain(`onEvent:${SETTINGS_OPEN_EVENT}`);

      t.emit(SETTINGS_OPEN_EVENT);
      expect(opened).toBe(1);

      off();
      expect(t.active(SETTINGS_OPEN_EVENT)).toBe(0);
    });
  });
});

describe("sessionEventName", () => {
  it("matches the backend format session:<id>:event", () => {
    expect(sessionEventName("abc")).toBe("session:abc:event");
  });
});
