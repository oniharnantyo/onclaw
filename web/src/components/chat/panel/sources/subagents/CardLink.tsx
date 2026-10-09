// The transcript card's deep-link affordance (add-subagent-runs-surface 6.3):
// `agent` / `background_shell` cards open the right panel's Subagents source —
// the trajectory detail on the joined task (D5 join, computed here from the
// task store), the list view when unjoinable. Cards carry NO inner child
// events; the affordance only navigates (one-card rule).
//
// ToolCall renders this as a SIBLING of the expand button (buttons never
// nest), the same placement as the generic open-in-panel control — for these
// two tool ids it replaces that control entirely (see ToolCall's candidate
// gate), and it works on running cards where the generic gate stays silent.

import { useStore } from "../../../../../store";
import { Icon } from "../../../../ui/Icon";
import { useTaskEntries } from "../../../../../lib/tasks";
import { cardOrderIndex, cardTaskDescription, deepLinkTaskId } from "./view";

export function SubagentsCardLink({ t, siblings }: { t: any; siblings?: any[] }) {
  // The chat the transcript is mounted over — ChatView remounts per
  // conversation, so pos.chatId IS this card's chat (the browser mirror's
  // identity rule).
  const chatId: string = useStore((s: any) => s.pos.chatId) || '';
  const entries = useTaskEntries(chatId);
  const description = cardTaskDescription(t);
  const orderIndex = cardOrderIndex(t, siblings);
  const taskId = deepLinkTaskId(entries, description, orderIndex);

  const open = () => {
    const payload: Record<string, unknown> = taskId ? { chatId, taskId } : { chatId };
    useStore.getState().openPanelTab({ kind: 'subagents', title: 'Subagents', payload });
  };

  return (
    <button type="button" onClick={(e) => { e.stopPropagation(); open(); }}
      data-od-id="tool-open-subagents" data-testid="tool-open-subagents"
      title="Open in Subagents panel" aria-label="Open in Subagents panel"
      className="mr-1.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-[5px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] hover:text-fg2">
      <Icon name="bot" size={13}/>
    </button>
  );
}
