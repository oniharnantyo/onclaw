/**
 * WebhooksSection (add-webhook-triggers tasks 8.1-8.4): the Channels pane's
 * Webhooks section. The listener row carries the live serving state — a bind
 * failure reads as an error state with its reason (the browser-use lockfile
 * precedent: never a silent dead port) and the editable listen address
 * (empty = the loopback default; applies without a restart; a non-loopback
 * value warns before saving). The endpoint list shows each endpoint's
 * target, enabled toggle, and last-delivery outcome, with Edit (the
 * wizard), Deliveries (the log), Test (the self-signed delivery through the
 * real listener, result inline), and Delete. Every backend mutation
 * announces webhooks:changed, which refetches here and bumps the refresh
 * key an open deliveries log rides.
 */
import { useCallback, useEffect, useRef, useState } from "react";
import type { ChangeEvent, ReactElement } from "react";

import type {
  WebhookEndpointView,
  WebhookListenerView,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import {
  ConfirmDialog,
  EmptyState,
  LoadingHint,
  SettingsList,
  SettingsRow,
} from "../../../../components";
import { Alert } from "../../../../components/ui/Alert";
import { Button } from "../../../../components/ui/Button";
import { Chip } from "../../../../components/ui/Chip";
import { Icon } from "../../../../components/ui/Icon";
import { Input } from "../../../../components/ui/Input";
import { Toggle } from "../../../../components/ui/Toggle";
import { client } from "../../../../lib/client";
import { DeliveriesModal } from "./DeliveriesModal";
import { WebhookWizard } from "./WebhookWizard";
import {
  OUTCOME_TONE_CLASSES,
  formatAttemptTime,
  isLoopbackAddr,
  outcomeTone,
} from "./shared";

/** The per-endpoint Test action's inline outcome. */
interface TestState {
  endpointID: string;
  status: "running" | "ok" | "error";
  message?: string;
}

/** Tone classes for the listener's serving chip (the StateChip idiom). */
function listenerChip(state: WebhookListenerView | null): {
  label: string;
  className: string;
} {
  if (state === null) return { label: "", className: "" };

  if (state.serving)
    return {
      label: "serving",
      className:
        "border-[color-mix(in_oklab,var(--success)_35%,transparent)] bg-[color-mix(in_oklab,var(--success)_8%,transparent)]",
    };

  if (state.bindError !== "")
    return {
      label: "bind failed",
      className:
        "border-[color-mix(in_oklab,var(--danger)_35%,transparent)] bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] text-dangertext",
    };

  return { label: "stopped", className: "" };
}

/**
 * WebhooksSection: self-contained — it owns its data loads, the
 * webhooks:changed subscription, and the wizard/deliveries/confirm dialogs.
 * The Channels pane renders it below the channel cards.
 */
export default function WebhooksSection(): ReactElement {
  const [endpoints, setEndpoints] = useState<WebhookEndpointView[] | null>(
    null
  );
  const [listener, setListener] = useState<WebhookListenerView | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  // Bumped on every webhooks:changed so an open deliveries log refetches.
  const [refreshKey, setRefreshKey] = useState(0);

  // The listener address draft; seeded from the bound address until touched.
  const [address, setAddress] = useState("");
  const [savingAddress, setSavingAddress] = useState(false);
  const [addressError, setAddressError] = useState<string | null>(null);

  // Touched-ness rides a ref, not state: making it state would change the
  // load callback's identity on every keystroke and refetch the section
  // while the user edits the field.
  const addressTouchedRef = useRef(false);

  const [wizardOpen, setWizardOpen] = useState(false);
  const [editing, setEditing] = useState<WebhookEndpointView | null>(null);
  const [deliveriesFor, setDeliveriesFor] =
    useState<WebhookEndpointView | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<WebhookEndpointView | null>(
    null
  );
  const [testState, setTestState] = useState<TestState | null>(null);
  const [togglingID, setTogglingID] = useState<string | null>(null);

  const load = useCallback(async (): Promise<void> => {
    setLoadError(null);

    try {
      const [rows, state] = await Promise.all([
        client.webhooks.list(),
        client.webhooks.listenerState(),
      ]);

      setEndpoints(rows ?? []);
      setListener(state);
      setAddress((prev: string): string =>
        addressTouchedRef.current ? prev : state.boundAddr
      );
    } catch (err: unknown) {
      setLoadError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect((): void => {
    void load();
  }, [load]);

  // Live updates (task 8.1): every save, toggle, delete, and completed
  // self-test re-lists here and bumps the deliveries log's refresh key.
  useEffect((): (() => void) => {
    const off = client.events.onWebhooksChanged((): void => {
      void load();
      setRefreshKey((k: number): number => k + 1);
    });

    return (): void => {
      off();
    };
  }, [load]);

  const patchEndpoint = (next: WebhookEndpointView): void => {
    setEndpoints(
      (prev: WebhookEndpointView[] | null): WebhookEndpointView[] => {
        const rows = prev ?? [];

        return rows.some(
          (row: WebhookEndpointView): boolean => row.id === next.id
        )
          ? rows.map((row: WebhookEndpointView): WebhookEndpointView =>
              row.id === next.id ? next : row
            )
          : [...rows, next];
      }
    );
  };

  const dropEndpoint = (id: string): void => {
    setEndpoints((prev: WebhookEndpointView[] | null): WebhookEndpointView[] =>
      (prev ?? []).filter((row: WebhookEndpointView): boolean => row.id !== id)
    );
  };

  const toggleEnabled = async (
    view: WebhookEndpointView,
    enabled: boolean
  ): Promise<void> => {
    setTogglingID(view.id);

    try {
      patchEndpoint(await client.webhooks.setEnabled(view.id, enabled));
    } finally {
      setTogglingID(null);
    }
  };

  const saveAddress = async (): Promise<void> => {
    setSavingAddress(true);
    setAddressError(null);

    try {
      await client.webhooks.setListenerAddress(address.trim());
      // The listener rebinds synchronously in the service; read the fresh
      // state instead of trusting the draft (a refused bind surfaces here).
      const state = await client.webhooks.listenerState();

      setListener(state);
      setAddress(state.boundAddr);
      addressTouchedRef.current = false;
    } catch (err: unknown) {
      setAddressError(
        err instanceof Error ? err.message : "Failed to set the address"
      );
    } finally {
      setSavingAddress(false);
    }
  };

  const runTest = async (view: WebhookEndpointView): Promise<void> => {
    setTestState({ endpointID: view.id, status: "running" });

    try {
      await client.webhooks.test(view.id);
      // The self-test's log row (and any endpoint change) rides
      // webhooks:changed; the outcome surfaces inline either way.
      setTestState({ endpointID: view.id, status: "ok" });
    } catch (err: unknown) {
      setTestState({
        endpointID: view.id,
        status: "error",
        message:
          err instanceof Error ? err.message : "The self-test delivery failed",
      });
    }
  };

  const removeEndpoint = async (view: WebhookEndpointView): Promise<void> => {
    await client.webhooks.delete(view.id);
    dropEndpoint(view.id);
    setDeleteTarget(null);
  };

  const chip = listenerChip(listener);
  const nonLoopback = !isLoopbackAddr(address);

  return (
    <section
      className="mt-10 border-t border-linesoft pt-6"
      data-testid="webhooks-section"
    >
      <div className="flex items-center gap-2">
        <h3 className="font-semibold">Webhooks</h3>
        <div className="ml-auto">
          <Button
            size="sm"
            data-testid="webhook-add"
            onClick={(): void => {
              setEditing(null);
              setWizardOpen(true);
            }}
          >
            <Icon name="plus" size={14} />
            Add endpoint
          </Button>
        </div>
      </div>
      <p className="mt-1 text-[13px] text-muted">
        External systems start agent or group work over HTTP — every delivery is
        authenticated and logged.
      </p>

      {loadError !== null && (
        <div role="alert" className="mt-3">
          <Alert tone="danger" testId="webhooks-error">
            {loadError}
          </Alert>
        </div>
      )}

      {/* Listener state + address (task 8.1): serving chip, bind failure as
          a first-class error state, address editable without a restart. */}
      <div className="mt-3">
        <SettingsList testId="webhook-listener">
          <SettingsRow
            title="Listener"
            testId="webhook-listener-row"
            badges={
              chip.label !== "" && (
                <Chip
                  className={chip.className}
                  data-testid="webhook-listener-state"
                >
                  {chip.label}
                </Chip>
              )
            }
            detail={
              listener !== null
                ? listener.boundAddr !== ""
                  ? `Bound to ${listener.boundAddr}`
                  : "Not bound."
                : undefined
            }
            extra={
              <>
                {listener !== null &&
                  !listener.serving &&
                  listener.bindError !== "" && (
                    <p
                      className="mt-1.5 text-xs text-dangertext"
                      data-testid="webhook-bind-error"
                    >
                      {listener.bindError}
                    </p>
                  )}
                <div className="mt-2 flex items-center gap-2">
                  <Input
                    className="w-64 font-mono text-[13px]"
                    data-testid="webhook-address"
                    aria-label="Listener address"
                    placeholder="127.0.0.1:8787"
                    value={address}
                    onChange={(e: ChangeEvent<HTMLInputElement>): void => {
                      addressTouchedRef.current = true;
                      setAddress(e.target.value);
                    }}
                  />
                  <Button
                    variant="ghost"
                    size="sm"
                    loading={savingAddress}
                    data-testid="webhook-address-save"
                    onClick={(): void => {
                      void saveAddress();
                    }}
                  >
                    Save
                  </Button>
                </div>
                <p className="mt-1 text-[11px] text-muted">
                  Applies without a restart — empty resets the loopback default
                  (127.0.0.1:8787).
                </p>
                {nonLoopback && (
                  <div className="mt-2" data-testid="webhook-loopback-warning">
                    <Alert tone="warn">
                      This address binds beyond loopback — the webhook endpoints
                      will be reachable from other machines on your network.
                    </Alert>
                  </div>
                )}
                {addressError !== null && (
                  <p
                    className="mt-1.5 text-xs text-dangertext"
                    role="alert"
                    data-testid="webhook-address-error"
                  >
                    {addressError}
                  </p>
                )}
              </>
            }
          />
        </SettingsList>
      </div>

      {/* The endpoint list (task 8.1): name, target, enabled, last delivery. */}
      <div className="mt-4">
        {endpoints === null ? (
          <LoadingHint testId="webhooks-loading" />
        ) : endpoints.length === 0 ? (
          <EmptyState
            variant="inline"
            testId="webhooks-empty"
            title="No webhook endpoints yet — add one to let external systems trigger work."
          />
        ) : (
          <SettingsList testId="webhook-endpoints">
            {endpoints.map((view: WebhookEndpointView) => {
              const tone = outcomeTone(view.lastOutcome);
              const test =
                testState !== null && testState.endpointID === view.id
                  ? testState
                  : null;

              return (
                <SettingsRow
                  key={view.id}
                  testId={`webhook-endpoint-${view.id}`}
                  title={view.name}
                  badges={
                    <>
                      <Chip
                        className={
                          view.targetName === ""
                            ? "border-[color-mix(in_oklab,var(--danger)_35%,transparent)] bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] text-dangertext"
                            : ""
                        }
                        data-testid={`webhook-target-${view.id}`}
                      >
                        {view.targetName === ""
                          ? "target missing"
                          : view.targetName}
                      </Chip>
                      {view.lastOutcome !== "" && (
                        <Chip
                          mono
                          className={OUTCOME_TONE_CLASSES[tone]}
                          data-testid={`webhook-last-${view.id}`}
                        >
                          {view.lastOutcome} · HTTP {view.lastStatus}
                        </Chip>
                      )}
                    </>
                  }
                  detail={
                    view.lastAttemptedAt !== ""
                      ? `Last attempt ${formatAttemptTime(view.lastAttemptedAt)}`
                      : "No deliveries yet."
                  }
                  extra={
                    test !== null && test.status !== "running" ? (
                      <div className="mt-1.5" role="alert">
                        <Alert
                          tone={test.status === "ok" ? "success" : "danger"}
                          testId={`webhook-test-result-${view.id}`}
                        >
                          {test.status === "ok"
                            ? "Accepted self-test — the delivery landed in the log."
                            : (test.message ??
                              "The self-test delivery failed.")}
                        </Alert>
                      </div>
                    ) : undefined
                  }
                  actions={
                    <>
                      <Toggle
                        on={view.enabled}
                        disabled={togglingID === view.id}
                        testId={`webhook-enabled-${view.id}`}
                        label={`Enable ${view.name}`}
                        onChange={(next: boolean): void => {
                          void toggleEnabled(view, next);
                        }}
                      />
                      <Button
                        variant="ghost"
                        size="sm"
                        loading={test !== null && test.status === "running"}
                        data-testid={`webhook-test-${view.id}`}
                        onClick={(): void => {
                          void runTest(view);
                        }}
                      >
                        Test
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        data-testid={`webhook-deliveries-${view.id}`}
                        onClick={(): void => setDeliveriesFor(view)}
                      >
                        Deliveries
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        data-testid={`webhook-edit-${view.id}`}
                        onClick={(): void => {
                          setEditing(view);
                          setWizardOpen(true);
                        }}
                      >
                        <Icon name="edit" size={14} />
                        Edit
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        aria-label={`Delete ${view.name}`}
                        title={`Delete ${view.name}`}
                        data-testid={`webhook-delete-${view.id}`}
                        onClick={(): void => setDeleteTarget(view)}
                      >
                        <Icon name="trash" size={14} />
                      </Button>
                    </>
                  }
                />
              );
            })}
          </SettingsList>
        )}
      </div>

      {wizardOpen && (
        <WebhookWizard
          endpoint={editing}
          onClose={(): void => setWizardOpen(false)}
          onSaved={(saved: WebhookEndpointView): void => patchEndpoint(saved)}
        />
      )}

      {deliveriesFor !== null && (
        <DeliveriesModal
          endpoint={deliveriesFor}
          refreshKey={refreshKey}
          onClose={(): void => setDeliveriesFor(null)}
        />
      )}

      {deleteTarget !== null && (
        <ConfirmDialog
          title={`Delete ${deleteTarget.name}?`}
          testId="webhook-delete-dialog"
          confirmLabel="Delete endpoint"
          tone="error"
          confirmTestId="webhook-delete-confirm"
          cancelTestId="webhook-delete-cancel"
          onConfirm={(): void => {
            void removeEndpoint(deleteTarget);
          }}
          onClose={(): void => setDeleteTarget(null)}
        >
          The endpoint&apos;s verifiers and delivery log go with it. Senders
          still pointed at its URL get 404s.
        </ConfirmDialog>
      )}
    </section>
  );
}
