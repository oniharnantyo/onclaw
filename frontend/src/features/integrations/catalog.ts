/**
 * Integration service catalog (design D2): the presentation source of truth
 * for the launch MCP services. The backend stores only an opaque `Service`
 * tag on the MCP server definition — membership, endpoints, auth methods and
 * connect-sheet copy live here, and the sheet offers exactly the declared
 * auth methods. The launch set is pinned by catalog.test.ts.
 */

/** Grid filter-rail categories, in rail order. */
export type IntegrationCategory =
  | "coding"
  | "productivity"
  | "communication"
  | "business"
  | "utilities";

/** How a connect sheet can authenticate against a service endpoint. */
export type IntegrationAuthMethodKind =
  | "oauth_dcr"
  | "oauth_confidential"
  | "token";

/** One auth method a service endpoint accepts, in offer order. */
export interface IntegrationAuthMethod {
  /** Which connect flow this method drives. */
  kind: IntegrationAuthMethodKind;
  /** The connect-sheet label for this method. */
  label: string;
  /** Header style for token methods: one of the backend's MCPAuthHeaderStyle*
   * values: "bearer" (default) | "token=" | "basic" | "custom:<Scheme>" |
   * "header:<Name>". */
  headerStyle?: string;
  /** Extra authorize-URL params for confidential OAuth (Google:
   * access_type=offline + prompt=consent). */
  extraAuthParams?: Record<string, string>;
}

/** One family product a family entry can connect as its own definition. */
export interface IntegrationFamilyProduct {
  /** Product service id — the stored tag for the product's definition. */
  id: string;
  /** Display label for the product checklist and tile. */
  label: string;
  /** Fixed MCP endpoint the product's definition connects to. */
  endpoint: string;
}

/** One service's tile, connect-sheet and endpoint metadata. */
export interface IntegrationCatalogEntry {
  /** Service id, e.g. "gitlab" — the stored tag on the definition. */
  id: string;
  /** Display label for the tile and search. */
  label: string;
  /** Filter-rail category. */
  category: IntegrationCategory;
  /** Lowercase search aliases beyond the label. */
  keywords: string[];
  /** Fixed endpoint URL; empty when templated or when this is a family head
   * whose products carry the endpoints. */
  endpoint: string;
  /** Endpoint template containing "{instance}"; non-empty means the connect
   * sheet asks for the instance URL, defaulting to endpointTemplateDefault. */
  endpointTemplate?: string;
  /** Default instance URL offered by the connect sheet. */
  endpointTemplateDefault?: string;
  /** Setup hint shown in the connect sheet (e.g. GitLab's fine-grained PAT
   * requirement). */
  setupHint?: string;
  /** Auth methods the endpoint accepts, in offer order. The sheet offers
   * exactly these. */
  authMethods: IntegrationAuthMethod[];
  /** Family products: this entry is a family head when non-empty. */
  family?: IntegrationFamilyProduct[];
}

/** The launch services, in tile order. */
export const INTEGRATION_CATALOG: IntegrationCatalogEntry[] = [
  {
    id: "gitlab",
    label: "GitLab",
    category: "coding",
    keywords: ["git", "devops", "self-managed", "merge requests"],
    endpoint: "",
    endpointTemplate: "{instance}/api/v4/mcp",
    endpointTemplateDefault: "https://gitlab.com",
    setupHint:
      "Token auth needs a fine-grained PAT with the MCP tool permission; " +
      "legacy PATs are rejected with 403.",
    authMethods: [
      { kind: "oauth_dcr", label: "OAuth sign-in" },
      { kind: "token", label: "API token", headerStyle: "bearer" },
    ],
  },
  {
    id: "github",
    label: "GitHub",
    category: "coding",
    keywords: ["git", "copilot", "pull requests", "issues"],
    endpoint: "https://api.githubcopilot.com/mcp/",
    setupHint:
      "Token auth accepts a personal access token and skips the Copilot " +
      "seat requirement.",
    authMethods: [
      { kind: "oauth_dcr", label: "OAuth sign-in" },
      { kind: "token", label: "API token", headerStyle: "bearer" },
    ],
  },
  {
    id: "atlassian",
    label: "Atlassian",
    category: "productivity",
    keywords: ["jira", "confluence"],
    endpoint: "https://mcp.atlassian.com/v1/mcp",
    setupHint:
      "Token auth takes your Atlassian email and API token joined as " +
      '"email:api-token" and is gated on the admin MCP option. The SSE ' +
      "endpoint retires 2027-03; v2 uses ?tools=all for the flat list.",
    authMethods: [
      { kind: "oauth_dcr", label: "OAuth sign-in" },
      { kind: "token", label: "API token", headerStyle: "basic" },
    ],
  },
  {
    id: "linear",
    label: "Linear",
    category: "productivity",
    keywords: ["issues", "projects", "cycles"],
    endpoint: "https://mcp.linear.app/mcp",
    authMethods: [
      { kind: "oauth_dcr", label: "OAuth sign-in" },
      { kind: "token", label: "API key", headerStyle: "bearer" },
    ],
  },
  {
    id: "notion",
    label: "Notion",
    category: "productivity",
    keywords: ["wiki", "docs", "notes"],
    endpoint: "https://mcp.notion.com/mcp",
    setupHint:
      "Token auth requires a Business or Enterprise plan, so Notion offers " +
      "OAuth sign-in only.",
    authMethods: [{ kind: "oauth_dcr", label: "OAuth sign-in" }],
  },
  {
    id: "sentry",
    label: "Sentry",
    category: "coding",
    keywords: ["errors", "monitoring", "tracing"],
    endpoint: "https://mcp.sentry.dev/mcp",
    setupHint:
      "Token auth uses the Sentry-Bearer header scheme; plain Bearer is " +
      "reserved.",
    authMethods: [
      { kind: "oauth_dcr", label: "OAuth sign-in" },
      {
        kind: "token",
        label: "API token",
        headerStyle: "custom:Sentry-Bearer",
      },
    ],
  },
  {
    id: "slack",
    label: "Slack",
    category: "communication",
    keywords: ["chat", "channels", "messages"],
    endpoint: "https://mcp.slack.com/mcp",
    setupHint:
      "Connects through an internal Slack app — paste its client ID and " +
      "secret; there is no sign-in flow or static token.",
    authMethods: [
      { kind: "oauth_confidential", label: "OAuth app (client ID + secret)" },
    ],
  },
  {
    id: "google-workspace",
    label: "Google Workspace",
    category: "productivity",
    keywords: ["google", "gmail", "calendar", "drive", "sheets", "docs"],
    endpoint: "",
    setupHint:
      'Needs an own Google Cloud OAuth client with the "Use this client for ' +
      'an AI-powered agent" checkbox, plus Developer Preview enrollment.',
    authMethods: [
      {
        kind: "oauth_confidential",
        label: "OAuth app (client ID + secret)",
        extraAuthParams: { access_type: "offline", prompt: "consent" },
      },
    ],
    family: [
      {
        id: "gmail",
        label: "Gmail",
        endpoint: "https://gmailmcp.googleapis.com/mcp/v1",
      },
      {
        id: "calendar",
        label: "Google Calendar",
        endpoint: "https://calendarmcp.googleapis.com/mcp/v1",
      },
      {
        id: "drive",
        label: "Google Drive",
        endpoint: "https://drivemcp.googleapis.com/mcp/v1",
      },
      {
        id: "sheets",
        label: "Google Sheets",
        endpoint: "https://sheetsmcp.googleapis.com/mcp/v1",
      },
      {
        id: "docs",
        label: "Google Docs",
        endpoint: "https://docsmcp.googleapis.com/mcp/v1",
      },
    ],
  },
  {
    id: "google-maps",
    label: "Google Maps",
    category: "utilities",
    keywords: ["google", "maps", "grounding lite", "places"],
    endpoint: "https://mapstools.googleapis.com/mcp",
    setupHint:
      "Uses a Google Maps Platform API key, sent as the X-Goog-Api-Key " +
      "header.",
    authMethods: [
      { kind: "token", label: "API key", headerStyle: "header:X-Goog-Api-Key" },
    ],
  },
];

/** The filter-rail categories with their chip labels, in rail order. */
export const INTEGRATION_CATEGORIES: {
  id: IntegrationCategory;
  label: string;
}[] = [
  { id: "coding", label: "Coding" },
  { id: "productivity", label: "Productivity" },
  { id: "communication", label: "Communication" },
  { id: "business", label: "Business" },
  { id: "utilities", label: "Utilities" },
];

/** Catalog entry for a service id, or undefined for unknown tags. */
export function integrationCatalogEntry(
  id: string
): IntegrationCatalogEntry | undefined {
  return INTEGRATION_CATALOG.find(
    (entry: IntegrationCatalogEntry): boolean => entry.id === id
  );
}

/**
 * Whether a catalog entry matches a filter-rail search: case-insensitive
 * match against the label and keywords; an empty query matches everything.
 */
export function matchesIntegrationQuery(
  entry: IntegrationCatalogEntry,
  query: string
): boolean {
  const needle = query.trim().toLowerCase();

  if (needle === "") {
    return true;
  }

  return (
    entry.label.toLowerCase().includes(needle) ||
    entry.keywords.some((keyword: string): boolean => keyword.includes(needle))
  );
}
