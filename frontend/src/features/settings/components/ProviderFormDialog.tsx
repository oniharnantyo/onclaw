import { useState } from "react";
import type { ChangeEvent, ReactElement } from "react";

import type {
  ProviderInput,
  ProviderView,
  VerifyResult,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import { FormField, FormGrid, ModalShell } from "../../../components";
import { Button } from "../../../components/ui/Button";
import { Input } from "../../../components/ui/Input";
import { Select } from "../../../components/ui/Select";
import { client } from "../../../lib/client";
import {
  DEFAULT_PROVIDER_TYPE,
  PROVIDER_CATALOG,
  providerCatalogEntry,
} from "../catalog";
import type { ProviderTypeMeta } from "../catalog";
import type { ProviderFormValues } from "../types";

export interface ProviderFormDialogProps {
  /** Which flow the dialog serves; edit shows the type but cannot change it. */
  mode: "add" | "edit";
  /** The edited row in edit mode (drives the id and stored-key hint); null on add. */
  provider: ProviderView | null;
  /** True while the pane persists the form. */
  saving: boolean;
  /** Called with the ProviderInput write shape (a blank APIKey keeps the
   * stored key); the pane addresses the row by id. */
  onSave: (input: ProviderInput) => void;
  onCancel: () => void;
}

/** Opening form values: blank for adds, the row minus its secret for edits. */
function initialValues(
  mode: "add" | "edit",
  provider: ProviderView | null
): ProviderFormValues {
  if (mode === "edit" && provider) {
    return {
      type: provider.Type,
      name: provider.Name,
      baseURL: provider.BaseURL,
      // Write-only: the stored key is never rendered back (design D2).
      apiKey: "",
      enabled: provider.Enabled,
    };
  }

  return {
    type: DEFAULT_PROVIDER_TYPE,
    name: "",
    baseURL: "",
    apiKey: "",
    enabled: true,
  };
}

/** Placeholder for the write-only key field: the stored-key hint when
 * editing, a plain prompt when adding or nothing is stored. */
function keyPlaceholder(
  mode: "add" | "edit",
  provider: ProviderView | null
): string {
  if (mode === "add") return "API key";

  if (!provider?.KeySet) return "API key (none stored)";

  return provider.KeyHint
    ? `Leave blank to keep (••••${provider.KeyHint})`
    : "Leave blank to keep the stored key";
}

/**
 * The provider add/edit dialog (design D7): a type select driven by the
 * catalog (disabled while editing), the type's Base URL placeholder and
 * required rule, and a write-only password key field. The inline
 * "Verify connection" probe reports its outcome without ever blocking Save;
 * nothing is persisted by it. Fields ride the kit Input/Select; the verify
 * result reads in the success/danger text tiers.
 */
export default function ProviderFormDialog({
  mode,
  provider,
  saving,
  onSave,
  onCancel,
}: ProviderFormDialogProps): ReactElement {
  const [values, setValues] = useState<ProviderFormValues>(() =>
    initialValues(mode, provider)
  );

  const [submitAttempted, setSubmitAttempted] = useState(false);
  const [verifying, setVerifying] = useState(false);
  const [verifyResult, setVerifyResult] = useState<VerifyResult | null>(null);
  const [verifyError, setVerifyError] = useState<string | null>(null);

  // SAFETY: rows only ever carry types the backend accepted on create/update,
  // so an unknown id cannot be selected; the badge fallback covers display.
  const entry = providerCatalogEntry(values.type);

  const baseURLMissing =
    (entry?.requiresBaseUrl ?? false) && values.baseURL.trim() === "";

  const update = (patch: Partial<ProviderFormValues>): void => {
    setValues((prev: ProviderFormValues): ProviderFormValues => ({
      ...prev,
      ...patch,
    }));
  };

  /** The ProviderInput write shape: the typed key verbatim, or "" meaning
   * "keep the stored key" on update (design D2). No ID or timestamps — the
   * service addresses the row and owns both (an empty-string timestamp from
   * a webview payload cannot unmarshal into the backend's time.Time). */
  const buildInput = (): ProviderInput => ({
    Type: values.type,
    Name: values.name,
    BaseURL: values.baseURL,
    APIKey: values.apiKey,
    Enabled: values.enabled,
  });

  const save = (): void => {
    // Client-side rejection for types that need a Base URL: no backend call.
    if (baseURLMissing) {
      setSubmitAttempted(true);

      return;
    }

    onSave(buildInput());
  };

  const verify = async (): Promise<void> => {
    setVerifying(true);
    setVerifyResult(null);
    setVerifyError(null);

    try {
      setVerifyResult(
        await client.settings.verifyProviderDraft(
          buildInput(),
          mode === "edit" && provider ? provider.ID : ""
        )
      );
    } catch (err) {
      setVerifyError(err instanceof Error ? err.message : String(err));
    } finally {
      setVerifying(false);
    }
  };

  const baseURLMessage =
    baseURLMissing && submitAttempted
      ? `Base URL is required for ${entry?.label ?? values.type}`
      : undefined;

  return (
    <ModalShell
      title={mode === "add" ? "Add provider" : `Edit ${provider?.Name ?? ""}`}
      testId="provider-dialog"
      onClose={onCancel}
      actions={
        <>
          <Button
            variant="ghost"
            size="sm"
            data-testid="provider-cancel"
            onClick={onCancel}
          >
            Cancel
          </Button>
          <Button
            size="sm"
            data-testid="provider-save"
            disabled={saving}
            onClick={save}
          >
            Save
          </Button>
        </>
      }
    >
      <div className="mt-4">
        <FormGrid>
          <FormField id="provider-name" label="Name">
            <Input
              id="provider-name"
              className="w-full"
              data-testid="provider-name"
              autoFocus
              value={values.name}
              onChange={(e: ChangeEvent<HTMLInputElement>) =>
                update({ name: e.target.value })
              }
            />
          </FormField>
          <FormField id="provider-type" label="Type">
            <Select
              id="provider-type"
              className="w-full"
              data-testid="provider-type"
              value={values.type}
              disabled={mode === "edit"}
              onChange={(e: ChangeEvent<HTMLSelectElement>) =>
                update({ type: e.target.value })
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
            invalid={baseURLMessage !== undefined}
            messageTestId="provider-baseurl-error"
          >
            <Input
              id="provider-baseurl"
              className="w-full"
              data-testid="provider-baseurl"
              placeholder={entry?.baseURLPlaceholder ?? ""}
              value={values.baseURL}
              onChange={(e: ChangeEvent<HTMLInputElement>) =>
                update({ baseURL: e.target.value })
              }
            />
          </FormField>
          <FormField id="provider-apikey" label="API key">
            <Input
              id="provider-apikey"
              type="password"
              autoComplete="off"
              className="w-full"
              data-testid="provider-apikey"
              placeholder={keyPlaceholder(mode, provider)}
              value={values.apiKey}
              onChange={(e: ChangeEvent<HTMLInputElement>) =>
                update({ apiKey: e.target.value })
              }
            />
          </FormField>
        </FormGrid>

        <div className="mt-3 flex items-center gap-3">
          <Button
            variant="ghost"
            size="sm"
            loading={verifying}
            disabled={saving}
            data-testid="provider-verify-btn"
            onClick={() => void verify()}
          >
            Verify connection
          </Button>
          {verifyResult && (
            <span
              role="status"
              className={`text-xs ${
                verifyResult.OK
                  ? "text-[color-mix(in_oklab,var(--success),black_25%)]"
                  : "text-dangertext"
              }`}
              data-testid="provider-verify-result"
            >
              {verifyResult.OK
                ? "Connection verified."
                : verifyResult.Detail || "Verification failed."}
            </span>
          )}
          {verifyError && (
            <span
              role="alert"
              className="text-xs text-dangertext"
              data-testid="provider-verify-result"
            >
              {verifyError}
            </span>
          )}
        </div>
      </div>
    </ModalShell>
  );
}
