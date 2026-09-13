import { useEffect, useMemo, useState } from "react";
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
  type GatewayTransport,
} from "../../lib/api";
import {
  formatCountdown,
  gatewayStatusView,
  useCanManageGateways,
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

// Workspace Telegram gateway (change integrate-telegram-gateway). Admins
// (gateways.write) manage the bot connection, default agent, transport, and
// group bindings; every member sees their personal pairing flow. Members get
// the pairing card only — admin controls are hidden.
export function GatewaysPane({ tenant, onToast = () => {}, canWrite }: GatewaysPaneProps) {
  const derivedCanManage = useCanManageGateways(tenant);
  const writer = canWrite !== undefined ? canWrite : derivedCanManage;

  const [gateway, setGateway] = useState<ApiGatewayConfig | null>(null);
  const [bindings, setBindings] = useState<ApiGatewayBinding[]>([]);
  const [agents, setAgents] = useState<ApiAgent[]>([]);
  const [myLink, setMyLink] = useState<ApiGatewayLink | null>(null);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<ApiError | Error | null>(null);

  const [tokenInput, setTokenInput] = useState("");
  const [tokenError, setTokenError] = useState("");
  const [webhookUrl, setWebhookUrl] = useState("");
  const [bindAgentId, setBindAgentId] = useState("");
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [pairingOpen, setPairingOpen] = useState(false);

  const targetWsId = tenant?.sub || tenant?.id;

  const load = async () => {
    setLoading(true);
    setLoadError(null);
    try {
      // Config + bindings are admin-gated server-side — a Member's 403s are
      // expected and skipped. The pairing link is member-scoped for everyone.
      const [configRes, bindingsRes, agentRes, linkRes] = await Promise.all([
        writer ? api.gateways.telegram.getConfig(targetWsId).catch(() => null) : Promise.resolve(null),
        writer ? api.gateways.telegram.bindings.list(targetWsId).catch(() => null) : Promise.resolve(null),
        api.agents.list(targetWsId).catch(() => null),
        api.gateways.telegram.links.getMine(targetWsId).catch(() => null),
      ]);
      if (configRes) setGateway(configRes.gateway || null);
      if (bindingsRes) setBindings(bindingsRes.bindings || []);
      if (agentRes) setAgents(agentRes.agents || []);
      if (linkRes) setMyLink(linkRes.link || null);
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

  const status = gatewayStatusView(gateway);
  const configured = Boolean(gateway && (gateway.token_hint || gateway.bot_username));
  const defaultAgent = gateway?.default_agent_id || "";
  const bindAgent = useMemo(
    () => bindAgentId || defaultAgent || agents[0]?.id || "",
    [bindAgentId, defaultAgent, agents]
  );
  const bindCommand = gateway?.bot_username
    ? `/bind@${gateway.bot_username} ${agents.find((a) => a.id === bindAgent)?.slug || "agent"}`
    : `/bind ${agents.find((a) => a.id === bindAgent)?.slug || "agent"}`;

  const handleConnect = async () => {
    const token = tokenInput.trim();
    if (!token) {
      setTokenError("A bot token is required");
      return;
    }
    setTokenError("");
    setBusyAction("connect");
    try {
      const res = await api.gateways.telegram.updateConfig(targetWsId, { token });
      setGateway(res.gateway);
      setTokenInput("");
      onToast(
        res.gateway?.bot_username
          ? `Bot connected as @${res.gateway.bot_username}`
          : "Bot connected"
      );
    } catch (err: unknown) {
      onToast(formatApiError(err, "Failed to connect the bot"), "danger");
    } finally {
      setBusyAction(null);
    }
  };

  const handleRotateToken = async () => {
    const token = tokenInput.trim();
    if (!token) return;
    setBusyAction("rotate");
    try {
      const res = await api.gateways.telegram.updateConfig(targetWsId, { token });
      setGateway(res.gateway);
      setTokenInput("");
      onToast("Bot token updated");
    } catch (err: unknown) {
      onToast(formatApiError(err, "Failed to update the token"), "danger");
    } finally {
      setBusyAction(null);
    }
  };

  const handleTest = async () => {
    setBusyAction("test");
    try {
      const res = await api.gateways.telegram.test(targetWsId);
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

  const handleDefaultAgent = async (agentId: string) => {
    try {
      const res = await api.gateways.telegram.updateConfig(targetWsId, {
        default_agent_id: agentId || null,
      });
      setGateway(res.gateway);
      onToast(
        agentId
          ? `${agentName(agentId) || "Agent"} now answers direct messages without a per-member choice`
          : "Default agent cleared"
      );
    } catch (err: unknown) {
      onToast(formatApiError(err, "Failed to set the default agent"), "danger");
    }
  };

  const handleTransport = async (transport: GatewayTransport) => {
    if (gateway?.transport === transport) return;
    try {
      const res = await api.gateways.telegram.updateConfig(targetWsId, { transport });
      setGateway(res.gateway);
      setWebhookUrl(res.gateway?.webhook_url || "");
      onToast(transport === "webhook" ? "Transport switched to webhook" : "Transport switched to long-polling");
    } catch (err: unknown) {
      onToast(formatApiError(err, "Failed to switch transport"), "danger");
    }
  };

  const handleSaveWebhookUrl = async () => {
    setBusyAction("webhook");
    try {
      const res = await api.gateways.telegram.updateConfig(targetWsId, {
        webhook_url: webhookUrl.trim() || undefined,
      });
      setGateway(res.gateway);
      onToast("Webhook URL saved");
    } catch (err: unknown) {
      onToast(formatApiError(err, "Failed to save the webhook URL"), "danger");
    } finally {
      setBusyAction(null);
    }
  };

  const handleToggleEnabled = async (next: boolean) => {
    if (!configured) return;
    setBusyAction("toggle");
    try {
      const res = next
        ? await api.gateways.telegram.enable(targetWsId)
        : await api.gateways.telegram.disable(targetWsId);
      setGateway(res.gateway);
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

  const handleUnpair = async () => {
    setBusyAction("unpair");
    try {
      await api.gateways.telegram.links.removeMine(targetWsId);
      setMyLink(null);
      onToast("Telegram account unlinked");
    } catch (err: unknown) {
      onToast(formatApiError(err, "Failed to unlink your Telegram account"), "danger");
    } finally {
      setBusyAction(null);
    }
  };

  return (
    <div className="max-w-xl" data-od-id="pane-gateways" data-testid="pane-gateways">
      {loading && !gateway && bindings.length === 0 ? (
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
      ) : (
        <div className="space-y-6">
          {writer && (
            <section data-testid="gateway-admin-section">
              <div className="mb-2">
                <h3 className="text-[15px] font-semibold text-fg">Telegram bot</h3>
                <p className="mt-0.5 text-[12px] text-muted">
                  Connect a bot to route direct messages and bound groups into agent sessions.
                </p>
              </div>

              {/* Connection card */}
              <div className="rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] px-4 py-3">
                <div className="flex items-center gap-3">
                  <span
                    className={cx(
                      "flex h-9 w-9 shrink-0 items-center justify-center rounded-md",
                      configured
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
                            : configured && gateway?.enabled
                            ? "text-[color-mix(in_oklab,var(--success),black_25%)]"
                            : "text-muted"
                        )}
                        data-testid="gateway-status"
                      >
                        <span
                          className={cx(
                            "h-1.5 w-1.5 rounded-full",
                            status.errored ? "bg-danger" : configured && gateway?.enabled ? "bg-success" : "bg-muted"
                          )}
                        />
                        {status.label}
                      </span>
                      {gateway?.bot_username ? (
                        <Chip mono>
                          <span data-testid="gateway-username">@{gateway.bot_username}</span>
                        </Chip>
                      ) : null}
                      {gateway?.token_hint ? (
                        <Chip mono>
                          <span data-testid="gateway-token-hint">token ••••{gateway.token_hint}</span>
                        </Chip>
                      ) : null}
                    </div>
                    {gateway?.status_error ? (
                      <p className="mt-0.5 truncate text-[11px] text-danger" title={gateway.status_error}>
                        {gateway.status_error}
                      </p>
                    ) : null}
                  </div>
                  {configured ? (
                    <button
                      type="button"
                      data-testid="btn-test-gateway"
                      onClick={() => void handleTest()}
                      disabled={busyAction === "test"}
                      className="h-8 shrink-0 rounded-md border border-line px-2.5 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-50"
                    >
                      {busyAction === "test" ? "Testing…" : "Test connection"}
                    </button>
                  ) : null}
                </div>

                <div className="mt-3">
                  <label htmlFor="gateway-token" className="mb-1 block text-[12px] font-medium text-fg2">
                    Bot token
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
                      placeholder={
                        configured && gateway?.token_hint
                          ? `••••${gateway.token_hint} — paste a new token to rotate`
                          : "Paste the token from BotFather"
                      }
                      className="h-9 min-w-0 flex-1 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2.5 font-mono text-[12px] text-fg2 focus:border-accent"
                    />
                    {configured ? (
                      <button
                        type="button"
                        data-testid="btn-rotate-token"
                        onClick={() => void handleRotateToken()}
                        disabled={!tokenInput.trim() || busyAction === "rotate"}
                        className="h-9 shrink-0 rounded-md bg-accent px-3.5 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-40"
                      >
                        {busyAction === "rotate" ? "Saving…" : "Update token"}
                      </button>
                    ) : (
                      <button
                        type="button"
                        data-testid="btn-connect-bot"
                        onClick={() => void handleConnect()}
                        disabled={busyAction === "connect"}
                        className="h-9 shrink-0 rounded-md bg-accent px-3.5 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-40"
                      >
                        {busyAction === "connect" ? "Connecting…" : "Connect bot"}
                      </button>
                    )}
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

              {/* Default agent */}
              <div className="mt-3 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] px-4 py-3">
                <label htmlFor="gateway-default-agent" className="mb-1 block text-[12px] font-medium text-fg2">
                  Default agent for direct messages
                </label>
                <p className="mb-2 text-[11px] text-muted">
                  Members who have not picked a personal agent get this one in direct messages.
                </p>
                <select
                  id="gateway-default-agent"
                  data-testid="select-default-agent"
                  className="h-8 w-full rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2 text-[12px] text-fg2 focus:border-accent"
                  value={defaultAgent}
                  onChange={(e) => void handleDefaultAgent(e.target.value)}
                >
                  <option value="">No default — unpaired choices fall back to nothing</option>
                  {agents.map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.name}
                    </option>
                  ))}
                </select>
              </div>

              {/* Transport */}
              <div className="mt-3 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] px-4 py-3">
                <p className="mb-1 text-[12px] font-medium text-fg2">Transport</p>
                <p className="mb-2 text-[11px] text-muted">
                  Long-polling works behind NAT; webhook needs a public HTTPS URL.
                </p>
                <Segmented
                  value={gateway?.transport || "long_polling"}
                  onChange={(v: GatewayTransport) => void handleTransport(v)}
                  options={[
                    { id: "long_polling", label: "Long-polling", testid: "seg-long-polling" },
                    { id: "webhook", label: "Webhook", testid: "seg-webhook" },
                  ]}
                />
                {gateway?.transport === "webhook" ? (
                  <div className="mt-2.5 flex gap-2">
                    <input
                      type="url"
                      aria-label="Webhook URL"
                      data-testid="input-webhook-url"
                      value={webhookUrl || gateway.webhook_url || ""}
                      onChange={(e) => setWebhookUrl(e.target.value)}
                      placeholder="https://example.com/api/v1/gateways/telegram/webhook"
                      className="h-8 min-w-0 flex-1 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2 font-mono text-[11px] text-fg2 focus:border-accent"
                    />
                    <button
                      type="button"
                      data-testid="btn-save-webhook"
                      onClick={() => void handleSaveWebhookUrl()}
                      disabled={busyAction === "webhook"}
                      className="h-8 shrink-0 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-50"
                    >
                      Save URL
                    </button>
                  </div>
                ) : null}
              </div>

              {/* Enable/disable */}
              <div className="mt-3 flex items-center justify-between rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] px-4 py-3">
                <div>
                  <p className="text-[14px] font-medium text-fg">Gateway enabled</p>
                  <p className="mt-0.5 text-[11px] text-muted">
                    Disabling stops ingestion and delivery without deleting configuration or bindings.
                  </p>
                </div>
                <Toggle
                  on={Boolean(gateway?.enabled)}
                  label="Enable gateway"
                  onChange={(next: boolean) => void handleToggleEnabled(next)}
                />
              </div>

              {/* Group bindings */}
              <div className="mt-6">
                <h3 className="text-[15px] font-semibold text-fg">Group bindings</h3>
                <p className="mt-0.5 text-[12px] text-muted">
                  One agent answers each bound group. Add the bot to a Telegram group, then send the bind command there.
                </p>

                {agents.length > 0 ? (
                  <div className="mt-2.5 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] px-4 py-3">
                    <label htmlFor="gateway-bind-agent" className="mb-1 block text-[12px] font-medium text-fg2">
                      Bind command
                    </label>
                    <div className="flex items-center gap-2">
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
                      The binding is created once the command is confirmed in the group.
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
                              {b.chat_title || "Unknown group"}
                            </p>
                            <Chip mono>{b.platform_chat_id}</Chip>
                            <Chip mono>{agentName(b.agent_id)}</Chip>
                          </div>
                        </div>
                        <button
                          type="button"
                          data-testid={"btn-unlink-" + b.id}
                          onClick={() => void handleUnlink(b)}
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
                      Bound groups appear here with their agent and an unlink control.
                    </p>
                  </div>
                )}
              </div>
            </section>
          )}

          {/* Member pairing flow — visible to every member */}
          <section data-testid="gateway-pairing-section">
            <h3 className="text-[15px] font-semibold text-fg">Your Telegram link</h3>
            <p className="mt-0.5 text-[12px] text-muted">
              Link your Telegram account to talk to the workspace&apos;s agents from Telegram. Runs execute under your
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
                  onClick={() => void handleUnpair()}
                  disabled={busyAction === "unpair"}
                  className="h-8 shrink-0 rounded-md px-2.5 text-[12px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-danger disabled:opacity-50"
                >
                  {busyAction === "unpair" ? "Unlinking…" : "Unlink"}
                </button>
              </div>
            ) : (
              <div className="mt-2.5 flex items-center justify-between rounded-md border border-dashed border-line px-4 py-3.5">
                <p className="text-[13px] text-fg2">No Telegram account linked yet.</p>
                <button
                  type="button"
                  data-testid="btn-open-pairing"
                  onClick={() => setPairingOpen(true)}
                  className="h-8 shrink-0 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
                >
                  Link Telegram account
                </button>
              </div>
            )}
          </section>
        </div>
      )}

      {pairingOpen ? (
        <PairingModal
          wsSlug={targetWsId}
          onClose={() => setPairingOpen(false)}
          onToast={onToast}
        />
      ) : null}
    </div>
  );
}

interface PairingModalProps {
  wsSlug: string;
  onClose: () => void;
  onToast: (text: string, kind?: string) => void;
}

// Member pairing modal: mints a one-time pairing token and shows it as a
// copyable /start command with a live expiry countdown. Closing keeps the
// token alive so the member can paste it in Telegram; Revoke cancels it.
export function PairingModal({ wsSlug, onClose, onToast }: PairingModalProps) {
  const [token, setToken] = useState<{ token: string; expires_at: string } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [minting, setMinting] = useState(false);
  const [revoking, setRevoking] = useState(false);
  const [now, setNow] = useState(() => Date.now());

  const mint = async () => {
    setMinting(true);
    setError(null);
    try {
      const res = await api.gateways.telegram.pairing.create(wsSlug);
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
      await api.gateways.telegram.pairing.revoke(wsSlug, token.token);
      onToast("Pairing token revoked");
      onClose();
    } catch (err: unknown) {
      onToast(formatApiError(err, "Failed to revoke the pairing token"), "danger");
      setRevoking(false);
    }
  };

  return (
    <Modal
      title="Link your Telegram account"
      onClose={onClose}
      odId="modal-pairing"
      data-testid="modal-pairing"
      footer={
        <>
          {token && !expired ? (
            <button
              type="button"
              data-testid="btn-revoke-pairing"
              onClick={() => void handleRevoke()}
              disabled={revoking}
              className="mr-auto flex h-9 items-center rounded-md px-3 text-[13px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-danger disabled:opacity-50"
            >
              {revoking ? "Revoking…" : "Revoke token"}
            </button>
          ) : null}
          <button
            type="button"
            data-testid="btn-pairing-done"
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
          Send this command to the workspace bot in a Telegram direct message. The command is single-use — after it
          confirms, your account is linked and the token cannot be reused.
        </p>

        {minting ? (
          <div className="mt-4 h-14 animate-pulse rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]" />
        ) : error ? (
          <div className="mt-4 rounded-md border border-[color-mix(in_oklab,var(--danger)_45%,transparent)] px-3.5 py-3" data-testid="pairing-error">
            <p className="text-[13px] text-danger">{error}</p>
            <button
              type="button"
              data-testid="btn-retry-pairing"
              onClick={() => void mint()}
              className="mt-2 h-8 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
            >
              Try again
            </button>
          </div>
        ) : token ? (
          <>
            <div className="mt-4 flex items-center gap-2 rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] px-3 py-2.5">
              <code className="min-w-0 flex-1 truncate font-mono text-[13px] text-fg" data-testid="pairing-command">
                {command}
              </code>
              <button
                type="button"
                aria-label="Copy pairing command"
                data-testid="btn-copy-pairing"
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
              data-testid="pairing-countdown"
            >
              <Icon name="clock" size={12} />
              {expired ? (
                <>
                  Expired — the token can no longer be used.
                  <button
                    type="button"
                    data-testid="btn-regenerate-pairing"
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
