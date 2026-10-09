// Subagents source registration (add-subagent-runs-surface 6.1, design D7):
// one source kind with two internal views, plus the candidate matcher mapping
// `agent` / `background_shell` cards to the panel. Importing this module
// registers both and installs the terminal badge watcher (6.2) — ordinary
// registrations through the same registry API a plugin would use (AGENTS.md);
// nothing downstream edits the panel shell.

import { registerPanelCandidate, registerPanelSource, type PanelCandidate, type PanelCardInput } from "../../../../../lib/panel/registry";
import { SubagentsSource } from "./SubagentsSource";
import { installTaskTerminalBadgeWatcher } from "./watcher";

/** Candidate matcher (D7): any reserved subagent-lane card is panel-able.
 * The payload stays empty on purpose — the card carries no session identity,
 * so the dedup key yields ONE Subagents tab per chat; the renderer resolves
 * chat/session from the store (the browser mirror's precedent). Deep-links
 * from the cards themselves (6.3) address `{chatId, taskId}` payloads. */
export function subagentsCandidateMatcher(card: PanelCardInput): PanelCandidate | null {
  if (card?.name !== 'agent' && card?.name !== 'background_shell') return null;
  return { kind: 'subagents', title: 'Subagents', payload: {} };
}

registerPanelSource('subagents', ({ tab, ctx }) => <SubagentsSource tab={tab} ctx={ctx}/>);
registerPanelCandidate(subagentsCandidateMatcher);
installTaskTerminalBadgeWatcher();
