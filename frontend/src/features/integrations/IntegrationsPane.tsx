import { useEffect, useMemo, useState } from "react";
import type {
  ChangeEvent,
  KeyboardEvent,
  MouseEvent,
  ReactElement,
} from "react";

import type { IntegrationConnectInput } from "../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { MCPServer } from "../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import {
  ConfirmDialog,
  EmptyState,
  FormField,
  IconButton,
  LoadingHint,
  ModalShell,
  PaneHeader,
} from "../../components";
import { Alert } from "../../components/ui/Alert";
import { Button } from "../../components/ui/Button";
import { Checkbox } from "../../components/ui/Checkbox";
import { Chip } from "../../components/ui/Chip";
import { Icon } from "../../components/ui/Icon";
import { Input } from "../../components/ui/Input";
import { StatusDot } from "../../components/ui/StatusDot";
import type { StatusDotStatus } from "../../components/ui/StatusDot";
import { ServiceMark } from "../../lib/brandMarks";
import { client } from "../../lib/client";
import type { MCPOAuthCompletedEvent } from "../../lib/events";
import { cn } from "../../lib/cn";
import type { SettingsPaneConfig } from "../settings/types";
import {
  INTEGRATION_CATALOG,
  INTEGRATION_CATEGORIES,
  matchesIntegrationQuery,
} from "./catalog";
import type {
  IntegrationAuthMethod,
  IntegrationCatalogEntry,
  IntegrationCategory,
  IntegrationFamilyProduct,
} from "./catalog";

/** Connection status vocabulary persisted by the backend probe (the store's
 * mcp_servers.status column) — the same grammar the MCP pane renders
 * (McpPane STATUS_*): auth_required is the OAuth row state, the definition
 * dialed but user authorization is the missing piece. */
const STATUS_CONNECTED = "connected";

const STATUS_ERROR = "error";

const STATUS_AUTH_REQUIRED = "auth_required";

type CategoryFilter = IntegrationCategory | "all";

/** The stored status as the kit dot's vocabulary: green when connected, red
 * when failed, amber when authorization is required, neutral otherwise. */
function dotStatus(status: string): StatusDotStatus {
  if (status === STATUS_CONNECTED) return "running";

  if (status === STATUS_ERROR) return "error";

  if (status === STATUS_AUTH_REQUIRED) return "warn";

  return "idle";
}

/** The human label for a tile's stored status (the MCP row grammar's
 * vocabulary, kept verbatim so both panes speak the same language). */
function statusLabel(status: string): string {
  if (status === STATUS_CONNECTED) return "connected";

  if (status === STATUS_ERROR) return "failed";

  if (status === STATUS_AUTH_REQUIRED) return "authorization required";

  return status || "not probed";
}

/** The stored rows backing one catalog entry's connect/edit sheet: the row
 * tagged with the entry's id, or — for a family head — the rows tagged with
 * the family's product ids (the backend's family connect tags each product
 * row with its product id, not "<head>/<product>"). */
function entryRows(
  entry: IntegrationCatalogEntry,
  byService: Map<string, MCPServer>
): MCPServer[] {
  if (entry.family?.length) {
    const rows: MCPServer[] = [];

    for (const product of entry.family) {
      const row = byService.get(product.id);

      if (row) rows.push(row);
    }

    return rows;
  }

  const row = byService.get(entry.id);

  return row ? [row] : [];
}

/** The fixed tail of a templated endpoint — the template with its instance
 * hole removed, e.g. "{instance}/api/v4/mcp" → "/api/v4/mcp". */
function templateSuffix(entry: IntegrationCatalogEntry): string {
  return (entry.endpointTemplate ?? "").replace("{instance}", "");
}

/** The connect input's Endpoint (the backend requires and stores it): the
 * template filled with the trimmed, de-slashed instance URL; for a family
 * the first checked product's endpoint (the backend stamps every product
 * row with it); otherwise the entry's fixed endpoint. */
function sheetEndpoint(
  entry: IntegrationCatalogEntry,
  instanceUrl: string,
  products: string[]
): string {
  if (entry.endpointTemplate) {
    const instance = instanceUrl.trim().replace(/\/+$/, "");

    return entry.endpointTemplate.replace("{instance}", instance);
  }

  if (entry.family?.length) {
    const first =
      entry.family.find((p: IntegrationFamilyProduct): boolean =>
        products.includes(p.id)
      ) ?? entry.family[0];

    return first.endpoint;
  }

  return entry.endpoint;
}

/** The connect/edit sheet's local draft: the catalog entry plus what the
 * user typed. Credential fields start blank on every open (write-only);
 * the mode is connect for a service with no backing row, edit otherwise. */
interface SheetState {
  entry: IntegrationCatalogEntry;
  mode: "connect" | "edit";
  /** Instance URL for templated endpoints. */
  instanceUrl: string;
  /** The selected auth method kind — one of entry.authMethods' kinds. */
  authKind: string;
  credential: string;
  clientId: string;
  clientSecret: string;
  /** Checked family product ids. */
  checked: string[];
  /** Product ids already backed by rows (edit mode): they render as
   * connected chips and are never re-submitted. */
  connected: string[];
}

/** One filter-rail chip: the category label plus its catalog-wide count,
 * accent-tinted while active (the OptionChips grammar). */
function FilterChip({
  id,
  label,
  count,
  active,
  onClick,
}: {
  id: string;
  label: string;
  count: number;
  active: boolean;
  onClick: () => void;
}): ReactElement {
  return (
    <button
      type="button"
      data-testid={`integrations-chip-${id}`}
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "flex h-8 items-center gap-1.5 rounded-md border px-3 text-[12px] font-medium transition-colors",
        active
          ? "border-accent bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] text-fg"
          : "border-line text-muted hover:border-[color-mix(in_oklab,var(--fg)_26%,transparent)] hover:text-fg2"
      )}
    >
      {label}
      <span className="text-muted">{count}</span>
    </button>
  );
}

/** One flat grid tile: a single-service catalog entry, or one product of a
 * family entry — each family product is its own tile with its own
 * connection state (spec: a product connects or removes independently),
 * while the sheet still opens on the family head's shared setup. */
interface GridTile {
  /** The tile's service id: the testid suffix and the stored row's tag. */
  serviceId: string;
  /** The tile's display label. */
  label: string;
  /** The catalog entry driving the tile's sheet (the family head for
   * products). */
  entry: IntegrationCatalogEntry;
  /** The family product when this tile is a family product tile. */
  product: IntegrationFamilyProduct | null;
}

/** One grid tile: brand glyph, label, status dot, and the connected tile's
 * state detail — the failure reason, the Authorize call to action, and the
 * Edit/Remove affordances. The whole tile opens the connect/edit sheet (for
 * a family product, the family's shared-setup sheet with the product
 * checklist). */
function IntegrationTile({
  tile,
  row,
  authorizing,
  onOpen,
  onAuthorize,
  onEdit,
  onRemove,
}: {
  tile: GridTile;
  row: MCPServer | null;
  authorizing: Set<string>;
  onOpen: () => void;
  onAuthorize: (row: MCPServer) => void;
  onEdit: () => void;
  onRemove: () => void;
}): ReactElement {
  // A definition switched off in the MCP pane keeps its tile but dims it and
  // pins the dot neutral (spec: toggle semantics carry over, kept simple).
  const disabled = row !== null && !row.Enabled;

  // The tile's state is its own row's state only — a family product never
  // inherits a sibling's status (spec: each product has its own connection
  // state).
  const status = row?.Status ?? "";
  const statusError = row?.StatusError ?? "";
  const connected = row !== null;
  const dot = disabled ? "idle" : dotStatus(status);

  return (
    <div
      data-testid={`integrations-tile-${tile.serviceId}`}
      role="button"
      tabIndex={0}
      onClick={onOpen}
      onKeyDown={(e: KeyboardEvent<HTMLDivElement>): void => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onOpen();
        }
      }}
      className={cn(
        "flex cursor-pointer flex-col gap-2 rounded-lg border border-line bg-surface p-3 text-left transition-colors hover:border-[color-mix(in_oklab,var(--fg)_25%,var(--border))]",
        disabled && "opacity-60"
      )}
    >
      <div className="flex items-start gap-2">
        <ServiceMark type={tile.serviceId} name={tile.label} size={36} />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <span className="truncate text-[13px] font-medium text-fg">
              {tile.label}
            </span>
            <span
              data-testid={`integrations-dot-${tile.serviceId}`}
              data-status={dot}
              className="inline-flex shrink-0"
            >
              <StatusDot status={dot} className="shrink-0" />
            </span>
          </div>
          <span className="text-[11px] text-muted">
            {connected ? statusLabel(status) : "Not connected"}
          </span>
        </div>
      </div>

      {status === STATUS_ERROR && statusError !== "" && (
        <div
          className="truncate text-xs text-dangertext"
          title={statusError}
          data-testid={`integrations-reason-${tile.serviceId}`}
        >
          {statusError}
        </div>
      )}

      {row !== null && status === STATUS_AUTH_REQUIRED && (
        <Button
          size="sm"
          loading={authorizing.has(row.ID)}
          data-testid={`integrations-authorize-${tile.serviceId}`}
          onClick={(e: MouseEvent<HTMLButtonElement>): void => {
            // The tile's own click opens the sheet; Authorize must not.
            e.stopPropagation();
            onAuthorize(row);
          }}
        >
          <Icon name="shield" size={14} />
          Authorize
        </Button>
      )}

      {connected && (
        // Stop the tile's sheet-opening click under the row affordances.
        <div
          className="flex items-center gap-1"
          onClick={(e: MouseEvent<HTMLDivElement>): void => e.stopPropagation()}
        >
          <IconButton
            label={`Edit ${tile.label}`}
            size="sm"
            testId={`integrations-edit-${tile.serviceId}`}
            icon={<Icon name="edit" size={16} />}
            onClick={onEdit}
          />
          <IconButton
            label={`Remove ${tile.label}`}
            size="sm"
            testId={`integrations-remove-${tile.serviceId}`}
            className="text-danger hover:bg-[color-mix(in_oklab,var(--danger)_12%,transparent)] hover:text-danger"
            icon={<Icon name="trash" size={16} />}
            onClick={onRemove}
          />
        </div>
      )}
    </div>
  );
}

/** IntegrationsPane: the service catalog as one flat tile grid (design D5)
 * over a filter rail — category chips with catalog-wide tile counts and a
 * keyword search that compose. Every family product is its own tile with
 * its own connection state (spec: a product connects or removes
 * independently after the shared setup exists); clicking any family tile
 * reopens the family's shared-setup sheet with the product checklist.
 * Clicking a not-connected tile opens the catalog-driven connect sheet;
 * clicking a tile with a backing definition reopens it in edit mode (stored
 * non-secret values prefilled, credential fields blank — the stored
 * credential survives a blank submission). Connecting submits one
 * IntegrationConnectInput the backend turns into probed, service-tagged MCP
 * server definitions (family: one per checked product, each mapped to its
 * catalog endpoint); tiles mirror the definitions' live state and refetch
 * on the mcp:oauth:completed event, exactly like the MCP pane. Removing
 * deletes the tile's own backing definition (a family product removes only
 * its row). */
export default function IntegrationsPane({
  pane,
}: {
  pane: SettingsPaneConfig;
}): ReactElement {
  const [servers, setServers] = useState<MCPServer[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [category, setCategory] = useState<CategoryFilter>("all");
  const [query, setQuery] = useState("");
  const [sheet, setSheet] = useState<SheetState | null>(null);
  const [sheetError, setSheetError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const [removing, setRemoving] = useState<{
    label: string;
    row: MCPServer;
  } | null>(null);

  // Rows with an authorization flow in flight — the id stays in the set from
  // the Authorize click until the mcp:oauth:completed event (or a rejection)
  // clears it, so the button holds its loading state (the MCP pane pattern).
  const [authorizing, setAuthorizing] = useState<Set<string>>(
    () => new Set<string>()
  );

  const load = async (): Promise<void> => {
    setLoading(true);
    setLoadError(null);

    try {
      setServers(await client.settings.mcpServers());
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  // OAuth completion: the backend emits mcp:oauth:completed once the browser
  // flow ends — after re-probing the row — so the fetched row carries the
  // fresh state; a failed flow surfaces its reason in the shared alert. The
  // returned unsubscribe tears the subscription down on unmount.
  useEffect(() => {
    const onCompleted = async (ev: MCPOAuthCompletedEvent): Promise<void> => {
      setAuthorizing((prev: Set<string>) => {
        const next = new Set(prev);

        next.delete(ev.serverId);

        return next;
      });

      if (!ev.ok) setLoadError(ev.error || "Authorization failed");

      try {
        const fresh = await client.settings.mcpServer(ev.serverId);
        setServers((prev: MCPServer[]): MCPServer[] => {
          const i = prev.findIndex(
            (s: MCPServer): boolean => s.ID === fresh.ID
          );

          if (i < 0) return [...prev, fresh];

          const next = [...prev];

          next[i] = fresh;

          return next;
        });
      } catch (err) {
        setLoadError(err instanceof Error ? err.message : String(err));
      }
    };

    return client.events.onMcpOAuthCompleted(
      (ev: MCPOAuthCompletedEvent): void => void onCompleted(ev)
    );
  }, []);

  // Service tag → row. Rows with an empty tag are ordinary MCP servers and
  // stay out of the grid entirely (spec: custom servers stay out).
  const byService = useMemo((): Map<string, MCPServer> => {
    const map = new Map<string, MCPServer>();

    for (const s of servers) {
      if (s.Service) map.set(s.Service, s);
    }

    return map;
  }, [servers]);

  // The flat tile list: one tile per single-service entry, one per family
  // product — the family head itself renders no tile (spec "Each product
  // SHALL appear as its own tile").
  const catalogTiles = useMemo((): GridTile[] => {
    const tiles: GridTile[] = [];

    for (const entry of INTEGRATION_CATALOG) {
      if (entry.family?.length) {
        for (const product of entry.family) {
          tiles.push({
            serviceId: product.id,
            label: product.label,
            entry,
            product,
          });
        }
      } else {
        tiles.push({
          serviceId: entry.id,
          label: entry.label,
          entry,
          product: null,
        });
      }
    }

    return tiles;
  }, []);

  // Chip counts are catalog-wide tile counts (a family contributes one per
  // product, matching the grid): never affected by the active search.
  const categoryCounts = useMemo((): Map<string, number> => {
    const counts = new Map<string, number>();

    for (const tile of catalogTiles) {
      counts.set(
        tile.entry.category,
        (counts.get(tile.entry.category) ?? 0) + 1
      );
    }

    return counts;
  }, [catalogTiles]);

  // The filters compose: the category narrows, the search narrows further.
  // Matching stays entry-level — a family matches as a whole, so all of its
  // product tiles appear or none do.
  const tiles = catalogTiles.filter(
    (tile: GridTile): boolean =>
      (category === "all" || tile.entry.category === category) &&
      matchesIntegrationQuery(tile.entry, query)
  );

  const clearFilters = (): void => {
    setCategory("all");
    setQuery("");
  };

  /** Opens the sheet for one entry: connect mode with the catalog defaults,
   * or edit mode with the stored non-secret values prefilled and the
   * credential fields blank. */
  const openSheet = (entry: IntegrationCatalogEntry): void => {
    const rows = entryRows(entry, byService);
    const head = rows[0] ?? null;
    const connected = rows.map((r: MCPServer): string => r.Service);
    const mode = rows.length > 0 ? "edit" : "connect";
    const suffix = templateSuffix(entry);

    const instanceUrl =
      entry.endpointTemplate !== undefined &&
      head !== null &&
      suffix !== "" &&
      head.URL.endsWith(suffix)
        ? head.URL.slice(0, head.URL.length - suffix.length)
        : (entry.endpointTemplateDefault ?? "");

    // A stored integration credential selects the token method on edit;
    // otherwise the catalog's first method stays preselected.
    const authKind =
      mode === "edit" &&
      head?.CredentialSet &&
      entry.authMethods.some(
        (m: IntegrationAuthMethod): boolean => m.kind === "token"
      )
        ? "token"
        : entry.authMethods[0].kind;

    setSheetError(null);
    setSheet({
      entry,
      mode,
      instanceUrl,
      authKind,
      credential: "",
      clientId: head?.Auth?.client_id ?? "",
      clientSecret: "",
      checked:
        (entry.family?.length ?? 0) > 0
          ? (entry.family ?? [])
              .map((p: IntegrationFamilyProduct): string => p.id)
              .filter((id: string): boolean => !connected.includes(id))
          : [],
      connected,
    });
  };

  const closeSheet = (): void => {
    setSheet(null);
    setSheetError(null);
  };

  const updateSheet = (patch: Partial<SheetState>): void => {
    if (!sheet) return;
    setSheet({ ...sheet, ...patch });
  };

  const toggleProduct = (id: string, next: boolean): void => {
    if (!sheet) return;

    updateSheet({
      checked: next
        ? [...sheet.checked, id]
        : sheet.checked.filter((c: string): boolean => c !== id),
    });
  };

  const method: IntegrationAuthMethod | undefined = sheet
    ? (sheet.entry.authMethods.find(
        (m: IntegrationAuthMethod): boolean => m.kind === sheet.authKind
      ) ?? sheet.entry.authMethods[0])
    : undefined;

  const isFamily = (sheet?.entry.family?.length ?? 0) > 0;

  // Family submits carry only the newly checked products (edit mode's
  // already-connected products render as chips and are never re-sent).
  const newlyChecked = sheet
    ? sheet.checked.filter(
        (id: string): boolean => !sheet.connected.includes(id)
      )
    : [];

  const familyDone = isFamily && sheet !== null && newlyChecked.length === 0;
  // The connect-time requirements mirror the backend's integrationAuth: a
  // token method needs the credential, a confidential method the client
  // pair. On edit the stored values survive blank submissions, so only the
  // client id (prefilled) is still required.
  let methodReady = true;

  if (sheet && method) {
    if (sheet.mode === "connect") {
      if (method.kind === "token") {
        methodReady = sheet.credential.trim() !== "";
      }

      if (method.kind === "oauth_confidential") {
        methodReady =
          sheet.clientId.trim() !== "" && sheet.clientSecret.trim() !== "";
      }
    } else if (method.kind === "oauth_confidential") {
      methodReady = sheet.clientId.trim() !== "";
    }
  }

  const canSubmit = sheet !== null && !familyDone && methodReady;

  const submit = async (): Promise<void> => {
    if (!sheet || !method) return;

    // Family connects map every created product to its own catalog endpoint
    // (design D6: products dial different hosts); single-service connects
    // carry none.
    const productEndpoints: { [id: string]: string } | null =
      isFamily && newlyChecked.length > 0
        ? Object.fromEntries(
            (sheet.entry.family ?? [])
              .filter((p: IntegrationFamilyProduct): boolean =>
                newlyChecked.includes(p.id)
              )
              .map((p: IntegrationFamilyProduct): [string, string] => [
                p.id,
                p.endpoint,
              ])
          )
        : null;

    const input: IntegrationConnectInput = {
      Service: sheet.entry.id,
      Endpoint: sheetEndpoint(sheet.entry, sheet.instanceUrl, newlyChecked),
      AuthMethod: method.kind,
      // A blank credential keeps the stored one (write-only fields): the
      // hint is never submitted as a value.
      Credential: sheet.credential,
      ClientID: sheet.clientId,
      ClientSecret: sheet.clientSecret,
      ExtraAuthParams: method.extraAuthParams
        ? { ...method.extraAuthParams }
        : null,
      // The catalog-declared token header style ("" = the bearer default).
      CredentialHeaderStyle: method.headerStyle ?? "",
      Products: newlyChecked.length > 0 ? newlyChecked : null,
      ProductEndpoints: productEndpoints,
    };

    setSaving(true);
    setSheetError(null);

    try {
      await client.settings.connectIntegration(input);
      setSheet(null);
      await load();
    } catch (err) {
      setSheetError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  // Row authorize: starts the out-of-band browser flow and returns
  // immediately — the completion event (handled above) refreshes the row.
  const authorize = async (row: MCPServer): Promise<void> => {
    setAuthorizing((prev: Set<string>) => new Set(prev).add(row.ID));

    try {
      await client.settings.authorizeMCPServer(row.ID);
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err));
      setAuthorizing((prev: Set<string>) => {
        const next = new Set(prev);

        next.delete(row.ID);

        return next;
      });
    }
  };

  // Removal deletes the tile's own backing definition — for a family
  // product tile, that product's row only (spec: a product is removed
  // independently).
  const confirmRemove = async (): Promise<void> => {
    if (!removing) return;

    await client.settings.disconnectIntegration(removing.row.ID);
    setRemoving(null);
    await load();
  };

  const storedRow: MCPServer | null = sheet
    ? (entryRows(sheet.entry, byService)[0] ?? null)
    : null;

  // The edit sheet's credential placeholder: only ever a redacted hint — the
  // stored value itself never round-trips to the frontend.
  const credentialPlaceholder =
    sheet?.mode === "edit" && storedRow?.CredentialSet
      ? `Stored (····${storedRow.CredentialHint})`
      : "";

  return (
    <div data-testid="integrations-pane">
      <PaneHeader description={pane.description} />

      <div className="mb-4 flex flex-wrap items-center gap-2">
        <div className="relative">
          <Icon
            name="search"
            size={14}
            className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-muted"
          />
          <Input
            data-testid="integrations-search"
            type="search"
            aria-label="Search services"
            placeholder="Search services…"
            className="w-56 pl-8"
            value={query}
            onChange={(e: ChangeEvent<HTMLInputElement>) =>
              setQuery(e.target.value)
            }
          />
        </div>
        <div className="flex flex-wrap items-center gap-1.5">
          <FilterChip
            id="all"
            label="All"
            count={catalogTiles.length}
            active={category === "all"}
            onClick={() => setCategory("all")}
          />
          {INTEGRATION_CATEGORIES.map(
            (c: { id: IntegrationCategory; label: string }) => (
              <FilterChip
                key={c.id}
                id={c.id}
                label={c.label}
                count={categoryCounts.get(c.id) ?? 0}
                active={category === c.id}
                onClick={() => setCategory(c.id)}
              />
            )
          )}
        </div>
      </div>

      {loading ? (
        <LoadingHint testId="integrations-loading" />
      ) : loadError ? (
        <div role="alert">
          <Alert tone="danger" testId="integrations-error">
            {loadError}
          </Alert>
        </div>
      ) : tiles.length === 0 ? (
        <EmptyState
          title="No services match."
          description="No integration matches the active category and search — clear the filters to see the full catalog."
          icon={<Icon name="search" size={18} />}
          testId="integrations-empty"
          action={
            <Button
              size="sm"
              data-testid="integrations-clear-filters"
              onClick={clearFilters}
            >
              Clear filters
            </Button>
          }
        />
      ) : (
        // One flat grid, never grouped by category — category lives only in
        // the filter rail (spec scenario "No category headers").
        <div
          data-testid="integrations-grid"
          className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-4"
        >
          {tiles.map((t: GridTile) => {
            const row = byService.get(t.serviceId) ?? null;

            return (
              <IntegrationTile
                key={t.serviceId}
                tile={t}
                row={row}
                authorizing={authorizing}
                onOpen={() => openSheet(t.entry)}
                onAuthorize={(r: MCPServer) => void authorize(r)}
                onEdit={() => openSheet(t.entry)}
                onRemove={(): void => {
                  if (row) setRemoving({ label: t.label, row });
                }}
              />
            );
          })}
        </div>
      )}

      {sheet && (
        <ModalShell
          title={
            sheet.mode === "connect"
              ? `Connect ${sheet.entry.label}`
              : `Edit ${sheet.entry.label}`
          }
          testId="integrations-connect-sheet"
          onClose={closeSheet}
          actions={
            <>
              <Button
                variant="ghost"
                size="sm"
                data-testid="integrations-cancel"
                onClick={closeSheet}
              >
                Cancel
              </Button>
              <Button
                size="sm"
                data-testid="integrations-connect-submit"
                disabled={!canSubmit || saving}
                loading={saving}
                onClick={() => void submit()}
              >
                {familyDone
                  ? "Everything connected"
                  : sheet.mode === "connect"
                    ? "Connect"
                    : "Save"}
              </Button>
            </>
          }
        >
          <div className="flex flex-col gap-3">
            {sheet.entry.setupHint && (
              <Alert tone="info" testId="integrations-setup-hint">
                {sheet.entry.setupHint}
              </Alert>
            )}

            {sheetError && (
              <div role="alert">
                <Alert tone="danger" testId="integrations-sheet-error">
                  {sheetError}
                </Alert>
              </div>
            )}

            {sheet.entry.endpointTemplate ? (
              <FormField id="integrations-instance-url" label="Instance URL">
                <Input
                  id="integrations-instance-url"
                  className="w-full"
                  data-testid="integrations-instance-url"
                  value={sheet.instanceUrl}
                  onChange={(e: ChangeEvent<HTMLInputElement>) =>
                    updateSheet({ instanceUrl: e.target.value })
                  }
                />
              </FormField>
            ) : sheet.entry.endpoint ? (
              <p
                className="text-xs text-muted"
                data-testid="integrations-endpoint"
              >
                Endpoint: {sheet.entry.endpoint}
              </p>
            ) : null}

            <div className="flex flex-col gap-1.5">
              <span className="text-[12px] font-medium text-fg2">
                Authentication
              </span>
              {sheet.entry.authMethods.map((m: IntegrationAuthMethod) => (
                <label
                  key={m.kind}
                  className="flex items-center gap-2 text-[13px] text-fg2"
                >
                  <input
                    type="radio"
                    name="integrations-auth-method"
                    data-testid={`integrations-auth-method-${m.kind}`}
                    checked={sheet.authKind === m.kind}
                    onChange={() => updateSheet({ authKind: m.kind })}
                  />
                  {m.label}
                </label>
              ))}
            </div>

            {method?.kind === "token" && (
              <FormField id="integrations-credential" label="API token">
                <Input
                  id="integrations-credential"
                  className="w-full"
                  data-testid="integrations-credential"
                  type="password"
                  autoComplete="off"
                  value={sheet.credential}
                  placeholder={credentialPlaceholder}
                  onChange={(e: ChangeEvent<HTMLInputElement>) =>
                    updateSheet({ credential: e.target.value })
                  }
                />
              </FormField>
            )}

            {method?.kind === "oauth_confidential" && (
              <>
                <FormField id="integrations-client-id" label="Client ID">
                  <Input
                    id="integrations-client-id"
                    className="w-full"
                    data-testid="integrations-client-id"
                    autoComplete="off"
                    value={sheet.clientId}
                    onChange={(e: ChangeEvent<HTMLInputElement>) =>
                      updateSheet({ clientId: e.target.value })
                    }
                  />
                </FormField>
                <FormField
                  id="integrations-client-secret"
                  label="Client secret"
                >
                  <Input
                    id="integrations-client-secret"
                    className="w-full"
                    data-testid="integrations-client-secret"
                    type="password"
                    autoComplete="off"
                    value={sheet.clientSecret}
                    placeholder={credentialPlaceholder}
                    onChange={(e: ChangeEvent<HTMLInputElement>) =>
                      updateSheet({ clientSecret: e.target.value })
                    }
                  />
                </FormField>
              </>
            )}

            {(sheet.entry.family?.length ?? 0) > 0 && (
              <div className="flex flex-col gap-1.5">
                <span className="text-[12px] font-medium text-fg2">
                  Products
                </span>
                {sheet.entry.family?.map((p: IntegrationFamilyProduct) =>
                  sheet.connected.includes(p.id) ? (
                    <span key={p.id} className="flex items-center gap-2">
                      <Chip
                        data-testid={`integrations-product-connected-${p.id}`}
                        className="border-[color-mix(in_oklab,var(--success)_35%,transparent)] bg-[color-mix(in_oklab,var(--success)_8%,transparent)]"
                      >
                        connected
                      </Chip>
                      <span className="text-[13px] text-fg2">{p.label}</span>
                    </span>
                  ) : (
                    <Checkbox
                      key={p.id}
                      checked={sheet.checked.includes(p.id)}
                      onChange={(next: boolean) => toggleProduct(p.id, next)}
                      label={p.label}
                      testId={`integrations-product-${p.id}`}
                    />
                  )
                )}
              </div>
            )}
          </div>
        </ModalShell>
      )}

      {removing && (
        <ConfirmDialog
          title={`Remove ${removing.label}?`}
          testId="integrations-remove-dialog"
          confirmLabel="Remove"
          confirmTestId="integrations-remove-confirm"
          cancelTestId="integrations-remove-cancel"
          onConfirm={() => void confirmRemove()}
          onClose={() => setRemoving(null)}
        >
          <p className="text-[13px] text-muted">
            This removes the integration's server definition.
          </p>
        </ConfirmDialog>
      )}
    </div>
  );
}
