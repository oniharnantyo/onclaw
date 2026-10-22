/**
 * ScheduleEditorModal (task 7.2, design D11/D12/D13): one add/edit form for
 * schedules — name, agent, prompt, enabled, and a RecurrenceBuilder — with
 * the server's Preview call (debounced) as the single source of the
 * recurrence's description and next fires. Validation is two-layered: the
 * local inline errors block Save while any input is invalid — but they only
 * display once the user has interacted with the field (typed in it or left
 * it), so a fresh form opens quiet — and the backend's `"<field>: <message>"`
 * validation errors map back onto their field (`recurrence` lands on the
 * builder) or the form when the shape is unknown. The save payload is
 * exactly ScheduleInput (design D13: no timestamps ever ride the binding
 * surface).
 */
import { useEffect, useState } from "react";
import type { ChangeEvent, ReactElement } from "react";

import type { Preview, ScheduleInput } from "../../../lib/client";
import type { Agent } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { FormField, ModalShell } from "../../../components";
import { Alert } from "../../../components/ui/Alert";
import { Button } from "../../../components/ui/Button";
import { Input } from "../../../components/ui/Input";
import { Select } from "../../../components/ui/Select";
import { Textarea } from "../../../components/ui/Textarea";
import { Toggle } from "../../../components/ui/Toggle";
import { client } from "../../../lib/client";
import { composeCron, defaultPreset } from "../../../lib/cronPresets";
import type { ScheduleEditorModalProps } from "../types";
import { RecurrenceBuilder } from "./RecurrenceBuilder";

/** How long the preview waits for the user to stop typing (design D11). */
const PREVIEW_DEBOUNCE_MS = 150;

/** The form's field names — also the backend validation error prefixes. */
type FieldName = "name" | "agent" | "prompt" | "recurrence";

type FieldErrors = Partial<Record<FieldName, string>>;

/** The host's IANA zone for the "times are local" note (design D5). */
const LOCAL_TIME_ZONE =
  Intl.DateTimeFormat().resolvedOptions().timeZone ?? "local time";

/** Maps a backend validation prefix to its field, or null when unknown. */
function serverField(prefix: string): FieldName | null {
  switch (prefix) {
    case "name":
      return "name";
    case "agent":
      return "agent";
    case "prompt":
      return "prompt";
    case "recurrence":
      return "recurrence";
    default:
      return null;
  }
}

/**
 * Splits a backend validation error on the FIRST ": " into its field and
 * message; unknown shapes return null and surface as the form-level error.
 */
function splitServerError(
  message: string
): { field: FieldName; text: string } | null {
  const sep = message.indexOf(": ");

  if (sep <= 0) return null;

  const field = serverField(message.slice(0, sep));

  if (!field) return null;

  return { field, text: message.slice(sep + 2) };
}

/** A five-field, no-empty-field completeness check — the one thing the pure
 * cron mappers do NOT guarantee (a weekly preset with every day deselected
 * composes short). Anything failing it blocks Save before the round-trip. */
function incompleteRecurrenceError(cron: string): string | null {
  const fields = cron.trim().split(/\s+/);

  if (fields.length === 5 && fields.every((f: string): boolean => f !== ""))
    return null;

  return "Pick a preset — the recurrence is incomplete.";
}

/** Formats one RFC3339 fire time in the host's local zone. */
function formatFireTime(iso: string): string {
  const at = new Date(iso);

  return Number.isNaN(at.getTime())
    ? iso
    : at.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

export function ScheduleEditorModal({
  open,
  schedule,
  agents,
  onClose,
  onSaved,
}: ScheduleEditorModalProps): ReactElement {
  // Field state seeds from the edited row; the add form starts empty with
  // the recurrence on the daily default (design D12).
  const [name, setName] = useState(schedule?.name ?? "");

  const [agentID, setAgentID] = useState(
    (): string => schedule?.agentId ?? agents[0]?.ID ?? ""
  );

  const [prompt, setPrompt] = useState(schedule?.prompt ?? "");

  const [enabled, setEnabled] = useState(schedule?.enabled ?? true);

  const [cron, setCron] = useState(
    (): string => schedule?.cron ?? composeCron(defaultPreset("daily"))
  );

  // Server-side validation errors land here after a rejected save; local
  // ones are derived during render below.
  const [serverErrors, setServerErrors] = useState<FieldErrors>({});

  // Local required-errors stay hidden until the user has interacted with
  // the field; validity alone still gates Save (a fresh form opens quiet).
  const [touched, setTouched] = useState<Partial<Record<FieldName, boolean>>>(
    {}
  );

  const markTouched = (field: FieldName): void => {
    setTouched(
      (
        prev: Partial<Record<FieldName, boolean>>
      ): Partial<Record<FieldName, boolean>> => ({ ...prev, [field]: true })
    );
  };

  const [formError, setFormError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  // The debounced Preview result for the latest SETTLED expression; null
  // until the first call lands. Pending is derived — the preview lags cron
  // by the debounce window, and that gap is the pending state.
  const [preview, setPreview] = useState<Preview | null>(null);

  const [previewedCron, setPreviewedCron] = useState<string | null>(null);

  const previewPending = previewedCron !== cron;

  // The live preview (design D11): debounce, then ask the one describer.
  // Stale responses and post-unmount settles are dropped.
  useEffect((): (() => void) => {
    let stale = false;

    const timer = setTimeout((): void => {
      client.schedules
        .preview(cron)
        .then((result: Preview): void => {
          if (stale) return;
          setPreview(result);
          setPreviewedCron(cron);
        })
        .catch((err: Error): void => {
          if (stale) return;
          setPreview({
            ok: false,
            error:
              err instanceof Error ? err.message : "Preview request failed",
          });
          setPreviewedCron(cron);
        });
    }, PREVIEW_DEBOUNCE_MS);

    return (): void => {
      stale = true;
      clearTimeout(timer);
    };
  }, [cron]);

  // Validity always gates Save; the local required-messages only display
  // once their field has been touched, so an untouched form shows none.
  const nameInvalid = !name.trim();

  const agentInvalid = agentID === "";

  const promptInvalid = !prompt.trim();

  const recurrenceProblem =
    incompleteRecurrenceError(cron) ??
    (preview && !preview.ok ? preview.error : undefined);

  // Server errors show until the field changes again; a local error wins
  // the slot while the input is still invalid.
  const nameMessage =
    serverErrors.name ??
    (touched.name && nameInvalid ? "A name is required." : undefined);

  const agentMessage =
    serverErrors.agent ??
    (touched.agent && agentInvalid ? "Select an agent." : undefined);

  const promptMessage =
    serverErrors.prompt ??
    (touched.prompt && promptInvalid ? "A prompt is required." : undefined);

  const recurrenceMessage = serverErrors.recurrence ?? recurrenceProblem;

  const saveDisabled =
    saving ||
    nameInvalid ||
    agentInvalid ||
    promptInvalid ||
    recurrenceProblem !== undefined ||
    previewPending;

  const requestClose = (): void => {
    if (!saving) onClose();
  };

  const handleCronChange = (next: string): void => {
    setCron(next);
    setServerErrors((prev: FieldErrors): FieldErrors => {
      if (prev.recurrence === undefined) return prev;

      const { recurrence: _recurrence, ...rest } = prev;

      return rest;
    });
  };

  const handleSave = async (): Promise<void> => {
    if (saveDisabled) return;
    setSaving(true);
    setFormError(null);
    setServerErrors({});

    const input: ScheduleInput = {
      name: name.trim(),
      agentId: agentID,
      prompt: prompt.trim(),
      cron,
      enabled,
    };

    try {
      if (schedule) await client.schedules.update(schedule.id, input);
      else await client.schedules.create(input);
      onSaved();

      return;
    } catch (err: unknown) {
      const message =
        err instanceof Error ? err.message : "Failed to save the schedule";

      const parsed = splitServerError(message);

      if (parsed) setServerErrors({ [parsed.field]: parsed.text });
      else setFormError(message);
    } finally {
      setSaving(false);
    }
  };

  if (!open) return <></>;

  return (
    <ModalShell
      title={schedule ? "Edit schedule" : "Add schedule"}
      testId="schedule-editor"
      onClose={requestClose}
      actions={
        <>
          <Button
            variant="ghost"
            data-testid="btn-cancel-schedule"
            disabled={saving}
            onClick={requestClose}
          >
            Cancel
          </Button>
          <Button
            data-testid="btn-save-schedule"
            disabled={saveDisabled}
            loading={saving}
            onClick={() => void handleSave()}
          >
            {schedule ? "Save changes" : "Add schedule"}
          </Button>
        </>
      }
    >
      {formError && (
        <div role="alert" className="mt-4">
          <Alert tone="danger" testId="schedule-editor-error">
            {formError}
          </Alert>
        </div>
      )}

      <div className="mt-4 flex flex-col gap-4">
        <FormField
          id="field-name"
          label="Name"
          message={nameMessage}
          invalid={nameMessage !== undefined}
          messageTestId="field-error-name"
        >
          <Input
            id="field-name"
            data-testid="field-name"
            invalid={nameMessage !== undefined}
            value={name}
            placeholder="e.g. Standup digest"
            autoFocus
            onBlur={(): void => markTouched("name")}
            onChange={(e: ChangeEvent<HTMLInputElement>) => {
              markTouched("name");
              setName(e.target.value);
              setServerErrors((prev: FieldErrors): FieldErrors => {
                const { name: _name, ...rest } = prev;

                return rest;
              });
            }}
          />
        </FormField>

        <FormField
          id="field-agent"
          label="Agent"
          message={agentMessage}
          invalid={agentMessage !== undefined}
          messageTestId="field-error-agent"
        >
          <Select
            id="field-agent"
            data-testid="field-agent"
            invalid={agentMessage !== undefined}
            value={agentID}
            onBlur={(): void => markTouched("agent")}
            onChange={(e: ChangeEvent<HTMLSelectElement>) => {
              markTouched("agent");
              setAgentID(e.target.value);
              setServerErrors((prev: FieldErrors): FieldErrors => {
                const { agent: _agent, ...rest } = prev;

                return rest;
              });
            }}
          >
            {agentID === "" && <option value="">Select an agent…</option>}
            {agents.map((a: Agent) => (
              <option key={a.ID} value={a.ID}>
                {a.Name}
              </option>
            ))}
          </Select>
        </FormField>

        <FormField
          id="field-prompt"
          label="Prompt"
          message={promptMessage}
          invalid={promptMessage !== undefined}
          messageTestId="field-error-prompt"
        >
          <Textarea
            id="field-prompt"
            data-testid="field-prompt"
            rows={3}
            invalid={promptMessage !== undefined}
            value={prompt}
            placeholder="What the agent should do on every run…"
            onBlur={(): void => markTouched("prompt")}
            onChange={(e: ChangeEvent<HTMLTextAreaElement>) => {
              markTouched("prompt");
              setPrompt(e.target.value);
              setServerErrors((prev: FieldErrors): FieldErrors => {
                const { prompt: _prompt, ...rest } = prev;

                return rest;
              });
            }}
          />
        </FormField>

        <div className="flex items-center justify-between gap-3">
          <div>
            <p className="text-[12px] font-medium text-fg2">Enabled</p>
            <p className="text-[11px] text-muted">
              Disabled schedules keep their history but never fire.
            </p>
          </div>
          <Toggle
            on={enabled}
            label="Enabled"
            testId="field-enabled"
            onChange={setEnabled}
          />
        </div>

        <FormField
          id="recurrence"
          label="Recurrence"
          message={recurrenceMessage}
          invalid={recurrenceMessage !== undefined}
          messageTestId="recurrence-error"
        >
          <RecurrenceBuilder value={cron} onChange={handleCronChange} />
        </FormField>

        <div data-testid="recurrence-preview">
          {previewPending && !preview && (
            <p className="text-[12px] text-muted">Checking recurrence…</p>
          )}
          {preview && preview.ok && (
            <div className="flex flex-col gap-1.5 rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] p-3">
              <p className="text-[13px] font-medium text-fg">
                {preview.description}
              </p>
              {preview.next && preview.next.length > 0 && (
                <ol className="flex flex-col gap-0.5 text-[12px] text-fg2">
                  {preview.next
                    .slice(0, 3)
                    .map((iso: string, index: number) => (
                      <li key={iso} data-testid={`recurrence-fire-${index}`}>
                        {formatFireTime(iso)}
                      </li>
                    ))}
                </ol>
              )}
              <p className="text-[11px] text-muted">
                Times are local ({LOCAL_TIME_ZONE}).
              </p>
            </div>
          )}
        </div>
      </div>
    </ModalShell>
  );
}
