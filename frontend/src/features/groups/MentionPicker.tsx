/**
 * The group composer's mention source (add-agent-groups task 5.4, spec
 * agent-groups "Group surfaces in the chat menu" — "Mention picker"): the
 * `@` trigger's item list in a group conversation. Offers the group's
 * members — resolved to their agent names, prefix-filtered with the
 * composer's legacy rule — plus the `@everyone` entry. Choosing a member
 * inserts the literal `@Name` text AND records the explicit recipient the
 * send binding carries (design D4: the same mechanism as a typed mention);
 * `@everyone` inserts `@everyone`, which the host parses from the text
 * alone.
 */
import type { TriggerItem } from "@assistant-ui/react";

import type {
  Agent,
  AgentGroupMember,
} from "../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";

/** The `@everyone` entry's item id — not an agent id; the pick handler
 * recognizes it through the metadata flag, not the id. */
export const EVERYONE_MENTION_ID = "everyone";

/** True for the group mention picker's `@everyone` entry. */
export function isEveryoneMention(item: TriggerItem): boolean {
  return item.metadata?.everyone === true;
}

/** The group conversation's mention items: the roster's member names, then
 * the everyone entry, each prefix-filtered with the composer's legacy rule
 * (the same one agentItems applies). */
export function groupMentionItems(
  members: AgentGroupMember[],
  agents: Agent[],
  query: string
): readonly TriggerItem[] {
  const needle = query.toLowerCase();
  const items: TriggerItem[] = [];

  for (const member of members) {
    const agent = agents.find((a: Agent): boolean => a.ID === member.AgentID);

    const name = agent?.Name || member.AgentID;

    if (!name.toLowerCase().startsWith(needle)) continue;

    items.push({
      id: member.AgentID,
      type: "agent",
      label: name,
      metadata: { icon: "agent" },
    });
  }

  if ("everyone".startsWith(needle)) {
    items.push({
      id: EVERYONE_MENTION_ID,
      type: "agent",
      label: "everyone",
      description: "Every member of the group",
      metadata: { icon: "agent", everyone: true },
    });
  }

  return items;
}
