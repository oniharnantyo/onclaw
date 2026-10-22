/**
 * Types for the agents menu: the props of each agent-surface component.
 */
import type { Agent } from "../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";

/**
 * Prompt-document lifecycle states exactly as the backend spells them in
 * Agent.PromptsStatus (internal/store PromptsStatus* constants).
 */
export const PROMPTS_STATUS = {
  GENERATING: "generating",
  READY: "ready",
  FAILED: "failed",
} as const;

export interface AgentCardProps {
  agent: Agent;
  onChat: (id: string) => void;
  onConfigure: (id: string) => void;
  /** Re-runs prompt generation for a failed agent (card retry affordance). */
  onRetry: (id: string) => void;
}

export interface AgentConfigPaneProps {
  agent: Agent;
  onClose?: () => void;
  onSaved: (agent: Agent) => void;
  /**
   * A confirmed delete from the configuration pane: parent drops the agent
   * and selects the next available agent or shows empty state.
   */
  onDeleted: (id: string) => void;
}

export type AgentConfigModalProps = AgentConfigPaneProps;

export interface CreateAgentModalProps {
  /** The loaded agents; the slug step validates uniqueness against them. */
  agents: Agent[];
  onClose: () => void;
  onCreated: (agent: Agent) => void;
}

export interface OnboardingLoaderProps {
  /** The new agent's name, in the "Onboarding <name>…" label (design D9). */
  name: string;
}

export interface DeleteAgentModalProps {
  agent: Agent;
  onClose: () => void;
  onDeleted: (id: string) => void;
}
