/**
 * The tool-renderer registry seam (task 2.4 → 4.2, design D6): the map of
 * tool name → renderer the toolkit registration is built from, plus the
 * heavy-turn fold slot. The export names and shapes are the contract with
 * the transcript (wave 2A): keep both.
 */
export {
  toolRenderers,
  type ToolRenderer,
  type ToolRendererMap,
} from "./renderers";

export { ChatToolGroup } from "./ChatToolGroup";

export type { ChatToolGroupProps, ToolGroupPart } from "./ChatToolGroup";
