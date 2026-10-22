import { useEffect, useState } from "react";
import type { ReactElement } from "react";
import type { AvatarFullConfig } from "react-nice-avatar";

import type {
  MCPServer,
  Skill,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type { Agent } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type {
  CreateAgentInput,
  ModelCapability,
  ProviderView,
  ToolEntry,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import { FormField, FormGrid, ModalShell } from "../../../components";
import { Button } from "../../../components/ui/Button";
import { Combobox } from "../../../components/ui/Combobox";
import { Icon } from "../../../components/ui/Icon";
import { Input } from "../../../components/ui/Input";
import { MicroLabel } from "../../../components/ui/MicroLabel";
import { Select } from "../../../components/ui/Select";
import { Textarea } from "../../../components/ui/Textarea";
import { Alert } from "../../../components/ui/Alert";
import { OptionChips } from "../../../components/ui/OptionChips";
import type { OptionChipOption } from "../../../components/ui/OptionChips";
import { client } from "../../../lib/client";
import { AvatarPicker, generateRandomAvatar } from "./AvatarPicker";
import { catalogContextWindow } from "../modelContextWindow";
import { parseOptionalInt } from "../parseOptionalInt";
import { isValidSlug, suggestSlug } from "../slug";
import type { CreateAgentModalProps } from "../types";
import { McpServerList } from "./McpServerList";
import { OnboardingLoader } from "./OnboardingLoader";

/** The three wizard steps, in order (spec: Create agent). */
const STEP_TITLES: readonly string[] = ["Identity", "Model", "Capabilities"];

/** The temperature slider's starting value (onclaw-v2's create default). */
const DEFAULT_TEMPERATURE = "1.0";

/**
 * Sentinel option value that switches the model picker to free-text entry
 * (onclaw-v2's "Custom model ID…" row): models outside the provider's list
 * stay reachable.
 */
const CUSTOM_MODEL_VALUE = "__custom__";

/**
 * Per-field validation errors, keyed by the wizard field (task 4.5): the
 * message renders under the offending control via FormField and the control
 * takes the danger border — never a detached message under the stepper.
 */
type FieldName =
  | "name"
  | "slug"
  | "brief"
  | "provider"
  | "model"
  | "temperature"
  | "maxTokens"
  | "contextWindow";

type FieldErrors = Partial<Record<FieldName, string>>;

/**
 * The three-step create wizard (tasks 5.1–5.2, spec: Create agent): Identity
 * (AvatarPicker, name, auto-suggested DNS-label slug, optional multi-line
 * description, brief), Model (provider/model plus an advanced section whose
 * context window auto-fills from the static catalog), Capabilities (skills,
 * tools, MCP opt-ins with the app's allowlist semantics). Each step validates
 * before advancing — per field, on the field (tasks.md 4.5); submission
 * persists the agent and runs prompt generation synchronously behind the
 * OnboardingLoader (design D4), which is the ONE spinner for the phase — the
 * busy footer never spins alongside it (tasks.md 4.6). The backend keeps the
 * row even when generation fails, so a rejected create refreshes the list
 * and hands over the failed row instead of losing the agent.
 */
export function CreateAgentModal({
  agents,
  onClose,
  onCreated,
}: CreateAgentModalProps): ReactElement {
  const [step, setStep] = useState(0);

  // Identity step.
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugTouched, setSlugTouched] = useState(false);
  const [description, setDescription] = useState("");
  const [brief, setBrief] = useState("");

  // A full random config rolls once per mount and then only changes through
  // the picker; it persists verbatim on the agent row (store.Agent.Avatar).
  const [avatar, setAvatar] = useState<AvatarFullConfig>(() =>
    generateRandomAvatar()
  );

  // Model step.
  const [providers, setProviders] = useState<ProviderView[]>([]);
  const [models, setModels] = useState<ModelCapability[]>([]);
  const [providerID, setProviderID] = useState("");
  const [model, setModel] = useState("");
  const [customMode, setCustomMode] = useState(false);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [temperature, setTemperature] = useState(DEFAULT_TEMPERATURE);
  const [maxTokens, setMaxTokens] = useState("");
  const [contextWindow, setContextWindow] = useState("");
  const [contextWindowTouched, setContextWindowTouched] = useState(false);

  // Capabilities step.
  const [skills, setSkills] = useState<Skill[]>([]);
  const [tools, setTools] = useState<ToolEntry[]>([]);
  const [mcpServers, setMcpServers] = useState<MCPServer[]>([]);
  const [selectedSkills, setSelectedSkills] = useState<string[]>([]);
  const [selectedTools, setSelectedTools] = useState<string[]>([]);
  const [selectedMcp, setSelectedMcp] = useState<string[]>([]);

  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect((): (() => void) => {
    let mounted = true;

    void client.settings.providers().then((p: ProviderView[]): void => {
      if (mounted) setProviders(p);
    });
    void client.settings.skills().then((s: Skill[]): void => {
      if (mounted) setSkills(s);
    });
    void client.settings.tools().then((t: ToolEntry[]): void => {
      if (mounted) setTools(t);
    });
    void client.settings.mcpServers().then((m: MCPServer[]): void => {
      if (mounted) setMcpServers(m);
    });

    return (): void => {
      mounted = false;
    };
  }, []);

  // Models populate whenever the chosen provider changes.
  useEffect((): (() => void) => {
    if (!providerID) {
      setModels([]);

      return (): void => undefined;
    }

    let mounted = true;

    void client.settings
      .providerModels(providerID)
      .then((m: ModelCapability[]): void => {
        if (mounted) setModels(m);
      })
      .catch((): void => {
        if (mounted) setModels([]);
      });

    return (): void => {
      mounted = false;
    };
  }, [providerID]);

  const providerType = providers.find(
    (p: ProviderView): boolean => p.ID === providerID
  )?.Type;

  // The selected model's static-catalog window, when known — it drives the
  // auto-fill, the "Reset to auto" action, and the field's hint text (D8).
  const catalogLimit =
    providerType !== undefined && model
      ? catalogContextWindow(providerType, model)
      : null;

  // Empty list = every catalog tool enabled (the config modal's default).
  const allToolsOn = selectedTools.length === 0;

  /** The chips' pressed set: the default renders every tool pressed. */
  const toolValue = allToolsOn
    ? tools.map((t: ToolEntry): string => t.Name)
    : selectedTools;

  const handleNameChange = (value: string): void => {
    setName(value);

    if (!slugTouched) setSlug(suggestSlug(value));
  };

  const handleSlugChange = (value: string): void => {
    // Manual edits stop the auto-suggestion for the rest of the session.
    setSlugTouched(true);
    setSlug(value);
  };

  const handleProviderChange = (value: string): void => {
    setProviderID(value);
    setModel("");
    setCustomMode(false);
    setContextWindowTouched(false);
    setContextWindow("");
  };

  // A hand-edited override wins: later model picks stop auto-filling until
  // "Reset to auto" releases it.
  const fillContextFromCatalog = (modelId: string): void => {
    if (contextWindowTouched) return;

    const known =
      providerType !== undefined
        ? catalogContextWindow(providerType, modelId)
        : null;

    setContextWindow(known === null ? "" : String(known));
  };

  const handleModelChange = (value: string): void => {
    if (value === CUSTOM_MODEL_VALUE) {
      setCustomMode(true);

      return;
    }

    setCustomMode(false);
    setModel(value);
    fillContextFromCatalog(value);
  };

  const handleCustomModelChange = (value: string): void => {
    setModel(value);
    fillContextFromCatalog(value);
  };

  const resetContextWindow = (): void => {
    setContextWindowTouched(false);
    setContextWindow(catalogLimit === null ? "" : String(catalogLimit));
  };

  // A model value outside the provider's list (a past pick after the catalog
  // changed) reads as the custom entry, like onclaw-v2's ModelCombobox —
  // derived during render rather than synced by an effect.
  const modelOutsideList =
    models.length > 0 &&
    model !== "" &&
    !models.some((m: ModelCapability): boolean => m.ID === model);

  const showCustomModelInput = customMode || modelOutsideList;

  const validateIdentity = (): FieldErrors => {
    const errors: FieldErrors = {};

    if (!name.trim()) errors.name = "Agent name is required";

    if (!slug) {
      errors.slug = "A slug is required — it names this agent's workspace.";
    } else if (!isValidSlug(slug)) {
      errors.slug =
        "Slugs use lowercase letters, digits, and single interior dashes — at most 63 characters.";
    } else if (agents.some((a: Agent): boolean => a.Slug === slug)) {
      errors.slug = `Another agent already uses "${slug}" — slugs must be unique.`;
    }

    if (!brief.trim()) {
      errors.brief =
        "A brief is required — it drives prompt generation: describe this agent's goal and behavior in a sentence or two.";
    }

    return errors;
  };

  const validateModel = (): FieldErrors => {
    const errors: FieldErrors = {};

    if (!providerID)
      errors.provider = "Select a provider — prompt generation runs on it.";

    if (!model) errors.model = "Select a model.";

    const parsedTemp = parseFloat(temperature);

    if (Number.isNaN(parsedTemp) || parsedTemp < 0 || parsedTemp > 2) {
      errors.temperature = "Temperature must be between 0.0 and 2.0";
    }

    if (maxTokens.trim() && parseOptionalInt(maxTokens) === null) {
      errors.maxTokens = "Max tokens must be a positive integer";
    }

    if (contextWindow.trim() && parseOptionalInt(contextWindow) === null) {
      errors.contextWindow =
        "Context window must be a positive integer (tokens)";
    }

    return errors;
  };

  const handleNext = (): void => {
    const errors = step === 0 ? validateIdentity() : validateModel();

    if (Object.keys(errors).length > 0) {
      setFieldErrors(errors);

      return;
    }

    setFieldErrors({});
    setStep(step + 1);
  };

  const handleBack = (): void => {
    setFieldErrors({});
    setStep(step - 1);
  };

  // Creation in flight disables closing: the backend commits the row one way
  // or the other, and the wizard owns reporting either outcome.
  const requestClose = (): void => {
    if (!submitting) onClose();
  };

  const handleCreate = async (): Promise<void> => {
    const identityErrors = validateIdentity();

    const modelErrors =
      Object.keys(identityErrors).length > 0 ? {} : validateModel();

    const errors = { ...identityErrors, ...modelErrors };

    if (Object.keys(errors).length > 0) {
      setFieldErrors(errors);

      if (Object.keys(identityErrors).length > 0) setStep(0);
      else setStep(1);

      return;
    }

    setSubmitting(true);
    setSubmitError(null);

    const input: CreateAgentInput = {
      Name: name.trim(),
      Slug: slug,
      Description: description,
      Brief: brief.trim(),
      Avatar: avatar,
      ProviderID: providerID,
      Model: model,
      // No wizard step sets a default effort yet; empty is the validated
      // inherit state (the agent runs at the provider default).
      Effort: "",
      Temperature: parseFloat(temperature) || 0,
      MaxTokens: parseOptionalInt(maxTokens) ?? 0,
      ContextWindow: parseOptionalInt(contextWindow) ?? 0,
      Tools: selectedTools,
      Skills: selectedSkills,
      MCPServers: selectedMcp,
      // Nil means the column default (on) — the wizard has no subagents step.
      SubagentsEnabled: null,
    };

    try {
      const created = await client.agents.create(input);
      onCreated(created);
      onClose();

      return;
    } catch (err: unknown) {
      // The backend persists the row even when generation fails and reports
      // the failure as a rejected promise (design D4): refresh the list and
      // hand over the failed row so the grid shows the failed card instead
      // of losing the user's input.
      try {
        const list = await client.agents.list();
        const failed = list.find((a: Agent): boolean => a.Slug === slug);

        if (failed) {
          onCreated(failed);
          onClose();

          return;
        }
      } catch {
        // The refresh failed too — fall through and report the create error.
      }

      setSubmitError(
        err instanceof Error ? err.message : "Failed to create agent"
      );
    } finally {
      setSubmitting(false);
    }
  };

  const handleToolsChange = (next: string[]): void => {
    // First user change materializes the explicit list from the default:
    // OptionChips hands back the full pressed set either way.
    setSelectedTools(next);
  };

  return (
    <ModalShell
      title="Create agent"
      testId="create-agent-modal"
      wide
      onClose={requestClose}
      actions={
        submitting ? (
          // Single-spinner rule (tasks.md 4.6): the OnboardingLoader below is
          // the one in-flight indicator for the create phase — this control
          // reports the state in words only.
          <Button data-testid="btn-create-busy" disabled>
            Creating…
          </Button>
        ) : (
          <>
            {step === 0 && (
              <Button variant="ghost" onClick={requestClose}>
                Cancel
              </Button>
            )}
            {step > 0 && (
              <>
                <Button
                  variant="ghost"
                  data-testid="btn-wizard-back"
                  onClick={handleBack}
                >
                  <Icon name="arrow-left" size={14} />
                  Back
                </Button>
                {/* onclaw-v2's footer split: Back on the left edge, the
                    primary action on the right. */}
                <div className="flex-1" />
              </>
            )}
            {step < 2 ? (
              <Button data-testid="btn-wizard-next" onClick={handleNext}>
                {step === 0 ? "Continue to model" : "Continue to capabilities"}
                <Icon name="arrow-right" size={14} />
              </Button>
            ) : (
              <Button
                data-testid="btn-create-agent"
                onClick={() => void handleCreate()}
              >
                Create agent
              </Button>
            )}
          </>
        )
      }
    >
      {submitting ? (
        <OnboardingLoader name={name.trim()} />
      ) : (
        <>
          {/* onclaw-v2's numbered step indicator (design D3): visited steps
              keep the accent dot, the current one the bright label. */}
          <div
            className="mt-4 flex items-center gap-3 border-b border-linesoft pb-3"
            data-testid="wizard-steps"
          >
            {STEP_TITLES.map((title: string, index: number) => (
              <div
                key={title}
                className="flex items-center gap-2"
                data-testid={`wizard-step-${title.toLowerCase()}`}
              >
                {index > 0 && <span className="text-muted">/</span>}
                <span
                  className={`flex h-6 w-6 items-center justify-center rounded-full text-[12px] font-semibold ${
                    index <= step
                      ? "bg-accent text-accenton"
                      : "bg-[color-mix(in_oklab,var(--fg)_10%,transparent)] text-muted"
                  }`}
                >
                  {index + 1}
                </span>
                <span
                  className={`text-[13px] font-medium ${
                    index === step ? "text-fg" : "text-muted"
                  }`}
                >
                  {title}
                </span>
              </div>
            ))}
          </div>

          {submitError && (
            <div role="alert" className="mt-3">
              <Alert tone="danger" testId="create-agent-error">
                {submitError}
              </Alert>
            </div>
          )}

          {step === 0 && (
            <section
              className="glass-panel mt-4 space-y-4 p-4"
              data-testid="wizard-step-identity"
            >
              <p className="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-muted">
                Identity &amp; Persona
              </p>

              <div>
                <span className="mb-1.5 block text-[12px] font-medium text-fg2">
                  Avatar
                </span>
                <div data-testid="create-agent-avatar-preview">
                  <AvatarPicker value={avatar} onChange={setAvatar} />
                </div>
              </div>

              <FormGrid columns={2}>
                <FormField
                  id="create-agent-name"
                  label="Agent name"
                  message={fieldErrors.name}
                  invalid={fieldErrors.name !== undefined}
                >
                  <Input
                    id="create-agent-name"
                    data-testid="input-create-agent-name"
                    invalid={fieldErrors.name !== undefined}
                    value={name}
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                      handleNameChange(e.target.value)
                    }
                    placeholder="e.g. Scout"
                    autoFocus
                  />
                </FormField>
                <FormField
                  id="create-agent-slug"
                  label="Slug"
                  message={
                    fieldErrors.slug ??
                    "Suggested from the name — edit it to override. Immutable after creation."
                  }
                  invalid={fieldErrors.slug !== undefined}
                >
                  <Input
                    id="create-agent-slug"
                    data-testid="input-create-agent-slug"
                    className="font-mono"
                    invalid={fieldErrors.slug !== undefined}
                    value={slug}
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                      handleSlugChange(e.target.value)
                    }
                    placeholder="e.g. scout"
                  />
                </FormField>
              </FormGrid>

              <FormField
                id="create-agent-description"
                label={
                  <>
                    Description{" "}
                    <span className="font-normal text-muted">
                      (optional summary)
                    </span>
                  </>
                }
              >
                <Textarea
                  id="create-agent-description"
                  data-testid="input-create-agent-description"
                  rows={2}
                  value={description}
                  onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) =>
                    setDescription(e.target.value)
                  }
                  placeholder="One or two lines shown on the agent's card…"
                />
              </FormField>

              <FormField
                id="create-agent-brief"
                label={
                  <>
                    Goal &amp; behavior brief{" "}
                    <span className="font-normal text-accenttext">
                      (drives prompt generation)
                    </span>
                  </>
                }
                message={fieldErrors.brief}
                invalid={fieldErrors.brief !== undefined}
              >
                <Textarea
                  id="create-agent-brief"
                  data-testid="input-create-agent-brief"
                  rows={4}
                  invalid={fieldErrors.brief !== undefined}
                  value={brief}
                  onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) =>
                    setBrief(e.target.value)
                  }
                  placeholder="Explain what this agent does, its tone, boundaries, and how it should interact with tools and users…"
                />
              </FormField>
            </section>
          )}

          {step === 1 && (
            <section
              className="glass-panel mt-4 space-y-4 p-4"
              data-testid="wizard-step-model"
            >
              <FormField
                id="create-provider"
                label="Provider"
                message={fieldErrors.provider}
                invalid={fieldErrors.provider !== undefined}
              >
                <Select
                  id="create-provider"
                  data-testid="select-create-provider"
                  invalid={fieldErrors.provider !== undefined}
                  value={providerID}
                  onChange={(e: React.ChangeEvent<HTMLSelectElement>) =>
                    handleProviderChange(e.target.value)
                  }
                >
                  <option value="">Select a provider…</option>
                  {providers.map((p: ProviderView) => (
                    <option key={p.ID} value={p.ID}>
                      {p.Name}
                    </option>
                  ))}
                </Select>
              </FormField>

              <FormField
                id="create-model"
                label="Model"
                message={fieldErrors.model}
                invalid={fieldErrors.model !== undefined}
              >
                {models.length === 0 ? (
                  <Input
                    id="create-model"
                    data-testid="input-create-custom-model"
                    className="font-mono"
                    invalid={fieldErrors.model !== undefined}
                    value={model}
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                      handleCustomModelChange(e.target.value)
                    }
                    placeholder="Enter custom model identifier…"
                  />
                ) : (
                  <div className="space-y-2">
                    <Combobox
                      id="create-model"
                      data-testid="select-create-model"
                      options={[
                        ...models.map((m: ModelCapability) => ({
                          value: m.ID,
                          label: m.ID,
                        })),
                        {
                          value: CUSTOM_MODEL_VALUE,
                          label: "Custom model ID…",
                        },
                      ]}
                      value={customMode ? CUSTOM_MODEL_VALUE : model}
                      onChange={handleModelChange}
                      error={fieldErrors.model !== undefined}
                      placeholder="Select a model…"
                      searchPlaceholder="Search models…"
                    />
                    {showCustomModelInput && (
                      <Input
                        data-testid="input-create-custom-model"
                        className="font-mono"
                        invalid={fieldErrors.model !== undefined}
                        value={model}
                        onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                          handleCustomModelChange(e.target.value)
                        }
                        placeholder="Enter custom model identifier…"
                      />
                    )}
                  </div>
                )}
              </FormField>

              {/* onclaw-v2's bordered collapsible, not a bare toggle button. */}
              <div className="rounded-lg border border-line p-3">
                <button
                  type="button"
                  data-testid="btn-create-advanced"
                  aria-expanded={advancedOpen}
                  onClick={() => setAdvancedOpen((prev: boolean) => !prev)}
                  className="flex w-full items-center justify-between text-left text-[13px] font-medium text-fg"
                >
                  <span>Advanced model configuration</span>
                  <Icon
                    name={advancedOpen ? "up" : "down"}
                    size={16}
                    className="text-muted"
                  />
                </button>

                {advancedOpen && (
                  <div
                    className="mt-3 space-y-4 border-t border-line pt-3"
                    data-testid="wizard-step-advanced"
                  >
                    <div className="flex flex-col gap-1.5">
                      <div className="flex items-baseline justify-between">
                        <label
                          htmlFor="create-temperature"
                          className="text-[12px] font-medium text-fg2"
                        >
                          Temperature
                        </label>
                        <span className="font-mono text-[12px] text-fg2">
                          {Number(temperature).toFixed(2)}
                        </span>
                      </div>
                      <input
                        id="create-temperature"
                        data-testid="input-create-temperature"
                        type="range"
                        min="0"
                        max="2"
                        step="0.05"
                        value={temperature}
                        onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                          setTemperature(e.target.value)
                        }
                        className="h-1.5 w-full cursor-pointer"
                      />
                      <div className="flex justify-between font-mono text-[10px] text-muted">
                        <span>0.0 (precise)</span>
                        <span>1.0 (standard)</span>
                        <span>2.0 (creative)</span>
                      </div>
                      {fieldErrors.temperature && (
                        <p
                          className="text-xs text-dangertext"
                          role="alert"
                          data-testid="field-error-create-temperature"
                        >
                          {fieldErrors.temperature}
                        </p>
                      )}
                    </div>

                    <FormField
                      id="create-max-tokens"
                      label={
                        <>
                          Max tokens{" "}
                          <span className="font-normal text-muted">
                            (optional limit)
                          </span>
                        </>
                      }
                      message={fieldErrors.maxTokens}
                      invalid={fieldErrors.maxTokens !== undefined}
                    >
                      <Input
                        id="create-max-tokens"
                        data-testid="input-create-max-tokens"
                        type="number"
                        min="1"
                        placeholder="e.g. 4096"
                        invalid={fieldErrors.maxTokens !== undefined}
                        value={maxTokens}
                        onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                          setMaxTokens(e.target.value)
                        }
                      />
                    </FormField>

                    <div className="flex flex-col gap-1.5">
                      <div className="flex items-center justify-between gap-2">
                        <label
                          htmlFor="create-context-window"
                          className="text-[12px] font-medium text-fg2"
                        >
                          Context window
                        </label>
                        <button
                          type="button"
                          data-testid="btn-reset-context-window"
                          onClick={resetContextWindow}
                          className="text-[11px] font-medium text-accent transition-colors hover:text-[var(--accent-hover)]"
                        >
                          Reset to auto
                        </button>
                      </div>
                      <div className="relative">
                        <Input
                          id="create-context-window"
                          data-testid="input-create-context-window"
                          type="number"
                          min="1"
                          step="1"
                          className={contextWindow ? "pr-14" : undefined}
                          invalid={fieldErrors.contextWindow !== undefined}
                          placeholder={
                            catalogLimit === null
                              ? "Auto (model default)"
                              : String(catalogLimit)
                          }
                          value={contextWindow}
                          onChange={(
                            e: React.ChangeEvent<HTMLInputElement>
                          ) => {
                            setContextWindowTouched(true);
                            setContextWindow(e.target.value);
                          }}
                        />
                        {contextWindow && (
                          <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center font-mono text-[11px] text-muted">
                            tokens
                          </span>
                        )}
                      </div>
                      <p className="text-[11px] leading-4 text-muted">
                        {contextWindowTouched || contextWindow
                          ? "Leave empty to fall back to the model's default window."
                          : `Auto-fills from the model's context limit${
                              catalogLimit === null
                                ? " when known"
                                : ` (${catalogLimit.toLocaleString()} tokens)`
                            }.`}
                      </p>
                      {fieldErrors.contextWindow && (
                        <p
                          className="text-xs text-dangertext"
                          role="alert"
                          data-testid="field-error-create-context-window"
                        >
                          {fieldErrors.contextWindow}
                        </p>
                      )}
                    </div>
                  </div>
                )}
              </div>
            </section>
          )}

          {step === 2 && (
            <section
              className="glass-panel mt-4 space-y-4 p-4"
              data-testid="wizard-step-capabilities"
            >
              <MicroLabel>Capabilities</MicroLabel>

              <div data-testid="create-section-tools">
                <h4 className="text-[12px] font-medium text-fg2">
                  Built-in Tools
                </h4>
                <p className="mt-0.5 text-xs text-muted">
                  {allToolsOn
                    ? "All catalog tools are enabled (default). Deselecting any tool switches to an explicit selection."
                    : "Explicit selection — only the pressed tools below are enabled for this agent."}
                </p>
                <OptionChips
                  testIdPrefix="create-tool"
                  options={tools.map((t: ToolEntry) => ({
                    id: t.Name,
                    label: t.DisplayName || t.Name,
                    detail: t.Description,
                  }))}
                  value={toolValue}
                  onChange={handleToolsChange}
                />
              </div>

              <div data-testid="create-section-skills">
                <h4 className="text-[12px] font-medium text-fg2">Skills</h4>
                {skills.length === 0 ? (
                  <p className="mt-1 text-sm text-muted">
                    No skills in the registry.
                  </p>
                ) : (
                  <OptionChips
                    testIdPrefix="create-skill"
                    disabledTitle="Disabled in Settings"
                    options={skills.map((s: Skill) => ({
                      id: s.Name,
                      label: s.Name,
                      detail: s.Description,
                    }))}
                    value={selectedSkills}
                    onChange={setSelectedSkills}
                    disabledOf={(o: OptionChipOption) =>
                      skills.find((s: Skill) => s.Name === o.id)?.Enabled ===
                      false
                    }
                  />
                )}
              </div>

              <div data-testid="create-section-mcp">
                <h4 className="text-[12px] font-medium text-fg2">
                  MCP Servers
                </h4>
                {mcpServers.length === 0 ? (
                  <p className="mt-1 text-sm text-muted">
                    No MCP servers configured.
                  </p>
                ) : (
                  <>
                    <p className="mt-0.5 text-xs text-muted">
                      {selectedMcp.length === 0
                        ? "Every enabled server is available (default). Turning any server off switches to an explicit selection."
                        : "Explicit selection — only the servers switched on below are available to this agent."}
                    </p>
                    <McpServerList
                      servers={mcpServers}
                      selected={selectedMcp}
                      onChange={setSelectedMcp}
                      testIdPrefix="create-mcp"
                    />
                  </>
                )}
              </div>
            </section>
          )}
        </>
      )}
    </ModalShell>
  );
}
