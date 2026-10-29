/**
 * Types for the settings panes: which pane is active, and the add/edit
 * dialog draft shapes the four CRUD panes keep in local state.
 */
import type {
  Hook,
  MCPServer,
} from "../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type {
  HookTestResult,
  ProviderView,
} from "../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { IconName } from "../../components/ui/Icon";

export type PaneKey =
  | "providers"
  | "integrations"
  | "channels"
  | "skills"
  | "tools"
  | "hooks"
  | "mcp";

/**
 * One static sidebar entry of the settings nav: which pane it opens plus
 * the label, kit icon, and one-line description. Entries are grouped into
 * sections (see SettingsPaneSection).
 */
export interface SettingsPaneConfig {
  key: PaneKey;
  label: string;
  icon: IconName;
  description: string;
}

/**
 * One grouped sidebar section of the settings nav (add-integrations
 * "Settings navigation layout"): a small uppercase label plus the panes
 * under it, in nav order.
 */
export interface SettingsPaneSection {
  /** Section id: "models" | "integrations" | "automation". */
  id: string;
  /** Uppercase section label: MODELS | INTEGRATIONS | AUTOMATION. */
  label: string;
  /** The panes in the section, in nav order. */
  panes: SettingsPaneConfig[];
}

export interface DialogState<T> {
  mode: "add" | "edit";
  draft: T;
}

/**
 * Form values of the provider add/edit dialog (design D7). The API key is
 * write-only: it starts blank on every open and is submitted verbatim, so a
 * blank key on edit preserves the stored secret (design D2).
 */
export interface ProviderFormValues {
  type: string;
  name: string;
  baseURL: string;
  apiKey: string;
  enabled: boolean;
}

/** Providers-pane dialog target: a new provider, or the row being edited. */
export interface ProviderDialogState {
  mode: "add" | "edit";
  /** The edited provider row in edit mode; null on add. */
  provider: ProviderView | null;
}

export type McpDialogState = DialogState<MCPServer> & {
  /** Args as one-per-line editor text. */
  argsText: string;
  /** Env as `KEY=VALUE` per line. */
  envText: string;
  /** Headers as `KEY=VALUE` per line. */
  headersText: string;
};

export type HookDialogState = DialogState<Hook>;

/** Per-hook inline test outcome. */
export interface TestOutcome {
  hookID: string;
  result?: HookTestResult;
  error?: string;
}
