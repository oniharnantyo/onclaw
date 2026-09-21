// Tool group timeline header (change adopt-assistant-ui-elements, tasks
// 10.1–10.2, design D8): a heavy turn (≥4 tool calls) collapses its card
// stack behind this resting summary — "N steps · M files changed" — and
// expansion reveals the familiar inline cards in stream order. Collapse is
// additive, never muting: light turns keep their inline cards untouched.
// Streaming branch (fix-tool-timeline-fold 3.1): while the turn streams the
// summary span yields to the shared ActivityLabel ("Running Shell · 4 s",
// shimmering, aria-live) and yields back to the untouched resting summary
// the moment the turn ends. Same button, same chevron, same toggle.
import { cx } from "../../lib/helpers";
import { parseArgs } from "../../lib/toolDisplay";
import { Icon } from "../ui/Icon";
import { ActivityLabel } from "./ActivityLabel";

/** File-edit tools whose successful calls count toward the changed-file
 * churn (design D8 — churn derives from the file-edit calls, edit_file's
 * old/new_string diff being the card's own expanded view). */
const FILE_EDIT_TOOLS = new Set(['edit_file', 'write_file']);

/** Changed-file churn for the resting summary: the distinct files named by
 * the turn's successful edit_file / write_file calls — repeated edits to one
 * file count once. Errored calls changed nothing and are skipped; args that
 * fail to parse (mid-stream partial JSON) name no file. Pure over the raw
 * card items, tolerant of absent tools. */
export function changedFileCount(tools: any[] | undefined | null): number {
  if (!Array.isArray(tools)) return 0;
  const files = new Set<string>();
  for (const t of tools) {
    if (!t || !FILE_EDIT_TOOLS.has(t.name) || t.error) continue;
    const args = parseArgs(t.args);
    const path = args && typeof args.file_path === 'string' ? args.file_path.trim() : '';
    if (path) files.add(path);
  }
  return files.size;
}

/** The collapsible timeline header itself: chevron + resting summary, in the
 * same card-row language as ToolCall and TodoUpdatedSummary. The file clause
 * is present-only (a turn that touched no files shows just "N steps").
 * Open state is owned by the turn — user-controlled, never automatic.
 * While `streaming`, the summary is replaced by the shimmering activity
 * label (fix-tool-timeline-fold 3.1); the header stays clickable so the
 * fold can be expanded mid-stream. */
export function ToolTimelineHeader({ steps, filesChanged, open, onToggle, odId, streaming, activityLabel, elapsedMs }: {
  steps: number;
  filesChanged: number;
  open: boolean;
  onToggle: () => void;
  odId: string;
  /** Live-turn marker: swaps the resting summary for the activity label.
   * Absent/false renders the resting summary exactly as before. */
  streaming?: boolean;
  /** Current activity ("Running Shell"); null/empty falls back to "Thinking". */
  activityLabel?: string | null;
  /** Client-measured turn elapsed; present-only via ActivityLabel. */
  elapsedMs?: number | null;
}) {
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-expanded={open}
      data-od-id={odId}
      className="mb-2 flex w-full items-center gap-2 rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] px-2.5 py-1.5 text-left transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)]"
    >
      <Icon name="chevright" size={13} className={cx('shrink-0 text-muted transition-transform', open && 'rotate-90')}/>
      {streaming ? (
        // Same layout classes as the resting span — the activity swaps in
        // place, nothing about the row's geometry changes.
        <ActivityLabel
          label={activityLabel || 'Thinking'}
          elapsedMs={elapsedMs}
          className="min-w-0 flex-1 truncate font-mono text-[11px] text-muted"
        />
      ) : (
        <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-muted">
          {steps} steps{filesChanged > 0 ? ` · ${filesChanged} file${filesChanged === 1 ? '' : 's'} changed` : ''}
        </span>
      )}
    </button>
  );
}
