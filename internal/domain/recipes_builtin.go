package domain

// The v1 built-in recipe registry contents (design.md D2, tasks.md 1.2):
// GitHub and GitLab available (PAT), Atlassian (one recipe covering Jira and
// Confluence), Slack, and Linear declared coming-soon (OAuth-kind), and Figma
// available (PAT, http kind — add-connection-http tasks.md 1.2). Endpoint and
// probe facts are release-shippable recipe data; the live-verification pass
// (tasks.md 5.3) re-confirms and pins them before release.
//
// GitHub and GitLab additionally declare webhook support
// (add-connection-webhooks tasks.md 1.1): event catalog, signature scheme,
// per-event prompt templates with whitelisted payload fields, and provider
// setup copy. Their scheme headers, delivery ids, and payload field paths are
// PLACEHOLDER pending the tasks.md 5.3 live verification — the pipeline
// verifies signatures at ingest, so an unpinned value fails safely at
// delivery instead of storing a broken integration. The mcp-kind tool-tier
// lists (add-integration-authority tasks.md 1.2) ride the same placeholder
// posture: fail-safe defaults keep undeclared tools write-tier until the live
// pinning.
//
// Origin parameters (add-recipe-base-url): GitLab is an http-kind REST recipe
// whose base-URL parameter covers SaaS and self-managed instances (the
// never-connectable PAT-against-remote-MCP wiring it replaces); GitHub's
// parameter covers the GitHub Enterprise Cloud data-residency hosts. Both are
// PAT-kind — an OAuth recipe's authorize and token hosts are separate from
// the API origin (add-recipe-base-url design.md non-goal).
//
// Registration order is the gallery order.
//
// MCP-kind tier declarations (add-integration-authority tasks.md 1.2): the
// GitHub recipe carries a curated tool-tier list over the tool names its
// remote MCP server assigns. THE LIST IS PLACEHOLDER pending the tasks.md 5.3
// live verification, like every other live-pinned value — the fail-safe
// default (undeclared = write) keeps unstated tools admin-tier, so an
// unverified list can only over-block visibly, never under-block silently.
// Curation rule: read = fetch/list/search/get verbs; write =
// create/update/merge/delete/close/assign verbs. The write list documents
// intent — anything the live server exposes beyond both lists already gates
// as write. GitLab's curation lives on its REST verbs (the http kind's tier
// surface); coming-soon mcp recipes (atlassian, slack, linear) declare no
// lists yet: their tool surfaces are unpinnable before the OAuth apps land,
// and the fail-safe default is the correct tier for everything they will
// expose until a release declares otherwise.
var (
	// githubReadTools is GitHub's curated read surface: profile, repo file
	// and history reads, search, PR/issue reads, notifications, security
	// alerts, and Actions reads. list_repositories is the probe tool's
	// PLACEHOLDER name (tasks.md 5.3 pins the live tool set).
	githubReadTools = []string{
		"get_me",
		"get_octocat",
		"list_repositories",
		"search_repositories",
		"search_code",
		"search_issues",
		"search_users",
		"get_file_contents",
		"get_commit",
		"list_commits",
		"list_branches",
		"list_tags",
		"get_tag",
		"list_pull_requests",
		"get_pull_request",
		"get_pull_request_files",
		"get_pull_request_diff",
		"get_pull_request_status",
		"get_pull_request_comments",
		"get_pull_request_reviews",
		"list_issues",
		"get_issue",
		"get_issue_comments",
		"list_notifications",
		"get_notification",
		"list_code_scanning_alerts",
		"get_code_scanning_alert",
		"list_secret_scanning_alerts",
		"get_secret_scanning_alert",
		"list_dependabot_alerts",
		"get_dependabot_alert",
		"list_workflow_runs",
		"get_workflow_run",
		"list_workflow_jobs",
		"get_job_logs",
		"list_workflow_run_artifacts",
		"download_workflow_run_artifact",
	}
	// githubWriteTools is GitHub's curated write surface: repo and branch
	// creation, file mutation, PR lifecycle, issue lifecycle and comment,
	// Actions triggering.
	githubWriteTools = []string{
		"create_repository",
		"fork_repository",
		"create_branch",
		"create_or_update_file",
		"push_files",
		"delete_file",
		"create_pull_request",
		"update_pull_request",
		"merge_pull_request",
		"update_pull_request_branch",
		"create_issue",
		"update_issue",
		"add_issue_comment",
		"assign_copilot_to_issue",
		"run_workflow",
		"cancel_workflow_run",
		"delete_workflow_run_logs",
	}
)

// toolTierRules builds a recipe's tool-tier list, reads first so the served
// order is deterministic and reviewable (read block, then write block).
func toolTierRules(read, write []string) []RecipeToolTierRule {
	rules := make([]RecipeToolTierRule, 0, len(read)+len(write))
	for _, tool := range read {
		rules = append(rules, RecipeToolTierRule{Tool: tool, Tier: RecipeToolTierRead})
	}
	for _, tool := range write {
		rules = append(rules, RecipeToolTierRule{Tool: tool, Tier: RecipeToolTierWrite})
	}
	return rules
}

func init() {
	// GitHub's read-flavored v1 catalog is the whole list, so the default
	// event set equals the catalog (spec: v1 event runs start from the
	// read-flavored default set). One declaration, reused for both — the
	// registry deep-copies per read.
	githubWebhookEvents := []string{
		"push",
		"pull_request.opened",
		"pull_request.closed",
		"pull_request.review_requested",
		"issues.opened",
		"issues.assigned",
		"issue_comment.created",
		"release.published",
	}

	RegisterRecipe(Recipe{
		ID:           "github",
		Service:      "GitHub",
		Icon:         "github",
		AuthKind:     RecipeAuthPAT,
		Availability: RecipeAvailable,
		Transport:    MCPTransportStreamableHTTP,
		// Official GitHub MCP server remote endpoint (tasks.md 5.3 pins).
		Endpoint: "https://api.githubcopilot.com/mcp/",
		// The MCP endpoint path is origin-stable: GitHub Enterprise Cloud
		// data-residency hosts serve the same /mcp/ path per subdomain
		// (copilot-api.<subdomain>.ghe.com). GitHub Enterprise Server offers
		// no remote MCP endpoint — the parameter covers ghe.com hosts only
		// (add-recipe-base-url tasks.md 4.1).
		OriginParam: &RecipeOriginParam{
			Name:    "GitHub API base URL",
			Default: "https://api.githubcopilot.com",
			Help: "GitHub Enterprise Cloud data-residency hosts follow the " +
				"copilot-api.<subdomain>.ghe.com pattern (the /mcp/ path is fixed). " +
				"GitHub Enterprise Server offers no remote MCP endpoint. " +
				"Fixed after connect — changing origins means reconnecting.",
		},
		TokenHeader: "Authorization",
		TokenScheme: "Bearer",
		Webhooks: &RecipeWebhook{
			// GitHub signs the raw body with HMAC-SHA256 and sends
			// "sha256=<hex>" in X-Hub-Signature-256 (tasks.md 5.3 pins).
			SignatureScheme: RecipeWebhookSchemeHMACSHA256,
			Events:          githubWebhookEvents,
			DefaultEvents:   githubWebhookEvents,
			Templates: []RecipeWebhookTemplate{
				{
					Event: "push",
					Fields: []string{
						"repository.full_name", "ref", "pusher.name", "compare",
					},
					Template: "Push to {repository.full_name}: {pusher.name} pushed {ref}. Diff: {compare}.",
				},
				{
					Event: "pull_request.opened",
					Fields: []string{
						"repository.full_name", "pull_request.number", "pull_request.title",
						"pull_request.html_url", "sender.login",
					},
					Template: "Pull request #{pull_request.number} opened in {repository.full_name} by {sender.login}: \"{pull_request.title}\" — {pull_request.html_url}",
				},
				{
					Event: "pull_request.closed",
					Fields: []string{
						"repository.full_name", "pull_request.number", "pull_request.title",
						"pull_request.html_url", "sender.login",
					},
					Template: "Pull request #{pull_request.number} closed in {repository.full_name} by {sender.login}: \"{pull_request.title}\" — {pull_request.html_url}",
				},
				{
					Event: "pull_request.review_requested",
					Fields: []string{
						"repository.full_name", "pull_request.number", "pull_request.title",
						"pull_request.html_url", "requested_reviewer.login",
					},
					Template: "Review requested on pull request #{pull_request.number} in {repository.full_name}: \"{pull_request.title}\" — reviewer: {requested_reviewer.login} — {pull_request.html_url}",
				},
				{
					Event: "issues.opened",
					Fields: []string{
						"repository.full_name", "issue.number", "issue.title",
						"issue.html_url", "sender.login",
					},
					Template: "Issue #{issue.number} opened in {repository.full_name} by {sender.login}: \"{issue.title}\" — {issue.html_url}",
				},
				{
					Event: "issues.assigned",
					Fields: []string{
						"repository.full_name", "issue.number", "issue.title",
						"issue.html_url", "assignee.login",
					},
					Template: "Issue #{issue.number} assigned in {repository.full_name}: \"{issue.title}\" — assignee: {assignee.login} — {issue.html_url}",
				},
				{
					Event: "issue_comment.created",
					Fields: []string{
						"repository.full_name", "issue.number", "comment.body",
						"comment.html_url", "sender.login",
					},
					Template: "New comment on issue #{issue.number} in {repository.full_name} by {sender.login}:\n\"{comment.body}\"\n{comment.html_url}",
				},
				{
					Event: "release.published",
					Fields: []string{
						"repository.full_name", "release.tag_name", "release.name",
						"release.html_url", "sender.login",
					},
					Template: "Release {release.tag_name} ({release.name}) published in {repository.full_name} by {sender.login} — {release.html_url}",
				},
			},
			Setup: RecipeWebhookSetup{
				SignatureHeader:  "X-Hub-Signature-256",
				EventTypeHeader:  "X-GitHub-Event",
				DeliveryIDHeader: "X-GitHub-Delivery",
				URLPathShape:     "/api/ingest/webhooks/{workspace_slug}/{connection_id}",
				Help: "In GitHub, open the repository or organization, go to Settings → " +
					"Webhooks → Add webhook. Set Payload URL to the ingest URL below, " +
					"Content type to application/json, Secret to the generated secret " +
					"shown once here, and select the events this connection subscribes " +
					"to. Keep SSL verification enabled.",
			},
		},
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
		// PLACEHOLDER pending tasks.md 5.3 live verification: the curated
		// read/write split over the remote server's tool names (see the tier
		// declaration note above the var block). Undeclared tools gate as
		// write.
		ToolTiers: toolTierRules(githubReadTools, githubWriteTools),
	})

	// The GitLab recipe is an http-kind REST recipe (add-recipe-base-url
	// tasks.md 3.1): GitLab's remote MCP endpoint is OAuth-only and in beta —
	// live-verified absent on self-managed instances — while the REST API +
	// PAT works on gitlab.com and every self-managed instance today. The
	// origin parameter substitutes the instance origin only; the /api/v4
	// prefix and the verb paths below stay recipe constants
	// (add-recipe-base-url design.md D1). The probe is the current-user
	// declared call (design.md D4: cheap, read-only, exercises auth + origin).
	//
	// PLACEHOLDER pending tasks.md 6.3 live PAT pass: the verb paths and
	// parameter shapes are drafted from GitLab's REST API documentation.
	// Connect is probe-gated, so an unpinned value fails safely at connect
	// instead of storing a broken connection.
	RegisterRecipe(Recipe{
		ID:           "gitlab",
		Service:      "GitLab",
		Icon:         "gitlab",
		AuthKind:     RecipeAuthPAT,
		Availability: RecipeAvailable,
		Kind:         RecipeKindHTTP,
		// A bare origin: the origin parameter replaces the base URL wholesale,
		// and every verb path below carries the /api/v4 prefix.
		BaseURL: "https://gitlab.com",
		OriginParam: &RecipeOriginParam{
			Name:    "GitLab instance URL",
			Default: "https://gitlab.com",
			Help: "The GitLab origin — gitlab.com or a self-managed instance " +
				"(e.g. https://gitlab.example.com); every instance serves its " +
				"REST API under /api/v4. Fixed after connect — changing " +
				"instances means disconnecting and reconnecting.",
		},
		// GitLab REST personal access tokens ride PRIVATE-TOKEN verbatim (no
		// scheme prefix — TokenScheme stays empty).
		TokenHeader: "PRIVATE-TOKEN",
		// GitLab authenticates webhooks with the shared secret carried verbatim
		// in X-Gitlab-Token (tasks.md 5.3 pins the header set and payload field
		// paths). The v1 catalog is the read-flavored notification subset; the
		// default selection starts narrower than the catalog.
		Webhooks: &RecipeWebhook{
			SignatureScheme: RecipeWebhookSchemeSecretToken,
			Events: []string{
				"push",
				"tag_push",
				"merge_request.open",
				"merge_request.merge",
				"merge_request.close",
				"issue.open",
				"issue.close",
				"note",
			},
			DefaultEvents: []string{
				"push",
				"merge_request.open",
				"issue.open",
				"note",
			},
			Templates: []RecipeWebhookTemplate{
				{
					Event:    "push",
					Fields:   []string{"project.path_with_namespace", "user_name", "ref"},
					Template: "Push to {project.path_with_namespace}: {user_name} pushed {ref}.",
				},
				{
					Event:    "tag_push",
					Fields:   []string{"project.path_with_namespace", "user_name", "ref"},
					Template: "Tag {ref} pushed to {project.path_with_namespace} by {user_name}.",
				},
				{
					Event: "merge_request.open",
					Fields: []string{
						"project.path_with_namespace", "object_attributes.iid",
						"object_attributes.title", "object_attributes.url", "user.name",
					},
					Template: "Merge request !{object_attributes.iid} opened in {project.path_with_namespace} by {user.name}: \"{object_attributes.title}\" — {object_attributes.url}",
				},
				{
					Event: "merge_request.merge",
					Fields: []string{
						"project.path_with_namespace", "object_attributes.iid",
						"object_attributes.title", "object_attributes.url", "user.name",
					},
					Template: "Merge request !{object_attributes.iid} merged in {project.path_with_namespace} by {user.name}: \"{object_attributes.title}\" — {object_attributes.url}",
				},
				{
					Event: "merge_request.close",
					Fields: []string{
						"project.path_with_namespace", "object_attributes.iid",
						"object_attributes.title", "object_attributes.url", "user.name",
					},
					Template: "Merge request !{object_attributes.iid} closed in {project.path_with_namespace} by {user.name}: \"{object_attributes.title}\" — {object_attributes.url}",
				},
				{
					Event: "issue.open",
					Fields: []string{
						"project.path_with_namespace", "object_attributes.iid",
						"object_attributes.title", "object_attributes.url", "user.name",
					},
					Template: "Issue #{object_attributes.iid} opened in {project.path_with_namespace} by {user.name}: \"{object_attributes.title}\" — {object_attributes.url}",
				},
				{
					Event: "issue.close",
					Fields: []string{
						"project.path_with_namespace", "object_attributes.iid",
						"object_attributes.title", "object_attributes.url", "user.name",
					},
					Template: "Issue #{object_attributes.iid} closed in {project.path_with_namespace} by {user.name}: \"{object_attributes.title}\" — {object_attributes.url}",
				},
				{
					Event: "note",
					Fields: []string{
						"project.path_with_namespace", "object_attributes.note",
						"object_attributes.url", "user.name",
					},
					Template: "New comment in {project.path_with_namespace} by {user.name}:\n\"{object_attributes.note}\"\n{object_attributes.url}",
				},
			},
			Setup: RecipeWebhookSetup{
				SignatureHeader:  "X-Gitlab-Token",
				EventTypeHeader:  "X-Gitlab-Event",
				DeliveryIDHeader: "X-Gitlab-Event-UUID",
				URLPathShape:     "/api/ingest/webhooks/{workspace_slug}/{connection_id}",
				Help: "In GitLab, open the project or group, go to Settings → Webhooks. " +
					"Add the ingest URL below as the URL, set the Secret token to the " +
					"generated secret shown once here, and select the events this " +
					"connection subscribes to.",
			},
		},
		AccessLevels: []string{
			ConnectionAccessReadOnly,
			ConnectionAccessReadWrite,
		},
		Steps: []RecipeStep{
			{
				Title: "Open GitLab access token settings",
				Detail: "Sign in to your GitLab instance — gitlab.com or your " +
					"self-managed origin — and go to Preferences → Access Tokens → " +
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
		Probe: RecipeProbe{Method: "GET", Path: "/api/v4/user"},
		// The curated REST verb surface (add-recipe-base-url design.md D4):
		// read = project/issue/MR/branch/pipeline reads and the current user;
		// write = issue and MR lifecycle, comments, pipeline retry/cancel.
		// Everything else stays undeclared — the connection tool surface is
		// exactly this list, and write verbs post their parameters through the
		// query string (the declared verb schema carries no request body).
		Verbs: []RecipeVerb{
			{
				Tool:        "gitlab.get_current_user",
				Method:      "GET",
				Path:        "/api/v4/user",
				Description: "Get the authenticated user — the account the connection's personal access token belongs to.",
				Tier:        RecipeToolTierRead,
			},
			{
				Tool:   "gitlab.list_projects",
				Method: "GET",
				Path:   "/api/v4/projects",
				Params: []RecipeVerbParam{
					{Name: "search", Type: RecipeParamString, Required: false, In: RecipeParamInQuery},
					{Name: "membership", Type: RecipeParamBoolean, Required: false, In: RecipeParamInQuery},
				},
				Description: "List projects visible to the token's account, optionally narrowed by a name search or membership.",
				Tier:        RecipeToolTierRead,
			},
			{
				Tool:   "gitlab.get_project",
				Method: "GET",
				Path:   "/api/v4/projects/{project_id}",
				Params: []RecipeVerbParam{
					{Name: "project_id", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
				},
				Description: "Get one project by its numeric id.",
				Tier:        RecipeToolTierRead,
			},
			{
				Tool:   "gitlab.list_issues",
				Method: "GET",
				Path:   "/api/v4/issues",
				Params: []RecipeVerbParam{
					{Name: "state", Type: RecipeParamString, Required: false, In: RecipeParamInQuery},
					{Name: "search", Type: RecipeParamString, Required: false, In: RecipeParamInQuery},
				},
				Description: "List issues across the projects the token's account can see; state filters opened or closed.",
				Tier:        RecipeToolTierRead,
			},
			{
				Tool:   "gitlab.get_issue",
				Method: "GET",
				Path:   "/api/v4/projects/{project_id}/issues/{issue_iid}",
				Params: []RecipeVerbParam{
					{Name: "project_id", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
					{Name: "issue_iid", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
				},
				Description: "Get one issue by project id and the issue's project-scoped iid.",
				Tier:        RecipeToolTierRead,
			},
			{
				Tool:   "gitlab.list_merge_requests",
				Method: "GET",
				Path:   "/api/v4/merge_requests",
				Params: []RecipeVerbParam{
					{Name: "state", Type: RecipeParamString, Required: false, In: RecipeParamInQuery},
					{Name: "search", Type: RecipeParamString, Required: false, In: RecipeParamInQuery},
				},
				Description: "List merge requests across the projects the token's account can see; state filters opened, closed, or merged.",
				Tier:        RecipeToolTierRead,
			},
			{
				Tool:   "gitlab.get_merge_request",
				Method: "GET",
				Path:   "/api/v4/projects/{project_id}/merge_requests/{merge_request_iid}",
				Params: []RecipeVerbParam{
					{Name: "project_id", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
					{Name: "merge_request_iid", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
				},
				Description: "Get one merge request by project id and the merge request's project-scoped iid.",
				Tier:        RecipeToolTierRead,
			},
			{
				Tool:   "gitlab.list_branches",
				Method: "GET",
				Path:   "/api/v4/projects/{project_id}/repository/branches",
				Params: []RecipeVerbParam{
					{Name: "project_id", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
					{Name: "search", Type: RecipeParamString, Required: false, In: RecipeParamInQuery},
				},
				Description: "List a project's repository branches, optionally narrowed by a name search.",
				Tier:        RecipeToolTierRead,
			},
			{
				Tool:   "gitlab.list_pipelines",
				Method: "GET",
				Path:   "/api/v4/projects/{project_id}/pipelines",
				Params: []RecipeVerbParam{
					{Name: "project_id", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
					{Name: "status", Type: RecipeParamString, Required: false, In: RecipeParamInQuery},
					{Name: "ref", Type: RecipeParamString, Required: false, In: RecipeParamInQuery},
				},
				Description: "List a project's CI/CD pipelines, newest first; status and ref narrow the listing.",
				Tier:        RecipeToolTierRead,
			},
			{
				Tool:   "gitlab.create_issue",
				Method: "POST",
				Path:   "/api/v4/projects/{project_id}/issues",
				Params: []RecipeVerbParam{
					{Name: "project_id", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
					{Name: "title", Type: RecipeParamString, Required: true, In: RecipeParamInQuery},
					{Name: "description", Type: RecipeParamString, Required: false, In: RecipeParamInQuery},
					{Name: "labels", Type: RecipeParamString, Required: false, In: RecipeParamInQuery},
				},
				Description: "Create an issue in a project with a title and optional description and comma-separated labels.",
				Tier:        RecipeToolTierWrite,
			},
			{
				Tool:   "gitlab.update_issue",
				Method: "PUT",
				Path:   "/api/v4/projects/{project_id}/issues/{issue_iid}",
				Params: []RecipeVerbParam{
					{Name: "project_id", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
					{Name: "issue_iid", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
					{Name: "title", Type: RecipeParamString, Required: false, In: RecipeParamInQuery},
					{Name: "description", Type: RecipeParamString, Required: false, In: RecipeParamInQuery},
					{Name: "state_event", Type: RecipeParamString, Required: false, In: RecipeParamInQuery},
				},
				Description: "Update an issue's title or description, or change its state with state_event close or reopen.",
				Tier:        RecipeToolTierWrite,
			},
			{
				Tool:   "gitlab.create_issue_note",
				Method: "POST",
				Path:   "/api/v4/projects/{project_id}/issues/{issue_iid}/notes",
				Params: []RecipeVerbParam{
					{Name: "project_id", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
					{Name: "issue_iid", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
					{Name: "body", Type: RecipeParamString, Required: true, In: RecipeParamInQuery},
				},
				Description: "Comment on an issue with the given body text.",
				Tier:        RecipeToolTierWrite,
			},
			{
				Tool:   "gitlab.create_merge_request",
				Method: "POST",
				Path:   "/api/v4/projects/{project_id}/merge_requests",
				Params: []RecipeVerbParam{
					{Name: "project_id", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
					{Name: "source_branch", Type: RecipeParamString, Required: true, In: RecipeParamInQuery},
					{Name: "target_branch", Type: RecipeParamString, Required: true, In: RecipeParamInQuery},
					{Name: "title", Type: RecipeParamString, Required: true, In: RecipeParamInQuery},
					{Name: "description", Type: RecipeParamString, Required: false, In: RecipeParamInQuery},
				},
				Description: "Open a merge request from a source branch into a target branch with a title and optional description.",
				Tier:        RecipeToolTierWrite,
			},
			{
				Tool:   "gitlab.update_merge_request",
				Method: "PUT",
				Path:   "/api/v4/projects/{project_id}/merge_requests/{merge_request_iid}",
				Params: []RecipeVerbParam{
					{Name: "project_id", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
					{Name: "merge_request_iid", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
					{Name: "title", Type: RecipeParamString, Required: false, In: RecipeParamInQuery},
					{Name: "description", Type: RecipeParamString, Required: false, In: RecipeParamInQuery},
					{Name: "state_event", Type: RecipeParamString, Required: false, In: RecipeParamInQuery},
				},
				Description: "Update a merge request's title or description, or change its state with state_event close or reopen.",
				Tier:        RecipeToolTierWrite,
			},
			{
				Tool:   "gitlab.merge_merge_request",
				Method: "PUT",
				Path:   "/api/v4/projects/{project_id}/merge_requests/{merge_request_iid}/merge",
				Params: []RecipeVerbParam{
					{Name: "project_id", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
					{Name: "merge_request_iid", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
				},
				Description: "Merge an open merge request.",
				Tier:        RecipeToolTierWrite,
			},
			{
				Tool:   "gitlab.retry_pipeline",
				Method: "POST",
				Path:   "/api/v4/projects/{project_id}/pipelines/{pipeline_id}/retry",
				Params: []RecipeVerbParam{
					{Name: "project_id", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
					{Name: "pipeline_id", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
				},
				Description: "Retry a finished pipeline.",
				Tier:        RecipeToolTierWrite,
			},
			{
				Tool:   "gitlab.cancel_pipeline",
				Method: "POST",
				Path:   "/api/v4/projects/{project_id}/pipelines/{pipeline_id}/cancel",
				Params: []RecipeVerbParam{
					{Name: "project_id", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
					{Name: "pipeline_id", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
				},
				Description: "Cancel a running pipeline.",
				Tier:        RecipeToolTierWrite,
			},
		},
		// Connects today with a PAT over the REST API; GitLab's remote MCP
		// endpoint is OAuth-only beta and returns through the MCP-via-OAuth
		// follow-up change.
		Notes: "Connects with a personal access token over the GitLab REST API " +
			"(gitlab.com or a self-managed instance). GitLab's remote MCP endpoint " +
			"is OAuth-only and in beta; MCP access will return through " +
			"instance-registered OAuth apps.",
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
				Tier:        RecipeToolTierRead,
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
				Tier:        RecipeToolTierRead,
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
				Tier:        RecipeToolTierRead,
			},
			{
				Tool:   "figma.get_file_comments",
				Method: "GET",
				Path:   "/v1/files/{file_key}/comments",
				Params: []RecipeVerbParam{
					{Name: "file_key", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
				},
				Description: "List the comments on a file.",
				Tier:        RecipeToolTierRead,
			},
			{
				Tool:   "figma.get_file_versions",
				Method: "GET",
				Path:   "/v1/files/{file_key}/versions",
				Params: []RecipeVerbParam{
					{Name: "file_key", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
				},
				Description: "List a file's version history, newest first.",
				Tier:        RecipeToolTierRead,
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
				Tier:        RecipeToolTierRead,
			},
			{
				Tool:   "figma.get_project_files",
				Method: "GET",
				Path:   "/v1/projects/{project_id}/files",
				Params: []RecipeVerbParam{
					{Name: "project_id", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
				},
				Description: "List the files in a Figma project.",
				Tier:        RecipeToolTierRead,
			},
			{
				Tool:   "figma.get_team_projects",
				Method: "GET",
				Path:   "/v1/teams/{team_id}/projects",
				Params: []RecipeVerbParam{
					{Name: "team_id", Type: RecipeParamString, Required: true, In: RecipeParamInPath},
				},
				Description: "List the projects in a Figma team.",
				Tier:        RecipeToolTierRead,
			},
		},
	})
}
