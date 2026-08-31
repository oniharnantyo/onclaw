package server

import (
	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/auth"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/storage"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// RouterOptions holds dependencies required by the HTTP API router.
type RouterOptions struct {
	Store         store.Store
	Storage       storage.Storage
	Issuer        auth.TokenIssuer
	Auth          auth.Service
	EncryptionKey []byte
	Providers     *providers.Registry
}

// router configures and builds the HTTP API routes and handlers.
type router struct {
	opts RouterOptions
	mw   *middlewares
}

// New creates a new router instance with all handlers and middlewares initialized.
func New(opts RouterOptions) *router {
	return &router{
		opts: opts,
		mw:   NewMiddlewares(opts.Store, opts.Issuer),
	}
}

// Engine builds and returns the configured Gin Engine.
func (rt *router) Engine() *gin.Engine {
	authService := rt.opts.Auth
	if authService == nil && rt.opts.Store != nil && rt.opts.Issuer != nil {
		reg := auth.NewRegistry()
		reg.Register(auth.NewPasswordProvider(rt.opts.Store.Users()))
		authService = auth.NewService(rt.opts.Store, rt.opts.Issuer, reg)
	}

	providerRegistry := rt.opts.Providers
	if providerRegistry == nil {
		providerRegistry = providers.NewRegistry()
	}

	authHandlers := handlers.NewAuthHandlers(authService, rt.opts.Storage)
	workspaceHandlers := handlers.NewWorkspaceHandlers(rt.opts.Store)
	memberHandlers := handlers.NewMemberHandlers(rt.opts.Store, rt.opts.Storage)
	roleHandlers := handlers.NewRoleHandlers(rt.opts.Store)
	userHandlers := handlers.NewUserHandlers(rt.opts.Store, rt.opts.Storage)
	fileHandlers := handlers.NewFileHandlers(rt.opts.Storage)
	adminWorkspaceHandlers := handlers.NewAdminWorkspaceHandlers(rt.opts.Store, rt.opts.Storage)
	adminUserHandlers := handlers.NewAdminUserHandlers(rt.opts.Store)
	adminSuperadminHandlers := handlers.NewAdminSuperadminHandlers(rt.opts.Store)
	providerHandlers := handlers.NewProviderHandlers(rt.opts.Store, rt.opts.EncryptionKey, providerRegistry)

	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.Use(gin.Recovery())
	r.Use(gin.Logger())

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
			}
		}
	}

	return r
}

// NewRouter builds and returns the configured Gin Engine.
func NewRouter(opts RouterOptions) *gin.Engine {
	return New(opts).Engine()
}
