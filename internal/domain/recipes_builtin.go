package domain

// The v1 built-in recipe registry contents (design.md D2, tasks.md 1.2):
// GitHub and GitLab available (PAT), Atlassian (one recipe covering Jira and
// Confluence), Slack, and Linear declared coming-soon (OAuth-kind), and Figma
// available (PAT, http kind — add-connection-http tasks.md 1.2). Endpoint and
// probe facts are release-shippable recipe data; the live-verification pass
// (tasks.md 5.3) re-confirms and pins them before release.
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
				Title:  "Copy the token",
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
				Title:  "Copy the token",
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
	// no PAT shortcut and no reason to model two cards. The OAuth facts below
	// are the reference recipe (add-connection-oauth D6); endpoints and scopes
	// are live-pinned at apply time (tasks.md 5.3). Availability stays
	// coming_soon in the declaration: the gallery flips the card to available
	// when the instance registers its Atlassian app (spec: OAuth recipe
	// availability).
	RegisterRecipe(Recipe{
		ID:           "atlassian",
		Service:      "Atlassian",
		Icon:         "atlassian",
		AuthKind:     RecipeAuthOAuth,
		Availability: RecipeComingSoon,
		Transport:    MCPTransportSSE,
		Endpoint:     "https://mcp.atlassian.com/v1/sse",
		TokenHeader:  "Authorization",
		TokenScheme:  "Bearer",
		AuthorizeURL: "https://auth.atlassian.com/authorize",
		TokenURL:     "https://auth.atlassian.com/oauth/token",
		AccessLevels: []string{
			ConnectionAccessReadOnly,
			ConnectionAccessReadWrite,
		},
		// Atlassian scopes are granular; offline_access is what makes the
		// token refreshable (design.md D3), and read:me is the minimal
		// account scope every 3LO app holds.
		Scopes: []RecipeScopes{
			{
				AccessLevel: ConnectionAccessReadOnly,
				Scopes: []string{
					"read:jira-work",
					"read:jira-user",
					"read:confluence-space.summary",
					"read:confluence-content.summary",
					"read:me",
					"offline_access",
				},
			},
			{
				AccessLevel: ConnectionAccessReadWrite,
				Scopes: []string{
					"read:jira-work",
					"write:jira-work",
					"read:jira-user",
					"read:confluence-space.summary",
					"read:confluence-content.summary",
					"write:confluence-content",
					"read:me",
					"offline_access",
				},
			},
		},
		AppRegistrationGuidance: "Create an OAuth 2.0 (3LO) app at developer.atlassian.com " +
			"(Apps → Create app → Authorization: OAuth 2.0). Add the shown redirect URI, " +
			"grant the scopes for the access levels you intend to offer, and copy the " +
			"client id and secret here.",
		Probe: RecipeProbe{Tool: "list_visible_jira_projects"},
		Notes: "Requires the instance admin to register the Atlassian OAuth app; " +
			"connect signs in with Atlassian (one connection covers Jira and Confluence). " +
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
		TokenHeader:  "Authorization",
		TokenScheme:  "Bearer",
		AuthorizeURL: "https://slack.com/oauth/v2/authorize",
		TokenURL:     "https://slack.com/api/oauth.v2.access",
		AccessLevels: []string{
			ConnectionAccessReadOnly,
			ConnectionAccessReadWrite,
		},
		Scopes: []RecipeScopes{
			{
				AccessLevel: ConnectionAccessReadOnly,
				Scopes: []string{
					"channels:read",
					"groups:read",
					"im:read",
					"mpim:read",
					"users:read",
				},
			},
			{
				AccessLevel: ConnectionAccessReadWrite,
				Scopes: []string{
					"channels:read",
					"groups:read",
					"im:read",
					"mpim:read",
					"users:read",
					"chat:write",
					"chat:write.public",
				},
			},
		},
		AppRegistrationGuidance: "Create a Slack app at api.slack.com/apps (From scratch). " +
			"Under OAuth & Permissions add the shown redirect URI and request the user " +
			"token scopes for the access levels you intend to offer, then copy the " +
			"client id and secret here.",
		Probe: RecipeProbe{Tool: "list_channels"},
		Notes: "Requires the instance admin to register the Slack OAuth app; " +
			"connect signs in with Slack.",
	})

	RegisterRecipe(Recipe{
		ID:           "linear",
		Service:      "Linear",
		Icon:         "linear",
		AuthKind:     RecipeAuthOAuth,
		Availability: RecipeComingSoon,
		Transport:    MCPTransportSSE,
		Endpoint:     "https://mcp.linear.app/sse",
		TokenHeader:  "Authorization",
		TokenScheme:  "Bearer",
		AuthorizeURL: "https://linear.app/oauth/authorize",
		TokenURL:     "https://api.linear.app/oauth/token",
		AccessLevels: []string{
			ConnectionAccessReadOnly,
			ConnectionAccessReadWrite,
		},
		Scopes: []RecipeScopes{
			{
				AccessLevel: ConnectionAccessReadOnly,
				Scopes:      []string{"read"},
			},
			{
				AccessLevel: ConnectionAccessReadWrite,
				Scopes:      []string{"read", "write"},
			},
		},
		AppRegistrationGuidance: "Create an OAuth2 application at linear.app/settings/api " +
			"(New OAuth application). Add the shown redirect URI, request the scopes for " +
			"the access levels you intend to offer, and copy the client id and secret here.",
		Probe: RecipeProbe{Tool: "list_issues"},
		Notes: "Requires the instance admin to register the Linear OAuth app; " +
			"connect signs in with Linear.",
	})

	// The http-kind reference recipe (add-connection-http design.md D5,
	// tasks.md 1.2). Figma's official MCP server is desktop-local and
	// unreachable from a deployed OnClaw, but its REST API is PAT-friendly —
	// so the connection contributes declared verb tools over the pinned base
	// URL instead of materializing a server.
	//
	// PLACEHOLDER pending tasks.md 5.3 live verification: the base URL, auth
	// header, probe call, and verb paths are drafted from Figma's REST API
	// documentation, not yet confirmed with a real token. Connect is
	// probe-gated, so an unpinned value fails safely at connect instead of
	// storing a broken connection. Task 5.3 pins the values.
	//
	// The verb list is the curated read surface (file/file-tree reads,
	// comments, project listings — design.md D5): all GET, parameterized via
	// path and query only. Extending it is a recipe release (verbs-only, no
	// free-form request tool); write verbs would need the recipe schema's
	// request-body extension first.
	RegisterRecipe(Recipe{
		ID:           "figma",
		Service:      "Figma",
		Icon:         "figma",
		AuthKind:     RecipeAuthPAT,
		Availability: RecipeAvailable,
		Kind:         RecipeKindHTTP,
		BaseURL:      "https://api.figma.com",
		// The raw token rides this header (no scheme prefix — TokenScheme
		// stays empty).
		TokenHeader: "X-Figma-Token",
		// Read-only is the whole declared surface: every curated verb is a
		// GET, so a read-and-write tier would guide toward scopes the tool
		// surface cannot exercise. Write verbs (e.g. posting comments) join
		// with the request-body schema extension.
		AccessLevels: []string{ConnectionAccessReadOnly},
		Steps: []RecipeStep{
			{
				Title: "Open Figma personal access tokens",
				Detail: "Sign in to Figma and go to Settings → Security → " +
					"Personal access tokens.",
				URL: "https://www.figma.com/settings",
			},
			{
				Title: "Generate a token",
				Detail: "Create a personal access token for this workspace. Figma " +
					"tokens carry no scope selection — the token inherits the " +
					"account's access to teams and files, so generate it on an " +
					"account whose reach matches what the agents should have.",
			},
			{
				Title:  "Copy the token",
				Detail: "Copy the token now — Figma shows it once. Paste it into the connect dialog.",
			},
		},
		Scopes: []RecipeScopes{
			{
				AccessLevel: ConnectionAccessReadOnly,
				Scopes:      []string{"Account access: the teams and files the token's account can view"},
			},
		},
		Probe: RecipeProbe{Method: "GET", Path: "/v1/me"},
		Verbs: []RecipeVerb{
			{
				Tool:        "figma.get_me",
				Method:      "GET",
				Path:        "/v1/me",
				Description: "Get the authenticated user's profile — the identity and team memberships the connection's token can reach.",
			},
			{
				Tool:   "figma.get_file",
				Method: "GET",
				Path:   "/v1/files/{file_key}",
				Params: []RecipeVerbParam{
					{Name: "file_key", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
					{Name: "depth", Type: RecipeParamNumber, Required: false, In: RecipeParamInQuery},
					{Name: "geometry", Type: RecipeParamString, Required: false, In: RecipeParamInQuery},
				},
				Description: "Get a file's metadata and node tree. Use depth to bound how deep the returned node tree goes on large files.",
			},
			{
				Tool:   "figma.get_file_nodes",
				Method: "GET",
				Path:   "/v1/files/{file_key}/nodes",
				Params: []RecipeVerbParam{
					{Name: "file_key", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
					{Name: "ids", Type: RecipeParamString, Required: true, In: RecipeParamInQuery},
					{Name: "depth", Type: RecipeParamNumber, Required: false, In: RecipeParamInQuery},
				},
				Description: "Get specific nodes of a file by comma-separated node ids — a shallow look at part of a large file.",
			},
			{
				Tool:   "figma.get_file_comments",
				Method: "GET",
				Path:   "/v1/files/{file_key}/comments",
				Params: []RecipeVerbParam{
					{Name: "file_key", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
				},
				Description: "List the comments on a file.",
			},
			{
				Tool:   "figma.get_file_versions",
				Method: "GET",
				Path:   "/v1/files/{file_key}/versions",
				Params: []RecipeVerbParam{
					{Name: "file_key", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
				},
				Description: "List a file's version history, newest first.",
			},
			{
				Tool:   "figma.get_file_images",
				Method: "GET",
				Path:   "/v1/images/{file_key}",
				Params: []RecipeVerbParam{
					{Name: "file_key", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
					{Name: "ids", Type: RecipeParamString, Required: true, In: RecipeParamInQuery},
					{Name: "scale", Type: RecipeParamNumber, Required: false, In: RecipeParamInQuery},
					{Name: "format", Type: RecipeParamString, Required: false, In: RecipeParamInQuery},
				},
				Description: "Render nodes of a file as images — returns image URLs for the requested comma-separated node ids.",
			},
			{
				Tool:   "figma.get_project_files",
				Method: "GET",
				Path:   "/v1/projects/{project_id}/files",
				Params: []RecipeVerbParam{
					{Name: "project_id", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
				},
				Description: "List the files in a Figma project.",
			},
			{
				Tool:   "figma.get_team_projects",
				Method: "GET",
				Path:   "/v1/teams/{team_id}/projects",
				Params: []RecipeVerbParam{
					{Name: "team_id", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
				},
				Description: "List the projects in a Figma team.",
			},
		},
	})
}
