/**
 * Composer rail pickers (task 5.3): agent, model, and reasoning-effort
 * controls on the composer's bottom rail, hosted on the vendored
 * ComposerModelTrigger / ComposerMenu surface. The model list is the session
 * provider's capability set mapped onto PickableModel (unknown fields stay
 * empty rather than invented); the effort control offers ONLY the effective
 * model's supported levels and renders nothing when it supports none
 * (design D2). An unset override shows the inherited value plus a "default"
 * marker. Picks route through the existing store actions (design D1: the
 * override rides the session row, and the sticky per-agent default seeds new
 * sessions); the agent picker switches the chat context through openChat
 * without rebinding any viewed session.
 */
import { useEffect, useRef, useState } from "react";
import type {
  ComponentProps,
  KeyboardEvent as ReactKeyboardEvent,
  ReactElement,
  ReactNode,
} from "react";

import type { Agent } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { ModelCapability } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";

import {
  ComposerMenu,
  ComposerMenuItem,
  ComposerModelTrigger,
} from "@/components/assistant-ui/elements/composer";
import {
  ModelPicker as ModelPickerList,
  type PickableModel,
} from "@/components/assistant-ui/elements/model-picker";
import {
  ReasoningEffort,
  type EffortLevel,
} from "@/components/assistant-ui/elements/reasoning-effort";

import { useNavStore } from "../../../stores/nav";
import { formatTokens, shortModelName } from "../format";
import { useChatStore } from "../store";
import type { ChatStore } from "../types";

/** The effective config one resolution rule (design D1) shares: the session
 * override when set, else the agent's configured default; the effort levels
 * come from the effective model's capability row. */
interface EffectiveConfig {
  agent: Agent | null;
  model: string;
  effort: string;
  efforts: string[];
  modelOverride: string;
  effortOverride: string;
}

function useEffectiveConfig(): EffectiveConfig {
  const agents = useChatStore((s: ChatStore) => s.agents);
  const models = useChatStore((s: ChatStore) => s.models);
  const activeAgentID = useChatStore((s: ChatStore) => s.activeAgentID);
  const modelOverride = useChatStore((s: ChatStore) => s.modelOverride);
  const effortOverride = useChatStore((s: ChatStore) => s.effortOverride);

  const agent = agents.find((a: Agent) => a.ID === activeAgentID) ?? null;
  const model = modelOverride || agent?.Model || "";
  const effort = effortOverride || agent?.Effort || "";

  const efforts =
    models.find((m: ModelCapability) => m.ID === model)?.Efforts ?? [];

  return { agent, model, effort, efforts, modelOverride, effortOverride };
}

/** Maps one capability row onto the vendored model list: only the fields the
 * store actually knows are filled (no invented family or price). */
function pickableModel(model: ModelCapability): PickableModel {
  const capabilities: string[] = [];

  if (model.ImageInput) capabilities.push("image");

  if (model.PDFInput) capabilities.push("pdf");

  if (model.VideoInput) capabilities.push("video");

  return {
    id: model.ID,
    name: model.ID,
    family: "",
    context: formatTokens(model.ContextWindow),
    price: "",
    capabilities,
  };
}

/** The selected model's supported thinking levels, at face value: the
 * capability row carries no per-level budget, so the bar shows the spend
 * alone. */
function effortLevels(efforts: string[]): readonly EffortLevel[] {
  return efforts.map((effort: string): EffortLevel => ({
    key: effort,
    label: effort,
    budget: 0,
  }));
}

/** Row activation for the rail menus: pointer-down, so a pick lands before the
 * focus move (and the menu unmounts before the follow-up click), plus click
 * for keyboard activation. */
function rowHandlers(
  select: () => void
): Pick<ComponentProps<"button">, "onMouseDown" | "onClick"> {
  return {
    onMouseDown: (): void => select(),
    onClick: (): void => select(),
  };
}

/** Shared rail popover: a ComposerModelTrigger opening a ComposerMenu above
 * the rail, dismissed by a pick, Escape, or an outside click. `absorb`
 * marks the picker whose label yields first under squeeze (the model family
 * name — the designed distill): the others carry a near-zero shrink weight
 * so short labels ("Scout", "high") keep their text and only the long one
 * ellipsizes; in extremis every trigger still truncates (truncate zeroes
 * the automatic minimum), never overflows. */
function RailPicker({
  testId,
  ariaLabel,
  label,
  title,
  inherited,
  absorb = false,
  children,
}: {
  testId: string;
  ariaLabel: string;
  label: string;
  /** The trigger's tooltip (the model picker shows the family name and keeps
   * the full id here, design D4); omitted when there is nothing to show. */
  title?: string;
  /** Shows the "default" marker: the value is inherited, not an explicit
   * choice (spec chat-view "Composer affordances"). */
  inherited: boolean;
  /** This picker's container yields its width first under squeeze. */
  absorb?: boolean;
  children: (close: () => void) => ReactNode;
}): ReactElement {
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement | null>(null);

  useEffect((): (() => void) | undefined => {
    if (!open) return undefined;

    const onMouseDown = (event: MouseEvent): void => {
      // SAFETY: DOM event targets are nodes, and contains() takes one.
      const target = event.target as Node | null;

      if (
        containerRef.current &&
        target &&
        !containerRef.current.contains(target)
      ) {
        setOpen(false);
      }
    };

    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key === "Escape") setOpen(false);
    };

    document.addEventListener("mousedown", onMouseDown);
    document.addEventListener("keydown", onKeyDown);

    return (): void => {
      document.removeEventListener("mousedown", onMouseDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  return (
    <div
      ref={containerRef}
      className={
        absorb
          ? "relative flex min-w-0 items-center gap-1"
          : "relative flex min-w-0 shrink-[0.01] items-center gap-1"
      }
    >
      {/* Squeeze contract: the vendored trigger is a flex pill whose bare
          text node wraps mid-token when the rail is squeezed (live 1100px:
          `laguna-s-2.1` broke onto two lines). The className prop is the
          sanctioned styling seam (the vendored file is frozen), and twMerge
          lets `block` override the vendored `flex` — only a block box can
          ellipsize its own text; a flex container cannot. The chevron is
          pinned absolute in a pe-6 reserve so the ellipsis can never eat it
          (inline it shared the truncatable line: Chrome swapped a 0.1px
          overflow for "…" and dropped the chevron). leading-8 re-centers the
          single line the block layout lost. */}
      <ComposerModelTrigger
        model={label}
        title={title}
        open={open}
        data-testid={testId}
        aria-label={ariaLabel}
        aria-haspopup="true"
        className="relative block min-w-0 max-w-full truncate leading-8 pe-6 [&>svg]:absolute [&>svg]:end-3 [&>svg]:top-1/2 [&>svg]:-translate-y-1/2"
        onClick={() => setOpen((current: boolean) => !current)}
        onKeyDown={(event: ReactKeyboardEvent<HTMLButtonElement>): void => {
          if (event.key === "Escape") setOpen(false);
        }}
      />
      {inherited && (
        <span
          className="shrink-0 text-[10px] text-muted"
          data-testid={`${testId}-default`}
        >
          default
        </span>
      )}
      <ComposerMenu
        open={open}
        align="start"
        data-testid={`${testId}-menu`}
        aria-label={`${ariaLabel} options`}
      >
        {open
          ? children((): void => {
              setOpen(false);
            })
          : null}
      </ComposerMenu>
    </div>
  );
}

/** Agent picker: switches the chat context to the chosen agent's conversation
 * through the existing openChat/chatTarget navigation. It never rebinds a
 * viewed session; the chat target consumer selects that agent's latest
 * session instead. */
function AgentPicker(): ReactElement {
  const agents = useChatStore((s: ChatStore) => s.agents);
  const activeAgentID = useChatStore((s: ChatStore) => s.activeAgentID);

  const openChat = useNavStore(
    (s: ReturnType<typeof useNavStore.getState>) => s.openChat
  );

  const active = agents.find((a: Agent) => a.ID === activeAgentID) ?? null;

  return (
    <RailPicker
      testId="composer-agent-picker"
      ariaLabel="Switch agent"
      label={active?.Name ?? "Agent"}
      inherited={false}
    >
      {(close: () => void) => (
        <>
          <p className="px-2.5 py-1 font-mono text-[10px] tracking-wider text-muted uppercase">
            Agents
          </p>
          {agents.map((agent: Agent): ReactElement => (
            <ComposerMenuItem
              key={agent.ID}
              active={agent.ID === activeAgentID}
              data-testid={`composer-agent-option-${agent.ID}`}
              {...rowHandlers((): void => {
                openChat(agent.ID);
                close();
              })}
            >
              <span className="min-w-0 flex-1 truncate text-start">
                {agent.Name}
              </span>
              <span className="font-mono text-[10px] text-muted">Agent</span>
            </ComposerMenuItem>
          ))}
        </>
      )}
    </RailPicker>
  );
}

/** Model picker: the session provider's models from the capability DTO, plus
 * an "agent default" row that clears the override back to inherit. */
function ModelPicker(): ReactElement {
  const models = useChatStore((s: ChatStore) => s.models);
  const setModelOverride = useChatStore((s: ChatStore) => s.setModelOverride);
  const { agent, model, modelOverride } = useEffectiveConfig();

  return (
    <RailPicker
      testId="composer-model-picker"
      ariaLabel="Session model"
      label={shortModelName(model) || "Model"}
      title={model || undefined}
      inherited={model !== "" && modelOverride === ""}
      absorb
    >
      {(close: () => void) => (
        <>
          <ComposerMenuItem
            data-testid="composer-model-option-default"
            active={modelOverride === ""}
            {...rowHandlers((): void => {
              void setModelOverride("");
              close();
            })}
          >
            <span className="flex-1 text-start">Agent default</span>
            {agent?.Model && (
              <span className="font-mono text-[10px] text-muted">
                {agent.Model}
              </span>
            )}
          </ComposerMenuItem>
          <ModelPickerList
            models={models.map(pickableModel)}
            selectedId={modelOverride}
            onSelect={(id: string): void => {
              void setModelOverride(id);
              close();
            }}
            className="max-w-none border-0 p-0"
          />
        </>
      )}
    </RailPicker>
  );
}

/** Effort picker: only the effective model's supported levels, hidden
 * entirely when that set is empty (design D2). */
function EffortPicker(): ReactElement | null {
  const setEffortOverride = useChatStore((s: ChatStore) => s.setEffortOverride);
  const { effort, efforts, effortOverride } = useEffectiveConfig();

  if (efforts.length === 0) return null;

  return (
    <RailPicker
      testId="composer-effort-picker"
      ariaLabel="Reasoning effort"
      label={effort || "Effort"}
      inherited={effort !== "" && effortOverride === ""}
    >
      {(close: () => void) => (
        <>
          <ComposerMenuItem
            data-testid="composer-effort-option-default"
            active={effortOverride === ""}
            {...rowHandlers((): void => {
              void setEffortOverride("");
              close();
            })}
          >
            <span className="flex-1 text-start">Agent default</span>
          </ComposerMenuItem>
          <ReasoningEffort
            levels={effortLevels(efforts)}
            selectedKey={effortOverride}
            spent={0}
            onSelect={(key: string): void => {
              void setEffortOverride(key);
              close();
            }}
            className="max-w-none px-2 pb-2"
          />
        </>
      )}
    </RailPicker>
  );
}

/** The three rail controls in rail order (design D9). min-w-0 keeps the
 * squeeze chain unbroken from the composer toolbar down to each trigger, so
 * the labels truncate instead of wrapping the rail (live 1100px regression:
 * the rail's content overflowed its box by 212px). */
export function ComposerPickers(): ReactElement {
  return (
    <div
      className="flex min-w-0 items-center gap-1"
      data-testid="composer-pickers"
    >
      <AgentPicker />
      <ModelPicker />
      <EffortPicker />
    </div>
  );
}
