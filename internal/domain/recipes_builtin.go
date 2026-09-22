package domain

// The v1 built-in recipe registry contents (design.md D2, tasks.md 1.2):
// GitHub and GitLab available (PAT), Atlassian (one recipe covering Jira and
// Confluence), Slack, and Linear declared coming-soon (OAuth-kind). Endpoint
// and probe facts are release-shippable recipe data; the live-verification
// pass (tasks.md 5.3) re-confirms and pins them before release.
//
// Registration order is the gallery order.
func init() {
	RegisterRecipe(Recipe{
		ID:           "github",
		Service:      "GitHub",
		Icon:         "github",
		AuthKind:     RecipeAuthPAT,
		Availability: RecipeAvailable,
		Transport:    MCPTransportStreamableHTTP,
		// Official GitHub MCP server remote endpoint (tasks.md 5.3 pins).
		Endpoint:    "https://api.githubcopilot.com/mcp/",
		TokenHeader: "Authorization",
		TokenScheme: "Bearer",
		AccessLevels: []string{
			ConnectionAccessReadOnly,
			ConnectionAccessReadWrite,
		},
		Steps: []RecipeStep{
			{
				Title: "Open GitHub token settings",
				Detail: "Sign in to GitHub and go to Settings → Developer settings → " +
					"Personal access tokens → Fine-grained tokens.",
				URL: "https://github.com/settings/personal-access-tokens",
			},
			{
				Title: "Generate a new token",
				Detail: "Create a token for this workspace. Under Repository access, select " +
					"the repositories the agents should reach, then grant the permissions listed below.",
			},
			{
				Title: "Copy the token",
				Detail: "Copy the token now — GitHub shows it once. Paste it into the connect dialog.",
			},
		},
		Scopes: []RecipeScopes{
			{
				AccessLevel: ConnectionAccessReadOnly,
				Scopes: []string{
					"Repositories: read-only",
					"Metadata: read-only",
				},
			},
			{
				AccessLevel: ConnectionAccessReadWrite,
				Scopes: []string{
					"Repositories: read and write",
					"Metadata: read-only",
				},
			},
		},
		Probe: RecipeProbe{Tool: "list_repositories"},
	})

	RegisterRecipe(Recipe{
		ID:           "gitlab",
		Service:      "GitLab",
		Icon:         "gitlab",
		AuthKind:     RecipeAuthPAT,
		Availability: RecipeAvailable,
		Transport:    MCPTransportStreamableHTTP,
		// PLACEHOLDER pending tasks.md 5.3 live pinning: hosted remote
		// endpoint preferred over the official stdio server (a stdio recipe
		// additionally requires the server binary in the deployment image).
		// Self-managed GitLab rides the Custom MCP card in v1; this recipe
		// targets gitlab.com only.
		Endpoint:    "https://gitlab.com/api/v4/mcp",
		TokenHeader: "Authorization",
		TokenScheme: "Bearer",
		AccessLevels: []string{
			ConnectionAccessReadOnly,
			ConnectionAccessReadWrite,
		},
		Steps: []RecipeStep{
			{
				Title: "Open GitLab access token settings",
				Detail: "Sign in to gitlab.com and go to Preferences → Access Tokens → " +
					"Personal access tokens.",
				URL: "https://gitlab.com/-/user_settings/personal_access_tokens",
			},
			{
				Title: "Create a granular token",
				Detail: "Create a personal access token with the scopes listed below — " +
					"granular PATs support read-only and read-and-write tiers.",
			},
			{
				Title: "Copy the token",
				Detail: "Copy the token now — GitLab shows it once. Paste it into the connect dialog.",
			},
		},
		Scopes: []RecipeScopes{
			{
				AccessLevel: ConnectionAccessReadOnly,
				Scopes:      []string{"read_api"},
			},
			{
				AccessLevel: ConnectionAccessReadWrite,
				Scopes:      []string{"api"},
			},
		},
		Probe: RecipeProbe{Tool: "list_projects"},
	})

	// One recipe covering both Jira and Confluence (design.md D2): Atlassian's
	// single remote MCP server serves both products from one OAuth 2.0 (3LO)
	// connection, and API tokens are not accepted by that server — so there is
	// no PAT shortcut and no reason to model two cards.
	RegisterRecipe(Recipe{
		ID:           "atlassian",
		Service:      "Atlassian",
		Icon:         "atlassian",
		AuthKind:     RecipeAuthOAuth,
		Availability: RecipeComingSoon,
		Transport:    MCPTransportSSE,
		Endpoint:     "https://mcp.atlassian.com/v1/sse",
		AccessLevels: []string{
			ConnectionAccessReadOnly,
			ConnectionAccessReadWrite,
		},
		Probe: RecipeProbe{Tool: "list_visible_jira_projects"},
		Notes: "Coming soon — requires OAuth sign-in with Atlassian (Jira and Confluence). " +
			"Access tokens are not accepted by Atlassian's MCP server.",
	})

	RegisterRecipe(Recipe{
		ID:           "slack",
		Service:      "Slack",
		Icon:         "slack",
		AuthKind:     RecipeAuthOAuth,
		Availability: RecipeComingSoon,
		Transport:    MCPTransportStreamableHTTP,
		Endpoint:     "https://mcp.slack.com/mcp",
		AccessLevels: []string{
			ConnectionAccessReadOnly,
			ConnectionAccessReadWrite,
		},
		Probe: RecipeProbe{Tool: "list_channels"},
		Notes: "Coming soon — requires OAuth sign-in with Slack.",
	})

	RegisterRecipe(Recipe{
		ID:           "linear",
		Service:      "Linear",
		Icon:         "linear",
		AuthKind:     RecipeAuthOAuth,
		Availability: RecipeComingSoon,
		Transport:    MCPTransportSSE,
		Endpoint:     "https://mcp.linear.app/sse",
		AccessLevels: []string{
			ConnectionAccessReadOnly,
			ConnectionAccessReadWrite,
		},
		Probe: RecipeProbe{Tool: "list_issues"},
		Notes: "Coming soon — requires OAuth sign-in with Linear.",
	})
}
