import { useEffect, useState } from "react";
import type { ReactElement } from "react";
import { Trash2 } from "lucide-react";

import type {
  Hook,
  HookExecution,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { TestHook } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/settingsservice";
import {
  ConfirmDialog,
  EmptyState,
  FormField,
  FormGrid,
  IconButton,
  LoadingHint,
  ModalShell,
  PaneHeader,
  SettingsList,
  SettingsRow,
} from "../../../components";
import { Alert } from "../../../components/ui/Alert";
import { Button } from "../../../components/ui/Button";
import { Chip } from "../../../components/ui/Chip";
import { Icon } from "../../../components/ui/Icon";
import { Input } from "../../../components/ui/Input";
import { Select } from "../../../components/ui/Select";
import { Textarea } from "../../../components/ui/Textarea";
import { Toggle } from "../../../components/ui/Toggle";
import { client } from "../../../lib/client";
import { relativeTime } from "../../../lib/relativeTime";
import type {
  HookDialogState,
  SettingsPaneConfig,
  TestOutcome,
} from "../types";

/**
 * The five lifecycle points the dispatcher fires at; the backend rejects any
 * other event on save, so the form offers exactly these as raw ids (the pane
 * shows ids everywhere, so no label mapping is needed).
 */
const HOOK_EVENTS = [
  "run_started",
  "user_prompt_submit",
  "pre_tool_use",
  "post_tool_use",
  "run_finished",
];

/** The two handler types the runtime implements; anything else is rejected. */
const HANDLER_TYPES = ["command", "script"];

/** Config placeholder per handler type (both are opaque JSON documents). */
const CONFIG_PLACEHOLDER = {
  command: '{"command": "echo done"}',
  script: '{"script": "(function(input){ console.log(input.event) })"}',
};

function emptyHook(): Hook {
  return {
    ID: "",
    Name: "",
    Event: HOOK_EVENTS[0],
    Matcher: "",
    HandlerType: "command",
    Config: "",
    TimeoutMS: 0,
    OnFailure: "",
    Enabled: true,
    Position: 0,
    Status: "",
    StatusError: "",
    CreatedAt: "",
    UpdatedAt: "",
  };
}

/** Parse and validate an opaque JSON field; returns an error message or "". */
function jsonError(value: string, allowEmpty: boolean): string {
  if (value.trim() === "") return allowEmpty ? "" : "Required JSON";

  try {
    JSON.parse(value);

    return "";
  } catch {
    return "Invalid JSON";
  }
}

/** Warn-tinted chip for a row that is switched off (kit Chip + warn tint). */
function DisabledChip(): ReactElement {
  return (
    <Chip className="border-[color-mix(in_oklab,var(--warn)_35%,transparent)] bg-[color-mix(in_oklab,var(--warn)_8%,transparent)]">
      disabled
    </Chip>
  );
}

/**
 * Audit-outcome chip: allowed on a success tint, blocked and error on danger
 * (color-mix over the tone tokens), so both themes re-skin it.
 */
function OutcomeChip({ outcome }: { outcome: string }): ReactElement {
  const tone =
    outcome === "allowed"
      ? "border-[color-mix(in_oklab,var(--success)_35%,transparent)] bg-[color-mix(in_oklab,var(--success)_8%,transparent)]"
      : "border-[color-mix(in_oklab,var(--danger)_35%,transparent)] bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] text-dangertext";

  return <Chip className={tone}>{outcome || "unknown"}</Chip>;
}

/** Alert tone of a dry run: a block or a failing handler is not a pass. */
function resultTone(result: {
  Success: boolean;
  Decision: string;
}): "success" | "danger" {
  return result.Success && result.Decision !== "block" ? "success" : "danger";
}

/** HooksPane: list/add/edit/remove lifecycle hooks with a per-hook Test
 * affordance (task 11.4) and the execution audit trail (task 4.7). The
 * form offers only the supported events and handler types and surfaces the
 * backend's validation error verbatim; Test calls the generated TestHook
 * binding directly and renders its decision, reason, and handler detail.
 * Rows keep the kit hierarchy: quiet ghost actions and a neutral ghost
 * Remove — red appears only in the confirm. */
export default function HooksPane({
  pane,
}: {
  pane: SettingsPaneConfig;
}): ReactElement {
  const [hooks, setHooks] = useState<Hook[]>([]);
  const [executions, setExecutions] = useState<HookExecution[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [dialog, setDialog] = useState<HookDialogState | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [removing, setRemoving] = useState<Hook | null>(null);
  const [testing, setTesting] = useState<string | null>(null);
  const [outcome, setOutcome] = useState<TestOutcome | null>(null);

  const load = async (): Promise<void> => {
    setLoading(true);
    setLoadError(null);

    try {
      const [defs, trail]: [Hook[], HookExecution[]] = await Promise.all([
        client.settings.hooks(),
        client.settings.hookExecutions(),
      ]);

      setHooks(defs);
      setExecutions(trail);
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const matcherErr = dialog ? jsonError(dialog.draft.Matcher, true) : "";
  const configErr = dialog ? jsonError(dialog.draft.Config, false) : "";

  const canSave =
    dialog !== null &&
    dialog.draft.Name.trim() !== "" &&
    HOOK_EVENTS.includes(dialog.draft.Event) &&
    HANDLER_TYPES.includes(dialog.draft.HandlerType) &&
    !matcherErr &&
    !configErr;

  const save = async (): Promise<void> => {
    if (!dialog) return;
    setSaving(true);
    setSaveError(null);

    try {
      if (dialog.mode === "add") {
        await client.settings.createHook(dialog.draft);
      } else {
        await client.settings.updateHook(dialog.draft);
      }

      setDialog(null);
      await load();
    } catch (err) {
      // The service validates the event, handler type, and config document;
      // its message is the only precise explanation, so show it as-is.
      setSaveError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const confirmRemove = async (): Promise<void> => {
    if (!removing) return;
    await client.settings.deleteHook(removing.ID);
    setRemoving(null);
    await load();
  };

  const test = async (hook: Hook): Promise<void> => {
    setTesting(hook.ID);
    setOutcome(null);

    try {
      const result = await TestHook(hook.ID);
      setOutcome({ hookID: hook.ID, result });
    } catch (err) {
      setOutcome({
        hookID: hook.ID,
        error: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setTesting(null);
    }
  };

  const updateDraft = (patch: Partial<Hook>): void => {
    if (!dialog) return;
    setSaveError(null);
    setDialog({ ...dialog, draft: { ...dialog.draft, ...patch } });
  };

  const openAdd = (): void => {
    setSaveError(null);
    setDialog({ mode: "add", draft: emptyHook() });
  };

  /** Hook name for an audit row; deleted hooks leave no row to resolve. */
  const hookName = (execution: HookExecution): string =>
    hooks.find((h: Hook): boolean => h.ID === execution.HookID)?.Name ??
    execution.HookID;

  return (
    <div data-testid="hooks-pane">
      <PaneHeader
        description={pane.description}
        actions={
          <Button size="sm" data-testid="hook-add" onClick={openAdd}>
            Add hook
          </Button>
        }
      />

      {loading ? (
        <LoadingHint testId="hooks-loading" />
      ) : loadError ? (
        <div role="alert">
          <Alert tone="danger" testId="hooks-error">
            {loadError}
          </Alert>
        </div>
      ) : hooks.length === 0 ? (
        <EmptyState
          title="No hooks configured."
          description="Lifecycle hooks run commands or scripts on gateway events — add one to react to turns."
          icon={<Icon name="bell" size={18} />}
          variant="boxed"
          testId="hooks-empty"
          action={
            <Button size="sm" data-testid="hook-add-empty" onClick={openAdd}>
              Add hook
            </Button>
          }
        />
      ) : (
        <SettingsList testId="hook-list">
          {hooks.map((hook: Hook) => (
            <SettingsRow
              key={hook.ID}
              title={hook.Name}
              testId={`hook-${hook.ID}`}
              badges={
                <>
                  <Chip>{hook.Event}</Chip>
                  <Chip>{hook.HandlerType}</Chip>
                  {!hook.Enabled && <DisabledChip />}
                </>
              }
              detail={hook.Matcher}
              extra={
                <>
                  {outcome?.hookID === hook.ID && outcome.result && (
                    <div role="status">
                      <Alert
                        tone={resultTone(outcome.result)}
                        className="mt-2"
                        testId={`hook-test-result-${hook.ID}`}
                      >
                        <div>
                          <div className="flex items-center gap-2 font-medium">
                            <span data-testid={`hook-test-decision-${hook.ID}`}>
                              Decision: {outcome.result.Decision || "none"}
                            </span>
                            {outcome.result.DurationMS > 0 && (
                              <Chip mono>{outcome.result.DurationMS} ms</Chip>
                            )}
                          </div>
                          {outcome.result.Reason && (
                            <p
                              className="mt-1"
                              data-testid={`hook-test-reason-${hook.ID}`}
                            >
                              {outcome.result.Reason}
                            </p>
                          )}
                          {/* Script handlers report their captured console lines, command
                              handlers their stdout; the error detail renders
                              when the handler itself failed. */}
                          {outcome.result.ConsoleLines &&
                            outcome.result.ConsoleLines.length > 0 && (
                              <pre
                                className="mt-1 max-h-32 overflow-auto font-mono text-xs whitespace-pre-wrap"
                                data-testid={`hook-test-console-${hook.ID}`}
                              >
                                {outcome.result.ConsoleLines.join("\n")}
                              </pre>
                            )}
                          {outcome.result.Err && (
                            <pre
                              className="mt-1 max-h-32 overflow-auto font-mono text-xs whitespace-pre-wrap"
                              data-testid={`hook-test-error-${hook.ID}`}
                            >
                              {outcome.result.Err}
                            </pre>
                          )}
                          {outcome.result.Output && (
                            <pre
                              className="mt-1 max-h-32 overflow-auto font-mono text-xs whitespace-pre-wrap"
                              data-testid={`hook-test-output-${hook.ID}`}
                            >
                              {outcome.result.Output}
                            </pre>
                          )}
                        </div>
                      </Alert>
                    </div>
                  )}
                  {outcome?.hookID === hook.ID && outcome.error && (
                    <div role="alert">
                      <Alert
                        tone="danger"
                        className="mt-2"
                        testId={`hook-test-result-${hook.ID}`}
                      >
                        {outcome.error}
                      </Alert>
                    </div>
                  )}
                </>
              }
              actions={
                <>
                  <Button
                    variant="ghost"
                    size="sm"
                    loading={testing === hook.ID}
                    data-testid={`hook-test-btn-${hook.ID}`}
                    onClick={() => void test(hook)}
                  >
                    Test
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    data-testid={`hook-edit-${hook.ID}`}
                    onClick={() => {
                      setSaveError(null);
                      setDialog({ mode: "edit", draft: { ...hook } });
                    }}
                  >
                    Edit
                  </Button>
                  <IconButton
                    label={`Remove ${hook.Name}`}
                    size="sm"
                    testId={`hook-remove-${hook.ID}`}
                    icon={<Trash2 aria-hidden="true" className="size-4" />}
                    onClick={() => setRemoving(hook)}
                  />
                </>
              }
            />
          ))}
        </SettingsList>
      )}

      {/* Audit trail (task 4.7): every hook evaluation the dispatcher recorded,
          newest first, with the outcome that decided the turn's path. */}
      <section className="mt-6" data-testid="hooks-audit">
        <h4 className="font-semibold">Recent executions</h4>
        {executions.length === 0 ? (
          <div className="mt-2">
            <EmptyState
              variant="inline"
              testId="hooks-audit-empty"
              title="No hook executions recorded yet."
            />
          </div>
        ) : (
          <div className="mt-2">
            <SettingsList testId="hooks-audit-list">
              {executions.map((execution: HookExecution) => (
                <SettingsRow
                  key={execution.ID}
                  title={hookName(execution)}
                  testId={`hook-execution-${execution.ID}`}
                  badges={
                    <>
                      <Chip>{execution.Event}</Chip>
                      <OutcomeChip outcome={execution.Outcome} />
                    </>
                  }
                  detail={`${execution.DurationMS} ms · ${
                    relativeTime(execution.OccurredAt) || execution.OccurredAt
                  }`}
                  extra={
                    execution.Error ? (
                      <p
                        className="mt-1 text-xs text-dangertext"
                        data-testid={`hook-execution-error-${execution.ID}`}
                      >
                        {execution.Error}
                      </p>
                    ) : undefined
                  }
                />
              ))}
            </SettingsList>
          </div>
        )}
      </section>

      {dialog && (
        <ModalShell
          title={
            dialog.mode === "add" ? "Add hook" : `Edit ${dialog.draft.Name}`
          }
          testId="hook-dialog"
          onClose={() => setDialog(null)}
          actions={
            <>
              <Button
                variant="ghost"
                size="sm"
                data-testid="hook-cancel"
                onClick={() => setDialog(null)}
              >
                Cancel
              </Button>
              <Button
                size="sm"
                data-testid="hook-save"
                disabled={!canSave || saving}
                onClick={() => void save()}
              >
                Save
              </Button>
            </>
          }
        >
          <div className="mt-4 space-y-3">
            {saveError && (
              <div role="alert">
                <Alert tone="danger" testId="hook-save-error">
                  {saveError}
                </Alert>
              </div>
            )}

            <FormGrid>
              <FormField id="hook-name" label="Name">
                <Input
                  id="hook-name"
                  className="w-full"
                  data-testid="hook-name"
                  autoFocus
                  value={dialog.draft.Name}
                  onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                    updateDraft({ Name: e.target.value })
                  }
                />
              </FormField>
              <FormField id="hook-event" label="Event">
                <Select
                  id="hook-event"
                  className="w-full"
                  data-testid="hook-event"
                  value={dialog.draft.Event}
                  onChange={(e: React.ChangeEvent<HTMLSelectElement>) =>
                    updateDraft({ Event: e.target.value })
                  }
                >
                  {HOOK_EVENTS.map((event: string) => (
                    <option key={event} value={event}>
                      {event}
                    </option>
                  ))}
                </Select>
              </FormField>
              <FormField
                id="hook-matcher"
                label="Matcher (JSON, optional)"
                message={matcherErr || undefined}
                messageTestId="hook-matcher-error"
                invalid={Boolean(matcherErr)}
              >
                <Textarea
                  id="hook-matcher"
                  className="w-full font-mono"
                  data-testid="hook-matcher"
                  value={dialog.draft.Matcher}
                  onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) =>
                    updateDraft({ Matcher: e.target.value })
                  }
                />
              </FormField>
              <FormField id="hook-handlertype" label="Handler type">
                <Select
                  id="hook-handlertype"
                  className="w-full"
                  data-testid="hook-handlertype"
                  value={dialog.draft.HandlerType}
                  onChange={(e: React.ChangeEvent<HTMLSelectElement>) =>
                    updateDraft({ HandlerType: e.target.value })
                  }
                >
                  {HANDLER_TYPES.map((type: string) => (
                    <option key={type} value={type}>
                      {type}
                    </option>
                  ))}
                </Select>
              </FormField>
              <FormField
                id="hook-config"
                label="Config (JSON)"
                message={configErr || undefined}
                messageTestId="hook-config-error"
                invalid={Boolean(configErr)}
              >
                <Textarea
                  id="hook-config"
                  className="w-full font-mono"
                  data-testid="hook-config"
                  placeholder={
                    dialog.draft.HandlerType === "script"
                      ? CONFIG_PLACEHOLDER.script
                      : CONFIG_PLACEHOLDER.command
                  }
                  value={dialog.draft.Config}
                  onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) =>
                    updateDraft({ Config: e.target.value })
                  }
                />
              </FormField>
              <FormField id="hook-timeout" label="Timeout (ms, 0 for default)">
                <Input
                  id="hook-timeout"
                  type="number"
                  className="w-full"
                  data-testid="hook-timeout"
                  value={dialog.draft.TimeoutMS}
                  onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                    updateDraft({ TimeoutMS: Number(e.target.value) })
                  }
                />
              </FormField>
              <label className="flex items-center gap-2 text-[12px] font-medium text-fg2">
                <Toggle
                  on={dialog.draft.Enabled}
                  testId="hook-enabled"
                  label="Enabled"
                  onChange={(next: boolean) => updateDraft({ Enabled: next })}
                />
                Enabled
              </label>
            </FormGrid>
          </div>
        </ModalShell>
      )}

      {removing && (
        <ConfirmDialog
          title={`Remove ${removing.Name}?`}
          testId="hook-remove-dialog"
          confirmLabel="Remove"
          confirmTestId="hook-remove-confirm"
          cancelTestId="hook-remove-cancel"
          onConfirm={() => void confirmRemove()}
          onClose={() => setRemoving(null)}
        />
      )}
    </div>
  );
}
