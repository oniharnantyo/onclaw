/**
 * The chat view's runtime provider (task 2.3, design D1): mounts the
 * assistant-ui runtime context built from the chat store adapter. Wave 1
 * mounts it inert around the existing view — nothing consumes the context
 * until the transcript/composer waves swap the surfaces over it.
 */
import type { ReactElement, ReactNode } from "react";

import { AssistantRuntimeProvider } from "@assistant-ui/react";

import { useChatRuntime } from "./useChatRuntime";

export function ChatRuntimeProvider({
  children,
}: {
  children: ReactNode;
}): ReactElement {
  const runtime = useChatRuntime();

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      {children}
    </AssistantRuntimeProvider>
  );
}
