/**
 * Attachment store tests (task 5.4, design D5): the lane caps reject with
 * the reason before any round trip, uploads land as capability keys, and a
 * failed upload retries.
 *
 * The composer-side scenarios (the send gate, the capability warning) and
 * the transcript-side ones (the image thumbnail, the document chip, the
 * unavailable state) moved to the new layer:
 * `features/chat/composer/__test__/attachments.test.tsx` and
 * `features/chat/thread/__test__/attachments.test.tsx`.
 */
import { mockedSurface } from "./mockedSurface";
import { waitFor } from "@testing-library/dom";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type {
  AttachmentContent,
  ModelCapability,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type {
  Agent,
  Session,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { Client } from "../../../lib/client";
import {
  laneRejection,
  MAX_MESSAGE_FILES,
  MAX_VIDEO_BYTES,
  useAttachmentStore,
  type AttachmentChip,
} from "../attachments";
import { useChatStore } from "../store";

const agent: Agent = {
  ID: "a1",
  Name: "Scout",
  ProviderID: "prov",
  Model: "vision-model",
  Effort: "",
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

const session: Session = {
  ID: "s1",
  AgentID: "a1",
  Title: "",
  Model: "",
  Effort: "",
  CreatedAt: "2026-01-01T00:00:00Z",
  LastActivityAt: "2026-01-01T00:00:00Z",
};

const capabilities: ModelCapability[] = [
  {
    ID: "vision-model",
    ContextWindow: 200000,
    Efforts: [],
    ImageInput: true,
    PDFInput: true,
    VideoInput: true,
  },
];

type MockFn = ReturnType<typeof vi.fn>;

/** The SaveAttachment payload shape the deferred-upload mocks resolve. */
type Stored = {
  key: string;
  name: string;
  mime: string;
  size: number;
};

/** The mocked client shape: the members this suite drives. */
type MockedClient = Client & {
  chat: {
    saveAttachment: MockFn;
    send: MockFn;
    attachmentContent: MockFn;
  };
};

function makeClient(): MockedClient {
  // SAFETY: the literal covers the surfaces this suite drives; the
  // Client-typed intersection defeats the structural check, so the
  // conversion funnels through the shared mockedSurface bridge.
  return mockedSurface<MockedClient, object>({
    chat: {
      saveAttachment: vi.fn(
        async (): Promise<{
          key: string;
          name: string;
          mime: string;
          size: number;
        }> => ({
          key: "cap-key-1",
          name: "shot",
          mime: "image/png",
          size: 4,
        })
      ),
      send: vi.fn(async (): Promise<string> => "turn-1"),
      attachmentContent: vi.fn(async (): Promise<AttachmentContent> => ({
        key: "cap-key-1",
        name: "shot.png",
        mime: "image/png",
        size: 4,
        // base64 of "data"
        data: "ZGF0YQ==",
      })),
    },
  });
}

vi.mock("../../../lib/client", () => ({ client: makeClient() }));

import { client } from "../../../lib/client";

// SAFETY: the client module is mocked with makeClient(), so its runtime
// shape is exactly this mock object.
const mockedClient = client as MockedClient;

/** jsdom lacks the blob URL factory; the suites stub it. */
const createObjectURL = vi.fn<(obj: Blob | MediaSource) => string>(
  (): string => "blob:mock-url"
);

const revokeObjectURL = vi.fn<(fileURL: string) => void>((): void => undefined);

beforeEach(() => {
  vi.clearAllMocks();
  window.URL.createObjectURL = createObjectURL;
  window.URL.revokeObjectURL = revokeObjectURL;
  useAttachmentStore.setState({ chips: [] });
  useChatStore.setState({
    agents: [agent],
    activeAgentID: "a1",
    activeSessionID: "s1",
    sessions: [session],
    models: capabilities,
    modelOverride: "",
    entries: [],
    loadingSession: false,
    busy: false,
    error: null,
  });
});

describe("lane caps (task 5.4)", () => {
  it("accepts each lane within its cap", () => {
    expect(laneRejection("a.png", "image/png", 5 << 20)).toBeNull();
    expect(laneRejection("a.pdf", "application/pdf", 20 << 20)).toBeNull();
    expect(laneRejection("clip.mp4", "video/mp4", 1 << 20)).toBeNull();
    expect(laneRejection("clip.mp4", "video/mp4", MAX_VIDEO_BYTES)).toBeNull();
    expect(laneRejection("a.txt", "text/plain", 1 << 20)).toBeNull();
  });

  it("rejects over-cap files with the lane's limit in the reason", () => {
    expect(laneRejection("a.png", "image/png", (5 << 20) + 1)).toContain(
      "5 MB"
    );
    expect(laneRejection("a.pdf", "application/pdf", (20 << 20) + 1)).toContain(
      "20 MB"
    );
    const video = laneRejection("clip.mp4", "video/mp4", MAX_VIDEO_BYTES + 1);

    expect(video).toContain("20 MB");
    expect(video).toContain("video");
    expect(laneRejection("a.txt", "text/plain", (1 << 20) + 1)).toContain(
      "1 MB"
    );
  });

  it("rejects a mime in no lane as unsupported", () => {
    const reason = laneRejection("a.bin", "application/octet-stream", 10);

    expect(reason).toContain("Unsupported");
    expect(reason).toContain("videos");
  });
});

describe("the chip store (task 5.4)", () => {
  it("uploads an accepted file to ready with its capability key", async () => {
    const file = new File(["data"], "shot.png", { type: "image/png" });

    await useAttachmentStore.getState().addFiles([file]);

    await waitFor(() =>
      expect(mockedClient.chat.saveAttachment).toHaveBeenCalledWith(
        "shot.png",
        "image/png",
        "ZGF0YQ=="
      )
    );
    await waitFor(() =>
      expect(
        useAttachmentStore.getState().chips.map((c: AttachmentChip) => c.status)
      ).toEqual(["ready"])
    );
    expect(useAttachmentStore.getState().chips[0]?.key).toBe("cap-key-1");
  });

  it("a video file uploads to ready like any other lane (design D6)", async () => {
    await useAttachmentStore
      .getState()
      .addFiles([new File(["data"], "clip.mp4", { type: "video/mp4" })]);

    await waitFor(() =>
      expect(mockedClient.chat.saveAttachment).toHaveBeenCalledWith(
        "clip.mp4",
        "video/mp4",
        "ZGF0YQ=="
      )
    );
    await waitFor(() =>
      expect(
        useAttachmentStore.getState().chips.map((c: AttachmentChip) => c.status)
      ).toEqual(["ready"])
    );
    expect(useAttachmentStore.getState().chips[0]?.key).toBe("cap-key-1");
  });

  it("rejects an over-cap file with the reason and never uploads", async () => {
    const big = new File([new Uint8Array(10).fill(0)], "big.png", {
      type: "image/png",
    });

    // SAFETY: jsdom derives size from content; pin the size the lane check
    // sees to the over-cap case directly.
    Object.defineProperty(big, "size", { value: (5 << 20) + 1 });

    await useAttachmentStore.getState().addFiles([big]);

    const chips = useAttachmentStore.getState().chips;

    expect(chips).toHaveLength(1);
    expect(chips[0]).toMatchObject({ status: "rejected" });
    expect(chips[0]?.reason).toContain("5 MB");
    expect(mockedClient.chat.saveAttachment).not.toHaveBeenCalled();
  });

  it("caps the tray at four live files per message", async () => {
    for (let i = 0; i < MAX_MESSAGE_FILES + 1; i += 1) {
      await useAttachmentStore
        .getState()
        .addFiles([new File(["x"], `f${i}.txt`, { type: "text/plain" })]);
    }

    await waitFor(() =>
      expect(mockedClient.chat.saveAttachment).toHaveBeenCalledTimes(
        MAX_MESSAGE_FILES
      )
    );
    const chips = useAttachmentStore.getState().chips;

    expect(chips).toHaveLength(MAX_MESSAGE_FILES + 1);
    expect(
      chips
        .filter((c: AttachmentChip) => c.status === "rejected")
        .map((c: AttachmentChip) => c.reason)
    ).toEqual(["At most 4 files per message"]);
  });

  it("a failed upload is retryable and retry succeeds", async () => {
    mockedClient.chat.saveAttachment.mockRejectedValueOnce(
      new Error("disk full")
    );
    await useAttachmentStore
      .getState()
      .addFiles([new File(["data"], "shot.png", { type: "image/png" })]);

    await waitFor(() =>
      expect(useAttachmentStore.getState().chips[0]?.status).toBe("rejected")
    );
    expect(useAttachmentStore.getState().chips[0]?.retryable).toBe(true);
    expect(useAttachmentStore.getState().chips[0]?.reason).toBe("disk full");

    await useAttachmentStore
      .getState()
      .retry(useAttachmentStore.getState().chips[0]?.id ?? "");

    await waitFor(() =>
      expect(useAttachmentStore.getState().chips[0]?.status).toBe("ready")
    );
    expect(mockedClient.chat.saveAttachment).toHaveBeenCalledTimes(2);
  });

  it("removing a chip mid-upload drops it without writing the result back", async () => {
    let release:
      | ((value: {
          key: string;
          name: string;
          mime: string;
          size: number;
        }) => void)
      | null = null;

    // The resolve handle is assigned inside the mock's executor, so the
    // invocation goes through a closure — there TS reads `release`'s
    // declared type instead of narrowing it to the `null` initializer.
    const unblock = (value: {
      key: string;
      name: string;
      mime: string;
      size: number;
    }): void => {
      release?.(value);
    };

    mockedClient.chat.saveAttachment.mockImplementationOnce(
      () =>
        new Promise((resolve: (value: Stored) => void) => {
          release = resolve;
        })
    );
    await useAttachmentStore
      .getState()
      .addFiles([new File(["data"], "shot.png", { type: "image/png" })]);
    // The upload must have reached the binding before the cancel lands —
    // and the pending once-mock must be consumed, or it leaks into the
    // next test (clearAllMocks does not drop queued once-implementations).
    await waitFor(() =>
      expect(mockedClient.chat.saveAttachment).toHaveBeenCalled()
    );

    const chipID = useAttachmentStore.getState().chips[0]?.id ?? "";
    useAttachmentStore.getState().remove(chipID);
    unblock({ key: "late", name: "shot", mime: "image/png", size: 4 });
    await Promise.resolve();

    expect(useAttachmentStore.getState().chips).toHaveLength(0);
  });
});
