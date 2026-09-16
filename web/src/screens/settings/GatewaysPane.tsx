import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { cx } from "../../lib/helpers";
import { Icon } from "../../components/ui/Icon";
import { Toggle } from "../../components/ui/Toggle";
import { Chip } from "../../components/ui/Chip";
import { Modal } from "../../components/ui/Modal";
import { Segmented } from "../../components/ui/Segmented";
import { ErrorState } from "../../components/ErrorState";
import {
  api,
  ApiError,
  formatApiError,
  type ApiAgent,
  type ApiGatewayBinding,
  type ApiGatewayConfig,
  type ApiGatewayLink,
  type ApiWhatsAppGatewayConfig,
  type ApiWhatsAppHealth,
  type ApiWhatsAppPairing,
  type GatewayLane,
  type GatewayTransport,
  type UpdateWhatsAppGatewayPayload,
} from "../../lib/api";
import {
  defaultGatewayPlatform,
  formatCountdown,
  maskWhatsAppId,
  telegramGatewayStatusView,
  useCanManageGateways,
  whatsappGatewayStatusView,
} from "../../lib/gateways";
import serverErrorSvg from "../../assets/server-error.svg";

export interface GatewaysPaneProps {
  tenant: any;
  /** Unused — the pane is API-backed; kept for SettingsPage compat. */
  onUpdate?: (fn: any) => void;
  onToast?: (text: string, kind?: string) => void;
  /** Override the derived gateways.write check (tests). */
  canWrite?: boolean;
}

const copyText = (text: string, onToast: (t: string, k?: string) => void, okMsg: string) => {
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(text).then(
      () => onToast(okMsg),
      () => onToast("Clipboard blocked by the browser", "danger")
    );
  } else {
    onToast("Clipboard blocked by the browser", "danger");
  }
};

const waInputCls =
  "h-9 w-full rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2.5 font-mono text-[12px] text-fg2 focus:border-accent";
const waIconBtnCls =
  "flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2 disabled:opacity-40";

export function GatewaysPane({ tenant, onToast = () => {}, canWrite }: GatewaysPaneProps) {
  const derivedCanManage = useCanManageGateways(tenant);
  const writer = canWrite !== undefined ? canWrite : derivedCanManage;

  const [tgGateways, setTgGateways] = useState<ApiGatewayConfig[]>([]);
  const [waGateways, setWaGateways] = useState<ApiWhatsAppGatewayConfig[]>([]);
  const [selectedTgId, setSelectedTgId] = useState<string | null>(null);
  const [selectedWaId, setSelectedWaId] = useState<string | null>(null);

  const [bindings, setBindings] = useState<ApiGatewayBinding[]>([]);
  const [agents, setAgents] = useState<ApiAgent[]>([]);
  const [myTgLink, setMyTgLink] = useState<ApiGatewayLink | null>(null);
  const [myWaLink, setMyWaLink] = useState<ApiGatewayLink | null>(null);
  const [waHealthMap, setWaHealthMap] = useState<Record<string, ApiWhatsAppHealth>>({});

  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<ApiError | Error | null>(null);

  // Platform selection (Telegram vs WhatsApp)
  const [userSelectedPlatform, setUserSelectedPlatform] = useState<"telegram" | "whatsapp" | null>(null);

  // Modals
  const [isTgWizardOpen, setIsTgWizardOpen] = useState(false);
  const [isWaWizardOpen, setIsWaWizardOpen] = useState(false);
  const [isAddBindingOpen, setIsAddBindingOpen] = useState(false);
  const [pairingModalPlatform, setPairingModalPlatform] = useState<"telegram" | "whatsapp" | null>(null);

  // Detail pane state: Telegram
  const [tokenInput, setTokenInput] = useState("");
  const [tokenError, setTokenError] = useState("");
  const [selectedTransport, setSelectedTransport] = useState<GatewayTransport | null>(null);
  const [webhookUrl, setWebhookUrl] = useState("");
  const [webhookError, setWebhookError] = useState("");
  const [bindAgentId, setBindAgentId] = useState("");
  const [bindBotId, setBindBotId] = useState("");
  const [busyAction, setBusyAction] = useState<string | null>(null);

  const targetWsId = tenant?.sub || tenant?.id;
  const wsId = tenant?.id || targetWsId;

  const load = async () => {
    setLoading(true);
    setLoadError(null);
    try {
      const [tgList, waList, bindingsRes, agentRes, tgLinkRes, waLinkRes] = await Promise.all([
        writer ? api.gateways.telegram.list(targetWsId).catch(() => [] as ApiGatewayConfig[]) : Promise.resolve([] as ApiGatewayConfig[]),
        writer ? api.gateways.whatsapp.list(targetWsId).catch(() => [] as ApiWhatsAppGatewayConfig[]) : Promise.resolve([] as ApiWhatsAppGatewayConfig[]),
        writer ? api.gateways.telegram.bindings.list(targetWsId).catch(() => [] as ApiGatewayBinding[]) : Promise.resolve([] as ApiGatewayBinding[]),
        api.agents.list(targetWsId).catch(() => ({ agents: [] })),
        api.gateways.telegram.links.getMine(targetWsId).catch(() => ({ link: null })),
        api.gateways.whatsapp.links.getMine(targetWsId).catch(() => ({ link: null })),
      ]);

      const tgAccounts = Array.isArray(tgList) ? tgList : [];
      const waAccounts = Array.isArray(waList) ? waList : [];
      setTgGateways(tgAccounts);
      setWaGateways(waAccounts);
      setBindings(Array.isArray(bindingsRes) ? bindingsRes : []);
      setAgents(agentRes?.agents || []);
      setMyTgLink(tgLinkRes?.link || null);
      setMyWaLink(waLinkRes?.link || null);

      if (tgAccounts.length > 0 && (!selectedTgId || !tgAccounts.some((g) => g.id === selectedTgId))) {
        setSelectedTgId(tgAccounts[0].id);
      }
      if (waAccounts.length > 0 && (!selectedWaId || !waAccounts.some((g) => g.id === selectedWaId))) {
        setSelectedWaId(waAccounts[0].id);
      }

      // Fetch health for WhatsApp accounts
      if (writer && waAccounts.length > 0) {
        const healthEntries = await Promise.all(
          waAccounts.map(async (wa) => {
            const h = await api.gateways.whatsapp.health(targetWsId, wa.id).catch(() => ({ status: "unconfigured" as const }));
            return [wa.id, h] as const;
          })
        );
        const map: Record<string, ApiWhatsAppHealth> = {};
        for (const [id, h] of healthEntries) {
          map[id] = h;
        }
        setWaHealthMap(map);
      }
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 0) {
        return;
      }
      setLoadError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (targetWsId) void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reload per workspace only
  }, [targetWsId, writer]);

  const agentName = (id: string | null | undefined) =>
    agents.find((a) => a.id === id)?.name || id || "";

  // Selected Telegram bot
  const currentTgBot = useMemo(() => {
    if (!tgGateways.length) return null;
    return tgGateways.find((g) => g.id === selectedTgId) || tgGateways[0];
  }, [tgGateways, selectedTgId]);

  // Selected WhatsApp account
  const currentWaAccount = useMemo(() => {
    if (!waGateways.length) return null;
    return waGateways.find((g) => g.id === selectedWaId) || waGateways[0];
  }, [waGateways, selectedWaId]);

  const tgStatus = telegramGatewayStatusView(currentTgBot);
  const waStatus = whatsappGatewayStatusView(
    currentWaAccount,
    currentWaAccount ? waHealthMap[currentWaAccount.id] : null,
    myWaLink
  );

  const activePlatform = userSelectedPlatform ?? defaultGatewayPlatform(tgStatus, waStatus);

  const tgConfigured = Boolean(currentTgBot && (currentTgBot.token_hint || currentTgBot.bot_username || currentTgBot.id));
  const currentTgAgent = currentTgBot?.agent_id || "";

  const activeBindBot = useMemo(() => {
    if (bindBotId) {
      const found = tgGateways.find((g) => g.id === bindBotId);
      if (found) return found;
    }
    return currentTgBot || tgGateways[0] || null;
  }, [bindBotId, currentTgBot, tgGateways]);

  const bindAgent = useMemo(
    () => bindAgentId || activeBindBot?.agent_id || agents[0]?.id || "",
    [bindAgentId, activeBindBot, agents]
  );

  const bindCommand = activeBindBot?.bot_username
    ? `/bind@${activeBindBot.bot_username} ${agents.find((a) => a.id === bindAgent)?.slug || "agent"}`
    : `/bind ${agents.find((a) => a.id === bindAgent)?.slug || "agent"}`;

  const tgIdentity = currentTgBot?.bot_username
    ? `@${currentTgBot.bot_username}`
    : currentTgBot?.token_hint
    ? `••••${currentTgBot.token_hint}`
    : currentTgBot?.identity || "";

  const waIdentity = myWaLink?.platform_user_id
    ? maskWhatsAppId(myWaLink.platform_user_id)
    : currentWaAccount?.bot_username ||
      (currentWaAccount?.lane === "multi_device"
        ? "Multi-device"
        : currentWaAccount?.lane === "cloud_api"
        ? "Cloud API"
        : "");

  // Update token for selected bot
  const handleRotateToken = async () => {
    if (!currentTgBot) return;
    const token = tokenInput.trim();
    if (!token) return;
    setBusyAction("rotate");
    try {
      const res = await api.gateways.telegram.update(targetWsId, currentTgBot.id, { token });
      setTgGateways((prev) => prev.map((g) => (g.id === res.id ? res : g)));
      setTokenInput("");
      onToast("Bot token updated");
    } catch (err: unknown) {
      onToast(formatApiError(err, "Failed to update the token"), "danger");
    } finally {
      setBusyAction(null);
    }
  };

  // Test connection for selected bot
  const handleTestTg = async () => {
    if (!currentTgBot) return;
    setBusyAction("test");
    try {
      const res = await api.gateways.telegram.test(targetWsId, currentTgBot.id);
      if (res.ok) {
        onToast(
          res.bot_username
            ? `Connection verified — bot is @${res.bot_username}`
            : "Connection verified"
        );
      } else {
        onToast(res.error || "Connection test failed", "danger");
      }
    } catch (err: unknown) {
      onToast(formatApiError(err, "Connection test failed"), "danger");
    } finally {
      setBusyAction(null);
    }
  };

  // Change bound agent for selected bot
  const handleTgAgentChange = async (agentId: string) => {
    if (!currentTgBot) return;
    try {
      const res = await api.gateways.telegram.update(targetWsId, currentTgBot.id, {
        agent_id: agentId,
      });
      setTgGateways((prev) => prev.map((g) => (g.id === res.id ? res : g)));
      onToast(
        agentId
          ? `${agentName(agentId) || "Agent"} now answers direct messages for this bot`
          : "Bound agent updated"
      );
    } catch (err: unknown) {
      onToast(formatApiError(err, "Failed to update the agent"), "danger");
    }
  };

  const activeTransport = selectedTransport || currentTgBot?.transport || "long_polling";

  const handleTgTransportChange = async (transport: GatewayTransport) => {
    if (!currentTgBot) return;
    setWebhookError("");
    if (transport === "long_polling") {
      if (currentTgBot?.transport === "long_polling" && !selectedTransport) return;
      setSelectedTransport("long_polling");
      try {
        const res = await api.gateways.telegram.update(targetWsId, currentTgBot.id, { transport: "long_polling" });
        setTgGateways((prev) => prev.map((g) => (g.id === res.id ? res : g)));
        setSelectedTransport(null);
        setWebhookUrl(res.webhook_url || "");
        onToast("Transport switched to long-polling");
      } catch (err: unknown) {
        onToast(formatApiError(err, "Failed to switch transport"), "danger");
      }
      return;
    }

    const existingUrl = currentTgBot?.webhook_url || "";
    if (currentTgBot?.transport === "webhook" && selectedTransport === null) {
      return;
    }
    if (existingUrl.startsWith("https://")) {
      setSelectedTransport(null);
      try {
        const res = await api.gateways.telegram.update(targetWsId, currentTgBot.id, {
          transport: "webhook",
          webhook_url: existingUrl,
        });
        setTgGateways((prev) => prev.map((g) => (g.id === res.id ? res : g)));
        setWebhookUrl(res.webhook_url || "");
        onToast("Transport switched to webhook");
      } catch (err: unknown) {
        if (err instanceof ApiError && err.details?.length) {
          const detail = err.details.find((d) => d.field === "webhook_url") || err.details[0];
          setWebhookError(detail.message || err.message);
        }
        onToast(formatApiError(err, "Failed to switch transport"), "danger");
      }
      return;
    }

    setSelectedTransport("webhook");
    setWebhookUrl(existingUrl);
  };

  const handleSaveTgWebhook = async () => {
    if (!currentTgBot) return;
    const url = (webhookUrl || currentTgBot?.webhook_url || "").trim();
    if (!url) {
      setWebhookError("A webhook URL is required");
      return;
    }
    setWebhookError("");
    setBusyAction("webhook");
    try {
      const res = await api.gateways.telegram.update(targetWsId, currentTgBot.id, {
        transport: "webhook",
        webhook_url: url,
      });
      setTgGateways((prev) => prev.map((g) => (g.id === res.id ? res : g)));
      setSelectedTransport(null);
      setWebhookUrl(res.webhook_url || "");
      onToast(currentTgBot?.transport === "webhook" ? "Webhook URL saved" : "Transport switched to webhook");
    } catch (err: unknown) {
      if (err instanceof ApiError && err.details?.length) {
        const detail = err.details.find((d) => d.field === "webhook_url") || err.details[0];
        setWebhookError(detail.message || err.message);
      } else if (err instanceof ApiError && err.message) {
        setWebhookError(err.message);
      }
      onToast(formatApiError(err, "Failed to save webhook configuration"), "danger");
    } finally {
      setBusyAction(null);
    }
  };

  const handleToggleTgEnabled = async (next: boolean) => {
    if (!currentTgBot) return;
    setBusyAction("toggle");
    try {
      if (next) {
        await api.gateways.telegram.enable(targetWsId, currentTgBot.id);
      } else {
        await api.gateways.telegram.disable(targetWsId, currentTgBot.id);
      }
      setTgGateways((prev) =>
        prev.map((g) => (g.id === currentTgBot.id ? { ...g, enabled: next } : g))
      );
      onToast(
        next
          ? "Gateway enabled — Telegram turns are ingested"
          : "Gateway disabled — configuration and bindings are preserved"
      );
    } catch (err: unknown) {
      onToast(formatApiError(err, "Failed to update the gateway"), "danger");
    } finally {
      setBusyAction(null);
    }
  };

  const handleDeleteTgBot = async () => {
    if (!currentTgBot) return;
    setBusyAction("delete-bot");
    try {
      await api.gateways.telegram.delete(targetWsId, currentTgBot.id);
      const remaining = tgGateways.filter((g) => g.id !== currentTgBot.id);
      setTgGateways(remaining);
      setSelectedTgId(remaining[0]?.id || null);
      onToast("Telegram bot deleted");
    } catch (err: unknown) {
      onToast(formatApiError(err, "Failed to delete bot"), "danger");
    } finally {
      setBusyAction(null);
    }
  };

  const handleUnlink = async (b: ApiGatewayBinding) => {
    setBusyAction("unlink-" + b.id);
    try {
      await api.gateways.telegram.bindings.remove(targetWsId, b.id);
      setBindings((prev) => prev.filter((x) => x.id !== b.id));
      onToast(`${b.chat_title || "Group"} unlinked`);
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 409) {
        onToast("Binding conflict — the group is bound to another agent. Reload and retry.", "danger");
      } else {
        onToast(formatApiError(err, "Failed to unlink the group"), "danger");
      }
    } finally {
      setBusyAction(null);
    }
  };

  const handleUnpairTg = async () => {
    setBusyAction("unpair-tg");
    try {
      await api.gateways.telegram.links.removeMine(targetWsId);
      setMyTgLink(null);
      onToast("Telegram account unlinked");
    } catch (err: unknown) {
      onToast(formatApiError(err, "Failed to unlink your Telegram account"), "danger");
    } finally {
      setBusyAction(null);
    }
  };

  return (
    <div className="w-full" data-od-id="pane-gateways" data-testid="pane-gateways">
      {/* Mobile collapse chip row (<768px) */}
      <div
        className="flex md:hidden overflow-x-auto gap-2 pb-3 mb-5 border-b border-line od-scroll max-w-full"
        data-testid="gateway-platform-tabs"
      >
        {/* Telegram account chips */}
        {tgGateways.length > 0 ? (
          tgGateways.map((g) => {
            const st = telegramGatewayStatusView(g);
            const isSelected = activePlatform === "telegram" && (selectedTgId === g.id || (!selectedTgId && g.id === tgGateways[0].id));
            const display = g.bot_username ? `@${g.bot_username}` : g.token_hint ? `••••${g.token_hint}` : g.identity || "Telegram Bot";
            return (
              <button
                key={g.id}
                type="button"
                data-testid={`tab-telegram-${g.id}`}
                onClick={() => {
                  setUserSelectedPlatform("telegram");
                  setSelectedTgId(g.id);
                }}
                className={cx(
                  "inline-flex items-center gap-2 rounded-full px-3.5 py-1.5 text-[12px] font-medium transition-colors shrink-0",
                  isSelected
                    ? "bg-accent text-accenton"
                    : "border border-line bg-surface text-fg2 hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)]"
                )}
              >
                <span
                  className={cx(
                    "h-1.5 w-1.5 rounded-full",
                    isSelected
                      ? st.dot === "bg-danger"
                        ? "bg-danger"
                        : "bg-accenton"
                      : st.dot
                  )}
                />
                <span className="truncate max-w-[120px]">{display}</span>
                <span className="text-[11px] opacity-75">{st.label}</span>
              </button>
            );
          })
        ) : (
          <button
            type="button"
            data-testid="tab-telegram"
            onClick={() => setUserSelectedPlatform("telegram")}
            className={cx(
              "inline-flex items-center gap-2 rounded-full px-3.5 py-1.5 text-[12px] font-medium transition-colors shrink-0",
              activePlatform === "telegram"
                ? "bg-accent text-accenton"
                : "border border-line bg-surface text-fg2 hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)]"
            )}
          >
            <span className="h-1.5 w-1.5 rounded-full bg-muted" />
            <span>Telegram</span>
            <span className="text-[11px] opacity-75">Not set up</span>
          </button>
        )}

        {writer && (
          <button
            type="button"
            data-testid="btn-add-telegram-bot-mobile"
            onClick={() => setIsTgWizardOpen(true)}
            className="inline-flex items-center gap-1.5 rounded-full border border-dashed border-line px-3 py-1.5 text-[12px] font-medium text-fg2 hover:border-accent hover:text-fg shrink-0"
          >
            <span>＋ Add bot</span>
          </button>
        )}

        {/* WhatsApp account chips */}
        {waGateways.length > 0 ? (
          waGateways.map((g) => {
            const st = whatsappGatewayStatusView(g, waHealthMap[g.id], myWaLink);
            const isSelected = activePlatform === "whatsapp" && (selectedWaId === g.id || (!selectedWaId && g.id === waGateways[0].id));
            const display = g.bot_username || (g.lane === "multi_device" ? "WhatsApp MD" : "WhatsApp Cloud");
            return (
              <button
                key={g.id}
                type="button"
                data-testid={`tab-whatsapp-${g.id}`}
                onClick={() => {
                  setUserSelectedPlatform("whatsapp");
                  setSelectedWaId(g.id);
                }}
                className={cx(
                  "inline-flex items-center gap-2 rounded-full px-3.5 py-1.5 text-[12px] font-medium transition-colors shrink-0",
                  isSelected
                    ? "bg-accent text-accenton"
                    : "border border-line bg-surface text-fg2 hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)]"
                )}
              >
                <span
                  className={cx(
                    "h-1.5 w-1.5 rounded-full",
                    isSelected
                      ? st.dot === "bg-danger"
                        ? "bg-danger"
                        : "bg-accenton"
                      : st.dot
                  )}
                />
                <span className="truncate max-w-[120px]">{display}</span>
                <span className="text-[11px] opacity-75">{st.label}</span>
              </button>
            );
          })
        ) : (
          <button
            type="button"
            data-testid="tab-whatsapp"
            onClick={() => setUserSelectedPlatform("whatsapp")}
            className={cx(
              "inline-flex items-center gap-2 rounded-full px-3.5 py-1.5 text-[12px] font-medium transition-colors shrink-0",
              activePlatform === "whatsapp"
                ? "bg-accent text-accenton"
                : "border border-line bg-surface text-fg2 hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)]"
            )}
          >
            <span className="h-1.5 w-1.5 rounded-full bg-muted" />
            <span>WhatsApp</span>
            <span className="text-[11px] opacity-75">Not set up</span>
          </button>
        )}

        {writer && (
          <button
            type="button"
            data-testid="btn-add-whatsapp-account-mobile"
            onClick={() => setIsWaWizardOpen(true)}
            className="inline-flex items-center gap-1.5 rounded-full border border-dashed border-line px-3 py-1.5 text-[12px] font-medium text-fg2 hover:border-accent hover:text-fg shrink-0"
          >
            <span>＋ Add WA</span>
          </button>
        )}
      </div>

      {/* Two-column layout on >=768px */}
      <div className="flex flex-col md:flex-row gap-6 items-start">
        {/* Sidebar (>=768px) */}
        <aside
          className="hidden md:flex w-56 shrink-0 flex-col gap-4"
          aria-label="Gateway platforms"
        >
          {/* Telegram Section */}
          <div>
            <div className="flex items-center justify-between px-2 pb-1.5">
              <span className="text-[11px] font-semibold uppercase tracking-wider text-muted">Telegram</span>
              {writer && (
                <button
                  type="button"
                  data-testid="btn-add-telegram-bot"
                  onClick={() => setIsTgWizardOpen(true)}
                  className="text-[11px] font-medium text-accent hover:underline flex items-center gap-1"
                >
                  <span>＋ Add a bot</span>
                </button>
              )}
            </div>
            <div className="space-y-1">
              {tgGateways.length > 0 ? (
                tgGateways.map((g) => {
                  const st = telegramGatewayStatusView(g);
                  const isSelected = activePlatform === "telegram" && (selectedTgId === g.id || (!selectedTgId && g.id === tgGateways[0].id));
                  const display = g.bot_username ? `@${g.bot_username}` : g.token_hint ? `••••${g.token_hint}` : g.identity || "Telegram Bot";
                  return (
                    <button
                      key={g.id}
                      type="button"
                      data-testid={`sidebar-item-telegram-${g.id}`}
                      onClick={() => {
                        setUserSelectedPlatform("telegram");
                        setSelectedTgId(g.id);
                      }}
                      className={cx(
                        "w-full flex items-center gap-2.5 rounded-lg border p-2.5 text-left transition-colors",
                        isSelected
                          ? "border-line bg-[color-mix(in_oklab,var(--accent)_12%,var(--surface))] shadow-sm"
                          : "border-transparent bg-transparent hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)]"
                      )}
                    >
                      <span
                        className={cx(
                          "flex h-8 w-8 shrink-0 items-center justify-center rounded-md",
                          st.state === "connected"
                            ? "bg-[color-mix(in_oklab,var(--accent)_15%,transparent)] text-accent"
                            : "bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] text-muted"
                        )}
                      >
                        <Icon name="bot" size={16} />
                      </span>
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center justify-between gap-1">
                          <span className="truncate text-[13px] font-medium text-fg">{display}</span>
                          <span
                            className={cx("h-2 w-2 shrink-0 rounded-full", st.dot)}
                            title={st.label}
                            data-testid={`sidebar-dot-telegram-${g.id}`}
                          />
                        </div>
                        <p className="truncate text-[11px] text-muted">
                          {agentName(g.agent_id) || st.label}
                        </p>
                      </div>
                    </button>
                  );
                })
              ) : (
                <button
                  type="button"
                  data-testid="sidebar-item-telegram"
                  onClick={() => setUserSelectedPlatform("telegram")}
                  className={cx(
                    "w-full flex items-center gap-2.5 rounded-lg border p-2.5 text-left transition-colors",
                    activePlatform === "telegram"
                      ? "border-line bg-[color-mix(in_oklab,var(--accent)_12%,var(--surface))] shadow-sm"
                      : "border-transparent bg-transparent hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)]"
                  )}
                >
                  <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] text-muted">
                    <Icon name="bot" size={16} />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center justify-between gap-1">
                      <span className="truncate text-[13px] font-medium text-fg">Telegram</span>
                      <span
                        className="h-2 w-2 shrink-0 rounded-full bg-muted"
                        title="Not set up"
                        data-testid="sidebar-dot-telegram"
                      />
                    </div>
                    <p className="truncate text-[11px] text-muted">Not set up</p>
                  </div>
                </button>
              )}
            </div>
          </div>

          {/* WhatsApp Section */}
          <div>
            <div className="flex items-center justify-between px-2 pb-1.5">
              <span className="text-[11px] font-semibold uppercase tracking-wider text-muted">WhatsApp</span>
              {writer && (
                <button
                  type="button"
                  data-testid="btn-add-whatsapp-account"
                  onClick={() => setIsWaWizardOpen(true)}
                  className="text-[11px] font-medium text-accent hover:underline flex items-center gap-1"
                >
                  <span>＋ Add an account</span>
                </button>
              )}
            </div>
            <div className="space-y-1">
              {waGateways.length > 0 ? (
                waGateways.map((g) => {
                  const st = whatsappGatewayStatusView(g, waHealthMap[g.id], myWaLink);
                  const isSelected = activePlatform === "whatsapp" && (selectedWaId === g.id || (!selectedWaId && g.id === waGateways[0].id));
                  const display = g.bot_username || (g.lane === "multi_device" ? "Multi-device" : "Cloud API");
                  return (
                    <button
                      key={g.id}
                      type="button"
                      data-testid={`sidebar-item-whatsapp-${g.id}`}
                      onClick={() => {
                        setUserSelectedPlatform("whatsapp");
                        setSelectedWaId(g.id);
                      }}
                      className={cx(
                        "w-full flex items-center gap-2.5 rounded-lg border p-2.5 text-left transition-colors",
                        isSelected
                          ? "border-line bg-[color-mix(in_oklab,var(--accent)_12%,var(--surface))] shadow-sm"
                          : "border-transparent bg-transparent hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)]"
                      )}
                    >
                      <span
                        className={cx(
                          "flex h-8 w-8 shrink-0 items-center justify-center rounded-md",
                          st.state === "connected" || st.state === "linked"
                            ? "bg-[color-mix(in_oklab,var(--accent)_15%,transparent)] text-accent"
                            : "bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] text-muted"
                        )}
                      >
                        <Icon name="chat" size={16} />
                      </span>
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center justify-between gap-1">
                          <span className="truncate text-[13px] font-medium text-fg">{display}</span>
                          <span
                            className={cx("h-2 w-2 shrink-0 rounded-full", st.dot)}
                            title={st.label}
                            data-testid={`sidebar-dot-whatsapp-${g.id}`}
                          />
                        </div>
                        <p className="truncate text-[11px] text-muted">
                          {agentName(g.agent_id) || st.label}
                        </p>
                      </div>
                    </button>
                  );
                })
              ) : (
                <button
                  type="button"
                  data-testid="sidebar-item-whatsapp"
                  onClick={() => setUserSelectedPlatform("whatsapp")}
                  className={cx(
                    "w-full flex items-center gap-2.5 rounded-lg border p-2.5 text-left transition-colors",
                    activePlatform === "whatsapp"
                      ? "border-line bg-[color-mix(in_oklab,var(--accent)_12%,var(--surface))] shadow-sm"
                      : "border-transparent bg-transparent hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)]"
                  )}
                >
                  <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] text-muted">
                    <Icon name="chat" size={16} />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center justify-between gap-1">
                      <span className="truncate text-[13px] font-medium text-fg">WhatsApp</span>
                      <span
                        className="h-2 w-2 shrink-0 rounded-full bg-muted"
                        title="Not set up"
                        data-testid="sidebar-dot-whatsapp"
                      />
                    </div>
                    <p className="truncate text-[11px] text-muted">Not set up</p>
                  </div>
                </button>
              )}
            </div>
          </div>
        </aside>

        {/* Detail Pane */}
        <div className="min-w-0 flex-1 w-full">
          {loading && tgGateways.length === 0 && waGateways.length === 0 ? (
            <div className="space-y-3">
              {[0, 1, 2].map((i) => (
                <div
                  key={i}
                  className="h-16 animate-pulse rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]"
                />
              ))}
            </div>
          ) : loadError ? (
            <div className="py-6">
              <ErrorState
                illustration={serverErrorSvg}
                title="Couldn't load the gateway"
                detail={loadError.message}
                primaryAction={{ label: "Retry", onClick: load }}
              />
            </div>
          ) : activePlatform === "whatsapp" ? (
            <WhatsAppDetailPane
              wsSlug={targetWsId}
              wsId={wsId}
              writer={writer}
              agents={agents}
              accounts={waGateways}
              currentAccount={currentWaAccount}
              healthMap={waHealthMap}
              link={myWaLink}
              onToast={onToast}
              onAccountsChange={setWaGateways}
              onLinkChange={setMyWaLink}
              onOpenWizard={() => setIsWaWizardOpen(true)}
              onReload={load}
            />
          ) : (
            <TelegramDetailPane
              wsSlug={targetWsId}
              wsId={wsId}
              writer={writer}
              agents={agents}
              accounts={tgGateways}
              currentBot={currentTgBot}
              bindings={bindings}
              myLink={myTgLink}
              onToast={onToast}
              onAccountsChange={setTgGateways}
              onBindingsChange={setBindings}
              onOpenWizard={() => setIsTgWizardOpen(true)}
              onOpenAddBinding={() => setIsAddBindingOpen(true)}
              onOpenPairing={() => setPairingModalPlatform("telegram")}
              onUnpairMine={handleUnpairTg}
              tokenInput={tokenInput}
              setTokenInput={setTokenInput}
              tokenError={tokenError}
              setTokenError={setTokenError}
              handleRotateToken={handleRotateToken}
              handleTest={handleTestTg}
              handleAgentChange={handleTgAgentChange}
              activeTransport={activeTransport}
              handleTransport={handleTgTransportChange}
              webhookUrl={webhookUrl}
              setWebhookUrl={setWebhookUrl}
              webhookError={webhookError}
              setWebhookError={setWebhookError}
              handleSaveWebhook={handleSaveTgWebhook}
              handleToggleEnabled={handleToggleTgEnabled}
              handleDeleteBot={handleDeleteTgBot}
              handleUnlinkBinding={handleUnlink}
              bindBotId={bindBotId}
              setBindBotId={setBindBotId}
              bindAgent={bindAgent}
              setBindAgentId={setBindAgentId}
              bindCommand={bindCommand}
              busyAction={busyAction}
            />
          )}
        </div>
      </div>

      {/* Telegram Connect Wizard Modal */}
      {isTgWizardOpen && (
        <TelegramConnectWizardModal
          wsId={targetWsId}
          agents={agents}
          onClose={() => setIsTgWizardOpen(false)}
          onCreated={(newBot) => {
            setTgGateways((prev) => [...prev, newBot]);
            setSelectedTgId(newBot.id);
            setUserSelectedPlatform("telegram");
            setIsTgWizardOpen(false);
          }}
          onToast={onToast}
        />
      )}

      {/* WhatsApp Connect Wizard Modal */}
      {isWaWizardOpen && (
        <WhatsAppConnectWizardModal
          wsId={targetWsId}
          agents={agents}
          onClose={() => setIsWaWizardOpen(false)}
          onCreated={(newAccount) => {
            setWaGateways((prev) => [...prev, newAccount]);
            setSelectedWaId(newAccount.id);
            setUserSelectedPlatform("whatsapp");
            setIsWaWizardOpen(false);
            void load();
          }}
          onToast={onToast}
        />
      )}

      {/* Create Group Binding Modal */}
      {isAddBindingOpen && (
        <CreateBindingModal
          wsId={targetWsId}
          bots={tgGateways}
          agents={agents}
          defaultBotId={currentTgBot?.id || tgGateways[0]?.id || ""}
          defaultAgentId={agents[0]?.id || ""}
          onClose={() => setIsAddBindingOpen(false)}
          onCreated={(newBinding) => {
            setBindings((prev) => [...prev, newBinding]);
            setIsAddBindingOpen(false);
            onToast("Group bound successfully");
          }}
          onToast={onToast}
        />
      )}

      {/* Member Pairing Modal */}
      {pairingModalPlatform && (
        <PairingModal
          wsSlug={targetWsId}
          platform={pairingModalPlatform}
          onClose={() => setPairingModalPlatform(null)}
          onToast={onToast}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Telegram Detail Pane Subcomponent
// ---------------------------------------------------------------------------

interface TelegramDetailPaneProps {
  wsSlug: string;
  wsId: string;
  writer: boolean;
  agents: ApiAgent[];
  accounts: ApiGatewayConfig[];
  currentBot: ApiGatewayConfig | null;
  bindings: ApiGatewayBinding[];
  myLink: ApiGatewayLink | null;
  onToast: (text: string, kind?: string) => void;
  onAccountsChange: React.Dispatch<React.SetStateAction<ApiGatewayConfig[]>>;
  onBindingsChange: React.Dispatch<React.SetStateAction<ApiGatewayBinding[]>>;
  onOpenWizard: () => void;
  onOpenAddBinding: () => void;
  onOpenPairing: () => void;
  onUnpairMine: () => Promise<void>;
  tokenInput: string;
  setTokenInput: (v: string) => void;
  tokenError: string;
  setTokenError: (v: string) => void;
  handleRotateToken: () => Promise<void>;
  handleTest: () => Promise<void>;
  handleAgentChange: (agentId: string) => Promise<void>;
  activeTransport: GatewayTransport;
  handleTransport: (t: GatewayTransport) => Promise<void>;
  webhookUrl: string;
  setWebhookUrl: (v: string) => void;
  webhookError: string;
  handleSaveWebhook: () => Promise<void>;
  setWebhookError: (v: string) => void;
  handleToggleEnabled: (next: boolean) => Promise<void>;
  handleDeleteBot: () => Promise<void>;
  handleUnlinkBinding: (b: ApiGatewayBinding) => Promise<void>;
  bindBotId: string;
  setBindBotId: (id: string) => void;
  bindAgent: string;
  setBindAgentId: (id: string) => void;
  bindCommand: string;
  busyAction: string | null;
}

function TelegramDetailPane(props: TelegramDetailPaneProps) {
  const {
    writer,
    agents,
    accounts,
    currentBot,
    bindings,
    myLink,
    onToast,
    onOpenWizard,
    onOpenAddBinding,
    onOpenPairing,
    onUnpairMine,
    tokenInput,
    setTokenInput,
    tokenError,
    setTokenError,
    handleRotateToken,
    handleTest,
    handleAgentChange,
    activeTransport,
    handleTransport,
    webhookUrl,
    setWebhookUrl,
    webhookError,
    setWebhookError,
    handleSaveWebhook,
    handleToggleEnabled,
    handleDeleteBot,
    handleUnlinkBinding,
    bindBotId,
    setBindBotId,
    bindAgent,
    setBindAgentId,
    bindCommand,
    busyAction,
  } = props;

  const agentName = (id: string | null | undefined) =>
    agents.find((a) => a.id === id)?.name || id || "";

  const botName = (botId: string) => {
    const b = accounts.find((a) => a.id === botId);
    return b?.bot_username ? `@${b.bot_username}` : b?.token_hint ? `••••${b.token_hint}` : b?.identity || botId;
  };

  const status = telegramGatewayStatusView(currentBot);
  const configured = Boolean(currentBot && (currentBot.token_hint || currentBot.bot_username || currentBot.id));

  return (
    <div className="space-y-6">
      {writer && (
        <section data-testid="gateway-admin-section">
          <div className="flex items-center justify-between mb-2">
            <div>
              <h3 className="text-[15px] font-semibold text-fg">Telegram bot</h3>
              <p className="mt-0.5 text-[12px] text-muted">
                Connect Telegram bot accounts to route direct messages and bound groups into agent sessions.
              </p>
            </div>
            {accounts.length > 0 && (
              <button
                type="button"
                data-testid="btn-add-bot"
                onClick={onOpenWizard}
                className="h-8 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
              >
                ＋ Add a bot
              </button>
            )}
          </div>

          {!configured || !currentBot ? (
            /* Unconfigured state */
            <div className="rounded-md border border-dashed border-line p-6 text-center" data-testid="gateway-unconfigured">
              <div className="mx-auto flex h-10 w-10 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] text-muted">
                <Icon name="bot" size={20} />
              </div>
              <h4 className="mt-3 text-[14px] font-medium text-fg">No Telegram bot connected</h4>
              <p className="mx-auto mt-1 max-w-sm text-[12px] text-muted">
                Connect a bot created with @BotFather to let users and groups interact with agents.
              </p>
              <button
                type="button"
                data-testid="btn-connect-bot"
                onClick={onOpenWizard}
                className="mt-4 inline-flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90"
              >
                Connect a bot
              </button>
            </div>
          ) : (
            /* Configured Telegram bot detail */
            <div className="space-y-3">
              {/* Connection card */}
              <div className="rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] px-4 py-3">
                <div className="flex items-center gap-3">
                  <span
                    className={cx(
                      "flex h-9 w-9 shrink-0 items-center justify-center rounded-md",
                      status.state === "connected"
                        ? "bg-[color-mix(in_oklab,var(--accent)_15%,transparent)] text-accent"
                        : "bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] text-muted"
                    )}
                  >
                    <Icon name="bot" size={16} />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <p className="text-[14px] font-medium text-fg">Telegram gateway</p>
                      <span
                        className={cx(
                          "inline-flex items-center gap-1.5 text-[11px]",
                          status.errored
                            ? "text-danger"
                            : currentBot?.enabled
                            ? "text-[color-mix(in_oklab,var(--success),black_25%)]"
                            : "text-muted"
                        )}
                        data-testid="gateway-status"
                      >
                        <span
                          className={cx(
                            "h-1.5 w-1.5 rounded-full",
                            status.errored ? "bg-danger" : currentBot?.enabled ? "bg-success" : "bg-muted"
                          )}
                        />
                        {status.label}
                      </span>
                      {currentBot?.bot_username ? (
                        <Chip mono>
                          <span data-testid="gateway-username">@{currentBot.bot_username}</span>
                        </Chip>
                      ) : null}
                      {currentBot?.token_hint ? (
                        <Chip mono>
                          <span data-testid="gateway-token-hint">token ••••{currentBot.token_hint}</span>
                        </Chip>
                      ) : null}
                    </div>
                    {currentBot?.status_error ? (
                      <p className="mt-0.5 truncate text-[11px] text-danger" title={currentBot.status_error}>
                        {currentBot.status_error}
                      </p>
                    ) : null}
                  </div>
                  <div className="flex items-center gap-2">
                    <button
                      type="button"
                      data-testid="btn-test-gateway"
                      onClick={() => void handleTest()}
                      disabled={busyAction === "test"}
                      className="h-8 shrink-0 rounded-md border border-line px-2.5 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-50"
                    >
                      {busyAction === "test" ? "Testing…" : "Test connection"}
                    </button>
                    <button
                      type="button"
                      data-testid="btn-delete-bot"
                      onClick={() => void handleDeleteBot()}
                      disabled={busyAction === "delete-bot"}
                      className="h-8 shrink-0 rounded-md border border-line px-2.5 text-[12px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-danger disabled:opacity-50"
                    >
                      {busyAction === "delete-bot" ? "Deleting…" : "Delete"}
                    </button>
                  </div>
                </div>

                <div className="mt-3">
                  <label htmlFor="gateway-token" className="mb-1 block text-[12px] font-medium text-fg2">
                    Rotate bot token
                  </label>
                  <div className="flex gap-2">
                    <input
                      id="gateway-token"
                      type="password"
                      autoComplete="off"
                      data-testid="input-gateway-token"
                      value={tokenInput}
                      onChange={(e) => {
                        setTokenInput(e.target.value);
                        if (tokenError) setTokenError("");
                      }}
                      placeholder={`••••${currentBot.token_hint || "••••"} — paste a new token to rotate`}
                      className="h-9 min-w-0 flex-1 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2.5 font-mono text-[12px] text-fg2 focus:border-accent"
                    />
                    <button
                      type="button"
                      data-testid="btn-rotate-token"
                      onClick={() => void handleRotateToken()}
                      disabled={!tokenInput.trim() || busyAction === "rotate"}
                      className="h-9 shrink-0 rounded-md bg-accent px-3.5 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-40"
                    >
                      {busyAction === "rotate" ? "Saving…" : "Update token"}
                    </button>
                  </div>
                  {tokenError ? (
                    <p className="mt-1 text-[11px] text-danger" data-testid="gateway-token-error">
                      {tokenError}
                    </p>
                  ) : (
                    <p className="mt-1 text-[11px] text-muted">
                      The token is stored encrypted and is never shown again after saving.
                    </p>
                  )}
                </div>
              </div>

              {/* Bound Agent */}
              <div className="rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] px-4 py-3">
                <label htmlFor="gateway-default-agent" className="mb-1 block text-[12px] font-medium text-fg2">
                  Agent this bot speaks for
                </label>
                <p className="mb-2 text-[11px] text-muted">
                  Direct messages sent to this bot will route to this workspace agent.
                </p>
                <select
                  id="gateway-default-agent"
                  data-testid="select-default-agent"
                  className="h-8 w-full rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2 text-[12px] text-fg2 focus:border-accent"
                  value={currentBot.agent_id || ""}
                  onChange={(e) => void handleAgentChange(e.target.value)}
                >
                  <option value="" disabled>Select an agent</option>
                  {agents.map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.name}
                    </option>
                  ))}
                </select>
              </div>

              {/* Transport */}
              <div className="rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] px-4 py-3">
                <p className="mb-1 text-[12px] font-medium text-fg2">Transport</p>
                <p className="mb-2 text-[11px] text-muted">
                  Long-polling works behind NAT; webhook needs a public HTTPS URL.
                </p>
                <Segmented
                  value={activeTransport}
                  onChange={(v: GatewayTransport) => void handleTransport(v)}
                  options={[
                    { id: "long_polling", label: "Long-polling", testid: "seg-long-polling" },
                    { id: "webhook", label: "Webhook", testid: "seg-webhook" },
                  ]}
                />
                {activeTransport === "webhook" ? (
                  <div className="mt-2.5">
                    <div className="flex gap-2">
                      <input
                        type="url"
                        aria-label="Webhook URL"
                        data-testid="input-webhook-url"
                        value={webhookUrl}
                        onChange={(e) => {
                          setWebhookUrl(e.target.value);
                          if (webhookError) setWebhookError("");
                        }}
                        placeholder="https://example.com/api/v1/webhooks/telegram/..."
                        className="h-8 min-w-0 flex-1 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2 font-mono text-[11px] text-fg2 focus:border-accent"
                      />
                      <button
                        type="button"
                        data-testid="btn-save-webhook"
                        onClick={() => void handleSaveWebhook()}
                        disabled={busyAction === "webhook"}
                        className="h-8 shrink-0 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-50"
                      >
                        {busyAction === "webhook"
                          ? "Saving…"
                          : currentBot?.transport === "webhook"
                          ? "Save URL"
                          : "Save & switch"}
                      </button>
                    </div>
                    {webhookError ? (
                      <p className="mt-1 text-[11px] text-danger" data-testid="webhook-error">
                        {webhookError}
                      </p>
                    ) : null}
                  </div>
                ) : null}
              </div>

              {/* Enable / Disable */}
              <div className="flex items-center justify-between rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] px-4 py-3">
                <div>
                  <p className="text-[14px] font-medium text-fg">Gateway enabled</p>
                  <p className="mt-0.5 text-[11px] text-muted">
                    Disabling stops ingestion and delivery without deleting configuration or bindings.
                  </p>
                </div>
                <Toggle
                  on={Boolean(currentBot?.enabled)}
                  label="Enable gateway"
                  onChange={(next: boolean) => void handleToggleEnabled(next)}
                />
              </div>
            </div>
          )}

          {/* Group Bindings Section */}
          <div className="mt-6">
            <div className="flex items-center justify-between">
              <div>
                <h3 className="text-[15px] font-semibold text-fg">Group bindings</h3>
                <p className="mt-0.5 text-[12px] text-muted">
                  Bind specific Telegram groups to agents. Add the bot to a group, then send the bind command there.
                </p>
              </div>
              {accounts.length > 0 && (
                <button
                  type="button"
                  data-testid="btn-open-add-binding"
                  onClick={onOpenAddBinding}
                  className="h-8 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
                >
                  ＋ Add group binding
                </button>
              )}
            </div>

            {accounts.length > 1 && (
              <div className="mt-2.5 flex items-start gap-2 rounded-md border border-[color-mix(in_oklab,var(--warn)_45%,transparent)] bg-[color-mix(in_oklab,var(--warn)_6%,transparent)] px-3.5 py-2.5">
                <span className="mt-0.5 text-[color-mix(in_oklab,var(--warn),black_38%)]">
                  <Icon name="alert" size={14} />
                </span>
                <p className="text-[12px] leading-4 text-[color-mix(in_oklab,var(--warn),black_38%)]">
                  Multiple bots are configured. When adding a bot to a group, use the explicit <code className="font-mono text-[11px]">/bind@&lt;bot_username&gt; &lt;agent_slug&gt;</code> syntax so the intended bot accepts the command.
                </p>
              </div>
            )}

            {agents.length > 0 && accounts.length > 0 ? (
              <div className="mt-2.5 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] px-4 py-3">
                <label htmlFor="gateway-bind-agent" className="mb-1 block text-[12px] font-medium text-fg2">
                  Bind command helper
                </label>
                <div className="flex flex-wrap items-center gap-2">
                  {accounts.length > 1 && (
                    <select
                      aria-label="Bot for the bind command"
                      data-testid="select-bind-bot"
                      className="h-8 shrink-0 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2 text-[12px] text-fg2 focus:border-accent"
                      value={bindBotId || currentBot?.id || accounts[0]?.id}
                      onChange={(e) => setBindBotId(e.target.value)}
                    >
                      {accounts.map((b) => (
                        <option key={b.id} value={b.id}>
                          {b.bot_username ? `@${b.bot_username}` : b.identity || b.id}
                        </option>
                      ))}
                    </select>
                  )}
                  <select
                    id="gateway-bind-agent"
                    aria-label="Agent for the bind command"
                    data-testid="select-bind-agent"
                    className="h-8 shrink-0 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2 text-[12px] text-fg2 focus:border-accent"
                    value={bindAgent}
                    onChange={(e) => setBindAgentId(e.target.value)}
                  >
                    {agents.map((a) => (
                      <option key={a.id} value={a.id}>
                        {a.name}
                      </option>
                    ))}
                  </select>
                  <code
                    className="min-w-0 flex-1 truncate rounded-[6px] border border-line bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] px-2 py-1 font-mono text-[11px] text-fg2"
                    data-testid="bind-command"
                  >
                    {bindCommand}
                  </code>
                  <button
                    type="button"
                    aria-label="Copy bind command"
                    data-testid="btn-copy-bind"
                    onClick={() => copyText(bindCommand, onToast, "Bind command copied to clipboard")}
                    className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2"
                  >
                    <Icon name="copy" size={13} />
                  </button>
                </div>
                <p className="mt-1 text-[11px] text-muted">
                  Copy and run this command inside your Telegram group chat.
                </p>
              </div>
            ) : null}

            {bindings.length > 0 ? (
              <ul className="mt-2.5 divide-y divide-[var(--border-soft)] rounded-md border border-line">
                {bindings.map((b) => (
                  <li
                    key={b.id}
                    className="flex items-center gap-3 px-4 py-2.5"
                    data-od-id={"binding-" + b.id}
                    data-testid={"binding-" + b.id}
                  >
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <p className="text-[13px] font-medium text-fg">
                          {b.chat_title || "Group"}
                        </p>
                        <Chip mono>{b.platform_chat_id}</Chip>
                        <Chip mono>
                          <span data-testid={`binding-bot-${b.id}`}>{botName(b.gateway_id)}</span>
                        </Chip>
                        <Chip mono>{agentName(b.agent_id)}</Chip>
                      </div>
                    </div>
                    <button
                      type="button"
                      data-testid={"btn-unlink-" + b.id}
                      onClick={() => void handleUnlinkBinding(b)}
                      disabled={busyAction === "unlink-" + b.id}
                      className="h-7 shrink-0 rounded-[6px] px-2 text-[11px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-danger disabled:opacity-50"
                    >
                      {busyAction === "unlink-" + b.id ? "Unlinking…" : "Unlink"}
                    </button>
                  </li>
                ))}
              </ul>
            ) : (
              <div className="mt-2.5 rounded-md border border-dashed border-line px-4 py-5 text-center" data-testid="bindings-empty">
                <p className="text-[13px] font-medium text-fg">No groups bound</p>
                <p className="mx-auto mt-1 max-w-sm text-[12px] text-muted">
                  Bound groups appear here with their owning bot, agent, and unlink control.
                </p>
              </div>
            )}
          </div>
        </section>
      )}

      {/* Member pairing flow */}
      <section data-testid="gateway-pairing-section">
        <h3 className="text-[15px] font-semibold text-fg">Your Telegram link</h3>
        <p className="mt-0.5 text-[12px] text-muted">
          Link your Telegram account to talk to workspace agents from Telegram. Runs execute under your
          identity and permissions.
        </p>
        {myLink ? (
          <div className="mt-2.5 flex items-center gap-3 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] px-4 py-3">
            <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-[color-mix(in_oklab,var(--accent)_15%,transparent)] text-accent">
              <Icon name="at" size={16} />
            </span>
            <div className="min-w-0 flex-1">
              <p className="text-[14px] font-medium text-fg" data-testid="gateway-link-identity">
                {myLink.display_name || myLink.username || myLink.platform_user_id}
                {myLink.username ? <span className="font-mono text-[12px] text-muted"> @{myLink.username}</span> : null}
              </p>
              <p className="font-mono text-[11px] text-muted">
                Linked {myLink.linked_at ? new Date(myLink.linked_at).toLocaleDateString() : ""}
              </p>
            </div>
            <button
              type="button"
              data-testid="btn-unpair"
              onClick={() => void onUnpairMine()}
              disabled={busyAction === "unpair-tg"}
              className="h-8 shrink-0 rounded-md px-2.5 text-[12px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-danger disabled:opacity-50"
            >
              {busyAction === "unpair-tg" ? "Unlinking…" : "Unlink"}
            </button>
          </div>
        ) : (
          <div className="mt-2.5 flex items-center justify-between rounded-md border border-dashed border-line px-4 py-3.5">
            <p className="text-[13px] text-fg2">No Telegram account linked yet.</p>
            <button
              type="button"
              data-testid="btn-open-pairing"
              onClick={onOpenPairing}
              className="h-8 shrink-0 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
            >
              Link Telegram account
            </button>
          </div>
        )}
      </section>
    </div>
  );
}

// ---------------------------------------------------------------------------
// WhatsApp Detail Pane Subcomponent
// ---------------------------------------------------------------------------

interface WhatsAppDetailPaneProps {
  wsSlug: string;
  wsId: string;
  writer: boolean;
  agents: ApiAgent[];
  accounts: ApiWhatsAppGatewayConfig[];
  currentAccount: ApiWhatsAppGatewayConfig | null;
  healthMap: Record<string, ApiWhatsAppHealth>;
  link: ApiGatewayLink | null;
  onToast: (text: string, kind?: string) => void;
  onAccountsChange: React.Dispatch<React.SetStateAction<ApiWhatsAppGatewayConfig[]>>;
  onLinkChange: (link: ApiGatewayLink | null) => void;
  onOpenWizard: () => void;
  onReload: () => Promise<void>;
}

function WhatsAppDetailPane({
  wsSlug,
  wsId,
  writer,
  agents,
  accounts,
  currentAccount,
  healthMap,
  link,
  onToast,
  onAccountsChange,
  onLinkChange,
  onOpenWizard,
  onReload,
}: WhatsAppDetailPaneProps) {
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [creds, setCreds] = useState({ access_token: "", phone_number_id: "", app_secret: "", verify_token: "" });
  const [credError, setCredError] = useState("");
  const [pairing, setPairing] = useState<ApiWhatsAppPairing | null>(null);
  const [pairingOpen, setPairingOpen] = useState(false);
  const [pairPhone, setPairPhone] = useState("");

  const cbRef = useRef({ onToast, onAccountsChange, onLinkChange, onReload });
  useEffect(() => {
    cbRef.current = { onToast, onAccountsChange, onLinkChange, onReload };
  });
  const hadSessionRef = useRef(false);

  const lane: GatewayLane | "" = currentAccount?.lane || "";
  const configured = Boolean(currentAccount && (currentAccount.lane || currentAccount.has_credentials || currentAccount.id));
  const health = currentAccount ? healthMap[currentAccount.id] : null;
  const status = whatsappGatewayStatusView(currentAccount, health, link);

  const agentName = (id: string | null | undefined) => agents.find((a) => a.id === id)?.name || id || "";
  const deviceConnected = lane === "multi_device" && health?.status === "ok" && !pairing;
  const webhookCallbackUrl = currentAccount ? `${window.location.origin}/api/v1/webhooks/whatsapp/${currentAccount.id}` : "";

  const apiErrorToast = (err: unknown, fallback: string) => {
    if (err instanceof ApiError && err.details && err.details.length > 0) {
      onToast(`${err.message}: ${err.details.map((d) => d.message).join("; ")}`, "danger");
      return;
    }
    onToast(formatApiError(err, fallback), "danger");
  };

  const refreshPairing = useCallback(async () => {
    if (!currentAccount) return;
    try {
      const res = await api.gateways.whatsapp.pairing.status(wsSlug, currentAccount.id);
      if (res.status === "connected") {
        const announce = hadSessionRef.current;
        hadSessionRef.current = false;
        setPairing(null);
        if (announce) {
          cbRef.current.onToast("WhatsApp device connected");
          void cbRef.current.onReload();
        }
      } else if (res.status === "waiting") {
        hadSessionRef.current = true;
        setPairing(res);
      } else {
        hadSessionRef.current = false;
        setPairing(null);
      }
    } catch {
      // Transient poll errors stay silent
    }
  }, [wsSlug, currentAccount]);

  useEffect(() => {
    if (!writer || lane !== "multi_device" || !currentAccount) return;
    const t = setTimeout(() => void refreshPairing(), 0);
    return () => clearTimeout(t);
  }, [writer, lane, currentAccount, refreshPairing]);

  useEffect(() => {
    if (lane !== "multi_device" || !pairing || !currentAccount) return;
    const h = setInterval(() => void refreshPairing(), 3000);
    return () => clearInterval(h);
  }, [lane, pairing, currentAccount, refreshPairing]);

  const startPairing = async (regenerate: boolean, phone?: string) => {
    if (!currentAccount) return;
    setBusyAction(regenerate ? "regenerate" : "pairing");
    try {
      if (!currentAccount.enabled) {
        await api.gateways.whatsapp.enable(wsSlug, currentAccount.id);
        onAccountsChange((prev) =>
          prev.map((g) => (g.id === currentAccount.id ? { ...g, enabled: true } : g))
        );
      }
      const res = regenerate
        ? await api.gateways.whatsapp.pairing.regenerate(wsSlug, currentAccount.id)
        : phone
        ? await api.gateways.whatsapp.pairing.start(wsSlug, currentAccount.id, phone)
        : await api.gateways.whatsapp.pairing.start(wsSlug, currentAccount.id);
      hadSessionRef.current = true;
      setPairing(res);
      void onReload();
    } catch (err: unknown) {
      apiErrorToast(err, "Failed to start pairing");
    } finally {
      setBusyAction(null);
    }
  };

  const startPairingWithCode = async () => {
    const phone = pairPhone.trim();
    if (!phone) return;
    await startPairing(false, phone);
  };

  const handleLogout = async () => {
    if (!currentAccount) return;
    setBusyAction("logout");
    try {
      await api.gateways.whatsapp.pairing.logout(wsSlug, currentAccount.id);
      hadSessionRef.current = false;
      setPairing(null);
      onToast("WhatsApp device logged out");
      void onReload();
    } catch (err: unknown) {
      apiErrorToast(err, "Failed to log out the device");
    } finally {
      setBusyAction(null);
    }
  };

  const handleAgentChange = async (agentId: string) => {
    if (!currentAccount) return;
    try {
      const res = await api.gateways.whatsapp.update(wsSlug, currentAccount.id, {
        agent_id: agentId,
      });
      onAccountsChange((prev) => prev.map((g) => (g.id === res.id ? res : g)));
      onToast(
        agentId
          ? `${agentName(agentId) || "Agent"} now answers direct messages for this WhatsApp account`
          : "Agent updated"
      );
    } catch (err: unknown) {
      apiErrorToast(err, "Failed to update the agent");
    }
  };

  const handleSaveCloud = async () => {
    if (!currentAccount) return;
    const trimmed = {
      access_token: creds.access_token.trim(),
      phone_number_id: creds.phone_number_id.trim(),
      app_secret: creds.app_secret.trim(),
      verify_token: creds.verify_token.trim(),
    };
    if (!currentAccount.has_credentials) {
      const missing = (Object.keys(trimmed) as Array<keyof typeof trimmed>).filter((k) => !trimmed[k]);
      if (missing.length > 0) {
        setCredError("All four credential fields are required to connect the gateway");
        return;
      }
    }
    setCredError("");
    setBusyAction("save");
    try {
      const body: UpdateWhatsAppGatewayPayload = {};
      if (trimmed.access_token) body.access_token = trimmed.access_token;
      if (trimmed.phone_number_id) body.phone_number_id = trimmed.phone_number_id;
      if (trimmed.app_secret) body.app_secret = trimmed.app_secret;
      if (trimmed.verify_token) body.verify_token = trimmed.verify_token;
      const res = await api.gateways.whatsapp.update(wsSlug, currentAccount.id, body);
      onAccountsChange((prev) => prev.map((g) => (g.id === res.id ? res : g)));
      setCreds({ access_token: "", phone_number_id: "", app_secret: "", verify_token: "" });
      onToast("WhatsApp gateway updated");
      void onReload();
    } catch (err: unknown) {
      apiErrorToast(err, "Failed to save the WhatsApp gateway");
    } finally {
      setBusyAction(null);
    }
  };

  const handleToggleEnabled = async (next: boolean) => {
    if (!currentAccount) return;
    setBusyAction("toggle");
    try {
      if (next) {
        await api.gateways.whatsapp.enable(wsSlug, currentAccount.id);
      } else {
        await api.gateways.whatsapp.disable(wsSlug, currentAccount.id);
      }
      onAccountsChange((prev) =>
        prev.map((g) => (g.id === currentAccount.id ? { ...g, enabled: next } : g))
      );
      onToast(
        next
          ? "WhatsApp gateway enabled"
          : "WhatsApp gateway disabled — configuration is preserved"
      );
    } catch (err: unknown) {
      apiErrorToast(err, "Failed to update the WhatsApp gateway");
    } finally {
      setBusyAction(null);
    }
  };

  const handleDeleteAccount = async () => {
    if (!currentAccount) return;
    setBusyAction("delete-wa");
    try {
      await api.gateways.whatsapp.delete(wsSlug, currentAccount.id);
      onAccountsChange((prev) => prev.filter((g) => g.id !== currentAccount.id));
      onToast("WhatsApp account deleted");
      void onReload();
    } catch (err: unknown) {
      apiErrorToast(err, "Failed to delete WhatsApp account");
    } finally {
      setBusyAction(null);
    }
  };

  const handleUnpair = async () => {
    setBusyAction("unpair");
    try {
      await api.gateways.whatsapp.links.removeMine(wsSlug);
      onLinkChange(null);
      onToast("WhatsApp account unlinked");
    } catch (err: unknown) {
      apiErrorToast(err, "Failed to unlink your WhatsApp account");
    } finally {
      setBusyAction(null);
    }
  };

  return (
    <div className="space-y-6">
      {writer && (
        <section data-testid="wa-gateway-admin-section">
          <div className="flex items-center justify-between mb-2">
            <div>
              <h3 className="text-[15px] font-semibold text-fg">WhatsApp</h3>
              <p className="mt-0.5 text-[12px] text-muted">
                Connect WhatsApp accounts to route direct messages into agent sessions.
              </p>
            </div>
            {accounts.length > 0 && (
              <button
                type="button"
                data-testid="btn-add-wa-account"
                onClick={onOpenWizard}
                className="h-8 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
              >
                ＋ Add an account
              </button>
            )}
          </div>

          {!configured || !currentAccount ? (
            /* Unconfigured state */
            <div className="rounded-md border border-dashed border-line p-6 text-center" data-testid="wa-unconfigured">
              <div className="mx-auto flex h-10 w-10 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] text-muted">
                <Icon name="chat" size={20} />
              </div>
              <h4 className="mt-3 text-[14px] font-medium text-fg">No WhatsApp account connected</h4>
              <p className="mx-auto mt-1 max-w-sm text-[12px] text-muted">
                Connect an Official Cloud API or Multi-device personal account to route turns to agents.
              </p>
              <button
                type="button"
                data-testid="btn-connect-wa"
                onClick={onOpenWizard}
                className="mt-4 inline-flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90"
              >
                Connect WhatsApp account
              </button>
            </div>
          ) : (
            /* Configured detail */
            <div className="space-y-3">
              {/* Account card */}
              <div className="rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] px-4 py-3">
                <div className="flex items-center gap-3">
                  <span
                    className={cx(
                      "flex h-9 w-9 shrink-0 items-center justify-center rounded-md",
                      status.state === "connected" || status.state === "linked"
                        ? "bg-[color-mix(in_oklab,var(--accent)_15%,transparent)] text-accent"
                        : "bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] text-muted"
                    )}
                  >
                    <Icon name="chat" size={16} />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <p className="text-[14px] font-medium text-fg">WhatsApp gateway</p>
                      <span
                        className={cx(
                          "inline-flex items-center gap-1.5 text-[11px]",
                          status.errored
                            ? "text-danger"
                            : status.state === "connected" || status.state === "linked"
                            ? "text-[color-mix(in_oklab,var(--success),black_25%)]"
                            : "text-muted"
                        )}
                        data-testid="wa-gateway-status"
                      >
                        <span className={cx("h-1.5 w-1.5 rounded-full", status.dot)} />
                        {status.label}
                      </span>
                      {currentAccount.bot_username ? (
                        <Chip mono>
                          <span data-testid="wa-gateway-username">{currentAccount.bot_username}</span>
                        </Chip>
                      ) : null}
                      <Chip mono>{currentAccount.lane === "multi_device" ? "Multi-device" : "Cloud API"}</Chip>
                      {currentAccount.has_credentials ? <Chip>credentials stored</Chip> : null}
                    </div>
                    {status.errored && health?.detail ? (
                      <p className="mt-0.5 truncate text-[11px] text-danger" title={health.detail}>
                        {health.detail}
                      </p>
                    ) : null}
                  </div>
                  <div className="flex items-center gap-2">
                    <Toggle
                      on={Boolean(currentAccount.enabled)}
                      label="Enable WhatsApp gateway"
                      onChange={(next: boolean) => void handleToggleEnabled(next)}
                    />
                    <button
                      type="button"
                      data-testid="wa-btn-delete"
                      onClick={() => void handleDeleteAccount()}
                      disabled={busyAction === "delete-wa"}
                      className="h-8 shrink-0 rounded-md border border-line px-2.5 text-[12px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-danger disabled:opacity-50"
                    >
                      {busyAction === "delete-wa" ? "Deleting…" : "Delete"}
                    </button>
                  </div>
                </div>

                {/* Bound Agent */}
                <div className="mt-3">
                  <label htmlFor="wa-default-agent" className="mb-1 block text-[12px] font-medium text-fg2">
                    Agent this account speaks for
                  </label>
                  <select
                    id="wa-default-agent"
                    data-testid="wa-select-default-agent"
                    className="h-8 w-full rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2 text-[12px] text-fg2 focus:border-accent"
                    value={currentAccount.agent_id || ""}
                    onChange={(e) => void handleAgentChange(e.target.value)}
                  >
                    <option value="" disabled>Select an agent</option>
                    {agents.map((a) => (
                      <option key={a.id} value={a.id}>
                        {a.name}
                      </option>
                    ))}
                  </select>
                </div>
              </div>

              {/* Cloud lane form */}
              {lane === "cloud_api" && (
                <div
                  className="rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] px-4 py-3"
                  data-testid="wa-cloud-form"
                >
                  <p className="text-[12px] font-medium text-fg2">Cloud API credentials</p>
                  <p className="mb-2.5 text-[11px] text-muted">
                    Stored encrypted and never shown again after saving. Leave a field empty to keep its stored value.
                  </p>
                  <div className="space-y-2.5">
                    <div>
                      <label htmlFor="wa-access-token" className="mb-1 block text-[12px] font-medium text-fg2">
                        Access token
                      </label>
                      <input
                        id="wa-access-token"
                        type="password"
                        autoComplete="off"
                        data-testid="wa-input-access-token"
                        value={creds.access_token}
                        onChange={(e) => setCreds((c) => ({ ...c, access_token: e.target.value }))}
                        placeholder={currentAccount.has_credentials ? "•••••••• — stored" : "Permanent access token"}
                        className={waInputCls}
                      />
                    </div>
                    <div>
                      <label htmlFor="wa-phone-number-id" className="mb-1 block text-[12px] font-medium text-fg2">
                        Phone number ID
                      </label>
                      <input
                        id="wa-phone-number-id"
                        type="text"
                        autoComplete="off"
                        data-testid="wa-input-phone-number-id"
                        value={creds.phone_number_id}
                        onChange={(e) => setCreds((c) => ({ ...c, phone_number_id: e.target.value }))}
                        placeholder={currentAccount.has_credentials ? "•••••••• — stored" : "123456789012345"}
                        className={waInputCls}
                      />
                    </div>
                    <div>
                      <label htmlFor="wa-app-secret" className="mb-1 block text-[12px] font-medium text-fg2">
                        App secret
                      </label>
                      <input
                        id="wa-app-secret"
                        type="password"
                        autoComplete="off"
                        data-testid="wa-input-app-secret"
                        value={creds.app_secret}
                        onChange={(e) => setCreds((c) => ({ ...c, app_secret: e.target.value }))}
                        placeholder={currentAccount.has_credentials ? "•••••••• — stored" : "From App Settings → Basic"}
                        className={waInputCls}
                      />
                    </div>
                    <div>
                      <label htmlFor="wa-verify-token" className="mb-1 block text-[12px] font-medium text-fg2">
                        Verify token
                      </label>
                      <input
                        id="wa-verify-token"
                        type="password"
                        autoComplete="off"
                        data-testid="wa-input-verify-token"
                        value={creds.verify_token}
                        onChange={(e) => setCreds((c) => ({ ...c, verify_token: e.target.value }))}
                        placeholder={currentAccount.has_credentials ? "•••••••• — stored" : "Verify token"}
                        className={waInputCls}
                      />
                    </div>
                  </div>
                  {credError ? (
                    <p className="mt-2 text-[11px] text-danger" data-testid="wa-credentials-error">
                      {credError}
                    </p>
                  ) : null}

                  {/* Webhook */}
                  <div className="mt-3 rounded-md border border-line px-3 py-2.5" data-testid="wa-webhook-block">
                    <p className="text-[12px] font-medium text-fg2">Webhook (paste in Meta dashboard)</p>
                    <div className="mt-2 flex items-center gap-2">
                      <code
                        className="min-w-0 flex-1 truncate rounded-[6px] border border-line bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] px-2 py-1 font-mono text-[11px] text-fg2"
                        data-testid="wa-webhook-url"
                      >
                        {webhookCallbackUrl}
                      </code>
                      <button
                        type="button"
                        aria-label="Copy webhook callback URL"
                        data-testid="wa-btn-copy-webhook-url"
                        onClick={() => copyText(webhookCallbackUrl, onToast, "Webhook URL copied to clipboard")}
                        className={waIconBtnCls}
                      >
                        <Icon name="copy" size={13} />
                      </button>
                    </div>
                  </div>

                  <div className="mt-3 flex justify-end">
                    <button
                      type="button"
                      data-testid="wa-btn-save"
                      onClick={() => void handleSaveCloud()}
                      disabled={busyAction === "save"}
                      className="h-9 rounded-md bg-accent px-3.5 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-40"
                    >
                      {busyAction === "save" ? "Saving…" : "Save credentials"}
                    </button>
                  </div>
                </div>
              )}

              {/* Multi-device lane */}
              {lane === "multi_device" && (
                <div
                  className="rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] px-4 py-3"
                  data-testid="wa-md-card"
                >
                  <div
                    className="flex items-start gap-2 rounded-md border border-[color-mix(in_oklab,var(--warn)_45%,transparent)] px-3.5 py-2.5"
                    data-testid="wa-md-warning"
                  >
                    <span className="mt-0.5 text-[color-mix(in_oklab,var(--warn),black_38%)]">
                      <Icon name="alert" size={14} />
                    </span>
                    <p className="text-[12px] leading-4 text-[color-mix(in_oklab,var(--warn),black_38%)]">
                      Uses an unofficial protocol. Meta may ban the account. Recommended for personal/test numbers only.
                    </p>
                  </div>

                  {deviceConnected ? (
                    <div className="mt-3">
                      <p
                        className="flex items-center gap-1.5 text-[13px] text-[color-mix(in_oklab,var(--success),black_25%)]"
                        data-testid="wa-pairing-status"
                      >
                        <span className="h-1.5 w-1.5 rounded-full bg-success" />
                        Connected
                      </p>
                      <button
                        type="button"
                        data-testid="wa-btn-logout"
                        onClick={() => void handleLogout()}
                        disabled={busyAction === "logout"}
                        className="mt-2.5 h-8 rounded-md border border-line px-3 text-[12px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-danger disabled:opacity-50"
                      >
                        {busyAction === "logout" ? "Logging out…" : "Log out device"}
                      </button>
                    </div>
                  ) : pairing ? (
                    <div className="mt-3 flex flex-wrap items-start gap-4">
                      {pairing.qr_data_url ? (
                        <img
                          src={pairing.qr_data_url}
                          alt="WhatsApp pairing QR code"
                          data-testid="wa-qr-image"
                          className="h-40 w-40 rounded-md border border-line bg-white p-1.5"
                        />
                      ) : pairing.qr ? (
                        <code
                          className="block w-40 break-all rounded-md border border-line p-2 font-mono text-[10px] leading-3 text-muted"
                          data-testid="wa-qr-payload"
                        >
                          {pairing.qr}
                        </code>
                      ) : null}
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <p className="text-[12px] font-medium text-fg2">Pair code</p>
                          <Chip mono>
                            <span data-testid="wa-pair-code">{pairing.pair_code}</span>
                          </Chip>
                        </div>
                        <p className="mt-1 text-[11px] leading-4 text-muted">
                          On the phone: Settings → Linked devices → Link a device. Scan the QR or enter the pair code.
                        </p>
                        <div className="mt-2.5 flex flex-wrap items-center gap-2.5">
                          <button
                            type="button"
                            data-testid="wa-btn-regenerate"
                            onClick={() => void startPairing(true)}
                            disabled={busyAction === "regenerate"}
                            className="h-8 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-50"
                          >
                            {busyAction === "regenerate" ? "Regenerating…" : "Regenerate"}
                          </button>
                          <p className="flex items-center gap-1.5 text-[12px] text-muted" data-testid="wa-pairing-status">
                            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-[color-mix(in_oklab,var(--warn),black_38%)]" />
                            Waiting for scan
                          </p>
                        </div>
                      </div>
                    </div>
                  ) : (
                    <div className="mt-3 space-y-2">
                      <div className="flex items-center justify-between rounded-md border border-dashed border-line px-3.5 py-3">
                        <p className="text-[12px] text-muted">No device linked — start pairing to get a QR code.</p>
                        <button
                          type="button"
                          data-testid="wa-btn-start-pairing"
                          onClick={() => void startPairing(false)}
                          disabled={busyAction === "pairing"}
                          className="h-8 shrink-0 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-50"
                        >
                          {busyAction === "pairing" ? "Starting…" : "Start pairing"}
                        </button>
                      </div>
                      <div
                        className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-dashed border-line px-3.5 py-3"
                        data-testid="wa-md-paircode-row"
                      >
                        <div className="flex min-w-0 flex-1 items-center gap-2">
                          <label htmlFor="wa-pair-phone" className="shrink-0 text-[12px] text-muted">
                            Pair with a code
                          </label>
                          <input
                            id="wa-pair-phone"
                            type="tel"
                            autoComplete="off"
                            data-testid="wa-input-pair-phone"
                            value={pairPhone}
                            onChange={(e) => setPairPhone(e.target.value)}
                            placeholder="Account number, e.g. 6281234567890"
                            className={waInputCls}
                          />
                        </div>
                        <button
                          type="button"
                          data-testid="wa-btn-pair-code"
                          onClick={() => void startPairingWithCode()}
                          disabled={busyAction === "pairing" || pairPhone.trim() === ""}
                          className="h-8 shrink-0 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-50"
                        >
                          {busyAction === "pairing" ? "Starting…" : "Pair with code"}
                        </button>
                      </div>
                    </div>
                  )}
                </div>
              )}
            </div>
          )}
        </section>
      )}

      {/* Member pairing flow */}
      <section data-testid="wa-pairing-section">
        <h3 className="text-[15px] font-semibold text-fg">Your WhatsApp link</h3>
        <p className="mt-0.5 text-[12px] text-muted">
          Link your WhatsApp account to talk to workspace agents from WhatsApp. Runs execute under your
          identity and permissions.
        </p>
        {link ? (
          <div className="mt-2.5 flex items-center gap-3 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] px-4 py-3">
            <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-[color-mix(in_oklab,var(--accent)_15%,transparent)] text-accent">
              <Icon name="chat" size={16} />
            </span>
            <div className="min-w-0 flex-1">
              <p className="text-[14px] font-medium text-fg" data-testid="wa-link-identity">
                {maskWhatsAppId(link.platform_user_id) || link.platform_user_id}
                {link.display_name ? (
                  <span className="text-[12px] font-normal text-muted"> {link.display_name}</span>
                ) : null}
              </p>
              <p className="font-mono text-[11px] text-muted">
                Linked {link.linked_at ? new Date(link.linked_at).toLocaleDateString() : ""}
              </p>
            </div>
            <button
              type="button"
              data-testid="wa-btn-unpair"
              onClick={() => void handleUnpair()}
              disabled={busyAction === "unpair"}
              className="h-8 shrink-0 rounded-md px-2.5 text-[12px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-danger disabled:opacity-50"
            >
              {busyAction === "unpair" ? "Unlinking…" : "Unlink"}
            </button>
          </div>
        ) : (
          <div className="mt-2.5 flex items-center justify-between rounded-md border border-dashed border-line px-4 py-3.5">
            <p className="text-[13px] text-fg2">No WhatsApp account linked yet.</p>
            <button
              type="button"
              data-testid="wa-btn-open-pairing"
              onClick={() => setPairingOpen(true)}
              className="h-8 shrink-0 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
            >
              Link WhatsApp account
            </button>
          </div>
        )}
      </section>

      {pairingOpen ? (
        <PairingModal
          wsSlug={wsSlug}
          platform="whatsapp"
          onClose={() => setPairingOpen(false)}
          onToast={onToast}
        />
      ) : null}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Telegram Connect Wizard Modal
// ---------------------------------------------------------------------------

interface TelegramConnectWizardModalProps {
  wsId: string;
  agents: ApiAgent[];
  onClose: () => void;
  onCreated: (gw: ApiGatewayConfig) => void;
  onToast: (text: string, kind?: string) => void;
}

function TelegramConnectWizardModal({
  wsId,
  agents,
  onClose,
  onCreated,
  onToast,
}: TelegramConnectWizardModalProps) {
  const [token, setToken] = useState("");
  const [agentId, setAgentId] = useState(agents[0]?.id || "");
  const [transport, setTransport] = useState<GatewayTransport>("long_polling");
  const [webhookUrl, setWebhookUrl] = useState("");
  const [tokenError, setTokenError] = useState("");
  const [agentError, setAgentError] = useState("");
  const [webhookError, setWebhookError] = useState("");
  const [busy, setBusy] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    let hasErr = false;
    if (!token.trim()) {
      setTokenError("A bot token is required");
      hasErr = true;
    } else {
      setTokenError("");
    }
    if (!agentId) {
      setAgentError("A bound agent is required");
      hasErr = true;
    } else {
      setAgentError("");
    }
    if (transport === "webhook") {
      if (!webhookUrl.trim() || !webhookUrl.startsWith("https://")) {
        setWebhookError("A valid https:// webhook URL is required");
        hasErr = true;
      } else {
        setWebhookError("");
      }
    }

    if (hasErr) return;

    setBusy(true);
    try {
      const res = await api.gateways.telegram.create(wsId, {
        token: token.trim(),
        agent_id: agentId,
        transport,
        webhook_url: transport === "webhook" ? webhookUrl.trim() : undefined,
      });
      onToast(
        res.bot_username
          ? `Bot connected as @${res.bot_username}`
          : "Bot connected"
      );
      onCreated(res);
    } catch (err: unknown) {
      if (err instanceof ApiError && err.details?.length) {
        for (const d of err.details) {
          if (d.field === "token") setTokenError(d.message);
          if (d.field === "agent_id") setAgentError(d.message);
          if (d.field === "webhook_url") setWebhookError(d.message);
        }
      }
      onToast(formatApiError(err, "Failed to connect bot"), "danger");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      title="Connect Telegram bot"
      onClose={onClose}
      odId="modal-telegram-wizard"
      data-testid="modal-telegram-wizard"
      footer={
        <>
          <button
            type="button"
            data-testid="btn-cancel-telegram-wizard"
            onClick={onClose}
            className="flex h-9 items-center rounded-md px-3 text-[13px] font-medium text-muted transition-colors hover:text-fg"
          >
            Cancel
          </button>
          <button
            type="submit"
            form="telegram-wizard-form"
            data-testid="btn-submit-telegram-wizard"
            disabled={busy}
            className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-40"
          >
            {busy ? "Connecting…" : "Connect bot"}
          </button>
        </>
      }
    >
      <form id="telegram-wizard-form" onSubmit={handleSubmit} className="p-5 space-y-4">
        <div>
          <label htmlFor="wizard-tg-token" className="mb-1 block text-[12px] font-medium text-fg2">
            Bot Token <span className="text-danger">*</span>
          </label>
          <input
            id="wizard-tg-token"
            type="password"
            autoComplete="off"
            data-testid="wizard-input-telegram-token"
            value={token}
            onChange={(e) => {
              setToken(e.target.value);
              if (tokenError) setTokenError("");
            }}
            placeholder="Paste token from @BotFather"
            className="h-9 w-full rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2.5 font-mono text-[12px] text-fg2 focus:border-accent"
          />
          {tokenError ? (
            <p className="mt-1 text-[11px] text-danger" data-testid="wizard-token-error">{tokenError}</p>
          ) : (
            <p className="mt-1 text-[11px] text-muted">Obtained from BotFather after creating your bot.</p>
          )}
        </div>

        <div>
          <label htmlFor="wizard-tg-agent" className="mb-1 block text-[12px] font-medium text-fg2">
            Bound Agent <span className="text-danger">*</span>
          </label>
          <select
            id="wizard-tg-agent"
            data-testid="wizard-select-telegram-agent"
            value={agentId}
            onChange={(e) => {
              setAgentId(e.target.value);
              if (agentError) setAgentError("");
            }}
            className="h-9 w-full rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2.5 text-[12px] text-fg2 focus:border-accent"
          >
            <option value="">Select an agent (mandatory)</option>
            {agents.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
          </select>
          {agentError ? (
            <p className="mt-1 text-[11px] text-danger" data-testid="wizard-agent-error">{agentError}</p>
          ) : (
            <p className="mt-1 text-[11px] text-muted">All messages sent to this bot will route to this agent.</p>
          )}
        </div>

        <div>
          <p className="mb-1 text-[12px] font-medium text-fg2">Transport mode</p>
          <Segmented
            value={transport}
            onChange={(v: GatewayTransport) => setTransport(v)}
            options={[
              { id: "long_polling", label: "Long-polling", testid: "wizard-seg-long-polling" },
              { id: "webhook", label: "Webhook", testid: "wizard-seg-webhook" },
            ]}
          />
        </div>

        {transport === "webhook" && (
          <div>
            <label htmlFor="wizard-tg-webhook" className="mb-1 block text-[12px] font-medium text-fg2">
              Webhook URL <span className="text-danger">*</span>
            </label>
            <input
              id="wizard-tg-webhook"
              type="url"
              data-testid="wizard-input-webhook-url"
              value={webhookUrl}
              onChange={(e) => {
                setWebhookUrl(e.target.value);
                if (webhookError) setWebhookError("");
              }}
              placeholder="https://your-domain.com/api/v1/webhooks/telegram/..."
              className="h-9 w-full rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2.5 font-mono text-[12px] text-fg2 focus:border-accent"
            />
            {webhookError ? (
              <p className="mt-1 text-[11px] text-danger" data-testid="wizard-webhook-error">{webhookError}</p>
            ) : null}
          </div>
        )}
      </form>
    </Modal>
  );
}

// ---------------------------------------------------------------------------
// WhatsApp Connect Wizard Modal
// ---------------------------------------------------------------------------

interface WhatsAppConnectWizardModalProps {
  wsId: string;
  agents: ApiAgent[];
  onClose: () => void;
  onCreated: (gw: ApiWhatsAppGatewayConfig) => void;
  onToast: (text: string, kind?: string) => void;
}

function WhatsAppConnectWizardModal({
  wsId,
  agents,
  onClose,
  onCreated,
  onToast,
}: WhatsAppConnectWizardModalProps) {
  const [lane, setLane] = useState<GatewayLane>("cloud_api");
  const [agentId, setAgentId] = useState(agents[0]?.id || "");
  const [creds, setCreds] = useState({ access_token: "", phone_number_id: "", app_secret: "", verify_token: "" });
  const [agentError, setAgentError] = useState("");
  const [credError, setCredError] = useState("");
  const [busy, setBusy] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    let hasErr = false;
    if (!agentId) {
      setAgentError("A bound agent is required");
      hasErr = true;
    } else {
      setAgentError("");
    }

    if (lane === "cloud_api") {
      const trimmed = {
        access_token: creds.access_token.trim(),
        phone_number_id: creds.phone_number_id.trim(),
        app_secret: creds.app_secret.trim(),
        verify_token: creds.verify_token.trim(),
      };
      if (!trimmed.access_token || !trimmed.phone_number_id || !trimmed.app_secret || !trimmed.verify_token) {
        setCredError("All four credential fields are required for Official Cloud API");
        hasErr = true;
      } else {
        setCredError("");
      }
    }

    if (hasErr) return;

    setBusy(true);
    try {
      const payload: any = {
        lane,
        agent_id: agentId,
      };
      if (lane === "cloud_api") {
        payload.access_token = creds.access_token.trim();
        payload.phone_number_id = creds.phone_number_id.trim();
        payload.app_secret = creds.app_secret.trim();
        payload.verify_token = creds.verify_token.trim();
      }
      const res = await api.gateways.whatsapp.create(wsId, payload);
      onToast("WhatsApp account connected");
      onCreated(res);
    } catch (err: unknown) {
      if (err instanceof ApiError && err.details?.length) {
        for (const d of err.details) {
          if (d.field === "agent_id") setAgentError(d.message);
          if (["access_token", "phone_number_id", "app_secret", "verify_token"].includes(d.field || "")) {
            setCredError(d.message);
          }
        }
      }
      onToast(formatApiError(err, "Failed to connect WhatsApp account"), "danger");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      title="Connect WhatsApp account"
      onClose={onClose}
      odId="modal-whatsapp-wizard"
      data-testid="modal-whatsapp-wizard"
      footer={
        <>
          <button
            type="button"
            data-testid="btn-cancel-wa-wizard"
            onClick={onClose}
            className="flex h-9 items-center rounded-md px-3 text-[13px] font-medium text-muted transition-colors hover:text-fg"
          >
            Cancel
          </button>
          <button
            type="submit"
            form="whatsapp-wizard-form"
            data-testid="btn-submit-wa-wizard"
            disabled={busy}
            className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-40"
          >
            {busy ? "Connecting…" : "Connect account"}
          </button>
        </>
      }
    >
      <form id="whatsapp-wizard-form" onSubmit={handleSubmit} className="p-5 space-y-4">
        {/* Lane Selection */}
        <div>
          <p className="mb-1.5 text-[12px] font-medium text-fg2">Account Lane <span className="text-danger">*</span></p>
          <div className="space-y-2">
            <label className="flex items-start gap-2.5 rounded-md border border-line p-2.5 text-[13px] text-fg2 cursor-pointer hover:bg-[color-mix(in_oklab,var(--fg)_3%,transparent)]">
              <input
                type="radio"
                name="wizard-wa-lane"
                value="cloud_api"
                checked={lane === "cloud_api"}
                onChange={() => setLane("cloud_api")}
                className="mt-1"
                data-testid="wizard-wa-lane-cloud"
              />
              <div>
                <span className="font-medium text-fg">Official Cloud API</span>
                <span className="block text-[11px] leading-4 text-muted">
                  Meta&apos;s official WhatsApp Business Cloud API. Webhook-based, high throughput, and production ready.
                </span>
              </div>
            </label>
            <label className="flex items-start gap-2.5 rounded-md border border-line p-2.5 text-[13px] text-fg2 cursor-pointer hover:bg-[color-mix(in_oklab,var(--fg)_3%,transparent)]">
              <input
                type="radio"
                name="wizard-wa-lane"
                value="multi_device"
                checked={lane === "multi_device"}
                onChange={() => setLane("multi_device")}
                className="mt-1"
                data-testid="wizard-wa-lane-md"
              />
              <div>
                <span className="font-medium text-fg">Multi-device (Personal)</span>
                <span className="block text-[11px] leading-4 text-muted">
                  Pairs a phone via QR code over the unofficial multi-device protocol.
                </span>
              </div>
            </label>
          </div>
        </div>

        {/* Bound Agent */}
        <div>
          <label htmlFor="wizard-wa-agent" className="mb-1 block text-[12px] font-medium text-fg2">
            Bound Agent <span className="text-danger">*</span>
          </label>
          <select
            id="wizard-wa-agent"
            data-testid="wizard-select-wa-agent"
            value={agentId}
            onChange={(e) => {
              setAgentId(e.target.value);
              if (agentError) setAgentError("");
            }}
            className="h-9 w-full rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2.5 text-[12px] text-fg2 focus:border-accent"
          >
            <option value="">Select an agent (mandatory)</option>
            {agents.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
          </select>
          {agentError ? (
            <p className="mt-1 text-[11px] text-danger" data-testid="wizard-wa-agent-error">{agentError}</p>
          ) : (
            <p className="mt-1 text-[11px] text-muted">All messages sent to this WhatsApp account will route to this agent.</p>
          )}
        </div>

        {lane === "cloud_api" ? (
          <div className="space-y-3 pt-1 border-t border-line">
            <p className="text-[12px] font-medium text-fg2">Cloud API credentials</p>
            <div>
              <label htmlFor="wizard-wa-token" className="mb-1 block text-[11px] font-medium text-fg2">
                Access token <span className="text-danger">*</span>
              </label>
              <input
                id="wizard-wa-token"
                type="password"
                data-testid="wizard-input-wa-access-token"
                value={creds.access_token}
                onChange={(e) => setCreds((c) => ({ ...c, access_token: e.target.value }))}
                placeholder="EAAG..."
                className={waInputCls}
              />
            </div>
            <div>
              <label htmlFor="wizard-wa-phone-id" className="mb-1 block text-[11px] font-medium text-fg2">
                Phone number ID <span className="text-danger">*</span>
              </label>
              <input
                id="wizard-wa-phone-id"
                type="text"
                data-testid="wizard-input-wa-phone-number-id"
                value={creds.phone_number_id}
                onChange={(e) => setCreds((c) => ({ ...c, phone_number_id: e.target.value }))}
                placeholder="123456789012345"
                className={waInputCls}
              />
            </div>
            <div>
              <label htmlFor="wizard-wa-app-secret" className="mb-1 block text-[11px] font-medium text-fg2">
                App secret <span className="text-danger">*</span>
              </label>
              <input
                id="wizard-wa-app-secret"
                type="password"
                data-testid="wizard-input-wa-app-secret"
                value={creds.app_secret}
                onChange={(e) => setCreds((c) => ({ ...c, app_secret: e.target.value }))}
                placeholder="App secret from Meta dashboard"
                className={waInputCls}
              />
            </div>
            <div>
              <label htmlFor="wizard-wa-verify-token" className="mb-1 block text-[11px] font-medium text-fg2">
                Verify token <span className="text-danger">*</span>
              </label>
              <input
                id="wizard-wa-verify-token"
                type="password"
                data-testid="wizard-input-wa-verify-token"
                value={creds.verify_token}
                onChange={(e) => setCreds((c) => ({ ...c, verify_token: e.target.value }))}
                placeholder="Custom secret string for webhook verification"
                className={waInputCls}
              />
            </div>
            {credError ? (
              <p className="text-[11px] text-danger" data-testid="wizard-wa-cred-error">{credError}</p>
            ) : null}
          </div>
        ) : (
          <div className="flex items-start gap-2 rounded-md border border-[color-mix(in_oklab,var(--warn)_45%,transparent)] px-3.5 py-2.5">
            <span className="mt-0.5 text-[color-mix(in_oklab,var(--warn),black_38%)]">
              <Icon name="alert" size={14} />
            </span>
            <p className="text-[12px] leading-4 text-[color-mix(in_oklab,var(--warn),black_38%)]">
              Uses an unofficial protocol. Meta may ban the account. After saving, scan the QR code to pair your phone.
            </p>
          </div>
        )}
      </form>
    </Modal>
  );
}

// ---------------------------------------------------------------------------
// Create Group Binding Modal
// ---------------------------------------------------------------------------

interface CreateBindingModalProps {
  wsId: string;
  bots: ApiGatewayConfig[];
  agents: ApiAgent[];
  defaultBotId: string;
  defaultAgentId: string;
  onClose: () => void;
  onCreated: (b: ApiGatewayBinding) => void;
  onToast: (text: string, kind?: string) => void;
}

function CreateBindingModal({
  wsId,
  bots,
  agents,
  defaultBotId,
  defaultAgentId,
  onClose,
  onCreated,
  onToast,
}: CreateBindingModalProps) {
  const [gatewayId, setGatewayId] = useState(defaultBotId || bots[0]?.id || "");
  const [agentId, setAgentId] = useState(defaultAgentId || agents[0]?.id || "");
  const [chatId, setChatId] = useState("");
  const [chatTitle, setChatTitle] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!gatewayId) {
      setError("Please select a Telegram bot");
      return;
    }
    if (!agentId) {
      setError("Please select an agent");
      return;
    }
    if (!chatId.trim()) {
      setError("Platform Chat ID is required (e.g. -100123456789)");
      return;
    }

    setError("");
    setBusy(true);
    try {
      const res = await api.gateways.telegram.bindings.create(wsId, {
        gateway_id: gatewayId,
        agent_id: agentId,
        platform_chat_id: chatId.trim(),
        chat_title: chatTitle.trim() || undefined,
      });
      onCreated(res);
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 409) {
        setError("Binding conflict — that group is already bound.");
      } else {
        setError(formatApiError(err, "Failed to create group binding"));
      }
      onToast(formatApiError(err, "Failed to create group binding"), "danger");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      title="Add Telegram group binding"
      onClose={onClose}
      odId="modal-create-binding"
      data-testid="modal-create-binding"
      footer={
        <>
          <button
            type="button"
            data-testid="btn-cancel-binding"
            onClick={onClose}
            className="flex h-9 items-center rounded-md px-3 text-[13px] font-medium text-muted transition-colors hover:text-fg"
          >
            Cancel
          </button>
          <button
            type="submit"
            form="create-binding-form"
            data-testid="btn-submit-binding"
            disabled={busy}
            className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-40"
          >
            {busy ? "Binding…" : "Create binding"}
          </button>
        </>
      }
    >
      <form id="create-binding-form" onSubmit={handleSubmit} className="p-5 space-y-4">
        <div>
          <label htmlFor="binding-bot-select" className="mb-1 block text-[12px] font-medium text-fg2">
            Owning Telegram Bot <span className="text-danger">*</span>
          </label>
          <select
            id="binding-bot-select"
            data-testid="select-create-binding-bot"
            value={gatewayId}
            onChange={(e) => setGatewayId(e.target.value)}
            className="h-9 w-full rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2.5 text-[12px] text-fg2 focus:border-accent"
          >
            {bots.map((b) => (
              <option key={b.id} value={b.id}>
                {b.bot_username ? `@${b.bot_username}` : b.identity || b.id}
              </option>
            ))}
          </select>
        </div>

        <div>
          <label htmlFor="binding-agent-select" className="mb-1 block text-[12px] font-medium text-fg2">
            Bound Agent <span className="text-danger">*</span>
          </label>
          <select
            id="binding-agent-select"
            data-testid="select-create-binding-agent"
            value={agentId}
            onChange={(e) => setAgentId(e.target.value)}
            className="h-9 w-full rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2.5 text-[12px] text-fg2 focus:border-accent"
          >
            {agents.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
          </select>
        </div>

        <div>
          <label htmlFor="binding-chat-id" className="mb-1 block text-[12px] font-medium text-fg2">
            Platform Chat ID <span className="text-danger">*</span>
          </label>
          <input
            id="binding-chat-id"
            type="text"
            data-testid="input-binding-chat-id"
            value={chatId}
            onChange={(e) => setChatId(e.target.value)}
            placeholder="-1001234567890"
            className="h-9 w-full rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2.5 font-mono text-[12px] text-fg2 focus:border-accent"
          />
        </div>

        <div>
          <label htmlFor="binding-chat-title" className="mb-1 block text-[12px] font-medium text-fg2">
            Chat Title (optional)
          </label>
          <input
            id="binding-chat-title"
            type="text"
            data-testid="input-binding-chat-title"
            value={chatTitle}
            onChange={(e) => setChatTitle(e.target.value)}
            placeholder="e.g. Operations Incident Room"
            className="h-9 w-full rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2.5 text-[12px] text-fg2 focus:border-accent"
          />
        </div>

        {error ? (
          <p className="text-[12px] text-danger" data-testid="create-binding-error">{error}</p>
        ) : null}
      </form>
    </Modal>
  );
}

// ---------------------------------------------------------------------------
// Member Pairing Modal
// ---------------------------------------------------------------------------

interface PairingModalProps {
  wsSlug: string;
  onClose: () => void;
  onToast: (text: string, kind?: string) => void;
  platform?: "telegram" | "whatsapp";
}

export function PairingModal({ wsSlug, onClose, onToast, platform = "telegram" }: PairingModalProps) {
  const whatsapp = platform === "whatsapp";
  const t = (id: string) => (whatsapp ? `wa-${id}` : id);
  const [token, setToken] = useState<{ token: string; expires_at: string } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [minting, setMinting] = useState(false);
  const [revoking, setRevoking] = useState(false);
  const [now, setNow] = useState(() => Date.now());

  const mint = async () => {
    setMinting(true);
    setError(null);
    try {
      const res = whatsapp
        ? await api.gateways.whatsapp.pairingTokens.create(wsSlug)
        : await api.gateways.telegram.pairing.create(wsSlug);
      setToken(res.token);
    } catch (err: unknown) {
      setError(formatApiError(err, "Failed to generate a pairing token"));
    } finally {
      setMinting(false);
    }
  };

  useEffect(() => {
    void mint();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- mint once on open
  }, []);

  useEffect(() => {
    if (!token) return;
    const h = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(h);
  }, [token]);

  const expired = token ? Date.parse(token.expires_at) - now <= 0 : false;
  const command = token ? `/start ${token.token}` : "";

  const handleRevoke = async () => {
    if (!token) return;
    setRevoking(true);
    try {
      if (whatsapp) {
        await api.gateways.whatsapp.pairingTokens.revoke(wsSlug, token.token);
      } else {
        await api.gateways.telegram.pairing.revoke(wsSlug, token.token);
      }
      onToast("Pairing token revoked");
      onClose();
    } catch (err: unknown) {
      onToast(formatApiError(err, "Failed to revoke the pairing token"), "danger");
      setRevoking(false);
    }
  };

  return (
    <Modal
      title={whatsapp ? "Link your WhatsApp account" : "Link your Telegram account"}
      onClose={onClose}
      odId={t("modal-pairing")}
      data-testid={t("modal-pairing")}
      footer={
        <>
          {token && !expired ? (
            <button
              type="button"
              data-testid={t("btn-revoke-pairing")}
              onClick={() => void handleRevoke()}
              disabled={revoking}
              className="mr-auto flex h-9 items-center rounded-md px-3 text-[13px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-danger disabled:opacity-50"
            >
              {revoking ? "Revoking…" : "Revoke token"}
            </button>
          ) : null}
          <button
            type="button"
            data-testid={t("btn-pairing-done")}
            onClick={onClose}
            className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90"
          >
            Done
          </button>
        </>
      }
    >
      <div className="p-5">
        <p className="text-[13px] leading-5 text-fg2">
          {whatsapp
            ? "Send this command to the workspace's WhatsApp number in a direct message. The command is single-use — after it confirms, your account is linked and the token cannot be reused."
            : "Send this command to any workspace bot in a Telegram direct message. The command is single-use — after it confirms, your account is linked and the token cannot be reused."}
        </p>

        {minting ? (
          <div className="mt-4 h-14 animate-pulse rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]" />
        ) : error ? (
          <div className="mt-4 rounded-md border border-[color-mix(in_oklab,var(--danger)_45%,transparent)] px-3.5 py-3" data-testid={t("pairing-error")}>
            <p className="text-[13px] text-danger">{error}</p>
            <button
              type="button"
              data-testid={t("btn-retry-pairing")}
              onClick={() => void mint()}
              className="mt-2 h-8 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
            >
              Try again
            </button>
          </div>
        ) : token ? (
          <>
            <div className="mt-4 flex items-center gap-2 rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] px-3 py-2.5">
              <code className="min-w-0 flex-1 truncate font-mono text-[13px] text-fg" data-testid={t("pairing-command")}>
                {command}
              </code>
              <button
                type="button"
                aria-label="Copy pairing command"
                data-testid={t("btn-copy-pairing")}
                onClick={() => copyText(command, onToast, "Pairing command copied to clipboard")}
                className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg"
              >
                <Icon name="copy" size={13} />
              </button>
            </div>
            <p
              className={cx(
                "mt-2 flex items-center gap-1.5 text-[12px]",
                expired ? "text-[color-mix(in_oklab,var(--warn),black_38%)]" : "text-muted"
              )}
              data-testid={t("pairing-countdown")}
            >
              <Icon name="clock" size={12} />
              {expired ? (
                <>
                  Expired — the token can no longer be used.
                  <button
                    type="button"
                    data-testid={t("btn-regenerate-pairing")}
                    onClick={() => void mint()}
                    className="h-6 rounded-[6px] border border-line px-2 text-[11px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
                  >
                    Generate a new command
                  </button>
                </>
              ) : (
                <>Expires in {formatCountdown(token.expires_at, now)}</>
              )}
            </p>
          </>
        ) : null}
      </div>
    </Modal>
  );
}
