/**
 * WebhookWizard (add-webhook-triggers tasks 8.2/8.3): one add/edit wizard
 * over the CreateAgentModal stepper pattern — Basics (name, agent-or-group
 * target, prompt template), Credentials (the preset gallery: GitLab,
 * GitHub, Stripe, Slack, Zoom, Shopify, Linear, Header as one-secret-field
 * cards seeded from the service's catalog, plus a Custom/manual card over
 * the VerifierEditor), and Finish (the generated delivery URL with a copy
 * affordance; secrets typed or generated this session shown ONCE with the
 * "won't be shown again" note, design D6).
 *
 * Edit flow: drafts seed from the endpoint's rows, so its preset identity
 * is preserved (a single preset row re-selects that gallery card; anything
 * else opens the manual editor); blank secret fields keep the stored
 * secrets (the backend's blank-keeps at row position).
 */
import { useEffect, useState } from "react";
import type { ChangeEvent, ReactElement } from "react";

import type {
  Agent,
  AgentGroup,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type {
  WebhookEndpointInput,
  WebhookEndpointView,
  WebhookPresetView,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import { FormField } from "../../../../components";
import { ModalShell } from "../../../../components";
import { Alert } from "../../../../components/ui/Alert";
import { Button } from "../../../../components/ui/Button";
import { Chip } from "../../../../components/ui/Chip";
import { Icon } from "../../../../components/ui/Icon";
import { Input } from "../../../../components/ui/Input";
import { Select } from "../../../../components/ui/Select";
import { Textarea } from "../../../../components/ui/Textarea";
import { client } from "../../../../lib/client";
import { VerifierEditor, verifierSummary } from "./VerifierEditor";
import {
  copyText,
  draftFromView,
  draftToInput,
  draftsFromPreset,
  emptyDraft,
  presetIdentity,
  randomSecret,
  type VerifierDraft,
} from "./shared";

/** The wizard steps in order. */
const STEP_TITLES: readonly string[] = ["Basics", "Credentials", "Finish"];

/** Stable gallery id of the manual builder card. */
const CUSTOM_CARD = "custom";

export interface WebhookWizardProps {
  /** The endpoint being edited; null creates a new one. */
  endpoint: WebhookEndpointView | null;
  onClose: () => void;
  /** Called with the saved view so the section refetches mid-flow. */
  onSaved: (view: WebhookEndpointView) => void;
}

type FieldErrors = Partial<
  Record<"name" | "target" | "prompt" | "verifiers", string>
>;

/** One readonly mono value with a copy affordance (the URL, a shown secret). */
function CopyRow({
  label,
  value,
  testId,
  copyTestId,
  note,
}: {
  label: string;
  value: string;
  testId: string;
  copyTestId: string;
  note?: string;
}): ReactElement {
  const [copied, setCopied] = useState(false);

  const copy = async (): Promise<void> => {
    if (await copyText(value)) {
      setCopied(true);
    }
  };

  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center justify-between gap-2">
        <span className="text-[12px] font-medium text-fg2">{label}</span>
        <Button
          variant="ghost"
          size="sm"
          data-testid={copyTestId}
          onClick={(): void => {
            void copy();
          }}
        >
          <Icon name="copy" size={14} />
          {copied ? "Copied" : "Copy"}
        </Button>
      </div>
      <p
        className="break-all rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] p-2.5 font-mono text-[12px] text-fg"
        data-testid={testId}
      >
        {value}
      </p>
      {note && <p className="text-[11px] text-muted">{note}</p>}
    </div>
  );
}

/**
 * WebhookWizard: the three-step add/edit surface. The save validates
 * Basics and the ≥1-credential rule, submits WebhookEndpointInput (a blank
 * secret keeps the stored one, backend-side), and flips to Finish with the
 * saved view's URL; secrets entered this session render exactly once there.
 */
export function WebhookWizard({
  endpoint,
  onClose,
  onSaved,
}: WebhookWizardProps): ReactElement {
  const [step, setStep] = useState(0);

  // Basics.
  const [name, setName] = useState(endpoint?.name ?? "");
  const [targetValue, setTargetValue] = useState((): string => {
    if (endpoint === null) return "";
    if (endpoint.agentId !== "") return `agent:${endpoint.agentId}`;

    return endpoint.groupId === "" ? "" : `group:${endpoint.groupId}`;
  });
  const [prompt, setPrompt] = useState(endpoint?.prompt ?? "");
  const [agents, setAgents] = useState<Agent[]>([]);
  const [groups, setGroups] = useState<AgentGroup[]>([]);
  const [presets, setPresets] = useState<WebhookPresetView[]>([]);

  // Credentials: the selected gallery card and the editable verifier rows.
  const [selected, setSelected] = useState((): string => {
    const rows = endpoint?.verifiers ?? [];

    if (rows.length === 0) return "";
    if (rows.length === 1 && rows[0].preset !== "") return rows[0].preset;

    return CUSTOM_CARD;
  });
  const [drafts, setDrafts] = useState<VerifierDraft[]>((): VerifierDraft[] =>
    (endpoint?.verifiers ?? []).map((row): VerifierDraft => draftFromView(row))
  );

  // The endpoint's own preset identity, pinned at open (the round-trip
  // contract: editing must keep showing it).
  const [endpointPreset] = useState((): string => {
    const rows = endpoint?.verifiers ?? [];

    if (rows.length === 1 && rows[0].preset !== "") return rows[0].preset;
    if (rows.length > 0) return CUSTOM_CARD;

    return "";
  });

  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});
  const [serverError, setServerError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  // Finish: the saved view and the secrets this session produced (each
  // rendered exactly once — design D6).
  const [result, setResult] = useState<WebhookEndpointView | null>(null);
  const [shownSecrets, setShownSecrets] = useState<string[]>([]);

  useEffect((): (() => void) => {
    let mounted = true;

    void client.agents.list().then((rows: Agent[]): void => {
      if (mounted) setAgents(rows);
    });
    void client.groups.list().then((rows: AgentGroup[]): void => {
      if (mounted) setGroups(rows);
    });
    void client.webhooks.presets().then((rows: WebhookPresetView[]): void => {
      if (mounted) setPresets(rows ?? []);
    });

    return (): void => {
      mounted = false;
    };
  }, []);

  const [targetKind, targetID] =
    targetValue === "" ? ["", ""] : targetValue.split(":");

  /** Selects a gallery card, seeding (or re-seeding) the verifier rows. */
  const selectCard = (card: string, preset: WebhookPresetView | null): void => {
    if (selected === card) return;

    setSelected(card);

    if (preset !== null) {
      setDrafts(draftsFromPreset(preset));
    } else {
      // Custom/manual: the editor starts from one blank row (re-selecting
      // the already-selected card returns above, keeping hand-built rows).
      setDrafts([emptyDraft()]);
    }
  };

  const validateBasics = (): FieldErrors => {
    const errors: FieldErrors = {};

    if (!name.trim()) errors.name = "An endpoint name is required.";
    if (targetValue === "")
      errors.target = "Pick the agent or group this endpoint triggers.";
    if (!prompt.trim())
      errors.prompt =
        "A prompt template is required — the delivery's payload rides it as a fenced block.";

    return errors;
  };

  const handleNext = (): void => {
    const errors = validateBasics();

    if (Object.keys(errors).length > 0) {
      setFieldErrors(errors);

      return;
    }

    setFieldErrors({});
    setStep(1);
  };

  const handleBack = (): void => {
    setFieldErrors({});
    setStep(step - 1);
  };

  const requestClose = (): void => {
    if (!saving) onClose();
  };

  const handleSave = async (): Promise<void> => {
    const errors: FieldErrors = validateBasics();

    if (drafts.length === 0) {
      errors.verifiers =
        "At least one verifier is required — pick a preset card or add one under Custom.";
    }

    if (Object.keys(errors).length > 0) {
      setFieldErrors(errors);

      if (
        errors.name !== undefined ||
        errors.target !== undefined ||
        errors.prompt !== undefined
      )
        setStep(0);

      return;
    }

    setSaving(true);
    setServerError(null);

    const input: WebhookEndpointInput = {
      name: name.trim(),
      agentId: targetKind === "agent" ? targetID : "",
      groupId: targetKind === "group" ? targetID : "",
      prompt: prompt.trim(),
      enabled: endpoint?.enabled ?? true,
      verifiers: drafts.map((draft: VerifierDraft) => draftToInput(draft)),
    };

    try {
      const saved =
        endpoint !== null
          ? await client.webhooks.update(endpoint.id, input)
          : await client.webhooks.create(input);

      // Secrets typed or generated this session, in row order: shown once.
      setShownSecrets(
        drafts
          .map((draft: VerifierDraft): string => draft.secret)
          .filter((secret: string): boolean => secret !== "")
      );
      setResult(saved);
      onSaved(saved);
      setStep(2);
    } catch (err: unknown) {
      setServerError(
        err instanceof Error ? err.message : "Failed to save the endpoint"
      );
    } finally {
      setSaving(false);
    }
  };

  const selectedPreset =
    selected === CUSTOM_CARD || selected === ""
      ? null
      : (presets.find((p: WebhookPresetView): boolean => p.name === selected) ??
        null);

  const selectedDraft = drafts.length > 0 ? drafts[0] : null;

  const patchFirst = (patch: Partial<VerifierDraft>): void => {
    setDrafts(
      drafts.map((row: VerifierDraft, index: number): VerifierDraft =>
        index === 0 ? { ...row, ...patch } : row
      )
    );
  };

  return (
    <ModalShell
      title={
        endpoint !== null ? "Edit webhook endpoint" : "Add webhook endpoint"
      }
      testId="webhook-wizard"
      wide
      onClose={requestClose}
      actions={
        saving ? (
          <Button data-testid="btn-webhook-busy" disabled>
            Saving…
          </Button>
        ) : (
          <>
            {step === 0 && (
              <Button variant="ghost" onClick={requestClose}>
                Cancel
              </Button>
            )}
            {step > 0 && step < 2 && (
              <>
                <Button
                  variant="ghost"
                  data-testid="btn-wizard-back"
                  onClick={handleBack}
                >
                  <Icon name="arrow-left" size={14} />
                  Back
                </Button>
                <div className="flex-1" />
              </>
            )}
            {step === 0 && (
              <Button data-testid="btn-wizard-next" onClick={handleNext}>
                Continue to credentials
                <Icon name="arrow-right" size={14} />
              </Button>
            )}
            {step === 1 && (
              <Button
                data-testid="btn-save-webhook"
                loading={saving}
                onClick={(): void => {
                  void handleSave();
                }}
              >
                {endpoint !== null ? "Save changes" : "Create endpoint"}
              </Button>
            )}
            {step === 2 && (
              <Button data-testid="btn-webhook-done" onClick={onClose}>
                Done
              </Button>
            )}
          </>
        )
      }
    >
      {/* The CreateAgentModal numbered step indicator. */}
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

      {serverError && (
        <div role="alert" className="mt-3">
          <Alert tone="danger" testId="webhook-wizard-error">
            {serverError}
          </Alert>
        </div>
      )}

      {step === 0 && (
        <section
          className="glass-panel mt-4 flex flex-col gap-4 p-4"
          data-testid="webhook-step-basics"
        >
          <FormField
            id="webhook-name"
            label="Name"
            message={fieldErrors.name}
            invalid={fieldErrors.name !== undefined}
            messageTestId="field-error-name"
          >
            <Input
              id="webhook-name"
              data-testid="field-webhook-name"
              invalid={fieldErrors.name !== undefined}
              value={name}
              placeholder="e.g. GitLab deploys"
              autoFocus
              onChange={(e: ChangeEvent<HTMLInputElement>): void => {
                setName(e.target.value);
                setFieldErrors((prev: FieldErrors): FieldErrors => {
                  const { name: _name, ...rest } = prev;

                  return rest;
                });
              }}
            />
          </FormField>

          <FormField
            id="webhook-target"
            label="Target"
            message={fieldErrors.target}
            invalid={fieldErrors.target !== undefined}
            messageTestId="field-error-target"
          >
            <Select
              id="webhook-target"
              data-testid="field-webhook-target"
              invalid={fieldErrors.target !== undefined}
              value={targetValue}
              onChange={(e: ChangeEvent<HTMLSelectElement>): void => {
                setTargetValue(e.target.value);
                setFieldErrors((prev: FieldErrors): FieldErrors => {
                  const { target: _target, ...rest } = prev;

                  return rest;
                });
              }}
            >
              <option value="">Select an agent or group…</option>
              {agents.length > 0 && (
                <optgroup label="Agents">
                  {agents.map((agent: Agent) => (
                    <option key={agent.ID} value={`agent:${agent.ID}`}>
                      {agent.Name}
                    </option>
                  ))}
                </optgroup>
              )}
              {groups.length > 0 && (
                <optgroup label="Groups">
                  {groups.map((group: AgentGroup) => (
                    <option key={group.ID} value={`group:${group.ID}`}>
                      {group.Title !== "" ? group.Title : group.Name}
                    </option>
                  ))}
                </optgroup>
              )}
            </Select>
          </FormField>

          <FormField
            id="webhook-prompt"
            label="Prompt template"
            message={
              fieldErrors.prompt ??
              "The delivery's payload rides this prompt as a fenced JSON (or raw-text) block. For a group target, @Name mentions route to that member."
            }
            invalid={fieldErrors.prompt !== undefined}
            messageTestId="field-error-prompt"
          >
            <Textarea
              id="webhook-prompt"
              data-testid="field-webhook-prompt"
              rows={4}
              invalid={fieldErrors.prompt !== undefined}
              value={prompt}
              placeholder={
                endpoint !== null && endpoint.groupId !== ""
                  ? "e.g. @Lead a webhook came in — triage it:\n<payload>"
                  : "e.g. Summarize this build result and list the failures:\n<payload>"
              }
              onChange={(e: ChangeEvent<HTMLTextAreaElement>): void => {
                setPrompt(e.target.value);
                setFieldErrors((prev: FieldErrors): FieldErrors => {
                  const { prompt: _prompt, ...rest } = prev;

                  return rest;
                });
              }}
            />
          </FormField>
        </section>
      )}

      {step === 1 && (
        <section
          className="glass-panel mt-4 flex flex-col gap-4 p-4"
          data-testid="webhook-step-credentials"
        >
          {endpoint !== null && endpoint.url !== "" && (
            <CopyRow
              label="Delivery URL"
              value={endpoint.url}
              testId="webhook-url"
              copyTestId="webhook-url-copy"
              note="Paste this into the sender's webhook configuration."
            />
          )}

          <div>
            <p className="text-[12px] font-medium text-fg2">
              How should deliveries authenticate?
            </p>
            <div
              className="mt-2 grid gap-2 sm:grid-cols-2"
              data-testid="preset-gallery"
            >
              {presets.map((preset: WebhookPresetView) => {
                const isSelected = selected === preset.name;

                return (
                  <div
                    key={preset.name}
                    className={`rounded-md border p-3 transition-colors ${
                      isSelected
                        ? "border-accent bg-[color-mix(in_oklab,var(--accent)_6%,transparent)]"
                        : "border-line hover:border-[color-mix(in_oklab,var(--fg)_25%,var(--border))]"
                    }`}
                    data-testid={`preset-card-${preset.name}`}
                  >
                    <button
                      type="button"
                      className="flex w-full items-center gap-2 text-left"
                      data-testid={`preset-card-select-${preset.name}`}
                      onClick={(): void => selectCard(preset.name, preset)}
                    >
                      <span
                        className={`text-[13px] font-semibold ${
                          isSelected ? "text-fg" : "text-fg2"
                        }`}
                      >
                        {preset.title}
                      </span>
                      {isSelected && endpointPreset === preset.name && (
                        <Chip
                          data-testid={`preset-card-current-${preset.name}`}
                        >
                          current preset
                        </Chip>
                      )}
                      {isSelected && (
                        <Icon
                          name="check"
                          size={14}
                          className="ml-auto text-accenttext"
                        />
                      )}
                    </button>

                    {isSelected &&
                      selectedPreset !== null &&
                      selectedDraft !== null && (
                        <div className="mt-2 flex flex-col gap-2">
                          <p className="font-mono text-[11px] text-muted">
                            {verifierSummary(selectedDraft)}
                          </p>
                          <div className="flex items-center gap-2">
                            <Input
                              type="password"
                              autoComplete="off"
                              className="flex-1 font-mono text-[13px]"
                              data-testid={`preset-secret-${preset.name}`}
                              aria-label={`${preset.title} signing secret`}
                              placeholder={
                                selectedDraft.storedSet
                                  ? `Stored ••••${selectedDraft.storedHint} — leave blank to keep`
                                  : "Secret"
                              }
                              value={selectedDraft.secret}
                              onChange={(
                                e: ChangeEvent<HTMLInputElement>
                              ): void => patchFirst({ secret: e.target.value })}
                            />
                            <Button
                              variant="ghost"
                              size="sm"
                              data-testid={`preset-generate-${preset.name}`}
                              title="Generate a fresh secret — shown once after saving"
                              onClick={(): void =>
                                patchFirst({ secret: randomSecret() })
                              }
                            >
                              <Icon name="rotate-ccw" size={14} />
                              {selectedDraft.storedSet
                                ? "Regenerate"
                                : "Generate"}
                            </Button>
                          </div>
                        </div>
                      )}

                    <p
                      className="mt-2 text-[11px] leading-4 text-muted"
                      data-testid={`preset-hint-${preset.name}`}
                    >
                      {preset.hint}
                    </p>
                  </div>
                );
              })}

              {/* The manual builder card over the five raw engines. */}
              <div
                className={`rounded-md border p-3 transition-colors ${
                  selected === CUSTOM_CARD
                    ? "border-accent bg-[color-mix(in_oklab,var(--accent)_6%,transparent)]"
                    : "border-line hover:border-[color-mix(in_oklab,var(--fg)_25%,var(--border))]"
                }`}
                data-testid="preset-card-custom"
              >
                <button
                  type="button"
                  className="flex w-full items-center gap-2 text-left"
                  data-testid="preset-card-select-custom"
                  onClick={(): void => selectCard(CUSTOM_CARD, null)}
                >
                  <span
                    className={`text-[13px] font-semibold ${
                      selected === CUSTOM_CARD ? "text-fg" : "text-fg2"
                    }`}
                  >
                    Custom / manual
                  </span>
                  {selected === CUSTOM_CARD &&
                    endpointPreset === CUSTOM_CARD && (
                      <Chip data-testid="preset-card-current-custom">
                        current preset
                      </Chip>
                    )}
                  {selected === CUSTOM_CARD && (
                    <Icon
                      name="check"
                      size={14}
                      className="ml-auto text-accenttext"
                    />
                  )}
                </button>
                <p className="mt-2 text-[11px] leading-4 text-muted">
                  Build the verifier list by hand — path token, header equality,
                  raw-body HMAC, Standard Webhooks, or timestamp HMAC.
                </p>
              </div>
            </div>
          </div>

          {selected === CUSTOM_CARD && (
            <div className="border-t border-linesoft pt-3">
              <VerifierEditor
                drafts={drafts}
                onChange={setDrafts}
                testIdPrefix="verifier-editor"
              />
            </div>
          )}

          {fieldErrors.verifiers !== undefined && (
            <p
              className="text-xs text-dangertext"
              role="alert"
              data-testid="field-error-verifiers"
            >
              {fieldErrors.verifiers}
            </p>
          )}

          {/* Round-trip anchor: what this endpoint authenticates with, even
              before any change (spec: the edit still shows preset identity). */}
          {endpoint !== null && drafts.length > 0 && (
            <p
              className="text-[11px] text-muted"
              data-testid="webhook-identity"
            >
              Credential: {presetIdentity(drafts)}
            </p>
          )}
        </section>
      )}

      {step === 2 && result !== null && (
        <section
          className="glass-panel mt-4 flex flex-col gap-4 p-4"
          data-testid="webhook-step-finish"
        >
          <p className="text-[13px] text-fg2">
            {endpoint !== null ? "Endpoint saved." : "Endpoint created."} Point
            the sender at the URL below
            {shownSecrets.length > 0
              ? " with the secret shown here."
              : " — its stored secret still authenticates."}
          </p>

          <CopyRow
            label="Delivery URL"
            value={result.url}
            testId="webhook-url"
            copyTestId="webhook-url-copy"
            note="The URL carries the path-token segment when this endpoint verifies one."
          />

          {shownSecrets.map((secret: string, index: number) => (
            <CopyRow
              key={`${result.id}-secret-${index}`}
              label={`Verifier ${index + 1} secret`}
              value={secret}
              testId={`shown-secret-${index}`}
              copyTestId={`shown-secret-copy-${index}`}
              note="Won't be shown again — copy it into the sender now."
            />
          ))}

          <p className="text-[12px] text-muted">
            Use Test on the endpoint's row to prove the whole path — binding,
            authentication, and dispatch — in one step.
          </p>
        </section>
      )}
    </ModalShell>
  );
}
