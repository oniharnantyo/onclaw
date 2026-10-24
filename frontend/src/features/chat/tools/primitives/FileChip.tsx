/**
 * FileChip (task 4.2): one file path rendered as a chip that opens the
 * right-panel preview — the same jailed path handling as transcript links
 * (PathText): the /workspace mount prefix strips to the workspace-relative
 * reader path, foreign paths pass through and open to the refused outcome.
 * With no active agent there is no workspace to read from, so the chip
 * renders inert.
 */
import type { ReactElement } from "react";
import { File as FileIcon } from "lucide-react";

import { cn } from "../../../../lib/cn";
import { toWorkspaceRel } from "../../toolPaths";
import { useChatStore } from "../../store";
import type { ChatStore } from "../../types";
import { workspaceFileTab } from "../../components/panelRenderers";

export interface FileChipProps {
  path: string;
  className?: string;
  testidPrefix?: string;
}

/**
 * The jailed path-opening callback every body shares (the chip's handler,
 * extractable for whole-row affordances): the /workspace mount prefix strips
 * to the workspace-relative reader path, foreign paths pass through and open
 * to the refused outcome. A no-op with no active agent — there is no
 * workspace to read from.
 */
export function useOpenWorkspacePath(): (path: string) => void {
  const activeAgentID = useChatStore((s: ChatStore) => s.activeAgentID);
  const openPanelTab = useChatStore((s: ChatStore) => s.openPanelTab);

  return (path: string): void => {
    if (activeAgentID === null) return;

    openPanelTab(workspaceFileTab(activeAgentID, toWorkspaceRel(path)));
  };
}

export function FileChip({
  path,
  className,
  testidPrefix = "file-chip",
}: FileChipProps): ReactElement {
  const activeAgentID = useChatStore((s: ChatStore) => s.activeAgentID);
  const openPath = useOpenWorkspacePath();

  const inner = (
    <>
      <FileIcon aria-hidden="true" className="size-3 shrink-0 text-muted" />
      <span className="truncate font-mono text-[11px]">{path}</span>
    </>
  );

  const classes = cn(
    "inline-flex max-w-full items-center gap-1.5 rounded-md border border-line px-1.5 py-0.5 text-fg2",
    activeAgentID !== null &&
      "cursor-pointer hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg",
    className
  );

  if (activeAgentID === null) {
    return (
      <span className={classes} data-testid={`${testidPrefix}-${path}`}>
        {inner}
      </span>
    );
  }

  return (
    <button
      type="button"
      className={classes}
      data-testid={`${testidPrefix}-${path}`}
      title="Preview in the right panel"
      onClick={() => openPath(path)}
    >
      {inner}
    </button>
  );
}
