import { describe, expect, it } from "vite-plus/test";

import {
  INTEGRATION_CATALOG,
  type IntegrationCatalogEntry,
  INTEGRATION_CATEGORIES,
  integrationCatalogEntry,
  matchesIntegrationQuery,
} from "../catalog";

/**
 * The launch service set (add-integrations spec "Initial catalog", design
 * D2: membership lives here, the backend stores an opaque Service tag).
 * A divergence from the researched launch set must fail this test.
 */
const launchServiceIds: string[] = [
  "gitlab",
  "github",
  "atlassian",
  "linear",
  "notion",
  "sentry",
  "slack",
  "google-workspace",
  "google-maps",
];

describe("INTEGRATION_CATALOG", () => {
  it("ships exactly the launch service set", () => {
    expect(
      INTEGRATION_CATALOG.map((e: { id: string }): string => e.id)
    ).toEqual(launchServiceIds);
  });

  it("gives every entry a known category, label and auth method", () => {
    const categoryIds: string[] = INTEGRATION_CATEGORIES.map(
      (c: { id: string }): string => c.id
    );

    for (const entry of INTEGRATION_CATALOG) {
      expect(categoryIds).toContain(entry.category);
      expect(entry.label.length).toBeGreaterThan(0);
      expect(entry.authMethods.length).toBeGreaterThan(0);

      for (const keyword of entry.keywords) {
        expect(keyword).toBe(keyword.toLowerCase());
      }
    }
  });

  it("carries the researched endpoint and auth facts", () => {
    expect(integrationCatalogEntry("github")?.endpoint).toBe(
      "https://api.githubcopilot.com/mcp/"
    );
    expect(integrationCatalogEntry("atlassian")?.authMethods).toContainEqual({
      kind: "token",
      label: "API token",
      headerStyle: "basic",
    });
    expect(integrationCatalogEntry("sentry")?.authMethods).toContainEqual({
      kind: "token",
      label: "API token",
      headerStyle: "custom:Sentry-Bearer",
    });
    expect(integrationCatalogEntry("google-maps")?.authMethods).toEqual([
      {
        kind: "token",
        label: "API key",
        headerStyle: "header:X-Goog-Api-Key",
      },
    ]);
  });

  it("offers no token option to OAuth-only services", () => {
    for (const id of ["notion", "slack", "google-workspace"]) {
      const entry = integrationCatalogEntry(id);
      expect(entry).toBeDefined();

      for (const method of entry?.authMethods ?? []) {
        expect(method.kind).not.toBe("token");
      }
    }

    expect(integrationCatalogEntry("slack")?.authMethods).toEqual([
      { kind: "oauth_confidential", label: "OAuth app (client ID + secret)" },
    ]);
    expect(integrationCatalogEntry("notion")?.authMethods).toEqual([
      { kind: "oauth_dcr", label: "OAuth sign-in" },
    ]);
  });

  it("ships Google Workspace as the only family, with 5 products", () => {
    const familyEntries = INTEGRATION_CATALOG.filter(
      (entry: IntegrationCatalogEntry): boolean =>
        (entry.family?.length ?? 0) > 0
    );

    expect(
      familyEntries.map((entry: IntegrationCatalogEntry): string => entry.id)
    ).toEqual(["google-workspace"]);

    expect(
      familyEntries[0]?.family?.map((p: { id: string }): string => p.id)
    ).toEqual(["gmail", "calendar", "drive", "sheets", "docs"]);

    const endpoints = familyEntries[0]?.family?.map(
      (p: { endpoint: string }): string => p.endpoint
    );

    expect(new Set(endpoints).size).toBe(endpoints?.length);
  });

  it("carries Google's confidential OAuth consent params", () => {
    const method = integrationCatalogEntry("google-workspace")?.authMethods[0];
    expect(method?.kind).toBe("oauth_confidential");
    expect(method?.extraAuthParams).toEqual({
      access_type: "offline",
      prompt: "consent",
    });
  });

  it("templates self-managed endpoints and fixes the rest", () => {
    const gitlab = integrationCatalogEntry("gitlab");
    expect(gitlab?.endpoint).toBe("");
    expect(gitlab?.endpointTemplate).toBe("{instance}/api/v4/mcp");
    expect(gitlab?.endpointTemplateDefault).toBe("https://gitlab.com");

    for (const entry of INTEGRATION_CATALOG) {
      if (entry.id === "gitlab") {
        continue;
      }

      expect(entry.endpointTemplate).toBeUndefined();

      if (entry.family === undefined) {
        expect(entry.endpoint).toMatch(/^https:\/\//);
      }
    }
  });

  it("falls back to undefined for unknown service tags", () => {
    expect(integrationCatalogEntry("decision-class")).toBeUndefined();
    expect(integrationCatalogEntry("gmail")).toBeUndefined();
  });
});

describe("matchesIntegrationQuery", () => {
  const gitlab = integrationCatalogEntry("gitlab")!;
  const gmail = integrationCatalogEntry("google-workspace")!;

  it("matches labels case-insensitively", () => {
    expect(matchesIntegrationQuery(gitlab, "gitlab")).toBe(true);
    expect(matchesIntegrationQuery(gitlab, "GitLab")).toBe(true);
    expect(matchesIntegrationQuery(gitlab, "git")).toBe(true);
  });

  it("matches catalog keywords", () => {
    expect(matchesIntegrationQuery(gitlab, "SELF-MANAGED")).toBe(true);
    expect(matchesIntegrationQuery(gmail, "gmail")).toBe(true);
    expect(matchesIntegrationQuery(gitlab, "jira")).toBe(false);
  });

  it("treats an empty query as matching everything", () => {
    expect(matchesIntegrationQuery(gitlab, "")).toBe(true);
    expect(matchesIntegrationQuery(gitlab, "   ")).toBe(true);
  });
});
