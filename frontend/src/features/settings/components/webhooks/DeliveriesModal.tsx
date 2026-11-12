/**
 * DeliveriesModal (add-webhook-triggers task 8.3): one endpoint's delivery
 * log, newest first per the store's order — HTTP status, outcome, the
 * verifier that authenticated (`via`), the human-readable note, the sender's
 * delivery id, and the attempt time. Accepted/ran rows link to what they
 * produced (the fire session, or the group transcript) through the nav
 * store — the notifications/scheduler link affordance. Auth-failed rows are
 * visually diagnosable: the danger-tinted chip plus the note in the lifted
 * danger ink (the spec's "auth failure is diagnosable" scenario).
 */
import { useEffect, useState } from "react";
import type { ReactElement } from "react";

import type {
  WebhookDeliveryView,
  WebhookEndpointView,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import { EmptyState, LoadingHint, ModalShell } from "../../../../components";
import { SettingsList, SettingsRow } from "../../../../components";
import { Button } from "../../../../components/ui/Button";
import { Chip } from "../../../../components/ui/Chip";
import { client } from "../../../../lib/client";
import { useNavStore } from "../../../../stores/nav";
import { OUTCOME_TONE_CLASSES, formatAttemptTime, outcomeTone } from "./shared";

export interface DeliveriesModalProps {
  endpoint: WebhookEndpointView;
  /** Bumped by the section's webhooks:changed subscription; refetches. */
  refreshKey: number;
  onClose: () => void;
}

/**
 * DeliveriesModal: the bounded per-endpoint log. Rows render in the
 * backend's newest-first order verbatim; the fetch re-runs while the modal
 * is open whenever the section's refreshKey bumps (a completed self-test
 * lands in the log live).
 */
export function DeliveriesModal({
  endpoint,
  refreshKey,
  onClose,
}: DeliveriesModalProps): ReactElement {
  const [deliveries, setDeliveries] = useState<WebhookDeliveryView[] | null>(
    null
  );
  const [loadError, setLoadError] = useState<string | null>(null);

  const openChat = useNavStore(
    (s: ReturnType<typeof useNavStore.getState>) => s.openChat
  );
  const openGroup = useNavStore(
    (s: ReturnType<typeof useNavStore.getState>) => s.openGroup
  );

  useEffect((): (() => void) => {
    let cancelled = false;

    void (async (): Promise<void> => {
      try {
        const rows = await client.webhooks.deliveries(endpoint.id);

        if (cancelled) return;

        setDeliveries(rows ?? []);
        setLoadError(null);
      } catch (err: unknown) {
        if (cancelled) return;

        setLoadError(
          err instanceof Error ? err.message : "Failed to load deliveries"
        );
      }
    })();

    return (): void => {
      cancelled = true;
    };
  }, [endpoint.id, refreshKey]);

  /** Dispatched rows link to the session or group they produced. */
  const openProduced = (row: WebhookDeliveryView): void => {
    if (row.sessionId !== "" && endpoint.agentId !== "") {
      openChat(endpoint.agentId, row.sessionId);

      return;
    }

    if (row.messageId !== "" && endpoint.groupId !== "") {
      openGroup(endpoint.groupId);
    }
  };

  return (
    <ModalShell
      title={`Deliveries — ${endpoint.name}`}
      testId="webhook-deliveries-modal"
      wide
      onClose={onClose}
    >
      {loadError !== null ? (
        <div role="alert">
          <EmptyState
            variant="inline"
            testId="webhook-deliveries-error"
            title={loadError}
          />
        </div>
      ) : deliveries === null ? (
        <LoadingHint testId="webhook-deliveries-loading" />
      ) : deliveries.length === 0 ? (
        <div className="mt-1">
          <EmptyState
            variant="inline"
            testId="webhook-deliveries-empty"
            title="No deliveries yet — point the sender at the endpoint's URL, or use Test."
          />
        </div>
      ) : (
        <SettingsList testId="webhook-deliveries-list">
          {deliveries.map((row: WebhookDeliveryView) => {
            const tone = outcomeTone(row.outcome);
            const dispatched =
              (row.sessionId !== "" || row.messageId !== "") &&
              (row.outcome === "accepted" || row.outcome === "ran");
            // Auth failures (and dispatch failures) read in the lifted
            // danger ink so a silently rotated secret jumps out of the log
            // (spec: "auth failure is diagnosable").
            const failure = tone === "danger";

            return (
              <SettingsRow
                key={row.id}
                testId={`webhook-delivery-${row.id}`}
                title={formatAttemptTime(row.attemptedAt)}
                badges={
                  <>
                    <Chip
                      className={OUTCOME_TONE_CLASSES[tone]}
                      data-testid={`webhook-delivery-outcome-${row.id}`}
                    >
                      {row.outcome}
                    </Chip>
                    <Chip
                      mono
                      data-testid={`webhook-delivery-status-${row.id}`}
                    >
                      HTTP {row.status}
                    </Chip>
                    {row.via !== "" && (
                      <Chip data-testid={`webhook-delivery-via-${row.id}`}>
                        via {row.via}
                      </Chip>
                    )}
                    {row.senderDeliveryId !== "" && (
                      <Chip
                        mono
                        data-testid={`webhook-delivery-sender-${row.id}`}
                      >
                        {row.senderDeliveryId}
                      </Chip>
                    )}
                  </>
                }
                extra={
                  <>
                    {row.note !== "" && (
                      <p
                        className={`mt-1 text-xs ${failure ? "text-dangertext" : "text-muted"}`}
                        data-testid={`webhook-delivery-note-${row.id}`}
                      >
                        {row.note}
                      </p>
                    )}
                  </>
                }
                actions={
                  dispatched ? (
                    <Button
                      variant="ghost"
                      size="sm"
                      data-testid={`webhook-delivery-open-${row.id}`}
                      title={
                        row.sessionId !== ""
                          ? "Open the session this delivery fired"
                          : "Open the group this delivery was delivered to"
                      }
                      onClick={(): void => openProduced(row)}
                    >
                      {row.sessionId !== "" ? "Open session" : "Open group"}
                    </Button>
                  ) : undefined
                }
              />
            );
          })}
        </SettingsList>
      )}
    </ModalShell>
  );
}
