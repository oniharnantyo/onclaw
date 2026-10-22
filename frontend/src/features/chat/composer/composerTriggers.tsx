/**
 * Composer trigger menus (task 5.2): "/" slash commands, "@" agent mentions,
 * and "$" skills as three ComposerTriggerPopover instances on the vendored
 * trigger surface. Detection mirrors the legacy composer's `detectMenu`
 * exactly — slash only at the very start of the text, mentions/skills from
 * the nearest trigger character with no whitespace before the caret, and the
 * nearest trigger character wins (so `$foo` never opens the mention menu) —
 * while filtering keeps the legacy prefix rule rather than the primitive's
 * substring search. Selection inserts the token literally (`/new `, `@Scout `,
 * `$search `) through the directive behaviour, so the textarea keeps the raw
 * text the store sends.
 */
import { useMemo } from "react";
import type { ReactElement } from "react";
import { Sparkles, Terminal, UserRound, type LucideIcon } from "lucide-react";

import type {
  DirectiveFormatter,
  DirectiveSegment,
  TriggerAdapter,
  TriggerCategory,
  TriggerItem,
  TriggerMatch,
} from "@assistant-ui/react";

import { ComposerTriggerPopover } from "@/components/assistant-ui/elements/composer-trigger-popover.aui";

import type {
  Agent,
  Skill,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { useChatStore } from "../store";
import type { ChatStore } from "../types";

/** The three trigger characters the composer owns. */
type TriggerChar = "/" | "@" | "$";

/** Base slash commands; `/new` is intercepted locally before any send. */
const BASE_COMMANDS: readonly { cmd: string; desc: string }[] = [
  { cmd: "/new", desc: "Start a new session" },
];

/** Icons keyed by each item's `metadata.icon` (read by the vendored popover). */
const TRIGGER_ICONS = {
  command: Terminal,
  agent: UserRound,
  skill: Sparkles,
} satisfies Record<string, LucideIcon>;

/** Literal directive formatter: the text the popover inserts is the token
 * itself, and parsing leaves the composer text as a single text segment (the
 * composer never renders directive chips). The inserted token carries a
 * trailing space — the legacy composer's grammar — so the pick separates the
 * token from what the user types next AND the trigger stops matching (the
 * detection rejects a token followed by whitespace), which is what closes the
 * menu after the pick. */
function literalFormatter(
  serialize: (item: TriggerItem) => string
): DirectiveFormatter {
  return {
    serialize,
    parse: (text: string): readonly DirectiveSegment[] => [
      { kind: "text", text },
    ],
  };
}

const SLASH_FORMATTER = literalFormatter(
  (item: TriggerItem): string => `${item.label} `
);

const MENTION_FORMATTER = literalFormatter(
  (item: TriggerItem): string => `@${item.label} `
);

const SKILL_FORMATTER = literalFormatter(
  (item: TriggerItem): string => `$${item.label} `
);

/**
 * The shared detection rule (legacy `detectMenu`): slash only at the start
 * with no space, otherwise the nearest `@`/`$` with no whitespace between it
 * and the caret — and only when that nearest character is this trigger.
 */
function matchComposerToken(
  char: TriggerChar,
  text: string,
  caret: number
): TriggerMatch | null {
  const before = text.slice(0, caret);

  if (char === "/") {
    if (!before.startsWith("/") || before.includes(" ")) return null;

    return { query: before.slice(1), offset: 0, endOffset: caret };
  }

  for (let i = before.length - 1; i >= 0; i -= 1) {
    const ch = before[i];

    if (ch === " " || ch === "\n" || ch === "\t") return null;

    if (ch === "@" || ch === "$") {
      if (ch !== char) return null;

      return { query: before.slice(i + 1), offset: i, endOffset: caret };
    }
  }

  return null;
}

function slashMatcher(
  text: string,
  _char: string,
  caret: number
): TriggerMatch | null {
  return matchComposerToken("/", text, caret);
}

function mentionMatcher(
  text: string,
  _char: string,
  caret: number
): TriggerMatch | null {
  return matchComposerToken("@", text, caret);
}

function skillMatcher(
  text: string,
  _char: string,
  caret: number
): TriggerMatch | null {
  return matchComposerToken("$", text, caret);
}

/** The slash catalogue, filtered with the legacy prefix rule. */
function slashItems(query: string): readonly TriggerItem[] {
  const needle = query.toLowerCase();

  return BASE_COMMANDS.flatMap(
    (command: { cmd: string; desc: string }): TriggerItem[] =>
      command.cmd.slice(1).startsWith(needle)
        ? [
            {
              id: command.cmd.slice(1),
              type: "command",
              label: command.cmd,
              description: command.desc,
              metadata: { icon: "command" },
            },
          ]
        : []
  );
}

/** The agents list (the legacy @-mention source), prefix-filtered. */
function agentItems(agents: Agent[], query: string): readonly TriggerItem[] {
  const needle = query.toLowerCase();

  return agents.flatMap((agent: Agent): TriggerItem[] =>
    agent.Name.toLowerCase().startsWith(needle)
      ? [
          {
            id: agent.ID,
            type: "agent",
            label: agent.Name,
            metadata: { icon: "agent" },
          },
        ]
      : []
  );
}

/** The enabled skills (store-filtered), prefix-filtered. */
function skillItems(skills: Skill[], query: string): readonly TriggerItem[] {
  const needle = query.toLowerCase();

  return skills.flatMap((skill: Skill): TriggerItem[] =>
    skill.Name.toLowerCase().startsWith(needle)
      ? [
          {
            id: skill.Name,
            type: "skill",
            label: skill.Name,
            description: skill.Description,
            metadata: { icon: "skill" },
          },
        ]
      : []
  );
}

/** One trigger's adapter: no categories, so the popover always renders the
 * flat search list the legacy menus showed. */
function triggerAdapter(
  search: (query: string) => readonly TriggerItem[]
): TriggerAdapter {
  return {
    categories: (): readonly TriggerCategory[] => [],
    categoryItems: (): readonly TriggerItem[] => [],
    search,
  };
}

/** The slash catalogue is static, so its adapter is module-level. */
const SLASH_ADAPTER: TriggerAdapter = triggerAdapter(slashItems);

/** The three trigger menus, mounted once inside TriggerPopoverRoot. */
export function ComposerTriggers(): ReactElement {
  const agents = useChatStore((s: ChatStore) => s.agents);
  const skills = useChatStore((s: ChatStore) => s.skills);

  const mentionAdapter = useMemo(
    (): TriggerAdapter =>
      triggerAdapter((query: string) => agentItems(agents, query)),
    [agents]
  );

  const skillAdapter = useMemo(
    (): TriggerAdapter =>
      triggerAdapter((query: string) => skillItems(skills, query)),
    [skills]
  );

  return (
    <>
      <ComposerTriggerPopover
        char="/"
        matcher={slashMatcher}
        adapter={SLASH_ADAPTER}
        directive={{ formatter: SLASH_FORMATTER }}
        iconMap={TRIGGER_ICONS}
        fallbackIcon={Terminal}
        aria-label="Slash commands"
      />
      <ComposerTriggerPopover
        char="@"
        matcher={mentionMatcher}
        adapter={mentionAdapter}
        directive={{ formatter: MENTION_FORMATTER }}
        iconMap={TRIGGER_ICONS}
        fallbackIcon={UserRound}
        aria-label="Mentions"
      />
      <ComposerTriggerPopover
        char="$"
        matcher={skillMatcher}
        adapter={skillAdapter}
        directive={{ formatter: SKILL_FORMATTER }}
        iconMap={TRIGGER_ICONS}
        fallbackIcon={Sparkles}
        aria-label="Skills"
      />
    </>
  );
}
