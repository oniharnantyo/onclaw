/**
 * Schedules menu (tasks 6.3 + 7.2, design D9/D10/D12): the schedule list —
 * rows with last-run outcome, agent, human-readable recurrence, relative
 * next fire, enabled toggle, and edit / run-now / delete actions — plus the
 * add/edit ScheduleEditorModal. The list fetches on mount (views unmount on
 * menu switch, so agent-cascade deletions surface on navigation, design D9)
 * and refetches coalesced on `schedules:changed`, the one event every
 * mutation, fire, and miss emits (design D10).
 */
import { useCallback, useEffect, useRef, useState } from "react";
import type { ReactElement } from "react";
import { Plus } from "lucide-react";

import type { Agent } from "../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { ConfirmDialog, EmptyState, PaneHeader } from "../../components";
import { Button } from "../../components/ui/Button";
import { Skeleton } from "../../components/ui/Skeleton";
import { client, type ScheduleView } from "../../lib/client";
import { toast } from "../../lib/toasts";
import { ScheduleEditorModal } from "./components/ScheduleEditorModal";
import { ScheduleRow } from "./components/ScheduleRow";

/** Bursts of schedules:changed (a run, its miss, a store update) coalesce
 * into one refetch inside this window. */
const REFETCH_COALESCE_MS = 100;

function errorMessage(cause: unknown, fallback: string): string {
  return cause instanceof Error ? cause.message : fallback;
}

/**
 * The open editor: absent when closed; `schedule` set for the edit form.
 * The view remounts the modal per open, so the form always seeds fresh.
 */
interface EditorState {
  schedule?: ScheduleView;
}

export default function SchedulesView(): ReactElement {
  const [schedules, setSchedules] = useState<ScheduleView[]>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [editor, setEditor] = useState<EditorState | null>(null);
  const [pendingDelete, setPendingDelete] = useState<ScheduleView | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [toggleBusyID, setToggleBusyID] = useState<string | null>(null);
  const [runBusyID, setRunBusyID] = useState<string | null>(null);

  // Handlers and the event subscription outlive render passes; they must
  // not write state after the menu switched away (views unmount).
  const mountedRef = useRef(true);

  useEffect((): (() => void) => {
    return (): void => {
      mountedRef.current = false;
    };
  }, []);

  const refetch = useCallback((): Promise<void> => {
    return client.schedules
      .list()
      .then((list: ScheduleView[]): void => {
        if (!mountedRef.current) return;
        setSchedules(list);
        setError(null);
      })
      .catch((err: Error): void => {
        if (!mountedRef.current) return;
        setError(errorMessage(err, "Failed to load schedules"));
      });
  }, []);

  // Mount-time fetch (design D9) plus the agents the rows and the editor
  // need. A failed agents load keeps the deterministic fallback avatars.
  useEffect((): (() => void) => {
    let live = true;

    void refetch().finally((): void => {
      if (live) setLoading(false);
    });
    client.agents
      .list()
      .then((list: Agent[]): void => {
        if (live) setAgents(list);
      })
      .catch((): void => undefined);

    return (): void => {
      live = false;
    };
  }, [refetch]);

  // Live updates (design D10): every mutation, fire, and miss emits
  // schedules:changed; bursts coalesce into one refetch.
  useEffect((): (() => void) => {
    let timer: ReturnType<typeof setTimeout> | null = null;

    const off = client.events.onSchedulesChanged((): void => {
      if (timer !== null) return;
      timer = setTimeout((): void => {
        timer = null;
        void refetch();
      }, REFETCH_COALESCE_MS);
    });

    return (): void => {
      off();

      if (timer !== null) clearTimeout(timer);
    };
  }, [refetch]);

  const handleToggle = (schedule: ScheduleView, next: boolean): void => {
    setToggleBusyID(schedule.id);
    client.schedules
      .setEnabled(schedule.id, next)
      .then((): Promise<void> => refetch())
      .catch((err: Error): void => {
        toast(errorMessage(err, "Failed to update the schedule"), "danger");
      })
      .finally((): void => {
        if (mountedRef.current) setToggleBusyID(null);
      });
  };

  const handleRunNow = (schedule: ScheduleView): void => {
    setRunBusyID(schedule.id);
    client.schedules
      .runNow(schedule.id)
      .then((): Promise<void> => refetch())
      .catch((err: Error): void => {
        toast(errorMessage(err, "Failed to run the schedule"), "danger");
      })
      .finally((): void => {
        if (mountedRef.current) setRunBusyID(null);
      });
  };

  const handleConfirmDelete = (): void => {
    const target = pendingDelete;

    if (!target) return;
    setDeleteBusy(true);
    client.schedules
      .delete(target.id)
      .then((): Promise<void> => refetch())
      .then((): void => {
        setPendingDelete(null);
      })
      .catch((err: Error): void => {
        toast(errorMessage(err, "Failed to delete the schedule"), "danger");
        setPendingDelete(null);
      })
      .finally((): void => {
        if (mountedRef.current) setDeleteBusy(false);
      });
  };

  const openEditor = (schedule?: ScheduleView): void => {
    setEditor(schedule ? { schedule } : {});
  };

  return (
    <div
      className="flex h-full min-h-0 min-w-0 flex-1 flex-col gap-4 p-6 lg:pt-10"
      data-testid="schedules-view"
    >
      <PaneHeader
        title="Schedules"
        actions={
          <Button
            size="sm"
            data-testid="btn-add-schedule"
            onClick={() => openEditor()}
          >
            <Plus aria-hidden="true" className="size-4" />
            Add schedule
          </Button>
        }
      />

      {error && (
        <p
          className="text-sm text-danger"
          role="alert"
          data-testid="schedules-error"
        >
          {error}
        </p>
      )}

      {loading ? (
        <div className="flex flex-col gap-2" aria-label="Loading schedules">
          {[0, 1, 2].map((i: number) => (
            <Skeleton key={i} className="h-16 w-full" />
          ))}
        </div>
      ) : schedules.length === 0 ? (
        <div className="flex flex-1 items-start justify-center pt-8">
          <EmptyState
            testId="schedules-empty"
            title="No schedules yet"
            description="Create one to run an agent on a recurring prompt — or ask an agent in chat: it can create schedules from there with its Scheduler tool."
            action={
              <Button
                size="sm"
                data-testid="btn-add-schedule-empty"
                onClick={() => openEditor()}
              >
                Add schedule
              </Button>
            }
          />
        </div>
      ) : (
        <div
          className="od-scroll flex flex-col gap-2 overflow-y-auto"
          role="list"
          aria-label="Schedules"
          data-testid="schedules-list"
        >
          {schedules.map((schedule: ScheduleView) => (
            <ScheduleRow
              key={schedule.id}
              schedule={schedule}
              agent={
                agents.find((a: Agent): boolean => a.ID === schedule.agentId) ??
                null
              }
              toggleBusy={toggleBusyID === schedule.id}
              runBusy={runBusyID === schedule.id}
              onToggle={(next: boolean): void => handleToggle(schedule, next)}
              onRunNow={(): void => handleRunNow(schedule)}
              onEdit={(): void => openEditor(schedule)}
              onDelete={(): void => setPendingDelete(schedule)}
            />
          ))}
        </div>
      )}

      {editor && (
        <ScheduleEditorModal
          open
          schedule={editor.schedule}
          agents={agents}
          onClose={(): void => setEditor(null)}
          onSaved={(): void => {
            setEditor(null);
            void refetch();
          }}
        />
      )}

      {pendingDelete && (
        <ConfirmDialog
          title="Delete schedule"
          testId="delete-schedule-dialog"
          confirmTestId="btn-confirm-delete-schedule"
          cancelTestId="btn-cancel-delete-schedule"
          confirmDisabled={deleteBusy}
          onConfirm={handleConfirmDelete}
          onClose={(): void => {
            if (!deleteBusy) setPendingDelete(null);
          }}
        >
          <p className="text-[13px] text-muted">
            Deleting “{pendingDelete.name}” removes it and its history. Sessions
            its runs created stay in chat.
          </p>
        </ConfirmDialog>
      )}
    </div>
  );
}
