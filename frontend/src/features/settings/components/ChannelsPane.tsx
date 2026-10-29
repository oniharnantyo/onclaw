import { useCallback, useEffect, useState } from "react";
import type { ChangeEvent, ReactElement } from "react";

import type {
  Agent,
  ChannelPairing,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type {
  ChannelChatView,
  ChannelStateEvent,
  ChannelView,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import {
  EmptyState,
  LoadingHint,
  PaneHeader,
  SettingsList,
  SettingsRow,
} from "../../../components";
import { Alert } from "../../../components/ui/Alert";
import { Button } from "../../../components/ui/Button";
import { Chip } from "../../../components/ui/Chip";
import { Input } from "../../../components/ui/Input";
import { Select } from "../../../components/ui/Select";
import { Toggle } from "../../../components/ui/Toggle";
import { client } from "../../../lib/client";
import type { SettingsPaneConfig } from "../types";

/**
 * The channel row the pane manages. The backend's Telegram channel defaults
 * to the "telegram" row (telegram.DefaultChannelID); the pane addresses it
 * directly, mirroring the GatewaySettings singleton precedent.
 */
const CHANNEL_ID = "telegram";

/**
 * Chip tint of one connection state (design D10): connected on the success
 * tint, degraded on warn, error on danger with the lifted danger text, and
 * disabled neutral — the HooksPane outcome-chip idiom, token mixes only, so
 * both themes re-skin it.
 */
function StateChip({ state }: { state: string }): ReactElement {
  const tone =
    state === "connected"
      ? "border-[color-mix(in_oklab,var(--success)_35%,transparent)] bg-[color-mix(in_oklab,var(--success)_8%,transparent)]"
      : state === "degraded"
        ? "border-[color-mix(in_oklab,var(--warn)_35%,transparent)] bg-[color-mix(in_oklab,var(--warn)_8%,transparent)]"
        : state === "error"
          ? "border-[color-mix(in_oklab,var(--danger)_35%,transparent)] bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] text-dangertext"
          : "";

  return (
    <Chip className={tone} data-testid="channel-state-chip">
      {state}
    </Chip>
  );
}

/**
 * ChannelsPane: the Telegram channel's configuration surface (add-telegram-
 * channel task 9.1, design D14). Connection: the enable toggle, the
 * write-only bot-token field (starts blank; the stored token shows only as a
 * last-four hint line, the provider apiKey precedent — a blank save keeps
 * the stored token), and the live connection-state chip fed by
 * `channels:state`. Below: pending-pairing cards (allow allowlists the
 * sender, dismiss drops the request) and the per-chat rows — kind badge,
 * pinned-agent select, the home-chat radio (one home; the store clears the
 * rest), and the free-response toggle for groups only. Every backend
 * mutation is announced as `channels:changed`, which refetches the pane.
 */
export default function ChannelsPane({
  pane,
}: {
  pane: SettingsPaneConfig;
}): ReactElement {
  const [view, setView] = useState<ChannelView | null>(null);
  const [chats, setChats] = useState<ChannelChatView[]>([]);
  const [pairings, setPairings] = useState<ChannelPairing[]>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [stateEvent, setStateEvent] = useState<ChannelStateEvent | null>(null);
  const [token, setToken] = useState("");
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [savingToken, setSavingToken] = useState(false);

  const load = useCallback(async (): Promise<void> => {
    setLoading(true);
    setLoadError(null);

    try {
      const [channel, chatRows, pending, agentRows] = await Promise.all([
        client.channels.channel(CHANNEL_ID),
        client.channels.chats(),
        client.channels.pendingPairings(),
        client.agents.list(),
      ]);

      setView(channel);
      setChats(chatRows);
      setPairings(pending);
      setAgents(agentRows);
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  // Live updates (task 9.1): every mutation re-lists; state transitions feed
  // the connection chip without a refetch.
  useEffect(() => {
    const offChanged = client.events.onChannelsChanged((): void => {
      void load();
    });

    const offState = client.events.onChannelsState(
      (event: ChannelStateEvent): void => {
        if (event.channelId === CHANNEL_ID) setStateEvent(event);
      }
    );

    return (): void => {
      offChanged();
      offState();
    };
  }, [load]);

  const toggleEnabled = async (enabled: boolean): Promise<void> => {
    if (!view) return;
    setView(await client.channels.setEnabled(CHANNEL_ID, enabled));
  };

  /**
   * The write shape with the typed token verbatim — or "" meaning "keep the
   * stored token" (task 1.4). The enabled flag and proxy ride along
   * unchanged; the field clears either way: it never echoes the secret.
   */
  const saveToken = async (): Promise<void> => {
    if (!view) return;
    setSavingToken(true);

    try {
      setView(
        await client.channels.saveChannel(CHANNEL_ID, {
          Enabled: view.enabled,
          BotToken: token,
          ProxyURL: view.proxyUrl,
        })
      );
      setToken("");
    } finally {
      setSavingToken(false);
    }
  };

  /**
   * Resolves one pending request; the resolution is a plain service call
   * with no view payload, so the pane refetches (the card drops either way:
   * allow moves the user onto the allowlist, dismiss drops the request).
   */
  const resolvePairing = async (
    action: "approve" | "dismiss",
    telegramUserID: number
  ): Promise<void> => {
    if (action === "approve") {
      await client.channels.approvePairing(telegramUserID);
    } else {
      await client.channels.dismissPairing(telegramUserID);
    }

    await load();
  };

  const patchChat = (next: ChannelChatView): void => {
    setChats((prev: ChannelChatView[]) =>
      prev.map((c: ChannelChatView) => (c.chatId === next.chatId ? next : c))
    );
  };

  const pinAgent = async (
    chat: ChannelChatView,
    agentID: string
  ): Promise<void> =>
    patchChat(await client.channels.setChatPinnedAgent(chat.chatId, agentID));

  const markHome = async (chat: ChannelChatView): Promise<void> =>
    patchChat(await client.channels.setHomeChat(chat.chatId));

  const setFreeResponse = async (
    chat: ChannelChatView,
    free: boolean
  ): Promise<void> =>
    patchChat(await client.channels.setChatFreeResponse(chat.chatId, free));

  return (
    <div data-testid="channels-pane">
      <PaneHeader description={pane.description} />

      {loading ? (
        <LoadingHint testId="channels-loading" />
      ) : loadError ? (
        <div role="alert">
          <Alert tone="danger" testId="channels-error">
            {loadError}
          </Alert>
        </div>
      ) : view ? (
        <>
          {/* Connection: enable toggle, write-only token, live state chip. */}
          <SettingsList testId="channels-connection">
            <SettingsRow
              title="Enabled"
              detail="Poll Telegram while the gateway is running."
              testId="channel-enabled-row"
              actions={
                <Toggle
                  on={view.enabled}
                  testId="channel-enabled"
                  label="Enable the Telegram channel"
                  onChange={(next: boolean) => void toggleEnabled(next)}
                />
              }
            />
            <SettingsRow
              title="Bot token"
              badges={
                view.tokenSet ? (
                  <Chip mono data-testid="channel-token-set">
                    set
                  </Chip>
                ) : undefined
              }
              testId="channel-token-row"
              extra={
                <div className="mt-2 flex items-center gap-2">
                  <Input
                    type="password"
                    autoComplete="off"
                    className="w-72"
                    data-testid="channel-token"
                    aria-label="Bot token"
                    value={token}
                    onChange={(e: ChangeEvent<HTMLInputElement>) =>
                      setToken(e.target.value)
                    }
                  />
                  <Button
                    size="sm"
                    loading={savingToken}
                    data-testid="channel-token-save"
                    onClick={() => void saveToken()}
                  >
                    Save token
                  </Button>
                </div>
              }
            />
            {/* The hint line rides under the field, inside the same row: the
                stored token is only ever a last-four hint (task 1.4). */}
            {view.tokenSet && (
              <p
                className="-mt-1 px-4 pb-3 text-xs text-muted"
                data-testid="channel-token-hint"
              >
                Stored token ends in ••••{view.tokenHint} — save a new token to
                replace it.
              </p>
            )}
            <SettingsRow
              title="Connection"
              detail={
                stateEvent
                  ? "Live state reported by the channel."
                  : "Waiting for the channel to report state."
              }
              testId="channel-state-row"
              actions={
                stateEvent ? <StateChip state={stateEvent.state} /> : undefined
              }
              extra={
                stateEvent?.state === "error" && stateEvent.detail ? (
                  <p
                    className="mt-1 text-xs text-dangertext"
                    data-testid="channel-state-detail"
                  >
                    {stateEvent.detail}
                  </p>
                ) : undefined
              }
            />
          </SettingsList>

          {/* Pending pairings (design D4): unknown senders offered for
              approval, display name plus their numeric Telegram id. */}
          <section className="mt-6" data-testid="channels-pairings">
            <h4 className="font-semibold">Pending pairings</h4>
            {pairings.length === 0 ? (
              <div className="mt-2">
                <EmptyState
                  variant="inline"
                  testId="channels-pairings-empty"
                  title="No pending pairing requests."
                />
              </div>
            ) : (
              <div className="mt-2">
                <SettingsList testId="channels-pairings-list">
                  {pairings.map((pairing: ChannelPairing) => (
                    <SettingsRow
                      key={pairing.TelegramUserID}
                      title={pairing.DisplayName}
                      badges={
                        <Chip
                          mono
                          data-testid={`pairing-id-${pairing.TelegramUserID}`}
                        >
                          {pairing.TelegramUserID}
                        </Chip>
                      }
                      testId={`pairing-card-${pairing.TelegramUserID}`}
                      actions={
                        <>
                          <Button
                            size="sm"
                            data-testid={`pairing-allow-${pairing.TelegramUserID}`}
                            onClick={() =>
                              void resolvePairing(
                                "approve",
                                pairing.TelegramUserID
                              )
                            }
                          >
                            Allow
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            data-testid={`pairing-dismiss-${pairing.TelegramUserID}`}
                            onClick={() =>
                              void resolvePairing(
                                "dismiss",
                                pairing.TelegramUserID
                              )
                            }
                          >
                            Dismiss
                          </Button>
                        </>
                      }
                    />
                  ))}
                </SettingsList>
              </div>
            )}
          </section>

          {/* Per-chat rows (design D4/D13): kind badge, pinned-agent select,
              one home radio, and the free-response gate for groups only. */}
          <section className="mt-6" data-testid="channels-chats">
            <h4 className="font-semibold">Chats</h4>
            {chats.length === 0 ? (
              <div className="mt-2">
                <EmptyState
                  variant="inline"
                  testId="channels-chats-empty"
                  title="No chats known yet — message the bot to register one."
                />
              </div>
            ) : (
              <div className="mt-2">
                <SettingsList testId="channels-chats-list">
                  {chats.map((chat: ChannelChatView) => (
                    <SettingsRow
                      key={chat.chatId}
                      title={chat.title}
                      badges={
                        <>
                          <Chip data-testid={`chat-kind-${chat.chatId}`}>
                            {chat.kind}
                          </Chip>
                          {chat.isHome && (
                            <Chip
                              data-testid={`chat-home-badge-${chat.chatId}`}
                            >
                              home
                            </Chip>
                          )}{" "}
                        </>
                      }
                      testId={`chat-row-${chat.chatId}`}
                      actions={
                        <>
                          <Select
                            className="w-40"
                            data-testid={`chat-pin-${chat.chatId}`}
                            aria-label={`Pinned agent for ${chat.title}`}
                            value={chat.pinnedAgentId}
                            onChange={(
                              e: ChangeEvent<HTMLSelectElement>
                            ): void => void pinAgent(chat, e.target.value)}
                          >
                            <option value="">Gateway default</option>
                            {agents.map((agent: Agent) => (
                              <option key={agent.ID} value={agent.ID}>
                                {agent.Name}
                              </option>
                            ))}
                          </Select>
                          <label className="flex items-center gap-1.5 text-xs text-fg2">
                            <input
                              type="radio"
                              name="channels-home-chat"
                              data-testid={`chat-home-${chat.chatId}`}
                              aria-label={`Home chat: ${chat.title}`}
                              checked={chat.isHome}
                              onChange={() => void markHome(chat)}
                            />
                            Home
                          </label>
                          {chat.kind === "group" && (
                            <Toggle
                              on={chat.freeResponse}
                              testId={`chat-free-${chat.chatId}`}
                              label={`Free response for ${chat.title}`}
                              onChange={(next: boolean) =>
                                void setFreeResponse(chat, next)
                              }
                            />
                          )}
                        </>
                      }
                    />
                  ))}
                </SettingsList>
              </div>
            )}
          </section>
        </>
      ) : null}
    </div>
  );
}
