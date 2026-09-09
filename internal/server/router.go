package server

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/agents"
	agenthooks "github.com/oniharnantyo/onclaw/internal/agents/hooks"
	"github.com/oniharnantyo/onclaw/internal/agents/mcp"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/promptgen"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/services"
	"github.com/oniharnantyo/onclaw/internal/skills"
	"github.com/oniharnantyo/onclaw/internal/storage"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// RouterOptions holds dependencies required by the HTTP API router.
type RouterOptions struct {
	Store         store.Store
	Storage       storage.Storage
	Issuer        services.TokenIssuer
	Auth          services.AuthService
	EncryptionKey []byte
	Providers     *providers.Registry
	ModelCatalog  *services.ModelCatalog
	AgentService  *promptgen.Service
	WorkspaceDir  string
	OnClawDir     string
	Runner        *agents.Runner
	ToolSettings  *agents.ToolSettingsService
	MCPSettings   *agents.MCPSettingsService
	MCPManager    *mcp.MCPManager
	// MCPProbeTimeout bounds each MCP probe's fresh connection attempt; 0
	// uses the handler default (10s).
	MCPProbeTimeout time.Duration
	// V1StreamKeepAlive is the /v1 SSE idle keepalive cadence; 0 uses the
	// handler default (15s).
	V1StreamKeepAlive time.Duration
	// HooksCommandEnabled is the command hook handler kill switch (D10,
	// ONCLAW_HOOKS_COMMAND_ENABLED; default on). It drives the shared hook
	// registry's runtime gate and save-time validation alike.
	HooksCommandEnabled bool
	// HooksScriptEnabled is the script hook handler kill switch (D22,
	// ONCLAW_HOOKS_SCRIPT_ENABLED; default on). It drives the shared hook
	// registry's runtime gate.
	HooksScriptEnabled bool
}

// router configures and builds the HTTP API routes and handlers.
type router struct {
	opts RouterOptions
	mw   *middlewares
	v1mw *v1Middlewares
}

// New creates a new router instance with all handlers and middlewares initialized.
func New(opts RouterOptions) *router {
	if opts.Store == nil {
		return &router{opts: opts}
	}
	return &router{
		opts: opts,
		mw: NewMiddlewares(
			opts.Store.Users(),
			opts.Store.Workspaces(),
			opts.Store.Members(),
			opts.Store.Roles(),
			opts.Issuer,
		),
		v1mw: NewV1Middlewares(opts.Store.APIKeys()),
	}
}

// Engine builds and returns the configured Gin Engine.
func (rt *router) Engine() *gin.Engine {
	authService := rt.opts.Auth
	if authService == nil && rt.opts.Store != nil && rt.opts.Issuer != nil {
		reg := services.NewRegistry()
		reg.Register(services.NewPasswordProvider(rt.opts.Store.Users()))
		authService = services.NewAuthService(rt.opts.Store.Users(), rt.opts.Store.Members(), rt.opts.Issuer, reg)
	}

	providerRegistry := rt.opts.Providers
	if providerRegistry == nil {
		providerRegistry = providers.NewRegistry()
	}

	modelCatalog := rt.opts.ModelCatalog
	if modelCatalog == nil {
		modelCatalog = services.NewModelCatalog(services.ModelCatalogOptions{Registry: providerRegistry})
	}

	agentService := rt.opts.AgentService
	if agentService == nil && rt.opts.Store != nil {
		agentService = promptgen.NewService(rt.opts.Store.Agents(), rt.opts.Store.Providers(), rt.opts.EncryptionKey)
	}

	workspaceDir := rt.opts.WorkspaceDir
	if workspaceDir == "" {
		workspaceDir = domain.WorkspaceRoot(domain.DefaultOnClawDir())
	}
	onClawDir := rt.opts.OnClawDir
	if onClawDir == "" {
		onClawDir = domain.DefaultOnClawDir()
	}

	authHandlers := handlers.NewAuthHandlers(authService, rt.opts.Storage)
	workspaceHandlers := handlers.NewWorkspaceHandlers(rt.opts.Store, rt.opts.EncryptionKey, providerRegistry, modelCatalog, agentService, workspaceDir)
	memberHandlers := handlers.NewMemberHandlers(rt.opts.Store, rt.opts.Storage)
	roleHandlers := handlers.NewRoleHandlers(rt.opts.Store.Roles())
	userHandlers := handlers.NewUserHandlers(rt.opts.Store.Users(), rt.opts.Storage)
	fileHandlers := handlers.NewFileHandlers(rt.opts.Storage)
	adminWorkspaceHandlers := handlers.NewAdminWorkspaceHandlers(rt.opts.Store, rt.opts.Storage)
	adminUserHandlers := handlers.NewAdminUserHandlers(rt.opts.Store.Users(), rt.opts.Store.Workspaces(), rt.opts.Store.Members(), rt.opts.Store.Roles())
	adminSuperadminHandlers := handlers.NewAdminSuperadminHandlers(rt.opts.Store)
	// MCP settings + connection manager: the service backs both the settings
	// API and the runtime policy; the manager owns the lazy connection cache.
	// The composition root may inject long-lived instances (whose Close rides
	// its lifecycle); the fallback builds fresh ones.
	mcpSettings := rt.opts.MCPSettings
	if mcpSettings == nil {
		mcpSettings = agents.NewMCPSettingsService(rt.opts.Store.WorkspaceMCPServers(), rt.opts.Store.AgentMCPServers(), rt.opts.Store.Agents(), rt.opts.EncryptionKey)
	}
	mcpManager := rt.opts.MCPManager
	if mcpManager == nil {
		mcpManager = mcp.NewMCPManager()
	}

	// Shared hook handler registry (D9/D10): one instance serves the REST
	// dry-run endpoint AND the runtime dispatcher. The command kill switch
	// rides the composition root's flag; the mcp_tool handler invokes
	// workspace-level servers through the shared manager; the prompt handler
	// resolves evaluator models through the workspace provider catalog.
	hookRegistry := agenthooks.NewRegistry(
		agenthooks.WithEncryptionKey(rt.opts.EncryptionKey),
		agenthooks.WithCommandEnabled(rt.opts.HooksCommandEnabled),
		agenthooks.WithScriptEnabled(rt.opts.HooksScriptEnabled),
		agenthooks.WithMCPInvoker(mcp.NewHooksInvoker(mcpSettings, mcpManager)),
		agenthooks.WithEvaluatorFactory(agents.NewHookEvaluatorFactory(rt.opts.Store.Providers(), rt.opts.EncryptionKey, agents.DefaultAgenticModelFactory)),
	)

	runner := rt.opts.Runner
	if runner == nil && rt.opts.Store != nil {
		runner = agents.NewRunner(
			rt.opts.Store.Workspaces(),
			rt.opts.Store.Agents(),
			rt.opts.Store.Users(),
			rt.opts.Store.Members(),
			rt.opts.Store.Roles(),
			rt.opts.Store.Providers(),
			rt.opts.Store.SessionEvents(),
			rt.opts.Store.SessionCheckpoints(),
			rt.opts.Store.Memories(),
			rt.opts.EncryptionKey,
			onClawDir,
			agents.WithEnabledSkillReader(WorkspaceSkillReader(rt.opts.Store.WorkspaceSkills())),
			agents.WithMCPPolicy(mcp.NewSettingsPolicy(mcpSettings)),
			agents.WithMCPManager(mcpManager),
			agents.WithMCPStatusWriter(mcp.NewSettingsStatusWriter(mcpSettings)),
			agents.WithHooks(agenthooks.NewDispatcher(rt.opts.Store.Hooks(), hookRegistry)),
		)
	}

	providerHandlers := handlers.NewProviderHandlers(rt.opts.Store.Providers(), rt.opts.Store.Agents(), rt.opts.EncryptionKey, providerRegistry, modelCatalog)
	agentHandlers := handlers.NewAgentHandlers(rt.opts.Store.Agents(), rt.opts.Store.Providers(), rt.opts.Store.SessionEvents(), rt.opts.EncryptionKey, providerRegistry, modelCatalog, agentService, workspaceDir, runner, runner)
	memoryHandlers := handlers.NewMemoryHandlers(rt.opts.Store.Memories())

	toolSettings := rt.opts.ToolSettings
	if toolSettings == nil {
		toolSettings = agents.NewToolSettingsService(rt.opts.Store.ToolSettings(), rt.opts.EncryptionKey)
	}
	toolSettingsHandlers := handlers.NewToolSettingsHandlers(toolSettings)

	mcpServerHandlers := handlers.NewMCPServerHandlers(mcpSettings, rt.opts.Store.Agents(), mcpManager, rt.opts.MCPProbeTimeout)

	// Save-time match counts (D8) enumerate the workspace-visible toolset:
	// the built-in tool surface (a fresh default registry's names — identical
	// to the runner's; no tool is constructed) plus the workspace's enabled
	// MCP servers' tools through the shared manager.
	hookToolValues := handlers.NewWorkspaceHookToolValueSource(
		agents.NewDefaultToolRegistry(rt.opts.Store.Memories()),
		rt.opts.Store.WorkspaceMCPServers(),
		mcpManager,
	)
	hookHandlers := handlers.NewHookHandlers(rt.opts.Store.Hooks(), rt.opts.Store.Agents(), rt.opts.Store.WorkspaceMCPServers(), hookRegistry, hookToolValues, rt.opts.EncryptionKey, rt.opts.HooksCommandEnabled)

	// Workspace skill library: the registry store bridges into the install
	// pipeline's port (skills.Store), the transaction seam binds to
	// store.WithTx, and the runner reads enabled names through the same
	// adapter (design D2–D5, D8).
	skillsAdapter := newWorkspaceSkillStoreAdapter(rt.opts.Store.WorkspaceSkills())
	installService := skills.NewInstallService(
		skillsAdapter,
		rt.opts.Store.Agents(),
		rt.opts.Store.ToolSettings(),
		skills.WithTxProvider(workspaceSkillsTxProvider(rt.opts.Store)),
	)
	skillHandlers := handlers.NewSkillHandlers(installService, rt.opts.Store.WorkspaceSkills(), rt.opts.Store.Agents(), onClawDir)

	apiKeyService := services.NewAPIKeyService(rt.opts.Store.APIKeys())
	apiKeyHandlers := handlers.NewAPIKeysHandlers(apiKeyService)

	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.Use(gin.Recovery())
	r.Use(gin.Logger())

	// /v1 (OpenResponses) surface: API-key authenticated only (JWTs rejected).
	v1Handlers := handlers.NewV1Handlers(runner, rt.opts.Store.Agents(), rt.opts.Store.SessionEvents(), rt.opts.V1StreamKeepAlive)
	v1 := r.Group("/v1")
	v1.Use(rt.v1mw.APIKeyAuthRequired())
	{
		v1.GET("/models", v1Handlers.ListModels)
		v1.POST("/responses", v1Handlers.CreateResponse)
	}

	// Basic health check
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// Public capability file serving routes
	r.GET("/files/:key", fileHandlers.ServeFile)
	r.GET("/files/avatars/:name", fileHandlers.ServeFile)

	api := r.Group("/api/v1")
	{
		api.GET("/health", func(c *gin.Context) {
			c.JSON(200, gin.H{"status": "ok"})
		})

		// Public capability file serving under /api/v1
		api.GET("/files/:key", fileHandlers.ServeFile)
		api.GET("/files/avatars/:name", fileHandlers.ServeFile)

		// Public auth endpoints
		authGroup := api.Group("/auth")
		{
			authGroup.POST("/login", authHandlers.Login)
			authGroup.POST("/logout", authHandlers.Logout)
		}

		// Authenticated endpoints
		authed := api.Group("")
		if rt.opts.Issuer != nil && rt.opts.Store != nil {
			authed.Use(rt.mw.AuthRequired())
		}
		{
			// Auth me
			authed.GET("/auth/me", authHandlers.Me)

			// Workspaces (unscoped user view & creation)
			authed.GET("/workspaces", workspaceHandlers.ListWorkspaces)
			authed.POST("/workspaces", workspaceHandlers.CreateWorkspace)

			// Credential preview for onboarding
			authed.POST("/providers/models-preview", providerHandlers.ModelsPreview)

			// Users self-profile & avatar management
			authed.PATCH("/users/me", userHandlers.PatchMe)
			authed.POST("/users/me/avatar", userHandlers.UploadAvatar)

			// Tenant-scoped routes under /workspaces/:ws
			wsGroup := authed.Group("/workspaces/:ws")
			if rt.opts.Store != nil {
				wsGroup.Use(rt.mw.RequireWorkspace("ws"))
			}
			{
				wsGroup.GET("", rt.mw.RequirePermission(domain.WorkspaceRead), workspaceHandlers.GetWorkspace)
				wsGroup.PATCH("", rt.mw.RequirePermission(domain.WorkspaceWrite), workspaceHandlers.PatchWorkspace)

				// Memory (design D8): own user memory is membership-only and
				// self-scoped to the authenticated caller; shared workspace
				// memory reads ride membership, writes use the same
				// workspace.write gate as the workspace PATCH above.
				wsGroup.GET("/me/memory", memoryHandlers.GetUserMemory)
				wsGroup.PUT("/me/memory", memoryHandlers.PutUserMemory)
				wsGroup.GET("/memory", memoryHandlers.GetWorkspaceMemory)
				wsGroup.PUT("/memory", rt.mw.RequirePermission(domain.WorkspaceWrite), memoryHandlers.PutWorkspaceMemory)

				wsGroup.GET("/roles", rt.mw.RequirePermission(domain.RolesRead), roleHandlers.ListRoles)

				wsGroup.GET("/members", rt.mw.RequirePermission(domain.MembersRead), memberHandlers.ListMembers)
				wsGroup.POST("/members", rt.mw.RequirePermission(domain.MembersWrite), memberHandlers.AddMember)
				wsGroup.PATCH("/members/:uid", rt.mw.RequirePermission(domain.MembersWrite), memberHandlers.PatchMember)
				wsGroup.DELETE("/members/:uid", memberHandlers.DeleteMember)

				// Providers management
				wsGroup.GET("/providers", rt.mw.RequirePermission(domain.ProvidersRead), providerHandlers.ListProviders)
				wsGroup.POST("/providers", rt.mw.RequirePermission(domain.ProvidersWrite), providerHandlers.CreateProvider)
				wsGroup.PATCH("/providers/:id", rt.mw.RequirePermission(domain.ProvidersWrite), providerHandlers.PatchProvider)
				wsGroup.DELETE("/providers/:id", rt.mw.RequirePermission(domain.ProvidersWrite), providerHandlers.DeleteProvider)
				wsGroup.POST("/providers/:id/verify", rt.mw.RequirePermission(domain.ProvidersWrite), providerHandlers.VerifyProvider)
				wsGroup.GET("/providers/:id/models", rt.mw.RequirePermission(domain.ProvidersRead), providerHandlers.GetProviderModels)

				// Agents management
				wsGroup.GET("/agents", rt.mw.RequirePermission(domain.AgentsRead), agentHandlers.ListAgents)
				wsGroup.POST("/agents", rt.mw.RequirePermission(domain.AgentsWrite), agentHandlers.CreateAgent)
				wsGroup.GET("/agents/:agent", rt.mw.RequirePermission(domain.AgentsRead), agentHandlers.GetAgent)
				wsGroup.PATCH("/agents/:agent", rt.mw.RequirePermission(domain.AgentsWrite), agentHandlers.PatchAgent)
				wsGroup.DELETE("/agents/:agent", rt.mw.RequirePermission(domain.AgentsWrite), agentHandlers.DeleteAgent)
				wsGroup.POST("/agents/:agent/regenerate", rt.mw.RequirePermission(domain.AgentsWrite), agentHandlers.RegenerateAgent)
				wsGroup.GET("/agents/:agent/sessions/:session/events", rt.mw.RequirePermission(domain.AgentsRead), agentHandlers.ListSessionEvents)
				wsGroup.POST("/agents/:agent/sessions/:session/approvals/:interruptID", rt.mw.RequirePermission(domain.AgentsWrite), agentHandlers.ResolveApproval)
				wsGroup.POST("/agents/:agent/sessions/:session/runs/:turn/cancel", rt.mw.RequirePermission(domain.AgentsWrite), agentHandlers.CancelRun)

				// Tool settings (workspace settings; read covered by membership,
				// writes are Owner/Admin via tools.write)
				wsGroup.GET("/tools", toolSettingsHandlers.ListTools)
				wsGroup.PATCH("/tools/:key", rt.mw.RequirePermission(domain.ToolsWrite), toolSettingsHandlers.PatchTool)

				// Workspace MCP registry (reads ride membership like /tools —
				// every built-in role holds tools.read; writes are Owner/Admin
				// via tools.write, design.md D9)
				wsGroup.GET("/mcp-servers", mcpServerHandlers.ListWorkspaceServers)
				wsGroup.POST("/mcp-servers", rt.mw.RequirePermission(domain.ToolsWrite), mcpServerHandlers.CreateWorkspaceServer)
				wsGroup.PATCH("/mcp-servers/:id", rt.mw.RequirePermission(domain.ToolsWrite), mcpServerHandlers.PatchWorkspaceServer)
				wsGroup.DELETE("/mcp-servers/:id", rt.mw.RequirePermission(domain.ToolsWrite), mcpServerHandlers.DeleteWorkspaceServer)
				wsGroup.POST("/mcp-servers/:id/probe", rt.mw.RequirePermission(domain.ToolsWrite), mcpServerHandlers.ProbeWorkspaceServer)

				// Workspace skill library (registry + system tier; Member reads,
				// Owner/Admin/Superadmin manage via skills.write)
				wsGroup.GET("/skills", rt.mw.RequirePermission(domain.SkillsRead), skillHandlers.ListSkills)
				wsGroup.POST("/skills", rt.mw.RequirePermission(domain.SkillsWrite), skillHandlers.CreateSkill)
				wsGroup.POST("/skills/inspect", rt.mw.RequirePermission(domain.SkillsWrite), skillHandlers.InspectUpload)
				wsGroup.POST("/skills/inspect/git", rt.mw.RequirePermission(domain.SkillsWrite), skillHandlers.InspectGit)
				wsGroup.GET("/skills/:name", rt.mw.RequirePermission(domain.SkillsRead), skillHandlers.GetSkill)
				wsGroup.PUT("/skills/:name", rt.mw.RequirePermission(domain.SkillsWrite), skillHandlers.UpdateSkill)
				wsGroup.PATCH("/skills/:name", rt.mw.RequirePermission(domain.SkillsWrite), skillHandlers.PatchSkill)
				wsGroup.DELETE("/skills/:name", rt.mw.RequirePermission(domain.SkillsWrite), skillHandlers.DeleteSkill)
				wsGroup.POST("/skills/:name/dependencies/recheck", rt.mw.RequirePermission(domain.SkillsWrite), skillHandlers.RecheckSkill)

				// Agent-tier skills: install/remove under the agent endpoints
				// (presence on disk is the state)
				wsGroup.GET("/agents/:agent/skills", rt.mw.RequirePermission(domain.SkillsRead), skillHandlers.ListAgentSkills)
				wsGroup.POST("/agents/:agent/skills", rt.mw.RequirePermission(domain.SkillsWrite), skillHandlers.InstallAgentSkill)
				wsGroup.DELETE("/agents/:agent/skills/:name", rt.mw.RequirePermission(domain.SkillsWrite), skillHandlers.RemoveAgentSkill)

				// Agent-private MCP servers (agents.write per design.md D9;
				// reads ride agents.read like the agent detail endpoint)
				wsGroup.GET("/agents/:agent/mcp-servers", rt.mw.RequirePermission(domain.AgentsRead), mcpServerHandlers.ListAgentServers)
				wsGroup.POST("/agents/:agent/mcp-servers", rt.mw.RequirePermission(domain.AgentsWrite), mcpServerHandlers.CreateAgentServer)
				wsGroup.PATCH("/agents/:agent/mcp-servers/:id", rt.mw.RequirePermission(domain.AgentsWrite), mcpServerHandlers.PatchAgentServer)
				wsGroup.DELETE("/agents/:agent/mcp-servers/:id", rt.mw.RequirePermission(domain.AgentsWrite), mcpServerHandlers.DeleteAgentServer)
				wsGroup.POST("/agents/:agent/mcp-servers/:id/probe", rt.mw.RequirePermission(domain.AgentsWrite), mcpServerHandlers.ProbeAgentServer)

				// API keys management (workspace settings; workspace.write is Owner/Admin only)
				// Exchange is Member-level: membership via RequireWorkspace suffices,
				// like the other member-readable routes (e.g. GET /tools).
				wsGroup.POST("/api-keys/exchange", apiKeyHandlers.ExchangeAPIKey)
				wsGroup.GET("/api-keys", rt.mw.RequirePermission(domain.WorkspaceWrite), apiKeyHandlers.ListAPIKeys)
				wsGroup.POST("/api-keys", rt.mw.RequirePermission(domain.WorkspaceWrite), apiKeyHandlers.CreateAPIKey)
				wsGroup.DELETE("/api-keys/:id", rt.mw.RequirePermission(domain.WorkspaceWrite), apiKeyHandlers.RevokeAPIKey)

				// Workspace agent lifecycle hooks (D13/D17): reads ride
				// hooks.read, writes hooks.write. The instance section of the
				// list is read-only visibility; the dry-run endpoint executes
				// the real handler against a synthetic event.
				wsGroup.GET("/hooks", rt.mw.RequirePermission(domain.HooksRead), hookHandlers.ListWorkspaceHooks)
				wsGroup.POST("/hooks", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.CreateWorkspaceHook)
				wsGroup.POST("/hooks/reorder", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.ReorderWorkspaceHooks)
				wsGroup.POST("/hooks/test", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.TestWorkspaceHook)
				wsGroup.GET("/hooks/executions", rt.mw.RequirePermission(domain.HooksRead), hookHandlers.ListWorkspaceHookExecutions)
				wsGroup.GET("/hooks/:id/executions", rt.mw.RequirePermission(domain.HooksRead), hookHandlers.ListWorkspaceHookExecutions)
				wsGroup.PATCH("/hooks/:id", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.PatchWorkspaceHook)
				wsGroup.DELETE("/hooks/:id", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.DeleteWorkspaceHook)

				// Agent-level hooks (agent config modal; private to the
				// owning agent, D13): same permission contract as the
				// workspace level.
				wsGroup.GET("/agents/:agent/hooks", rt.mw.RequirePermission(domain.HooksRead), hookHandlers.ListAgentLevelHooks)
				wsGroup.POST("/agents/:agent/hooks", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.CreateAgentHook)
				wsGroup.POST("/agents/:agent/hooks/reorder", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.ReorderAgentHooks)
				wsGroup.PATCH("/agents/:agent/hooks/:id", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.PatchAgentHook)
				wsGroup.DELETE("/agents/:agent/hooks/:id", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.DeleteAgentHook)
			}

			// Instance Admin route group (master tenant control plane)
			adminGroup := authed.Group("/admin")
			if rt.opts.Store != nil {
				adminGroup.Use(rt.mw.RequireMasterWorkspace())
			}
			{
				// Workspaces management
				adminGroup.GET("/workspaces", rt.mw.RequirePermission(domain.AdminWorkspacesRead), adminWorkspaceHandlers.AdminListWorkspaces)
				adminGroup.POST("/workspaces", rt.mw.RequirePermission(domain.AdminWorkspacesWrite), adminWorkspaceHandlers.AdminCreateWorkspace)
				adminGroup.PATCH("/workspaces/:ws", rt.mw.RequirePermission(domain.AdminWorkspacesWrite), adminWorkspaceHandlers.AdminPatchWorkspace)
				adminGroup.PATCH("/workspaces/:ws/owner", rt.mw.RequirePermission(domain.AdminWorkspacesWrite), adminWorkspaceHandlers.AdminTransferWorkspaceOwner)
				adminGroup.GET("/workspaces/:ws/members", rt.mw.RequirePermission(domain.AdminWorkspacesRead), adminWorkspaceHandlers.AdminListWorkspaceMembers)
				adminGroup.POST("/workspaces/:ws/members", rt.mw.RequirePermission(domain.AdminWorkspacesWrite), adminWorkspaceHandlers.AdminAddWorkspaceMember)
				adminGroup.POST("/workspaces/:ws/disable", rt.mw.RequirePermission(domain.AdminWorkspacesWrite), adminWorkspaceHandlers.AdminDisableWorkspace)
				adminGroup.POST("/workspaces/:ws/enable", rt.mw.RequirePermission(domain.AdminWorkspacesWrite), adminWorkspaceHandlers.AdminEnableWorkspace)

				// Users management
				adminGroup.GET("/users", rt.mw.RequirePermission(domain.AdminUsersRead), adminUserHandlers.AdminListUsers)
				adminGroup.POST("/users", rt.mw.RequirePermission(domain.AdminUsersWrite), adminUserHandlers.AdminCreateUser)
				adminGroup.POST("/users/:uid/disable", rt.mw.RequirePermission(domain.AdminUsersWrite), adminUserHandlers.AdminDisableUser)
				adminGroup.POST("/users/:uid/enable", rt.mw.RequirePermission(domain.AdminUsersWrite), adminUserHandlers.AdminEnableUser)

				// Superadmins management
				adminGroup.POST("/superadmins", rt.mw.RequirePermission(domain.AdminSuperadminsWrite), adminSuperadminHandlers.AdminGrantSuperadmin)
				adminGroup.DELETE("/superadmins/:uid", rt.mw.RequirePermission(domain.AdminSuperadminsWrite), adminSuperadminHandlers.AdminRevokeSuperadmin)

				// Instance hooks (D13/D15/D17): managed rows under superadmin
				// CRUD plus the read-only builtin listing. Hooks permissions
				// gate the surface (the built-in master-tenant Superadmin role
				// holds them via the permission backfill); no workspace scope
				// anywhere in these paths.
				adminGroup.GET("/hooks", rt.mw.RequirePermission(domain.HooksRead), hookHandlers.AdminListHooks)
				adminGroup.POST("/hooks", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.AdminCreateHook)
				adminGroup.POST("/hooks/reorder", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.AdminReorderHooks)
				adminGroup.PATCH("/hooks/:id", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.AdminPatchHook)
				adminGroup.DELETE("/hooks/:id", rt.mw.RequirePermission(domain.HooksWrite), hookHandlers.AdminDeleteHook)
			}
		}
	}

	return r
}

// NewRouter builds and returns the configured Gin Engine.
func NewRouter(opts RouterOptions) *gin.Engine {
	return New(opts).Engine()
}
