import { useEffect, useRef, useState } from "react";
import type {
  ChangeEvent,
  KeyboardEvent as ReactKeyboardEvent,
  MouseEvent as ReactMouseEvent,
  ReactElement,
} from "react";

import type { Agent } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type {
  ProviderInput,
  ProviderModelInput,
  ProviderModelView,
  ProviderView,
  VerifyResult,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import {
  ConfirmDialog,
  EmptyState,
  FormField,
  FormGrid,
  LoadingHint,
} from "../../../components";
import { Alert } from "../../../components/ui/Alert";
import { Button } from "../../../components/ui/Button";
import { Checkbox } from "../../../components/ui/Checkbox";
import { Chip } from "../../../components/ui/Chip";
import { Icon } from "../../../components/ui/Icon";
import { Input } from "../../../components/ui/Input";
import { Select } from "../../../components/ui/Select";
import { StatusDot } from "../../../components/ui/StatusDot";
import { Toggle } from "../../../components/ui/Toggle";
import { BrandMark } from "../../../lib/brandMarks";
import { client } from "../../../lib/client";
import { cn } from "../../../lib/cn";
import { formatTokens } from "../../chat/format";
import {
  DEFAULT_PROVIDER_TYPE,
  PROVIDER_CATALOG,
  isCanonicalProviderType,
  providerCatalogEntry,
  providerTypeLabel,
  type ProviderTypeMeta,
} from "../catalog";
import type { SettingsPaneConfig } from "../types";

/** Outcome of row verification against a saved provider. */
interface VerifyOutcome {
  providerID: string;
  result?: VerifyResult;
  error?: string;
}

/** The ProviderInput for one row toggle: every current field with Enabled
 * flipped and a blank APIKey so the stored key survives (design D2). */
function toggleInput(provider: ProviderView): ProviderInput {
  return {
    Type: provider.Type,
    Name: provider.Name,
    BaseURL: provider.BaseURL,
    APIKey: "",
    Enabled: !provider.Enabled,
  };
}

/** How long the inline delete confirmation stays armed before reverting to
 * the quiet × button (onclaw-v2's two-step delete). */
const DELETE_ARM_MS = 4000;

/** Placeholder for the write-only key field: the stored-key hint when
 * editing, a plain prompt when adding or nothing is stored. */
function keyPlaceholder(
  isAddMode: boolean,
  provider: ProviderView | null
): string {
  if (isAddMode) return "API key";

  if (!provider?.KeySet) return "API key (none stored)";

  return provider.KeyHint
    ? `Leave blank to keep (••••${provider.KeyHint})`
    : "Leave blank to keep the stored key";
}

const verifyText = (result: VerifyResult): string =>
  result.OK ? "Connection verified." : result.Detail || "Verification failed.";

export default function ProvidersPane(_props?: {
  pane?: SettingsPaneConfig;
}): ReactElement {
  const [providers, setProviders] = useState<ProviderView[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [isAddMode, setIsAddMode] = useState(false);

  // Provider configuration form draft state
  const [draftName, setDraftName] = useState("");
  const [draftType, setDraftType] = useState(DEFAULT_PROVIDER_TYPE);
  const [draftBaseURL, setDraftBaseURL] = useState("");
  const [draftApiKey, setDraftApiKey] = useState("");
  const [revealKey, setRevealKey] = useState(false);
  const [submitAttempted, setSubmitAttempted] = useState(false);
  const [savingProvider, setSavingProvider] = useState(false);

  // Draft verification state
  const [verifyingDraft, setVerifyingDraft] = useState(false);

  const [draftVerifyResult, setDraftVerifyResult] =
    useState<VerifyResult | null>(null);

  const [draftVerifyError, setDraftVerifyError] = useState<string | null>(null);

  // Saved provider verification state
  const [verifyingSaved, setVerifyingSaved] = useState<string | null>(null);

  const [savedVerifyOutcome, setSavedVerifyOutcome] =
    useState<VerifyOutcome | null>(null);

  // Provider toggle & delete states
  const [togglingProvider, setTogglingProvider] = useState<string | null>(null);
  const [confirmDeleteID, setConfirmDeleteID] = useState<string | null>(null);
  const deleteArmTimer = useRef<number | null>(null);
  const [removing, setRemoving] = useState<ProviderView | null>(null);
  const [usedBy, setUsedBy] = useState<Agent[]>([]);

  // Models section state
  const [models, setModels] = useState<ProviderModelView[]>([]);
  const [loadingModels, setLoadingModels] = useState(false);
  const [modelsError, setModelsError] = useState<string | null>(null);

  // Model add/edit form state
  const [modelForm, setModelForm] = useState<{
    mode: "add" | "edit";
    model?: ProviderModelView;
  } | null>(null);

  const [modelFormID, setModelFormID] = useState("");
  const [modelFormContext, setModelFormContext] = useState<number>(128000);
  const [modelFormImage, setModelFormImage] = useState(false);
  const [modelFormPDF, setModelFormPDF] = useState(false);
  const [modelFormVideo, setModelFormVideo] = useState(false);
  const [modelFormEfforts, setModelFormEfforts] = useState<string[]>([]);
  const [modelFormError, setModelFormError] = useState<string | null>(null);
  const [savingModel, setSavingModel] = useState(false);
  const [togglingModelID, setTogglingModelID] = useState<number | null>(null);
  const [deletingModelID, setDeletingModelID] = useState<number | null>(null);

  const selectedProvider =
    !isAddMode && selectedID
      ? (providers.find((p: ProviderView): boolean => p.ID === selectedID) ??
        null)
      : null;

  useEffect(
    () => (): void => {
      if (deleteArmTimer.current !== null) {
        window.clearTimeout(deleteArmTimer.current);
      }
    },
    []
  );

  const armDelete = (provider: ProviderView): void => {
    setConfirmDeleteID(provider.ID);

    if (deleteArmTimer.current !== null) {
      window.clearTimeout(deleteArmTimer.current);
    }

    deleteArmTimer.current = window.setTimeout((): void => {
      setConfirmDeleteID(null);
      deleteArmTimer.current = null;
    }, DELETE_ARM_MS);
  };

  const load = async (): Promise<void> => {
    setLoading(true);
    setLoadError(null);

    try {
      const list = await client.settings.providers();
      setProviders(list);

      if (!isAddMode) {
        setSelectedID((prev: string | null): string | null => {
          if (prev && list.some((p: ProviderView): boolean => p.ID === prev)) {
            return prev;
          }

          return list.length > 0 ? list[0].ID : null;
        });
      }
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const loadModels = async (providerID: string): Promise<void> => {
    setLoadingModels(true);
    setModelsError(null);

    try {
      const list = await client.settings.listProviderModels(providerID);
      setModels(list);
    } catch (err) {
      setModelsError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoadingModels(false);
    }
  };

  // Sync draft form and load models when selected provider changes
  useEffect(() => {
    if (isAddMode) return;

    if (selectedProvider) {
      setDraftName(selectedProvider.Name);
      setDraftType(selectedProvider.Type);
      setDraftBaseURL(selectedProvider.BaseURL);
      setDraftApiKey("");
      setRevealKey(false);
      setSubmitAttempted(false);
      setDraftVerifyResult(null);
      setDraftVerifyError(null);
      setSavedVerifyOutcome(null);
      setModelForm(null);
      void loadModels(selectedProvider.ID);
    } else {
      setModels([]);
    }
  }, [selectedProvider?.ID, isAddMode]);

  const openAdd = (): void => {
    setIsAddMode(true);
    setSelectedID(null);
    setDraftName("");
    setDraftType(DEFAULT_PROVIDER_TYPE);
    setDraftBaseURL("");
    setDraftApiKey("");
    setRevealKey(false);
    setSubmitAttempted(false);
    setDraftVerifyResult(null);
    setDraftVerifyError(null);
    setSavedVerifyOutcome(null);
    setModelForm(null);
    setModels([]);
  };

  const cancelAdd = (): void => {
    setIsAddMode(false);

    if (providers.length > 0) {
      setSelectedID(providers[0].ID);
    }
  };

  const selectProvider = (id: string): void => {
    setIsAddMode(false);
    setSelectedID(id);
  };

  // Type catalog entry and Base URL validation
  const currentType = isAddMode
    ? draftType
    : (selectedProvider?.Type ?? draftType);

  const entry = providerCatalogEntry(currentType);

  const baseURLMissing =
    (entry?.requiresBaseUrl ?? false) && draftBaseURL.trim() === "";

  const baseURLMessage =
    baseURLMissing && submitAttempted
      ? `Base URL is required for ${entry?.label ?? currentType}`
      : undefined;

  const buildInput = (): ProviderInput => ({
    Type: isAddMode ? draftType : (selectedProvider?.Type ?? draftType),
    Name: draftName,
    BaseURL: draftBaseURL,
    APIKey: draftApiKey,
    Enabled: isAddMode ? true : (selectedProvider?.Enabled ?? true),
  });

  const saveProvider = async (): Promise<void> => {
    if (baseURLMissing) {
      setSubmitAttempted(true);

      return;
    }

    setSavingProvider(true);

    try {
      if (isAddMode) {
        const created = await client.settings.createProvider(buildInput());
        await load();
        setIsAddMode(false);
        setSelectedID(created.ID);
      } else if (selectedProvider) {
        await client.settings.updateProvider(selectedProvider.ID, buildInput());
        setDraftApiKey("");
        await load();
      }
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err));
    } finally {
      setSavingProvider(false);
    }
  };

  const verifyDraft = async (): Promise<void> => {
    setVerifyingDraft(true);
    setDraftVerifyResult(null);
    setDraftVerifyError(null);

    try {
      const res = await client.settings.verifyProviderDraft(
        buildInput(),
        isAddMode ? "" : (selectedProvider?.ID ?? "")
      );

      setDraftVerifyResult(res);
    } catch (err) {
      setDraftVerifyError(err instanceof Error ? err.message : String(err));
    } finally {
      setVerifyingDraft(false);
    }
  };

  const verifySaved = async (provider: ProviderView): Promise<void> => {
    setVerifyingSaved(provider.ID);
    setSavedVerifyOutcome(null);

    try {
      const result = await client.settings.verifyProvider(provider.ID);
      setSavedVerifyOutcome({ providerID: provider.ID, result });
    } catch (err) {
      setSavedVerifyOutcome({
        providerID: provider.ID,
        error: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setVerifyingSaved(null);
    }
  };

  const toggleEnabled = async (provider: ProviderView): Promise<void> => {
    setTogglingProvider(provider.ID);

    try {
      await client.settings.updateProvider(provider.ID, toggleInput(provider));
      await load();
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err));
    } finally {
      setTogglingProvider(null);
    }
  };

  const requestRemove = async (provider: ProviderView): Promise<void> => {
    let users: Agent[] = [];

    try {
      const agents = await client.agents.list();
      users = agents.filter(
        (a: Agent): boolean => a.ProviderID === provider.ID
      );
    } catch {
      users = [];
    }

    if (users.length > 0) {
      setUsedBy(users);
      setRemoving(provider);

      return;
    }

    armDelete(provider);
  };

  const confirmRemove = async (): Promise<void> => {
    if (!removing) return;
    const removedID = removing.ID;
    setRemoving(null);
    await client.settings.deleteProvider(removedID);

    const remaining = providers.filter(
      (p: ProviderView): boolean => p.ID !== removedID
    );

    setSelectedID(remaining.length > 0 ? remaining[0].ID : null);
    await load();
  };

  const deleteDirectly = async (provider: ProviderView): Promise<void> => {
    setConfirmDeleteID(null);
    await client.settings.deleteProvider(provider.ID);

    const remaining = providers.filter(
      (p: ProviderView): boolean => p.ID !== provider.ID
    );

    setSelectedID(remaining.length > 0 ? remaining[0].ID : null);
    await load();
  };

  // Model actions
  const openAddModel = (): void => {
    setModelForm({ mode: "add" });
    setModelFormID("");
    setModelFormContext(128000);
    setModelFormImage(false);
    setModelFormPDF(false);
    setModelFormVideo(false);
    setModelFormEfforts([]);
    setModelFormError(null);
  };

  const startEditModel = (m: ProviderModelView): void => {
    setModelForm({ mode: "edit", model: m });
    setModelFormID(m.ModelID);
    setModelFormContext(m.ContextWindow);
    setModelFormImage(m.ImageInput);
    setModelFormPDF(m.PDFInput);
    setModelFormVideo(m.VideoInput);
    // The bridge renders an empty Go slice as null; undeclared edits to no rows.
    setModelFormEfforts(m.Efforts ?? []);
    setModelFormError(null);
  };

  const saveModel = async (): Promise<void> => {
    if (!selectedProvider || !modelForm) return;

    if (!modelFormID.trim()) {
      setModelFormError("Model ID is required");

      return;
    }

    setSavingModel(true);
    setModelFormError(null);

    // Efforts go up as typed; the backend normalizes (trims, dedupes) and
    // resolves an empty declaration to ["high"] (design D7).
    const input: ProviderModelInput = {
      ModelID: modelFormID.trim(),
      ContextWindow: Number(modelFormContext) || 0,
      ImageInput: modelFormImage,
      PDFInput: modelFormPDF,
      VideoInput: modelFormVideo,
      Efforts: modelFormEfforts,
      Enabled:
        modelForm.mode === "edit" && modelForm.model
          ? modelForm.model.Enabled
          : true,
    };

    try {
      if (modelForm.mode === "add") {
        await client.settings.createModel(selectedProvider.ID, input);
      } else if (modelForm.model) {
        await client.settings.updateModel(modelForm.model.ID, input);
      }

      await loadModels(selectedProvider.ID);
      setModelForm(null);
    } catch (err) {
      setModelFormError(err instanceof Error ? err.message : String(err));
    } finally {
      setSavingModel(false);
    }
  };

  const toggleModel = async (m: ProviderModelView): Promise<void> => {
    if (!selectedProvider) return;
    setTogglingModelID(m.ID);

    try {
      await client.settings.toggleModel(m.ID, !m.Enabled);
      await loadModels(selectedProvider.ID);
    } catch (err) {
      setModelsError(err instanceof Error ? err.message : String(err));
    } finally {
      setTogglingModelID(null);
    }
  };

  const deleteModel = async (m: ProviderModelView): Promise<void> => {
    if (!selectedProvider) return;
    setDeletingModelID(m.ID);

    try {
      await client.settings.deleteModel(m.ID);
      await loadModels(selectedProvider.ID);
    } catch (err) {
      setModelsError(err instanceof Error ? err.message : String(err));
    } finally {
      setDeletingModelID(null);
    }
  };

  const canonicalProviders = providers.filter((p: ProviderView): boolean =>
    isCanonicalProviderType(p.Type)
  );

  const customProviders = providers.filter(
    (p: ProviderView): boolean => !isCanonicalProviderType(p.Type)
  );

  const renderProviderRow = (p: ProviderView): ReactElement => {
    const isSelected = !isAddMode && selectedID === p.ID;
    const isReady = p.Enabled && p.KeySet;

    return (
      <div
        key={p.ID}
        role="button"
        tabIndex={0}
        aria-selected={isSelected}
        data-testid={`provider-${p.ID}`}
        onClick={() => selectProvider(p.ID)}
        onKeyDown={(e: ReactKeyboardEvent<HTMLDivElement>) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            selectProvider(p.ID);
          }
        }}
        className={cn(
          "group flex items-center justify-between gap-2.5 rounded-md p-2 text-left transition-colors cursor-pointer",
          isSelected
            ? "bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] text-fg"
            : "text-fg2 hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg",
          !p.Enabled && "opacity-70"
        )}
      >
        <div className="flex items-center gap-2.5 min-w-0 flex-1">
          <BrandMark type={p.Type} name={p.Name} size={28} />
          <span className="truncate text-[13px] font-medium text-fg">
            {p.Name}
          </span>
        </div>
        <div className="flex items-center gap-1.5 shrink-0">
          <StatusDot
            status={isReady ? "running" : "warn"}
            className="shrink-0"
          />
          <button
            type="button"
            aria-label={`Edit ${p.Name}`}
            data-testid={`provider-edit-${p.ID}`}
            onClick={(e: ReactMouseEvent<HTMLButtonElement>) => {
              e.stopPropagation();
              selectProvider(p.ID);
            }}
            className="sr-only focus:not-sr-only"
          >
            Edit
          </button>
        </div>
      </div>
    );
  };

  if (loading) {
    return (
      <div
        className="flex min-h-[400px] w-full items-center justify-center p-8"
        data-testid="providers-pane"
      >
        <LoadingHint testId="providers-loading" />
      </div>
    );
  }

  if (loadError) {
    return (
      <div className="w-full p-4" data-testid="providers-pane">
        <div role="alert">
          <Alert tone="danger" testId="providers-error">
            {loadError}
          </Alert>
        </div>
      </div>
    );
  }

  if (providers.length === 0 && !isAddMode) {
    return (
      <div
        className="flex min-h-[400px] w-full items-center justify-center p-8"
        data-testid="providers-pane"
      >
        <EmptyState
          title="No providers configured"
          description="Configure credentials for OpenAI, Anthropic, Gemini, OpenRouter, or custom compatible endpoints to power workspace agents."
          icon={<Icon name="sliders" size={20} />}
          variant="boxed"
          testId="providers-empty"
          action={
            <Button
              variant="ghost"
              size="sm"
              data-testid="provider-add-empty"
              onClick={openAdd}
            >
              Add your first provider
            </Button>
          }
        />
      </div>
    );
  }

  return (
    <div
      className="flex flex-col md:flex-row min-h-[600px] rounded-lg border border-linesoft bg-surface overflow-hidden w-full"
      data-testid="providers-pane"
    >
      {/* Left List Column */}
      <aside
        className="flex w-full md:w-64 shrink-0 flex-col border-b md:border-b-0 md:border-r border-linesoft bg-surface/50"
        data-testid="providers-list-column"
        aria-label="Providers list"
      >
        <header className="flex h-12 shrink-0 items-center justify-between border-b border-linesoft px-4">
          <h2 className="text-[15px] font-semibold text-fg">Providers</h2>
          <Button
            variant="ghost"
            size="sm"
            data-testid="provider-add"
            onClick={openAdd}
            aria-label="Add provider"
          >
            <Icon name="plus" size={14} className="mr-1" />
            Add
          </Button>
        </header>

        <div
          className="od-scroll flex-1 overflow-y-auto p-2 space-y-4"
          data-testid="provider-list"
        >
          {canonicalProviders.length > 0 && (
            <div>
              <h3 className="px-2 pb-1.5 text-[11px] font-semibold uppercase tracking-wider text-muted">
                Providers
              </h3>
              <div className="space-y-1">
                {canonicalProviders.map(renderProviderRow)}
              </div>
            </div>
          )}
          {customProviders.length > 0 && (
            <div>
              <h3 className="px-2 pb-1.5 text-[11px] font-semibold uppercase tracking-wider text-muted">
                Custom providers
              </h3>
              <div className="space-y-1">
                {customProviders.map(renderProviderRow)}
              </div>
            </div>
          )}
          {providers.length === 0 && (
            <div className="p-4 text-center text-xs text-muted">
              No providers configured yet.
            </div>
          )}
        </div>
      </aside>

      {/* Right Detail Column */}
      <section
        className="od-scroll flex min-h-0 min-w-0 flex-1 flex-col overflow-y-auto bg-surface p-4 md:p-6"
        data-testid="providers-detail-column"
        aria-label="Provider details"
      >
        {isAddMode ? (
          <div>
            <div className="mb-6 flex items-center justify-between border-b border-linesoft pb-4">
              <div className="flex items-center gap-3">
                <BrandMark type={draftType} name={draftName} size={36} />
                <div>
                  <h2 className="text-base font-semibold text-fg">
                    Add provider
                  </h2>
                  <p className="text-xs text-muted">
                    Configure a new model provider
                  </p>
                </div>
              </div>
              <Button
                variant="ghost"
                size="sm"
                data-testid="provider-cancel-header"
                onClick={cancelAdd}
              >
                Cancel
              </Button>
            </div>

            <div className="space-y-4 max-w-xl">
              <FormGrid>
                <FormField id="provider-name" label="Name">
                  <Input
                    id="provider-name"
                    data-testid="provider-name"
                    autoFocus
                    value={draftName}
                    onChange={(e: ChangeEvent<HTMLInputElement>) =>
                      setDraftName(e.target.value)
                    }
                    placeholder="e.g. OpenAI, Anthropic, Custom"
                  />
                </FormField>
                <FormField id="provider-type" label="Type">
                  <Select
                    id="provider-type"
                    data-testid="provider-type"
                    value={draftType}
                    disabled={false}
                    onChange={(e: ChangeEvent<HTMLSelectElement>) =>
                      setDraftType(e.target.value)
                    }
                  >
                    {PROVIDER_CATALOG.map((t: ProviderTypeMeta) => (
                      <option key={t.id} value={t.id}>
                        {t.label}
                      </option>
                    ))}
                  </Select>
                </FormField>
                <FormField
                  id="provider-baseurl"
                  label="Base URL"
                  message={baseURLMessage}
                  invalid={baseURLMissing && submitAttempted}
                  messageTestId="provider-baseurl-error"
                >
                  <Input
                    id="provider-baseurl"
                    data-testid="provider-baseurl"
                    placeholder={entry?.baseURLPlaceholder ?? ""}
                    value={draftBaseURL}
                    onChange={(e: ChangeEvent<HTMLInputElement>) =>
                      setDraftBaseURL(e.target.value)
                    }
                  />
                </FormField>
                <FormField id="provider-apikey" label="API key">
                  <div className="relative flex items-center">
                    <Input
                      id="provider-apikey"
                      type={revealKey ? "text" : "password"}
                      autoComplete="off"
                      className="w-full pr-9"
                      data-testid="provider-apikey"
                      placeholder={keyPlaceholder(true, null)}
                      value={draftApiKey}
                      onChange={(e: ChangeEvent<HTMLInputElement>) =>
                        setDraftApiKey(e.target.value)
                      }
                    />
                    <button
                      type="button"
                      aria-label={revealKey ? "Hide API key" : "Show API key"}
                      data-testid="provider-apikey-reveal"
                      onClick={() => setRevealKey(!revealKey)}
                      className="absolute right-2 text-muted hover:text-fg"
                    >
                      <Icon name="eye" size={14} />
                    </button>
                  </div>
                </FormField>
              </FormGrid>

              <div className="flex flex-wrap items-center justify-between gap-3 pt-2">
                <div className="flex items-center gap-3">
                  <Button
                    variant="ghost"
                    size="sm"
                    loading={verifyingDraft}
                    data-testid="provider-verify-btn"
                    onClick={() => void verifyDraft()}
                  >
                    Verify draft
                  </Button>
                  {draftVerifyResult && (
                    <span
                      role="status"
                      className={`text-xs ${
                        draftVerifyResult.OK
                          ? "text-[color-mix(in_oklab,var(--success),black_25%)]"
                          : "text-dangertext"
                      }`}
                      data-testid="provider-verify-result"
                    >
                      {verifyText(draftVerifyResult)}
                    </span>
                  )}
                  {draftVerifyError && (
                    <span
                      role="alert"
                      className="text-xs text-dangertext"
                      data-testid="provider-verify-result"
                    >
                      {draftVerifyError}
                    </span>
                  )}
                </div>

                <div className="flex items-center gap-2">
                  <Button
                    variant="ghost"
                    size="sm"
                    data-testid="provider-cancel"
                    onClick={cancelAdd}
                  >
                    Cancel
                  </Button>
                  <Button
                    size="sm"
                    loading={savingProvider}
                    data-testid="provider-save"
                    onClick={() => void saveProvider()}
                  >
                    Create provider
                  </Button>
                </div>
              </div>
            </div>
          </div>
        ) : selectedProvider ? (
          <div>
            {/* Header */}
            <div className="mb-6 flex flex-wrap items-center justify-between gap-4 border-b border-linesoft pb-4">
              <div className="flex items-center gap-3">
                <BrandMark
                  type={selectedProvider.Type}
                  name={selectedProvider.Name}
                  size={36}
                />
                <div>
                  <div className="flex items-center gap-2">
                    <h2 className="text-base font-semibold text-fg">
                      {selectedProvider.Name}
                    </h2>
                    <Chip>{providerTypeLabel(selectedProvider.Type)}</Chip>
                  </div>
                  <div className="mt-0.5 flex items-center gap-2">
                    <span
                      className="inline-flex"
                      data-testid={`provider-key-${selectedProvider.ID}`}
                    >
                      {selectedProvider.KeySet ? (
                        <span className="font-mono text-[11px] text-muted">
                          Key: ••••{selectedProvider.KeyHint || "••••"}
                        </span>
                      ) : (
                        <span className="text-[11px] text-[color-mix(in_oklab,var(--warn),black_38%)]">
                          No key set
                        </span>
                      )}
                    </span>
                    {selectedProvider.BaseURL && (
                      <span className="truncate font-mono text-[11px] text-muted max-w-xs">
                        {selectedProvider.BaseURL}
                      </span>
                    )}
                  </div>
                </div>
              </div>

              <div className="flex items-center gap-2">
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={!selectedProvider.KeySet}
                  loading={verifyingSaved === selectedProvider.ID}
                  data-testid={`provider-verify-btn-${selectedProvider.ID}`}
                  onClick={() => void verifySaved(selectedProvider)}
                >
                  Verify
                </Button>

                <Toggle
                  on={selectedProvider.Enabled}
                  disabled={togglingProvider === selectedProvider.ID}
                  testId={`provider-toggle-${selectedProvider.ID}`}
                  label={
                    selectedProvider.Enabled
                      ? `Disable ${selectedProvider.Name}`
                      : `Enable ${selectedProvider.Name}`
                  }
                  onChange={() => void toggleEnabled(selectedProvider)}
                />

                {confirmDeleteID === selectedProvider.ID ? (
                  <button
                    type="button"
                    data-testid={`provider-remove-${selectedProvider.ID}`}
                    onClick={() => void deleteDirectly(selectedProvider)}
                    className="h-7 shrink-0 rounded-md border border-[color-mix(in_oklab,var(--danger)_45%,transparent)] px-2 text-[11px] font-medium text-dangertext transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)]"
                  >
                    Click to confirm
                  </button>
                ) : (
                  <button
                    type="button"
                    aria-label={`Remove ${selectedProvider.Name}`}
                    data-testid={`provider-remove-${selectedProvider.ID}`}
                    onClick={() => void requestRemove(selectedProvider)}
                    className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_12%,transparent)] hover:text-dangertext"
                  >
                    <Icon name="trash" size={14} />
                  </button>
                )}
              </div>
            </div>

            {/* Saved Verify Result Alert */}
            {savedVerifyOutcome && (
              <div
                role={savedVerifyOutcome.result?.OK ? "status" : "alert"}
                className="mb-4 max-w-xl"
              >
                <Alert
                  tone={savedVerifyOutcome.result?.OK ? "success" : "danger"}
                  testId={`provider-verify-result-${selectedProvider.ID}`}
                >
                  {savedVerifyOutcome.result
                    ? verifyText(savedVerifyOutcome.result)
                    : savedVerifyOutcome.error}
                </Alert>
              </div>
            )}

            {/* Provider Configuration Form */}
            <div className="mb-8 max-w-xl space-y-4">
              <h3 className="text-[13px] font-semibold text-fg">
                Provider settings
              </h3>
              <FormGrid>
                <FormField id="provider-name" label="Name">
                  <Input
                    id="provider-name"
                    data-testid="provider-name"
                    value={draftName}
                    onChange={(e: ChangeEvent<HTMLInputElement>) =>
                      setDraftName(e.target.value)
                    }
                  />
                </FormField>
                <FormField id="provider-type" label="Type">
                  <Select
                    id="provider-type"
                    data-testid="provider-type"
                    value={draftType}
                    disabled={true}
                    onChange={(e: ChangeEvent<HTMLSelectElement>) =>
                      setDraftType(e.target.value)
                    }
                  >
                    {PROVIDER_CATALOG.map((t: ProviderTypeMeta) => (
                      <option key={t.id} value={t.id}>
                        {t.label}
                      </option>
                    ))}
                  </Select>
                </FormField>
                <FormField
                  id="provider-baseurl"
                  label="Base URL"
                  message={baseURLMessage}
                  invalid={baseURLMissing && submitAttempted}
                  messageTestId="provider-baseurl-error"
                >
                  <Input
                    id="provider-baseurl"
                    data-testid="provider-baseurl"
                    placeholder={entry?.baseURLPlaceholder ?? ""}
                    value={draftBaseURL}
                    onChange={(e: ChangeEvent<HTMLInputElement>) =>
                      setDraftBaseURL(e.target.value)
                    }
                  />
                </FormField>
                <FormField id="provider-apikey" label="API key">
                  <div className="relative flex items-center">
                    <Input
                      id="provider-apikey"
                      type={revealKey ? "text" : "password"}
                      autoComplete="off"
                      className="w-full pr-9"
                      data-testid="provider-apikey"
                      placeholder={keyPlaceholder(false, selectedProvider)}
                      value={draftApiKey}
                      onChange={(e: ChangeEvent<HTMLInputElement>) =>
                        setDraftApiKey(e.target.value)
                      }
                    />
                    <button
                      type="button"
                      aria-label={revealKey ? "Hide API key" : "Show API key"}
                      data-testid="provider-apikey-reveal"
                      onClick={() => setRevealKey(!revealKey)}
                      className="absolute right-2 text-muted hover:text-fg"
                    >
                      <Icon name="eye" size={14} />
                    </button>
                  </div>
                </FormField>
              </FormGrid>

              <div className="flex flex-wrap items-center justify-between gap-3 pt-2">
                <div className="flex items-center gap-3">
                  <Button
                    variant="ghost"
                    size="sm"
                    loading={verifyingDraft}
                    disabled={savingProvider}
                    data-testid="provider-verify-btn"
                    onClick={() => void verifyDraft()}
                  >
                    Verify connection
                  </Button>
                  {draftVerifyResult && (
                    <span
                      role="status"
                      className={`text-xs ${
                        draftVerifyResult.OK
                          ? "text-[color-mix(in_oklab,var(--success),black_25%)]"
                          : "text-dangertext"
                      }`}
                      data-testid="provider-verify-result"
                    >
                      {verifyText(draftVerifyResult)}
                    </span>
                  )}
                  {draftVerifyError && (
                    <span
                      role="alert"
                      className="text-xs text-dangertext"
                      data-testid="provider-verify-result"
                    >
                      {draftVerifyError}
                    </span>
                  )}
                </div>

                <Button
                  size="sm"
                  loading={savingProvider}
                  data-testid="provider-save"
                  onClick={() => void saveProvider()}
                >
                  Save
                </Button>
              </div>
            </div>

            {/* Models Section */}
            <div className="max-w-2xl border-t border-linesoft pt-6 space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <h3 className="text-sm font-semibold text-fg flex items-center gap-1.5">
                    Models
                    <span className="text-xs font-normal text-muted">
                      ({models.length})
                    </span>
                  </h3>
                  <p className="text-xs text-muted">
                    Models available for agents using this provider
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  <Button
                    variant="ghost"
                    size="sm"
                    data-testid="model-add-btn"
                    onClick={openAddModel}
                  >
                    <Icon name="plus" size={13} className="mr-1.5" />
                    Add model
                  </Button>
                </div>
              </div>

              {modelsError && (
                <div role="alert">
                  <Alert tone="danger" testId="models-error">
                    {modelsError}
                  </Alert>
                </div>
              )}

              {/* Add / Edit Model Card */}
              {modelForm && (
                <div
                  className="rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] p-3.5 space-y-3"
                  data-testid="model-form"
                >
                  <h4 className="text-xs font-semibold text-fg">
                    {modelForm.mode === "add"
                      ? "Add model"
                      : `Edit ${modelForm.model?.ModelID}`}
                  </h4>
                  <FormGrid>
                    <FormField id="model-form-id" label="Model ID">
                      <Input
                        id="model-form-id"
                        data-testid="model-form-id"
                        className="font-mono text-[13px]"
                        placeholder="e.g. gpt-4o, claude-3-5-sonnet"
                        autoFocus
                        value={modelFormID}
                        onChange={(e: ChangeEvent<HTMLInputElement>) =>
                          setModelFormID(e.target.value)
                        }
                      />
                    </FormField>
                    <FormField
                      id="model-form-context"
                      label="Context window (tokens)"
                    >
                      <Input
                        id="model-form-context"
                        type="number"
                        data-testid="model-form-context"
                        placeholder="128000"
                        value={modelFormContext}
                        onChange={(e: ChangeEvent<HTMLInputElement>) =>
                          setModelFormContext(parseInt(e.target.value, 10) || 0)
                        }
                      />
                    </FormField>
                  </FormGrid>
                  <div className="flex flex-col gap-1.5">
                    <span className="text-[12px] font-medium text-fg2">
                      Inputs
                    </span>
                    <div className="flex flex-wrap items-center gap-6 py-1">
                      <Checkbox
                        checked={modelFormImage}
                        testId="model-form-image"
                        label={
                          <span className="inline-flex items-center gap-1">
                            <Icon name="image" size={13} />
                            Image
                          </span>
                        }
                        onChange={(next: boolean): void =>
                          setModelFormImage(next)
                        }
                      />
                      <Checkbox
                        checked={modelFormPDF}
                        testId="model-form-pdf"
                        label={
                          <span className="inline-flex items-center gap-1">
                            <Icon name="file" size={13} />
                            PDF
                          </span>
                        }
                        onChange={(next: boolean): void =>
                          setModelFormPDF(next)
                        }
                      />
                      <Checkbox
                        checked={modelFormVideo}
                        testId="model-form-video"
                        label={
                          <span className="inline-flex items-center gap-1">
                            <Icon name="video" size={13} />
                            Video
                          </span>
                        }
                        onChange={(next: boolean): void =>
                          setModelFormVideo(next)
                        }
                      />
                    </div>
                  </div>
                  <FormField
                    id="model-form-efforts"
                    label="Reasoning efforts"
                    message="Leave empty to default to high."
                  >
                    <div className="flex flex-wrap items-center gap-2">
                      {modelFormEfforts.map(
                        (effort: string, i: number): ReactElement => (
                          <div key={i} className="flex w-40 items-center gap-1">
                            <Input
                              id={i === 0 ? "model-form-efforts" : undefined}
                              data-testid={`model-form-effort-${i}`}
                              aria-label={`Effort ${i + 1}`}
                              value={effort}
                              onChange={(e: ChangeEvent<HTMLInputElement>) =>
                                setModelFormEfforts(
                                  modelFormEfforts.map(
                                    (v: string, j: number): string =>
                                      j === i ? e.target.value : v
                                  )
                                )
                              }
                            />
                            <button
                              type="button"
                              aria-label="Remove effort"
                              data-testid={`model-form-effort-remove-${i}`}
                              onClick={(): void =>
                                setModelFormEfforts(
                                  modelFormEfforts.filter(
                                    (_: string, j: number): boolean => j !== i
                                  )
                                )
                              }
                              className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg"
                            >
                              <Icon name="x" size={13} />
                            </button>
                          </div>
                        )
                      )}
                      <Button
                        variant="ghost"
                        size="sm"
                        data-testid="model-form-effort-add"
                        onClick={(): void =>
                          setModelFormEfforts([...modelFormEfforts, ""])
                        }
                      >
                        <Icon name="plus" size={13} className="mr-1.5" />
                        Add effort
                      </Button>
                    </div>
                  </FormField>
                  {modelFormError && (
                    <div role="alert">
                      <Alert tone="danger" testId="model-form-error">
                        {modelFormError}
                      </Alert>
                    </div>
                  )}
                  <div className="flex justify-end gap-2 pt-1">
                    <Button
                      variant="ghost"
                      size="sm"
                      data-testid="model-form-cancel"
                      onClick={() => setModelForm(null)}
                    >
                      Cancel
                    </Button>
                    <Button
                      size="sm"
                      data-testid="model-form-save"
                      loading={savingModel}
                      onClick={() => void saveModel()}
                    >
                      Save
                    </Button>
                  </div>
                </div>
              )}

              {/* Stored Models List */}
              {loadingModels ? (
                <LoadingHint testId="models-loading" />
              ) : models.length === 0 ? (
                <div
                  className="rounded-md border border-dashed border-line p-4 text-center text-[13px] text-muted"
                  data-testid="models-empty"
                >
                  No models configured. Click &quot;Add model&quot; to populate.
                </div>
              ) : (
                <div className="space-y-1.5" data-testid="models-list">
                  {models.map((m: ProviderModelView) => (
                    <div
                      key={m.ID}
                      data-testid={`model-row-${m.ID}`}
                      className={cn(
                        "flex items-center justify-between rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] px-3 py-2 transition-colors",
                        !m.Enabled && "opacity-60"
                      )}
                    >
                      <div className="flex items-center gap-2 min-w-0 flex-1">
                        <span
                          className="font-mono text-[13px] text-fg truncate"
                          data-testid={`model-id-${m.ID}`}
                        >
                          {m.ModelID}
                        </span>
                        <Chip mono data-testid={`model-context-${m.ID}`}>
                          {formatTokens(m.ContextWindow)}
                        </Chip>
                        {m.ImageInput && (
                          <Chip data-testid={`model-vision-${m.ID}`}>
                            Vision
                          </Chip>
                        )}
                        {m.PDFInput && (
                          <Chip data-testid={`model-pdf-${m.ID}`}>PDF</Chip>
                        )}
                        {m.VideoInput && (
                          <Chip data-testid={`model-video-${m.ID}`}>Video</Chip>
                        )}
                      </div>
                      <div className="flex items-center gap-2 shrink-0">
                        <Toggle
                          on={m.Enabled}
                          disabled={togglingModelID === m.ID}
                          testId={`model-toggle-${m.ID}`}
                          label={
                            m.Enabled
                              ? `Disable ${m.ModelID}`
                              : `Enable ${m.ModelID}`
                          }
                          onChange={() => void toggleModel(m)}
                        />
                        <button
                          type="button"
                          aria-label={`Edit ${m.ModelID}`}
                          data-testid={`model-edit-${m.ID}`}
                          onClick={() => startEditModel(m)}
                          className="flex h-7 w-7 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg"
                        >
                          <Icon name="edit" size={13} />
                        </button>
                        <button
                          type="button"
                          aria-label={`Delete ${m.ModelID}`}
                          data-testid={`model-delete-${m.ID}`}
                          disabled={deletingModelID === m.ID}
                          onClick={() => void deleteModel(m)}
                          className="flex h-7 w-7 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_12%,transparent)] hover:text-dangertext"
                        >
                          <Icon name="trash" size={13} />
                        </button>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        ) : (
          <div className="flex flex-1 items-center justify-center p-8 text-center text-muted">
            <p>Select a provider from the list to view its configuration.</p>
          </div>
        )}
      </section>

      {/* Warning confirmation dialog for provider removal when in use by agents */}
      {removing && (
        <ConfirmDialog
          title={`Remove ${removing.Name}?`}
          testId="provider-remove-dialog"
          confirmLabel="Remove"
          confirmTestId="provider-remove-confirm"
          cancelTestId="provider-remove-cancel"
          onConfirm={() => void confirmRemove()}
          onClose={() => setRemoving(null)}
        >
          <div role="alert">
            <Alert
              tone="warn"
              className="mt-3"
              testId="provider-in-use-warning"
            >
              Still used by {usedBy.length} agent
              {usedBy.length > 1 ? "s" : ""}:{" "}
              {usedBy.map((a: Agent): string => a.Name).join(", ")}. Those
              agents will lose their model.
            </Alert>
          </div>
          <p className="mt-3 text-[13px] text-muted">This cannot be undone.</p>
        </ConfirmDialog>
      )}
    </div>
  );
}
