/**
 * ArtifactsView (add-turn-artifacts task 6.3, design D7): the chips row a
 * turn's produced files render as — the outbound counterpart of the
 * AttachmentView chips, sharing their grammar and tokens (icon by extension,
 * the file name, the formatBytes size) so both themes work with no new
 * colours. The change kind rides the title tooltip ("Created · 1.2 KB");
 * up to six chips render inline with a "+N more" disclosure expanding the
 * rest, and a backend-truncated list (the 50 cap) says so. Activation calls
 * `onOpen` with the artifact's full workspace-relative path — the caller
 * wires openPanelTab(workspaceFileTab(activeAgentID, path)); without an
 * onOpen the chips render non-interactive. Zero artifacts render nothing.
 */
import { useState } from "react";
import type { ReactElement } from "react";
import {
  FileCode,
  FileSpreadsheet,
  FileText,
  Image as ImageIcon,
} from "lucide-react";

import type { Artifact } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import { ArtifactChange } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import { cn } from "../../../lib/cn";
import { formatBytes } from "./AttachmentView";

/** How many chips render inline before the "+N more" disclosure (spec
 * "Artifacts chips row": six). */
const INLINE_COUNT = 6;

/** The workspace prefix the display name strips; the open call and the
 * tooltip keep the full path. */
const ARTIFACTS_PREFIX = "artifacts/";

const CODE_EXTS: ReadonlySet<string> = new Set([
  "ts",
  "tsx",
  "js",
  "jsx",
  "mjs",
  "cjs",
  "py",
  "go",
  "rs",
  "java",
  "rb",
  "c",
  "h",
  "cpp",
  "cs",
  "sh",
  "json",
  "yaml",
  "yml",
  "toml",
  "html",
  "css",
  "sql",
]);

const SHEET_EXTS: ReadonlySet<string> = new Set(["csv", "tsv", "xls", "xlsx"]);

const IMAGE_EXTS: ReadonlySet<string> = new Set([
  "png",
  "jpg",
  "jpeg",
  "gif",
  "webp",
  "svg",
]);

/** The chip icon element by file extension: code, sheet, and image
 * extensions pick their glyph; everything else falls back to the document
 * icon the attachment chips use. */
function extensionIcon(path: string): ReactElement {
  const className = "size-3.5 shrink-0 text-muted";
  const ext = path.slice(path.lastIndexOf(".") + 1).toLowerCase();

  if (CODE_EXTS.has(ext)) {
    return <FileCode aria-hidden="true" className={className} />;
  }

  if (SHEET_EXTS.has(ext)) {
    return <FileSpreadsheet aria-hidden="true" className={className} />;
  }

  if (IMAGE_EXTS.has(ext)) {
    return <ImageIcon aria-hidden="true" className={className} />;
  }

  return <FileText aria-hidden="true" className={className} />;
}

/** The chip's display name: the path under artifacts/, so a nested file
 * stays identifiable; the full path rides the open call and the tooltip. */
function displayName(path: string): string {
  return path.startsWith(ARTIFACTS_PREFIX)
    ? path.slice(ARTIFACTS_PREFIX.length)
    : path;
}

/** The chip's tooltip: the full path, the change kind when known
 * ("Created" / "Modified"), and the human-readable size. */
function chipTitle(artifact: Artifact): string {
  const change =
    artifact.change === ArtifactChange.ArtifactCreated
      ? "Created"
      : artifact.change === ArtifactChange.ArtifactModified
        ? "Modified"
        : null;

  return [artifact.path, change, formatBytes(artifact.size)]
    .filter((part: string | null): boolean => part !== null)
    .join(" · ");
}

/** The shared chip chrome — the attachment chip's surface, tightened for an
 * inline row. */
const CHIP_CLASS =
  "flex max-w-56 items-center gap-1.5 rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] px-2 py-1 text-[12px] text-fg2";

function ChipBody({ artifact }: { artifact: Artifact }): ReactElement {
  return (
    <>
      {extensionIcon(artifact.path)}
      <span className="min-w-0 truncate font-medium">
        {displayName(artifact.path)}
      </span>
      <span className="shrink-0 font-mono text-[10px] text-muted">
        {formatBytes(artifact.size)}
      </span>
    </>
  );
}

/** One artifact chip: a button when it can open the preview, a plain chip
 * otherwise (no active agent — nothing to open it with). */
function ArtifactChip({
  artifact,
  onOpen,
}: {
  artifact: Artifact;
  onOpen?: (path: string) => void;
}): ReactElement {
  const title = chipTitle(artifact);

  if (onOpen === undefined) {
    return (
      <span className={CHIP_CLASS} data-testid="artifact-chip" title={title}>
        <ChipBody artifact={artifact} />
      </span>
    );
  }

  return (
    <button
      type="button"
      className={cn(
        CHIP_CLASS,
        "cursor-pointer text-left hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)]"
      )}
      data-testid="artifact-chip"
      title={title}
      aria-label={displayName(artifact.path)}
      onClick={() => onOpen(artifact.path)}
    >
      <ChipBody artifact={artifact} />
    </button>
  );
}

/** The turn's artifacts chips row (add-turn-artifacts design D7): zero
 * artifacts render nothing; the rest collapse behind the "+N more"
 * disclosure with the truncation note beside them when the backend hit its
 * cap. */
export function ArtifactsView({
  artifacts,
  truncated,
  onOpen,
}: {
  artifacts: Artifact[];
  truncated?: boolean;
  onOpen?: (path: string) => void;
}): ReactElement | null {
  const [expanded, setExpanded] = useState(false);

  if (artifacts.length === 0) return null;

  const visible = expanded ? artifacts : artifacts.slice(0, INLINE_COUNT);
  const hidden = artifacts.length - visible.length;

  return (
    <div
      className="mt-2 flex flex-wrap items-center gap-1.5"
      data-testid="entry-artifacts"
    >
      {visible.map((artifact: Artifact): ReactElement => (
        <ArtifactChip key={artifact.path} artifact={artifact} onOpen={onOpen} />
      ))}
      {hidden > 0 && (
        <button
          type="button"
          className="cursor-pointer rounded-md border border-dashed border-line px-2 py-1 text-[11px] text-muted hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)]"
          data-testid="artifacts-more"
          onClick={() => setExpanded(true)}
        >
          +{hidden} more
        </button>
      )}
      {truncated && (
        <span
          className="text-[11px] text-muted"
          data-testid="artifacts-truncated"
        >
          Only the first 50 artifacts are shown.
        </span>
      )}
    </div>
  );
}
