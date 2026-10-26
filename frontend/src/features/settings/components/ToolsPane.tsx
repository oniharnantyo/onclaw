import { useEffect, useState } from "react";
import type { ReactElement } from "react";

import type { ToolEntry } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import { EmptyState, LoadingHint } from "../../../components";
import { Alert } from "../../../components/ui/Alert";
import { Button } from "../../../components/ui/Button";
import { Icon, type IconName } from "../../../components/ui/Icon";
import { Toggle } from "../../../components/ui/Toggle";
import { client } from "../../../lib/client";
import { cn } from "../../../lib/cn";
import BrowserConfigDialog from "./BrowserConfigDialog";
import DocumentConfigDialog from "./DocumentConfigDialog";
import WebSearchConfigDialog from "./WebSearchConfigDialog";

/** The tools with structured configuration; each one's gear opens its
 * editor. onclaw-v2 keys this off the catalog's configurable flag —
 * buddies' catalog carries no config schema yet, so the configurable tools
 * are named here. */
const WEB_SEARCH_TOOL = "web_search";

const BROWSER_TOOL = "browser";

const DOCUMENT_TOOL = "create_document";

/** Kit glyph per row, from the backend catalog's IconKey. */
function toolIcon(entry: ToolEntry): IconName {
  // SAFETY: IconKey is one of the catalog's fixed glyph names; anything
  // outside the kit map still renders the plug fallback at the call site.
  return (entry.IconKey || "plug") as IconName;
}

/** Row label: the catalog's Title Case name, falling back to the raw key
 * for bindings that predate DisplayName. */
function toolLabel(entry: ToolEntry): string {
  return entry.DisplayName || entry.Name;
}

/**
 * ToolsPane: the catalog listing with a global enable/disable per tool
 * (task 11.3), rendered in onclaw-v2's card anatomy — an accent icon tile,
 * the display name, the muted description, and a toggle; a disabled tool
 * dims to 70%. The web_search, browser, and create_document rows carry
 * gears opening their configuration dialogs. Disabled tools are simply
 * toggled off here; agent
 * turns read the same catalog. The empty state offers a next step (task
 * 4.8): rescan the tool catalog.
 */
export default function ToolsPane(): ReactElement {
  const [tools, setTools] = useState<ToolEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [configOpen, setConfigOpen] = useState(false);
  const [browserConfigOpen, setBrowserConfigOpen] = useState(false);
  const [documentConfigOpen, setDocumentConfigOpen] = useState(false);

  /** Bumped by the rescan button to re-run the catalog fetch; mount is
   * tick 0. The fetch itself lives in the effect (the dialog pattern) so no
   * setState is synchronously reachable from it. */
  const [reloadTick, setReloadTick] = useState(0);

  useEffect(() => {
    const load = async (): Promise<void> => {
      try {
        const list = await client.settings.tools();
        setTools(list);
      } catch (err) {
        setLoadError(err instanceof Error ? err.message : String(err));
      } finally {
        setLoading(false);
      }
    };

    void load();
  }, [reloadTick]);

  const toggle = async (tool: ToolEntry, enabled: boolean): Promise<void> => {
    await client.settings.setToolEnabled(tool.Name, enabled);
    setTools((prev: ToolEntry[]) =>
      prev.map((t: ToolEntry) =>
        t.Name === tool.Name ? { ...t, Enabled: enabled } : t
      )
    );
  };

  return (
    <div data-testid="tools-pane">
      <div className="mb-4">
        <h3 className="text-[15px] font-semibold text-fg">Tools</h3>
        <p className="mt-0.5 text-[12px] text-muted">
          The workspace-wide tool status. Disabled tools are removed from every
          agent in this workspace.
        </p>
      </div>

      {loading ? (
        <LoadingHint testId="tools-loading" />
      ) : loadError ? (
        <div role="alert">
          <Alert tone="danger" testId="tools-error">
            {loadError}
          </Alert>
        </div>
      ) : tools.length === 0 ? (
        <EmptyState
          title="No tools available."
          description="The tool catalog is empty — rescan to pick up registered tools."
          icon={<Icon name="zap" size={18} />}
          variant="boxed"
          testId="tools-empty"
          action={
            <Button
              variant="ghost"
              size="sm"
              data-testid="tools-reload"
              onClick={() => {
                setLoading(true);
                setLoadError(null);
                setReloadTick((n: number) => n + 1);
              }}
            >
              Rescan catalog
            </Button>
          }
        />
      ) : (
        <div className="space-y-3" data-testid="tool-list">
          {tools.map((tool: ToolEntry) => {
            const isBrowser = tool.Name === BROWSER_TOOL;
            const isDocument = tool.Name === DOCUMENT_TOOL;

            return (
              <div
                key={tool.Name}
                className={cn(
                  "flex items-center gap-3 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] p-3.5 transition-colors",
                  !tool.Enabled && "opacity-70"
                )}
                data-testid={`tool-${tool.Name}`}
              >
                <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-[color-mix(in_oklab,var(--accent)_15%,transparent)] text-accent">
                  <Icon name={toolIcon(tool)} size={16} />
                </span>

                <div className="min-w-0 flex-1">
                  <p className="truncate text-[14px] font-medium text-fg">
                    {toolLabel(tool)}
                  </p>
                  <p className="truncate text-[12px] text-muted">
                    {tool.Description}
                  </p>
                </div>

                <div className="flex items-center gap-2">
                  {(tool.Name === WEB_SEARCH_TOOL ||
                    isBrowser ||
                    isDocument) && (
                    <button
                      type="button"
                      aria-label={
                        isBrowser
                          ? "Configure Browser"
                          : isDocument
                            ? "Configure Create Document"
                            : "Configure Web Search"
                      }
                      data-testid={`btn-tool-config-${tool.Name}`}
                      onClick={() =>
                        isBrowser
                          ? setBrowserConfigOpen(true)
                          : isDocument
                            ? setDocumentConfigOpen(true)
                            : setConfigOpen(true)
                      }
                      className="flex h-8 w-8 items-center justify-center rounded-md border border-line text-muted transition-colors hover:border-accent hover:text-fg"
                    >
                      <Icon name="cog" size={15} />
                    </button>
                  )}
                  <Toggle
                    on={tool.Enabled}
                    testId={`tool-toggle-${tool.Name}`}
                    label={`Enable ${tool.Name}`}
                    onChange={(next: boolean) => void toggle(tool, next)}
                  />
                </div>
              </div>
            );
          })}
        </div>
      )}

      {configOpen && (
        <WebSearchConfigDialog onClose={() => setConfigOpen(false)} />
      )}
      {browserConfigOpen && (
        <BrowserConfigDialog onClose={() => setBrowserConfigOpen(false)} />
      )}
      {documentConfigOpen && (
        <DocumentConfigDialog onClose={() => setDocumentConfigOpen(false)} />
      )}
    </div>
  );
}
