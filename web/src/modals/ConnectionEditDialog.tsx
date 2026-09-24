import { useEffect, useState } from "react";
import { cx } from "../lib/helpers";
import { Modal } from "../components/ui/Modal";
import { Chip } from "../components/ui/Chip";
import { Icon } from "../components/ui/Icon";
import { inputCls } from "../components/ui/constants";
import { AttachAgentsList } from "../components/connections/AttachAgentsList";
import {
  accessLevelLabel,
  connectionAttachId,
  connectionServiceName,
  connectionsApi,
  type ApiConnection,
  type ApiIntegrationRecipe,
} from "../lib/connectionsApi";
import { api, formatApiError, type ApiAgent } from "../lib/api";

export interface ConnectionEditDialogProps {
  tenant: any;
  connection: ApiConnection;
  /** The workspace recipe registry — resolves display name and auth kind. */
  recipes: ApiIntegrationRecipe[];
  onClose: () => void;
  onToast?: (text: string, kind?: string) => void;
  /** Fired after a fully successful save — the gallery re-reads the list. */
  onSaved?: () => void;
}

function SectionLabel({ children }: { children: any }) {
  return (
    <p className="mb-2 font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-muted">
      {children}
    </p>
  );
}

/**
 * Connection edit surface (add-connection-edit D4): the shared agent-selection
 * list plus, per auth kind, either a probe-gated token replacement field
 * (token-auth kinds) or a Reauthorize hand-off (OAuth kinds — their
 * credential-rotation path; the token endpoint refuses them). Origin and
 * access level ride as display-only chips — immutable after connect. Save is
 * all-or-nothing in dialog order (D3): the token replaces FIRST — a failed
 * probe aborts before any attachment change; an empty token field skips the
 * replace and still applies agent changes.
 */
export function ConnectionEditDialog({
  tenant,
  connection,
  recipes,
  onClose,
  onToast = () => {},
  onSaved,
}: ConnectionEditDialogProps) {
  const ws = tenant?.sub || tenant?.id;
  const name = connectionServiceName(connection, recipes);
  const recipe = recipes.find((r) => r.id === connection.service);
  const isOAuth = recipe?.auth_kind === 'oauth';
  // The attach handle: the materialized server id when one exists (MCP kind),
  // else the raw connection id (HTTP kind has no server row).
  const attachId = connectionAttachId(connection);

  const [agents, setAgents] = useState<ApiAgent[]>([]);
  // Desired attachment set — initialized from the agents' enabled_mcps once
  // the workspace list lands; toggles stay local until Save.
  const [desired, setDesired] = useState<Set<string>>(new Set());
  // Save stays disabled until the agent list resolves — an empty desired set
  // from a failed load must never detach everyone server-side.
  const [agentsLoaded, setAgentsLoaded] = useState(false);
  const [agentsError, setAgentsError] = useState<string | null>(null);
  const [token, setToken] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [reauthorizing, setReauthorizing] = useState(false);

  useEffect(() => {
    let mounted = true;
    api.agents
      .list(ws)
      .then((res) => {
        if (!mounted) return;
        const list = res?.agents || [];
        setAgents(list);
        setDesired(
          new Set(
            list
              .filter((a) => (a.enabled_mcps ?? []).includes(attachId))
              .map((a) => a.id)
          )
        );
        setAgentsLoaded(true);
      })
      .catch((err: unknown) => {
        if (mounted) setAgentsError(formatApiError(err, "Couldn't load the workspace agents"));
      });
    return () => {
      mounted = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- one-shot load per opened connection
  }, [connection.id, ws]);

  // Diff against the live enabled_mcps, not the captured initial set — same
  // answer while nothing else writes, but honest if the list re-resolves.
  const agentsChanged = agents.some(
    (a) => desired.has(a.id) !== (a.enabled_mcps ?? []).includes(attachId)
  );
  const tokenDirty = token.trim() !== '';
  const dirty = agentsChanged || tokenDirty;
  const desiredIds = agents.filter((a) => desired.has(a.id)).map((a) => a.id);

  const handleSave = async () => {
    if (saving || !dirty || !agentsLoaded) return;
    setSaving(true);
    setError(null);
    // D3 ordering: token first — a failed probe must leave attachment
    // untouched, so the agents call only happens after the replace resolves.
    let tokenReplaced = false;
    if (tokenDirty) {
      try {
        await connectionsApi.replaceToken(ws, connection.id, token.trim());
        tokenReplaced = true;
      } catch (err: unknown) {
        setError(formatApiError(err, `Couldn't replace the ${name} token`));
        setSaving(false);
        return;
      }
    }
    try {
      await connectionsApi.setAgents(ws, connection.id, desiredIds);
    } catch (err: unknown) {
      // The token DID change — say so; the dialog stays open on the error.
      if (tokenReplaced) {
        onToast(`${name}'s new token was saved, but the agent update failed`, 'danger');
      }
      setError(formatApiError(err, "Couldn't update the attached agents"));
      setSaving(false);
      return;
    }
    onToast(`${name} updated`);
    onSaved?.();
    onClose();
  };

  // Same consent hand-off as the card's Reauthorize (add-connection-oauth):
  // the server returns the authorize URL and the browser goes around the
  // provider; the callback replaces the token set in place.
  const handleReauthorize = async () => {
    if (reauthorizing) return;
    setReauthorizing(true);
    try {
      const res = await connectionsApi.reauthorize(ws, connection.id);
      if (res?.authorize_url) {
        window.location.assign(res.authorize_url);
        return; // navigation in flight
      }
      onToast(`No authorization URL was returned for ${name}`, 'danger');
    } catch (err: unknown) {
      onToast(formatApiError(err, `Failed to start reauthorization for ${name}`), 'danger');
    } finally {
      setReauthorizing(false);
    }
  };

  return (
    <Modal
      title={`Edit ${name}`}
      onClose={onClose}
      odId="modal-connection-edit"
      data-testid="modal-connection-edit"
      footer={
        <>
          <button
            type="button"
            onClick={onClose}
            data-testid="btn-edit-cancel"
            className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => void handleSave()}
            disabled={!dirty || saving || !agentsLoaded}
            data-testid="btn-edit-save"
            className="flex h-9 items-center gap-2 rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-40"
          >
            {saving && <Icon name="loader" size={13} className="animate-spin" />}
            {saving ? (tokenDirty ? 'Verifying…' : 'Saving…') : 'Save changes'}
          </button>
        </>
      }
    >
      <div className="space-y-5 p-5">
        <div data-testid="edit-attached-agents">
          <SectionLabel>Attached agents</SectionLabel>
          {agentsError ? (
            <p className="text-[12px] leading-4 text-muted">
              Agents couldn't be loaded — reopen this dialog to retry.
            </p>
          ) : agents.length === 0 ? (
            <p className="text-[12px] leading-4 text-muted">
              No agents yet — create one to put {name} to work.
            </p>
          ) : (
            <AttachAgentsList
              agents={agents}
              isAttached={(a) => desired.has(a.id)}
              onToggle={(a) =>
                setDesired((prev) => {
                  const next = new Set(prev);
                  if (next.has(a.id)) {
                    next.delete(a.id);
                  } else {
                    next.add(a.id);
                  }
                  return next;
                })
              }
              disabled={saving}
            />
          )}
          <p className="mt-2 text-[11px] leading-4 text-muted">
            Agents gain or lose its tools on their next run.
          </p>
        </div>

        {isOAuth ? (
          <div data-testid="edit-reauthorize-section">
            <SectionLabel>Access token</SectionLabel>
            <p className="text-[12px] leading-4 text-muted">
              {name} holds its credentials through OAuth — there's no token to paste. Rotate them
              by signing in again.
            </p>
            <button
              type="button"
              onClick={() => void handleReauthorize()}
              disabled={reauthorizing}
              data-testid="btn-edit-reauthorize"
              className="mt-2 flex h-8 items-center gap-2 rounded-md bg-accent px-3 text-[12px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-50"
            >
              <Icon name="external-link" size={12} />
              {reauthorizing ? 'Redirecting…' : 'Reauthorize'}
            </button>
          </div>
        ) : (
          <div>
            <SectionLabel>Replace access token</SectionLabel>
            <input
              type="password"
              autoComplete="off"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              disabled={saving}
              placeholder="Paste a new token to replace"
              data-testid="input-edit-token"
              className={cx(inputCls, 'font-mono')}
            />
            <p className="mt-1.5 text-[11px] leading-4 text-muted">
              Leave empty to keep the stored token. Verified with a check before it replaces the
              old one.
              {connection.token_hint ? ` Stored token ends ····${connection.token_hint}.` : ''}
            </p>
          </div>
        )}

        {/* Display-only: origins and access levels are immutable after
            connect — nothing edits them here. */}
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5" data-testid="edit-connection-meta">
          {connection.origin && (
            <span
              className="flex min-w-0 items-center gap-1.5 text-[11px] text-muted"
              title="Connected origin — fixed after connect"
            >
              Origin <Chip mono>{connection.origin}</Chip>
            </span>
          )}
          <span className="flex items-center gap-1.5 text-[11px] text-muted">
            Access <Chip mono>{accessLevelLabel(connection.access_level)}</Chip>
          </span>
        </div>

        {(error || agentsError) && (
          <p
            role="alert"
            data-testid="edit-error"
            className="flex items-start gap-1.5 rounded-md border border-[color-mix(in_oklab,var(--danger)_35%,transparent)] bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] px-3 py-2 text-[12px] leading-4 text-danger"
          >
            <Icon name="alert" size={13} className="mt-0.5 shrink-0" />
            {error || agentsError}
          </p>
        )}
      </div>
    </Modal>
  );
}
