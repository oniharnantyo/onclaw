/**
 * Group stopped notice (align-group-task-lifecycle design D6, spec
 * agent-groups "State stays off messages"): one row per stopped ROOT run —
 * a paused or failed task whose RootTaskID is its own — rendered once at the
 * transcript tail beside the activity line. Task state leaves the message
 * rows entirely; the notice is the only remaining task surface, so it also
 * carries Reassign (the spec's "Reassign moves ownership" scenario needs a
 * user-reachable affordance, and an open task is exactly what a stopped
 * chain has). Every stopped root — paused or failed — offers Continue
 * (re-queues the task and its stopped descendants with "Continue" as the
 * follow-up; the spec's "continue a stopped task" covers failed runs too)
 * and Cancel (ends the whole root chain — the backend refuses non-stopped
 * targets, so the danger toast covers an active task whose root drifted).
 * Only a paused root offers Reassign (moves the open task to another
 * member, picked from the roster): failed is terminal, and reassignment
 * needs an open task. Every
 * control calls the corresponding groups binding and refreshes the open
 * group's tasks through the store; a refusal surfaces as a danger toast
 * (the session delete precedent).
 */
import { useState } from "react";
import type { ReactElement } from "react";

import type {
  Agent,
  AgentGroupMember,
  AgentGroupTask,
} from "../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { AgentAvatar } from "../../components";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { ModalShell } from "../../components/ModalShell";
import { StatusDot } from "../../components/ui/StatusDot";
import type { StatusDotStatus } from "../../components/ui/StatusDot";
import { cn } from "../../lib/cn";
import { client } from "../../lib/client";
import { toast } from "../../lib/toasts";
import { useChatStore } from "../chat/store";
import type { ExtendedChatStore } from "../chat/store";
import { GroupTaskState } from "./groupEntries";

/** The quiet control chip the row's actions share (the transcript action
 * grammar's small scale — the per-message task chip the rows inherit). */
const CONTROL_CLASS =
  "flex h-6 items-center gap-1 rounded px-1.5 text-[11px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg";

function errorMessage(cause: unknown, fallback: string): string {
  return cause instanceof Error ? cause.message : fallback;
}

/** The reason the notice shows (spec: "with the stop reason"): the pause
 * reason for a paused run, the failure's error text for a failed one. Null
 * when neither carries text — the row then rests on its state marker. */
function reasonOf(task: AgentGroupTask): string | null {
  if (task.State === GroupTaskState.Failed)
    return task.Error === null || task.Error === "" ? null : task.Error;

  return task.StopReason === null || task.StopReason === ""
    ? null
    : task.StopReason;
}

/** The stopped run's dot (the shared vocabulary's warn/error pair) and its
 * display word. `satisfies` keeps the literal types — no assertions — and
 * the Record target gives the unmarked fallback its dot vocabulary. */
const STOPPED_MARKER = {
  [GroupTaskState.Paused]: { dot: "warn", word: "Paused" },
  [GroupTaskState.Failed]: { dot: "error", word: "Failed" },
} satisfies Record<string, { dot: StatusDotStatus; word: string }>;

/** One stopped root run's notice row: the marker, the reason, and the
 * lifecycle controls the run's state admits. */
function StoppedRun({ task }: { task: AgentGroupTask }): ReactElement {
  const agents = useChatStore((s: ExtendedChatStore) => s.agents);
  const members = useChatStore((s: ExtendedChatStore) => s.groupMembers);

  const refreshTasks = useChatStore(
    (s: ExtendedChatStore) => s.refreshGroupTasks
  );

  const [reassigning, setReassigning] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [working, setWorking] = useState(false);

  // The continue gate matches the backend's: a stopped task — paused or
  // failed — continues. Reassign stays paused-only: it needs an open task.
  const stopped =
    task.State === GroupTaskState.Paused ||
    task.State === GroupTaskState.Failed;

  const paused = task.State === GroupTaskState.Paused;

  const run = (action: () => Promise<void>): void => {
    setWorking(true);

    void action()
      .then((): Promise<void> => refreshTasks())
      .catch((err: Error): void => {
        toast(errorMessage(err, "The task update failed"), "danger");
      })
      .finally((): void => {
        setWorking(false);
      });
  };

  // The parent filter only admits the two stopped states; the fallback
  // rests on the raw state word rather than a wrong marker.
  const marker = STOPPED_MARKER[task.State] ?? {
    dot: "idle",
    word: task.State,
  };

  return (
    <div
      className="flex flex-wrap items-center gap-2 py-1 text-[11px] text-muted"
      data-testid={`group-stopped-notice-${task.TaskID}`}
      data-task-state={task.State}
    >
      <span className="flex items-center gap-1.5">
        <StatusDot status={marker.dot} />
        <span>{marker.word}</span>
      </span>
      {reasonOf(task) !== null && (
        <span
          className={
            task.State === GroupTaskState.Failed
              ? "text-dangertext"
              : "text-warntext"
          }
          data-testid={`group-task-stop-reason-${task.TaskID}`}
        >
          {reasonOf(task)}
        </span>
      )}
      <span className={cn("flex items-center gap-1", working && "opacity-50")}>
        {stopped && (
          <button
            type="button"
            data-testid={`group-task-continue-${task.TaskID}`}
            disabled={working}
            className={CONTROL_CLASS}
            onClick={(): void =>
              run((): Promise<void> =>
                client.groups.continueTask(task.TaskID, "Continue")
              )
            }
          >
            Continue
          </button>
        )}
        {paused && members.length > 1 && (
          <button
            type="button"
            data-testid={`group-task-reassign-${task.TaskID}`}
            disabled={working}
            className={CONTROL_CLASS}
            onClick={(): void => setReassigning(true)}
          >
            Reassign
          </button>
        )}
        <button
          type="button"
          data-testid={`group-task-cancel-${task.TaskID}`}
          disabled={working}
          className={cn(CONTROL_CLASS, "hover:text-dangertext")}
          onClick={(): void => setCancelling(true)}
        >
          Cancel
        </button>
      </span>
      {reassigning && (
        <ModalShell
          title="Reassign task"
          testId="group-task-reassign-modal"
          onClose={(): void => setReassigning(false)}
        >
          <div className="flex flex-col gap-1.5">
            {members.map((m: AgentGroupMember): ReactElement => {
              const agent = agents.find(
                (a: Agent): boolean => a.ID === m.AgentID
              );

              return (
                <button
                  key={m.AgentID}
                  type="button"
                  data-testid={`group-task-reassign-to-${m.AgentID}`}
                  disabled={m.AgentID === (task.OwnerAgentID ?? "")}
                  className="flex items-center gap-2 rounded-md px-2 py-1.5 text-start text-[13px] text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg disabled:cursor-not-allowed disabled:opacity-40"
                  onClick={(): void => {
                    setReassigning(false);
                    run((): Promise<void> =>
                      client.groups.reassignTask(task.TaskID, m.AgentID)
                    );
                  }}
                >
                  <AgentAvatar
                    id={m.AgentID}
                    avatar={agent?.Avatar ?? null}
                    size={18}
                  />
                  <span>{agent?.Name || m.AgentID}</span>
                  {m.AgentID === (task.OwnerAgentID ?? "") && (
                    <span className="text-[11px] text-muted">owner</span>
                  )}
                </button>
              );
            })}
          </div>
        </ModalShell>
      )}
      {cancelling && (
        <ConfirmDialog
          title="Cancel task?"
          testId="group-task-cancel-confirm"
          confirmTestId={`btn-confirm-cancel-task-${task.TaskID}`}
          cancelTestId="btn-cancel-cancel-task"
          confirmLabel="Cancel task"
          onConfirm={(): void => {
            setCancelling(false);
            run((): Promise<void> => client.groups.cancelTask(task.TaskID));
          }}
          onClose={(): void => setCancelling(false)}
        >
          <p className="text-[13px] text-muted">
            The whole chain abandons — every open task of this run's root ends
            and no further member turns start for them. Stop remains the control
            for active work.
          </p>
        </ConfirmDialog>
      )}
    </div>
  );
}

/** The transcript tail's stopped-notice block: one row per stopped root run
 * of the open group. Renders nothing on the session surface or while the
 * open group has no stopped root (design D6). */
export function GroupStoppedNotice(): ReactElement | null {
  const activeGroupID = useChatStore((s: ExtendedChatStore) => s.activeGroupID);
  const groupTasks = useChatStore((s: ExtendedChatStore) => s.groupTasks);

  if (activeGroupID === null) return null;

  const stoppedRoots = groupTasks.filter(
    (t: AgentGroupTask): boolean =>
      t.RootTaskID === t.TaskID &&
      (t.State === GroupTaskState.Paused || t.State === GroupTaskState.Failed)
  );

  if (stoppedRoots.length === 0) return null;

  return (
    <>
      {stoppedRoots.map((task: AgentGroupTask): ReactElement => {
        return <StoppedRun key={task.TaskID} task={task} />;
      })}
    </>
  );
}
