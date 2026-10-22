/**
 * Shared harness for the wave-2B tool-layer suites: hand-built part props
 * for standalone renderer mounts, and a runtime-mounted message surface
 * (ChatRuntimeProvider + GroupedParts wired to the wave-2B registry) for the
 * scenarios that need the real part enrichment — status, respondToApproval,
 * and the fold's PartByIndex cards.
 */
import type { ReactElement } from "react";
import {
  MessagePrimitive,
  ThreadPrimitive,
  groupPartByType,
} from "@assistant-ui/react";
import type { ToolCallMessagePartProps } from "@assistant-ui/react";

import { renderUI } from "../../../__test__/test-utils";
import { ChatRuntimeProvider } from "../../../runtime/ChatRuntimeProvider";
import type { Entry } from "../../../types";
import { useChatStore } from "../../../store";
import { ChatToolGroup } from "../ChatToolGroup";
import { toolRenderers } from "../renderers";

/** The standalone part props one renderer mount receives: a completed call
 * with the given fields and inert runtime callbacks. */
export function partProps(
  overrides: Partial<ToolCallMessagePartProps> & {
    toolCallId: string;
    toolName: string;
  }
): ToolCallMessagePartProps {
  return {
    type: "tool-call",
    args: {},
    argsText: "",
    status: { type: "complete" },
    addResult: (): void => undefined,
    resume: (): void => undefined,
    respondToApproval: async (): Promise<void> => undefined,
    ...overrides,
  };
}

/** The message surface the fold and approval suites mount: the same wiring
 * the transcript uses (GroupedParts → ChatToolGroup, tool-call leaves
 * through the registry). */
function ToolMessageSurface(): ReactElement {
  return (
    <MessagePrimitive.Root>
      <MessagePrimitive.GroupedParts
        groupBy={groupPartByType({
          "tool-call": ["group-tool"],
        })}
      >
        {({
          part,
          children,
        }: MessagePrimitive.GroupedParts.RenderInfo): ReactElement | null => {
          if (part.type === "group-tool") {
            return <ChatToolGroup group={part}>{children}</ChatToolGroup>;
          }

          if (part.type === "tool-call") {
            const Renderer =
              toolRenderers[part.toolName] ?? toolRenderers.fallback;

            return <Renderer {...part} />;
          }

          if (part.type === "text") return <p>{part.text}</p>;

          return null;
        }}
      </MessagePrimitive.GroupedParts>
    </MessagePrimitive.Root>
  );
}

/** Mounts the runtime over the given entries and renders the tool-message
 * surface — the store is the adapter's single source of truth, so the
 * entries arrive as mapped parts with the runtime's enrichment. */
/** The thread scope the message primitives read their state through. */
function ThreadSurface(): ReactElement {
  return (
    <ThreadPrimitive.Root>
      <ThreadPrimitive.Messages>
        {(): ReactElement => <ToolMessageSurface />}
      </ThreadPrimitive.Messages>
    </ThreadPrimitive.Root>
  );
}

export async function renderWithEntries(
  entries: Entry[],
  busy: boolean = false
): Promise<void> {
  useChatStore.setState({
    entries,
    busy,
    activeSessionID: "s1",
    activeAgentID: "a1",
  });

  await renderUI(
    <ChatRuntimeProvider>
      <ThreadSurface />
    </ChatRuntimeProvider>
  );
}
