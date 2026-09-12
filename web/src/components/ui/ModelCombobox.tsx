import { useState, useEffect, useMemo, useRef, type ReactNode } from "react";
import { api, type ApiModel, type ApiModelsResult } from "../../lib/api";
import { cx } from "../../lib/helpers";
import { inputCls, labelCls } from "./constants";
import { Combobox, type ComboboxOption } from "./Combobox";
import { Icon } from "./Icon";

// Capability icons (fix-image-attachment-lane D5): rendered from the catalog
// data the models fetch already resolved — no new lookup. Each icon appears
// only when the catalog affirmatively supports that capability; unknown or
// unsupported rows stay quiet (no struck-through state, no placeholder).
function capabilityIcons(m: ApiModel): ReactNode {
  const nodes: { key: string; title: string; icon: string }[] = [];
  if (m.image_input) nodes.push({ key: "image", title: "Accepts image input", icon: "eye" });
  if (m.pdf_input) nodes.push({ key: "pdf", title: "Accepts PDF input", icon: "file" });
  if (m.reasoning) nodes.push({ key: "reasoning", title: "Supports reasoning", icon: "brain" });
  if (m.tool_call) nodes.push({ key: "tools", title: "Supports tool calling", icon: "wrench" });
  if (nodes.length === 0) return undefined;
  return nodes.map((n) => (
    <span key={n.key} title={n.title} aria-label={n.title} className="flex items-center">
      <Icon name={n.icon} size={12} />
    </span>
  ));
}

export interface ModelComboboxProps {
  workspaceId?: string;
  providerId?: string;
  previewCreds?: {
    type: string;
    base_url?: string;
    key?: string;
    api_key?: string;
    /** Catalog-mapping hint threaded to models-preview (D3). */
    catalog_provider?: string;
  };
  model: string;
  onModelChange: (model: string) => void;
  effort?: string | null;
  onEffortChange?: (effort: string | null) => void;
  onAvailableEffortsChange?: (efforts: string[]) => void;
  onContextLimitChange?: (limit: number | null) => void;
  disabled?: boolean;
  modelError?: string;
  effortError?: string;
  hideEffort?: boolean;
  className?: string;
}

export function ModelCombobox({
  workspaceId,
  providerId,
  previewCreds,
  model,
  onModelChange,
  effort,
  onEffortChange,
  onAvailableEffortsChange,
  onContextLimitChange,
  disabled,
  modelError,
  effortError,
  hideEffort,
  className,
}: ModelComboboxProps) {
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState<ApiModelsResult | null>(null);
  const [customMode, setCustomMode] = useState(false);
  const [customText, setCustomText] = useState("");
  const prevProviderRef = useRef<string | undefined>(providerId);

  // Fetch models whenever provider or previewCreds changes
  useEffect(() => {
    let cancelled = false;

    async function fetchModels() {
      if (previewCreds && previewCreds.type) {
        setLoading(true);
        try {
          const res = await api.providers.modelsPreview({
            type: previewCreds.type,
            base_url: previewCreds.base_url,
            key: previewCreds.key || previewCreds.api_key,
            catalog_provider: previewCreds.catalog_provider,
          });
          if (!cancelled) {
            setResult(res);
          }
        } catch {
          if (!cancelled) {
            setResult({ source: "none", models: [] });
          }
        } finally {
          if (!cancelled) setLoading(false);
        }
      } else if (workspaceId && providerId) {
        setLoading(true);
        try {
          const res = await api.providers.models(workspaceId, providerId);
          if (!cancelled) {
            setResult(res);
          }
        } catch {
          if (!cancelled) {
            setResult({ source: "none", models: [] });
          }
        } finally {
          if (!cancelled) setLoading(false);
        }
      } else {
        setResult(null);
      }
    }

    fetchModels();

    return () => {
      cancelled = true;
    };
  }, [workspaceId, providerId, previewCreds?.type, previewCreds?.base_url, previewCreds?.key, previewCreds?.api_key]);

  const models = useMemo(() => result?.models || [], [result]);
  const source = result?.source || (models.length > 0 ? "catalog" : "none");

  // Handle provider switch: reset model to first available model or empty
  useEffect(() => {
    if (prevProviderRef.current !== undefined && prevProviderRef.current !== providerId) {
      if (models.length > 0) {
        setCustomMode(false);
        onModelChange(models[0].id);
      } else {
        setCustomMode(true);
        onModelChange("");
      }
      if (onEffortChange) {
        onEffortChange(null);
      }
    }
    prevProviderRef.current = providerId;
  }, [providerId, models, onModelChange, onEffortChange]);

  // Auto-select first model if model is empty and models exist
  useEffect(() => {
    if (!model && models.length > 0) {
      onModelChange(models[0].id);
    }
  }, [model, models, onModelChange]);

  // Sync custom mode if model is not in model list
  useEffect(() => {
    if (models.length > 0) {
      const exists = models.some((m) => m.id === model);
      if (!exists && model) {
        setCustomMode(true);
        setCustomText(model);
      } else if (exists) {
        setCustomMode(false);
      }
    } else if (source === "none" || (result && models.length === 0)) {
      setCustomMode(true);
      if (model && !customText) {
        setCustomText(model);
      }
    }
  }, [models, model, source, result]);

  // Selected model metadata (for efforts)
  const selectedModelObj = useMemo(() => {
    return models.find((m) => m.id === model);
  }, [models, model]);

  const availableEfforts = useMemo(() => {
    return selectedModelObj?.efforts || [];
  }, [selectedModelObj]);

  const contextLimit = useMemo(() => {
    return selectedModelObj?.context_limit ?? null;
  }, [selectedModelObj]);

  // Expose available efforts to parent
  useEffect(() => {
    if (onAvailableEffortsChange) {
      onAvailableEffortsChange(availableEfforts);
    }
  }, [availableEfforts, onAvailableEffortsChange]);

  // Expose the selected model's catalog context limit to the parent so it can
  // auto-fill context_window when the user has not set one explicitly.
  useEffect(() => {
    if (onContextLimitChange) {
      onContextLimitChange(contextLimit);
    }
  }, [contextLimit, onContextLimitChange]);

  const handleSelectChange = (val: string) => {
    if (val === "__custom__") {
      setCustomMode(true);
      const nextVal = customText || "";
      onModelChange(nextVal);
    } else {
      setCustomMode(false);
      onModelChange(val);
    }
  };

  const handleCustomTextChange = (val: string) => {
    setCustomText(val);
    onModelChange(val.trim());
  };

  const comboboxOptions = useMemo(() => {
    const opts: ComboboxOption[] = models.map(m => ({
      value: m.id,
      label: m.name || m.id,
      icons: capabilityIcons(m),
    }));
    opts.push({ value: "__custom__", label: "Custom model ID…" });
    return opts;
  }, [models]);

  return (
    <div className={cx("space-y-4", className)}>
      <div>
        <div className="mb-1.5 flex items-center justify-between">
          <label className={cx(labelCls, "mb-0")} htmlFor="model-select">
            Model
          </label>
          <div className="flex items-center gap-1.5">
            {loading && (
              <span className="text-[11px] text-muted font-mono">loading…</span>
            )}
            {source === "live" && (
              <span
                data-testid="badge-source-live"
                className="inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-semibold bg-[color-mix(in_oklab,var(--success)_15%,transparent)] text-success"
              >
                live
              </span>
            )}
            {source === "catalog" && (
              <span
                data-testid="badge-source-catalog"
                className="inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-semibold bg-[color-mix(in_oklab,var(--accent)_15%,transparent)] text-accent"
              >
                catalog
              </span>
            )}
            {source === "none" && (
              <span
                data-testid="badge-source-none"
                className="inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-semibold bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] text-muted"
              >
                none
              </span>
            )}
          </div>
        </div>

        {source === "none" || models.length === 0 ? (
          <div>
            <input
              id="model-select"
              data-testid="input-custom-model"
              aria-label="Model ID"
              disabled={disabled}
              className={cx(inputCls, "font-mono text-[13px]", modelError && "border-danger")}
              placeholder="e.g. claude-3-7-sonnet-20250219"
              value={customText || model}
              onChange={(e) => handleCustomTextChange(e.target.value)}
            />
          </div>
        ) : (
          <div className="space-y-2">
            <Combobox
              id="model-select"
              options={comboboxOptions}
              value={customMode ? "__custom__" : model}
              onChange={handleSelectChange}
              disabled={disabled}
              error={!!modelError}
              placeholder="Select a model..."
              data-testid="select-model"
            />
            {customMode && (
              <input
                data-testid="input-custom-model"
                aria-label="Custom model ID"
                disabled={disabled}
                className={cx(inputCls, "font-mono text-[13px]", modelError && "border-danger")}
                placeholder="Enter custom model identifier…"
                value={customText}
                onChange={(e) => handleCustomTextChange(e.target.value)}
              />
            )}
          </div>
        )}
        {modelError && <p className="mt-1 text-[12px] text-danger">{modelError}</p>}
      </div>

      {/* Effort dropdown - only shown when availableEfforts is non-empty and not hidden */}
      {!hideEffort && availableEfforts.length > 0 && onEffortChange && (
        <div data-testid="effort-selection-container">
          <label className={labelCls} htmlFor="effort-select">
            Reasoning Effort
          </label>
          <select
            id="effort-select"
            data-testid="select-effort"
            aria-label="Reasoning Effort"
            disabled={disabled}
            className={cx(inputCls, effortError && "border-danger")}
            value={effort || ""}
            onChange={(e) => onEffortChange(e.target.value ? e.target.value : null)}
          >
            <option value="">Default (unspecified)</option>
            {availableEfforts.map((eff) => (
              <option key={eff} value={eff}>
                {eff}
              </option>
            ))}
          </select>
          {effortError && <p className="mt-1 text-[12px] text-danger">{effortError}</p>}
        </div>
      )}
    </div>
  );
}
