// Post-turn memory chip (integrate-agent-zero-memory D11, task 5.2): renders
// a `memory_ingested` transcript entry — the count and visibility breakdown
// of what the background pipeline committed for the turn, NEVER the content.
// Clicking opens an inline drawer listing the stored facts' ids with their
// provenance (fetched through the notes detail endpoint) and per-fact
// deletion. Private extractions are disclosed as counts, not revealed.
//
// Hydration-safe: the transcript translator mints the same entry shape for
// the live/catch-up stream and the reloaded history, so the chip renders
// identically after a reload — it is just another session event.
import { useState } from "react";
import { cx } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { api, formatApiError, type ApiMemoryNote } from "../../lib/api";

export interface MemoryIngestedChipProps {
  m: any;
  /** Workspace slug the notes detail endpoint is called against; absent
   * (e.g. legacy contexts) degrades the drawer to ids-only. */
  workspaceId?: string;
  onToast?: (text: string, kind?: string) => void;
}

/** "2 shared · 1 private" — the spec's disclosure vocabulary, counts only. */
export function memoryChipSummary(counts: { shared: number; user: number; agent: number }): string {
  const parts: string[] = [];
  if (counts.shared > 0) parts.push(`${counts.shared} shared`);
  if (counts.user > 0) parts.push(`${counts.user} private`);
  if (counts.agent > 0) parts.push(`${counts.agent} agent`);
  return parts.join(" · ");
}

interface DetailRow {
  id: string;
  note: ApiMemoryNote | null;
}

export function MemoryIngestedChip({ m, workspaceId, onToast = () => {} }: MemoryIngestedChipProps) {
  const counts = m?.memory?.counts || { shared: 0, user: 0, agent: 0 };
  const noteIds: string[] = m?.memory?.noteIds || [];
  const total = counts.shared + counts.user + counts.agent;
  const summary = memoryChipSummary(counts);

  const [open, setOpen] = useState(false);
  const [rows, setRows] = useState<DetailRow[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);

  const loadDetails = async () => {
    if (!workspaceId || noteIds.length === 0) {
      setRows([]);
      return;
    }
    setLoading(true);
    setLoadError(null);
    try {
      // One detail call per committed id — the payload carries no content,
      // so provenance rides the read path.
      const details = await Promise.all(
        noteIds.map(async (id) => {
          try {
            const res = await api.memory.note(workspaceId, id);
            return { id, note: res.note };
          } catch {
            return { id, note: null };
          }
        })
      );
      setRows(details);
    } finally {
      setLoading(false);
    }
  };

  const toggle = () => {
    const next = !open;
    setOpen(next);
    if (next && rows === null) void loadDetails();
  };

  const handleDelete = async (row: DetailRow) => {
    if (!workspaceId || !row.note) return;
    try {
      await api.memory.deleteNote(workspaceId, row.note.id);
      setRows((prev) => (prev ? prev.filter((r) => r.id !== row.id) : prev));
      onToast("Fact deleted — hidden everywhere, the deletion is recorded");
    } catch (err: unknown) {
      onToast(formatApiError(err, "Failed to delete fact"), "danger");
    }
  };

  if (total === 0 && noteIds.length === 0) return null;

  return (
    <div className="flex px-2" data-od-id={'msg-' + m.id} data-role="memory-chip">
      <div className="min-w-0 flex-1">
        <div
          data-testid={'memory-chip-' + m.id}
          className="inline-flex max-w-full flex-col rounded-md border border-[color-mix(in_oklab,var(--accent)_28%,transparent)] bg-[color-mix(in_oklab,var(--accent)_6%,transparent)]"
        >
          <button
            type="button"
            onClick={toggle}
            aria-expanded={open}
            data-testid={'memory-chip-toggle-' + m.id}
            className="flex items-center gap-2 px-3 py-1.5 text-left transition-colors hover:bg-[color-mix(in_oklab,var(--accent)_8%,transparent)]"
          >
            <Icon name="memory" size={12} className="shrink-0 text-accent" />
            <span className="text-[12px] font-medium text-fg2">
              Remembered{summary ? " · " + summary : ""}
            </span>
            <Icon
              name="chevdown"
              size={11}
              className={cx("shrink-0 text-muted transition-transform", open && "rotate-180")}
            />
          </button>
          {open && (
            <div className="border-t border-[color-mix(in_oklab,var(--accent)_20%,transparent)] px-3 py-2">
              {!workspaceId ? (
                <p className="text-[11px] text-muted">
                  {noteIds.length} {noteIds.length === 1 ? "fact" : "facts"} stored this turn — provenance
                  needs a workspace context.
                </p>
              ) : loading ? (
                <p className="text-[11px] text-muted" role="status">
                  Loading provenance…
                </p>
              ) : loadError ? (
                <p className="text-[11px] text-danger">{loadError}</p>
              ) : (rows || []).length === 0 ? (
                <p className="text-[11px] text-muted" data-testid={'memory-chip-empty-' + m.id}>
                  Nothing remains from this turn — every fact has been deleted.
                </p>
              ) : (
                <ul className="space-y-1.5">
                  {(rows || []).map((row) => (
                    <li key={row.id} className="flex items-center gap-2 text-[11px] leading-5" data-testid={'memory-chip-row-' + row.id}>
                      <span className="font-mono text-muted">{row.id.slice(0, 8)}</span>
                      {row.note ? (
                        <>
                          <span className="rounded-[4px] bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] px-1 py-0.5 font-mono text-[10px] text-fg2">
                            {row.note.visibility}
                          </span>
                          <span className="min-w-0 truncate text-muted" title={`origin ${row.note.origin} · learned ${row.note.learned_at} · source event ${row.note.source_event_id}`}>
                            {row.note.origin} · {new Date(row.note.learned_at).toLocaleString()} · ev{" "}
                            {row.note.source_event_id.slice(0, 8)}
                          </span>
                          <button
                            type="button"
                            aria-label="Delete fact"
                            data-testid={'memory-chip-delete-' + row.id}
                            onClick={() => void handleDelete(row)}
                            className="ml-auto flex h-6 w-6 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-danger"
                          >
                            <Icon name="trash" size={12} />
                          </button>
                        </>
                      ) : (
                        <span className="text-muted" title="The fact was deleted or is not visible to you">
                          no longer visible
                        </span>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
