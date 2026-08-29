package server

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/auth"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// middlewares holds dependencies for HTTP middleware handlers.
type middlewares struct {
	store  store.Store
	issuer auth.TokenIssuer
}

// NewMiddlewares creates a new middlewares instance with the given store and token issuer.
func NewMiddlewares(st store.Store, issuer auth.TokenIssuer) *middlewares {
	return &middlewares{
		store:  st,
		issuer: issuer,
	}
}

// AuthRequired validates the Bearer token, loads the user from the store,
// and rejects disabled accounts immediately.
func (m *middlewares) AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			AbortUnauthenticated(c, "missing authorization header")
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
			AbortUnauthenticated(c, "invalid authorization header format")
			return
		}

		tokenStr := strings.TrimSpace(parts[1])
		claims, err := m.issuer.Verify(c.Request.Context(), tokenStr)
		if err != nil {
			AbortUnauthenticated(c, "invalid or expired token")
			return
		}

		user, err := m.store.Users().ByID(c.Request.Context(), claims.UserID)
		if err != nil {
			AbortUnauthenticated(c, "user not found")
			return
		}

		if user.IsDisabled() {
			AbortUnauthenticated(c, "user account is disabled")
			return
		}

		c.Set(UserContextKey, user)
		c.Next()
	}
}

// RequireWorkspace resolves the workspace by URL slug param, ensures the authenticated
// user is a member of that workspace, binds the workspace, member, and role to the context.
// Non-members receive a 404 (indistinguishable from unknown workspace for enumeration defense).
// Suspended workspaces return 403.
func (m *middlewares) RequireWorkspace(slugParam ...string) gin.HandlerFunc {
	paramName := "ws"
	if len(slugParam) > 0 && slugParam[0] != "" {
		paramName = slugParam[0]
	}

	return func(c *gin.Context) {
		slug := c.Param(paramName)
		if slug == "" {
			AbortNotFound(c, "workspace not found")
			return
		}

		user, ok := CurrentUser(c)
		if !ok || user == nil {
			AbortUnauthenticated(c, "unauthenticated")
			return
		}

		ws, err := m.store.Workspaces().BySlug(c.Request.Context(), slug)
		if err != nil {
			AbortNotFound(c, "workspace not found")
			return
		}

		member, err := m.store.Members().Get(c.Request.Context(), ws.ID, user.ID)
		if err != nil {
			// Non-member gets 404 (indistinguishable from unknown slug)
			AbortNotFound(c, "workspace not found")
			return
		}

		if member.Role == nil {
			role, err := m.store.Roles().ByID(c.Request.Context(), member.RoleID)
			if err != nil {
				AbortNotFound(c, "role not found")
				return
			}
			member.Role = role
		}

		if ws.IsSuspended() {
			AbortForbidden(c, "workspace is suspended")
			return
		}

		c.Set(WorkspaceContextKey, ws)
		c.Set(MemberContextKey, member)
		c.Set(RoleContextKey, member.Role)
		c.Next()
	}
}

// RequireMasterWorkspace resolves the master workspace, ensures the authenticated
// user is a member of the master workspace, and binds the master workspace, member, and role to the context.
// Non-members receive a 404 (indistinguishable from unknown workspace for enumeration defense).
// Suspended workspaces return 403.
func (m *middlewares) RequireMasterWorkspace() gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := CurrentUser(c)
		if !ok || user == nil {
			AbortUnauthenticated(c, "unauthenticated")
			return
		}

		ws, err := m.store.Workspaces().BySlug(c.Request.Context(), domain.MasterWorkspaceSlug)
		if err != nil {
			AbortNotFound(c, "workspace not found")
			return
		}

		member, err := m.store.Members().Get(c.Request.Context(), ws.ID, user.ID)
		if err != nil {
			// Non-member in master tenant gets 404 (enumeration defense)
			AbortNotFound(c, "workspace not found")
			return
		}

		if member.Role == nil {
			role, err := m.store.Roles().ByID(c.Request.Context(), member.RoleID)
			if err != nil {
				AbortNotFound(c, "role not found")
				return
			}
			member.Role = role
		}

		if ws.IsSuspended() {
			AbortForbidden(c, "workspace is suspended")
			return
		}

		c.Set(WorkspaceContextKey, ws)
		c.Set(MemberContextKey, member)
		c.Set(RoleContextKey, member.Role)
		c.Next()
	}
}

// RequirePermission verifies that the member's resolved role contains the required permission.
func (m *middlewares) RequirePermission(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, ok := CurrentRole(c)
		if !ok || role == nil {
			member, ok := CurrentMember(c)
			if ok && member != nil && member.Role != nil {
				role = member.Role
			}
		}

		if role == nil {
			AbortForbidden(c, "insufficient permissions")
			return
		}

		if !domain.HasPermission(role.Permissions, permission) {
			AbortForbidden(c, "insufficient permissions")
			return
		}

		c.Next()
	}
}

// RequireAllPermissions verifies that the member's resolved role contains all specified permissions.
func (m *middlewares) RequireAllPermissions(permissions ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, ok := CurrentRole(c)
		if !ok || role == nil {
			member, ok := CurrentMember(c)
			if ok && member != nil && member.Role != nil {
				role = member.Role
			}
		}

		if role == nil {
			AbortForbidden(c, "insufficient permissions")
			return
		}

		for _, p := range permissions {
			if !domain.HasPermission(role.Permissions, p) {
				AbortForbidden(c, "insufficient permissions")
				return
			}
		}

		c.Next()
	}
}
