import { MultiSelectCombobox } from '../ui/MultiSelectCombobox';
import type { ApiAgent } from '../../lib/api';

export interface AttachAgentsListProps {
  agents: ApiAgent[];
  /** Whether the agent is currently attached — the CALLER owns attachment
   * state (optimistic updates stay with callers, add-connection-edit D4). */
  isAttached: (agent: ApiAgent) => boolean;
  onToggle: (agent: ApiAgent) => void;
  disabled?: boolean;
}

/**
 * Searchable multi-select of the workspace's agents (add-connection-edit D4):
 * the shared agent-selection picker rendered by the connect hand-off and the
 * connection edit dialog. Workspaces can hold many agents, so the list is a
 * `MultiSelectCombobox` — one row per agent with a search box — instead of a
 * long column of per-agent toggles. The caller still owns attachment state:
 * `isAttached` derives the current selection and `onToggle` is called once per
 * agent whose selection changed (per-agent error toasts, optimistic rows, or
 * atomic saves remain caller policy).
 */
export function AttachAgentsList({ agents, isAttached, onToggle, disabled = false }: AttachAgentsListProps) {
  const options = agents.map((a) => ({ value: a.id, label: a.name }));
  const selectedIds = agents.filter(isAttached).map((a) => a.id);

  const handleChange = (nextIds: string[]) => {
    // The primitive emits the whole next selection; callers expect a single
    // per-agent toggle, so diff against the state they handed us and emit one
    // call per changed agent, in `agents` order.
    for (const agent of agents) {
      const wasSelected = isAttached(agent);
      const isSelected = nextIds.includes(agent.id);
      if (wasSelected !== isSelected) onToggle(agent);
    }
  };

  return (
    <div className="space-y-1.5" data-testid="attach-agents-list">
      <MultiSelectCombobox
        options={options}
        value={selectedIds}
        onChange={handleChange}
        placeholder="Select agents…"
        searchPlaceholder="Search agents…"
        emptyText="No agents match."
        disabled={disabled}
        data-testid="attach-agents"
        aria-label="Attached agents"
      />
    </div>
  );
}
