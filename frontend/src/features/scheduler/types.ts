/**
 * Local types for the scheduler feature (tasks 6.3 + 7.2, design D12): the
 * editor modal's prop contract, shared by the Schedules view (which mounts
 * it), the modal itself, and the component tests.
 */
import type { Agent } from "../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { ScheduleView } from "../../lib/client";

/** Props of the add/edit schedule dialog (one form for both, design D12). */
export interface ScheduleEditorModalProps {
  /** Whether the dialog is mounted-open; false renders nothing. */
  open: boolean;
  /** The schedule being edited; absent for the add form. */
  schedule?: ScheduleView;
  /** The agents the schedule may target; the Select renders over it. */
  agents: Agent[];
  /** Closes the dialog without saving (cancel, backdrop, Escape). */
  onClose: () => void;
  /** Called after a successful save; the view refetches and unmounts the
   * dialog (the list owns the close, per design D12). */
  onSaved: () => void;
}
