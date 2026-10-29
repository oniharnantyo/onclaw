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
  SettingsList,
  SettingsRow,
} from "../../../components";
import { Alert } from "../../../components/ui/Alert";
import { Button } from "../../../components/ui/Button";
import { Chip } from "../../../components/ui/Chip";
import { Input } from "../../../components/ui/Input";
import { Select } from "../../../components/ui/Select";
import { Toggle } from "../../../components/ui/Toggle";
import { BrandMark } from "../../../lib/brandMarks";
import { client } from "../../../lib/client";
import type { SettingsPaneConfig } from "../types";

/**
 * Display names per channel id; the BrandMark renders the glyph (the
 * initial-tile fallback covers an id the grammar doesn't know yet). A new
 * channel registers here and in `channelBody` — everything else on the pane
 * is channel-agnostic.
 */
const CHANNEL_NAMES = {
  telegram: "Telegram",
} satisfies Record<string, string>;

/** The display name for a channel id; unknown ids fall back to the id. */
function channelName(id: string): string {
  switch (id) {
    case "telegram":
      return CHANNEL_NAMES.telegram;
    default:
      return id;
  }
}

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
 * One channel card: the identity header (brand mark, name, live state chip,
 * enable toggle) with the channel's own body below. State is ambient — the
 * chip sits at the identity, and only an error earns a detail line; a
 * disabled channel says so before any event arrives, an enabled one stays
 * quiet until the channel actually reports.
 */
function ChannelCard({
  view,
  stateEvent,
  refreshKey,
  onToggle,
}: {
  view: ChannelView;
  stateEvent: ChannelStateEvent | undefined;
  refreshKey: number;
  onToggle: (next: boolean) => void;
}): ReactElement {
  const name = channelName(view.id);
  const state = stateEvent?.state ?? (view.enabled ? null : "disabled");

  return (
    <section data-testid={`channel-card-${view.id}`}>
      <div className="flex items-center gap-3">
        <BrandMark type={view.id} name={name} size={28} />
        <div className="flex min-w-0 items-center gap-2">
          <h3
            className="text-[15px] font-semibold text-fg"
            data-testid={`channel-name-${view.id}`}
          >
            {name}
          </h3>
          {state && <StateChip state={state} />}
        </div>
        <div className="ml-auto">
          <Toggle
            on={view.enabled}
            testId={`channel-enabled-${view.id}`}
            label={`Enable ${name}`}
            onChange={onToggle}
          />
        </div>
      </div>
      {stateEvent?.state === "error" && stateEvent.detail && (
        <p
          className="mt-1.5 text-xs text-dangertext"
          data-testid="channel-state-detail"
        >
          {stateEvent.detail}
        </p>
      )}
      <div className="mt-3">{channelBody(view, refreshKey)}</div>
    </section>
  );
}

/**
 * The body registry: each channel renders its own configuration surface.
 * Unknown ids render nothing yet — a channel's body lands with the channel.
 */
function channelBody(
  view: ChannelView,
  refreshKey: number
): ReactElement | null {
  switch (view.id) {
    case "telegram":
      return <TelegramBody view={view} refreshKey={refreshKey} />;
    default:
      return null;
  }
}

/**
 * TelegramChannelBody: the Telegram channel's configuration (add-telegram-
 * channel design D14). Connection: the write-only bot-token field (starts
 * blank; the stored token shows only as a last-four hint line, the provider
 * apiKey precedent — a blank save keeps the stored token). Below: pending-
 * pairing cards (allow allowlists the sender, dismiss drops the request) and
 * the per-chat rows — kind badge, pinned-agent select, the home-chat radio
 * (one home; the store clears the rest), and the free-response toggle for
 * groups only. `refreshKey` bumps on every `channels:changed`, so mutation
 * refetches ride the same event the pane listens on.
 */
function TelegramBody({
  view,
  refreshKey,
}: {
  view: ChannelView;
  refreshKey: number;
}): ReactElement {
  const [chats, setChats] = useState<ChannelChatView[]>([]);
  const [pairings, setPairings] = useState<ChannelPairing[]>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [token, setToken] = useState("");
  const [savingToken, setSavingToken] = useState(false);

  useEffect(() => {
    let cancelled = false;
    void (async (): Promise<void> => {
      try {
        const [chatRows, pending, agentRows] = await Promise.all([
          client.channels.chats(),
          client.channels.pendingPairings(),
          client.agents.list(),
        ]);

        if (cancelled) return;

        setChats(chatRows);
        setPairings(pending);
        setAgents(agentRows);
        setLoaded(true);
      } catch {
        // The pane-level error surface covers load failures; the body's
        // lists just stay stale until the next channels:changed.
      }
    })();

    return (): void => {
      cancelled = true;
    };
  }, [refreshKey]);

  /**
   * The write shape with the typed token verbatim — or "" meaning "keep the
   * stored token" (task 1.4). The enabled flag and proxy ride along
   * unchanged; the field clears either way: it never echoes the secret.
   */
  const saveToken = async (): Promise<void> => {
    setSavingToken(true);

    try {
      await client.channels.saveChannel(view.id, {
        Enabled: view.enabled,
        BotToken: token,
        ProxyURL: view.proxyUrl,
      });
      setToken("");
    } finally {
      setSavingToken(false);
    }
  };

  /**
   * Resolves one pending request; the resolution is a plain service call
   * with no view payload, so the body refetches (the card drops either way:
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
    <>
      {/* Connection: the write-only token row. */}
      <SettingsList testId="channels-connection">
        <SettingsRow
          title="Bot token"
          badges={
            view.tokenSet ? (
              <Chip mono data-testid="channel-token-set">
                set
              </Chip>
            ) : undefined
          }
          detail={
            view.tokenSet
              ? undefined
              : "From BotFather — stored locally, never shown again."
          }
          testId="channel-token-row"
          extra={
            <>
              <div className="mt-2 flex items-center gap-2">
                <Input
                  type="password"
                  autoComplete="off"
                  className="w-72"
                  data-testid="channel-token"
                  aria-label="Bot token"
                  placeholder="Paste bot token"
                  value={token}
                  onChange={(e: ChangeEvent<HTMLInputElement>) =>
                    setToken(e.target.value)
                  }
                />
                <Button
                  variant="ghost"
                  size="sm"
                  loading={savingToken}
                  data-testid="channel-token-save"
                  onClick={() => void saveToken()}
                >
                  Save
                </Button>
              </div>
              {view.tokenSet && (
                <p
                  className="mt-1.5 text-xs text-muted"
                  data-testid="channel-token-hint"
                >
                  Stored token ends in ••••{view.tokenHint} — paste a new one to
                  replace it.
                </p>
              )}
            </>
          }
        />
      </SettingsList>

      {/* Pending pairings (design D4): unknown senders offered for
          approval, display name plus their numeric Telegram id. */}
      <section className="mt-6" data-testid="channels-pairings">
        <h4 className="font-semibold">Pending pairings</h4>
        {!loaded ? null : pairings.length === 0 ? (
          <div className="mt-2">
            <EmptyState
              variant="inline"
              testId="channels-pairings-empty"
              title="No pending requests — when someone messages the bot from an unknown account, their request shows up here."
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
                          void resolvePairing("approve", pairing.TelegramUserID)
                        }
                      >
                        Allow
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        data-testid={`pairing-dismiss-${pairing.TelegramUserID}`}
                        onClick={() =>
                          void resolvePairing("dismiss", pairing.TelegramUserID)
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
        {!loaded ? null : chats.length === 0 ? (
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
                        <Chip data-testid={`chat-home-badge-${chat.chatId}`}>
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
                        onChange={(e: ChangeEvent<HTMLSelectElement>): void =>
                          void pinAgent(chat, e.target.value)
                        }
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
  );
}

/** Live channel-connection events by channel id (the pane's chip feeds). */
type ChannelStateMap = Record<string, ChannelStateEvent>;

/**
 * ChannelsPane: every configured channel renders as its own card (task 9.3)
 * — brand mark, live connection state, enable toggle, then the channel's
 * own body. The pane is channel-agnostic: adding a channel means a row in
 * CHANNEL_NAMES and a body in `channelBody`.
 */
export default function ChannelsPane(_props: {
  pane: SettingsPaneConfig;
}): ReactElement {
  const [channels, setChannels] = useState<ChannelView[] | null>(null);
  const [stateByChannel, setStateByChannel] = useState<ChannelStateMap>({});

  const [refreshKey, setRefreshKey] = useState(0);
  const [loadError, setLoadError] = useState<string | null>(null);

  const load = useCallback(async (): Promise<void> => {
    setLoadError(null);

    try {
      setChannels(await client.channels.list());
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  // Live updates (task 9.1): every mutation re-lists; state transitions key
  // by channel so each card's chip tracks its own channel.
  useEffect(() => {
    const offChanged = client.events.onChannelsChanged((): void => {
      void load();
      setRefreshKey((k: number) => k + 1);
    });

    const offState = client.events.onChannelsState(
      (event: ChannelStateEvent): void => {
        setStateByChannel((prev: Record<string, ChannelStateEvent>) => ({
          ...prev,
          [event.channelId]: event,
        }));
      }
    );

    return (): void => {
      offChanged();
      offState();
    };
  }, [load]);

  const toggleEnabled = async (
    view: ChannelView,
    enabled: boolean
  ): Promise<void> => {
    const next = await client.channels.setEnabled(view.id, enabled);
    setChannels((prev: ChannelView[] | null): ChannelView[] =>
      (prev ?? []).map((c: ChannelView) => (c.id === next.id ? next : c))
    );
  };

  if (loadError) {
    return (
      <div data-testid="channels-pane">
        <div role="alert">
          <Alert tone="danger" testId="channels-error">
            {loadError}
          </Alert>
        </div>
      </div>
    );
  }

  // A fresh install has no rows yet: the Telegram card renders from the
  // same not-configured default the backend reads for an absent row — the
  // first toggle or save materializes it.
  const cards: ChannelView[] | null =
    channels === null
      ? null
      : channels.length > 0
        ? channels
        : [
            {
              id: "telegram",
              enabled: false,
              tokenSet: false,
              tokenHint: "",
              proxyUrl: "",
              createdAt: "",
              updatedAt: "",
            },
          ];

  return (
    <div data-testid="channels-pane">
      {cards === null ? (
        <LoadingHint testId="channels-loading" />
      ) : (
        cards.map((view: ChannelView, index: number) => (
          <div className={index > 0 ? "mt-8" : undefined} key={view.id}>
            <ChannelCard
              view={view}
              stateEvent={stateByChannel[view.id]}
              refreshKey={refreshKey}
              onToggle={(next: boolean): void => {
                void toggleEnabled(view, next);
              }}
            />
          </div>
        ))
      )}
    </div>
  );
}
