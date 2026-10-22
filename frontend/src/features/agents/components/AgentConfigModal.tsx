/**
 * Re-export AgentConfigPane as AgentConfigModal for backwards compatibility.
 * The modal shell is retired per design D8 in favor of AgentConfigPane.
 */
export {
  AgentConfigPane,
  AgentConfigPane as AgentConfigModal,
} from "./AgentConfigPane";
