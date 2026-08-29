package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

const (
	// UserContextKey is the gin context key for the authenticated user (*domain.User).
	UserContextKey = "current_user"

	// WorkspaceContextKey is the gin context key for the resolved workspace (*domain.Workspace).
	WorkspaceContextKey = "current_workspace"

	// MemberContextKey is the gin context key for the user's membership (*domain.Member).
	MemberContextKey = "current_member"

	// RoleContextKey is the gin context key for the member's role (*domain.Role).
	RoleContextKey = "current_role"
)

// CurrentUser retrieves the authenticated *domain.User from the Gin context.
func CurrentUser(c *gin.Context) (*domain.User, bool) {
	val, exists := c.Get(UserContextKey)
	if !exists {
		return nil, false
	}
	u, ok := val.(*domain.User)
	return u, ok && u != nil
}

// MustCurrentUser retrieves the authenticated *domain.User from the Gin context or panics.
func MustCurrentUser(c *gin.Context) *domain.User {
	u, ok := CurrentUser(c)
	if !ok {
		panic("current user not found in context")
	}
	return u
}

// CurrentWorkspace retrieves the resolved *domain.Workspace from the Gin context.
func CurrentWorkspace(c *gin.Context) (*domain.Workspace, bool) {
	val, exists := c.Get(WorkspaceContextKey)
	if !exists {
		return nil, false
	}
	w, ok := val.(*domain.Workspace)
	return w, ok && w != nil
}

// MustCurrentWorkspace retrieves the resolved *domain.Workspace from the Gin context or panics.
func MustCurrentWorkspace(c *gin.Context) *domain.Workspace {
	w, ok := CurrentWorkspace(c)
	if !ok {
		panic("current workspace not found in context")
	}
	return w
}

// CurrentMember retrieves the resolved *domain.Member from the Gin context.
func CurrentMember(c *gin.Context) (*domain.Member, bool) {
	val, exists := c.Get(MemberContextKey)
	if !exists {
		return nil, false
	}
	m, ok := val.(*domain.Member)
	return m, ok && m != nil
}

// MustCurrentMember retrieves the resolved *domain.Member from the Gin context or panics.
func MustCurrentMember(c *gin.Context) *domain.Member {
	m, ok := CurrentMember(c)
	if !ok {
		panic("current member not found in context")
	}
	return m
}

// CurrentRole retrieves the resolved *domain.Role from the Gin context.
func CurrentRole(c *gin.Context) (*domain.Role, bool) {
	val, exists := c.Get(RoleContextKey)
	if !exists {
		return nil, false
	}
	r, ok := val.(*domain.Role)
	return r, ok && r != nil
}

// MustCurrentRole retrieves the resolved *domain.Role from the Gin context or panics.
func MustCurrentRole(c *gin.Context) *domain.Role {
	r, ok := CurrentRole(c)
	if !ok {
		panic("current role not found in context")
	}
	return r
}
