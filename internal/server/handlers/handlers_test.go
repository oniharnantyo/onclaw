package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/services"
	storagefake "github.com/oniharnantyo/onclaw/internal/storage/fake"
	"github.com/oniharnantyo/onclaw/internal/storage/resolver"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

func TestHandlers_New(t *testing.T) {
	gin.SetMode(gin.TestMode)

	st := storefake.New()
	stor := storagefake.New()
	issuer := services.NewJWTIssuer(services.JWTConfig{
		Secret: "secret-for-handlers-unit-tests-32-bytes!!",
		TTL:    time.Hour,
	})
	authSvc := services.NewAuthService(st.Users(), st.Members(), issuer, nil)

	authH := handlers.NewAuthHandlers(authSvc, stor)
	if authH == nil {
		t.Fatal("expected non-nil AuthHandlers instance")
	}

	wsH := handlers.NewWorkspaceHandlers(st, []byte("01234567890123456789012345678901"), nil, nil, nil, t.TempDir(), mustTestAuthorizer(t, st))
	if wsH == nil {
		t.Fatal("expected non-nil WorkspaceHandlers instance")
	}

	memH := handlers.NewMemberHandlers(st, stor, mustTestAuthorizer(t, st))
	if memH == nil {
		t.Fatal("expected non-nil MemberHandlers instance")
	}

	roleH := handlers.NewRoleHandlers(st.Roles())
	if roleH == nil {
		t.Fatal("expected non-nil RoleHandlers instance")
	}

	userH := handlers.NewUserHandlers(st.Users(), stor)
	if userH == nil {
		t.Fatal("expected non-nil UserHandlers instance")
	}

	wsStorage := resolver.New(stor, st.WorkspaceStorage(), st.Attachments(), []byte("01234567890123456789012345678901"), t.TempDir())
	fileH := handlers.NewFileHandlers(stor, st.Attachments(), st.ReferenceDocuments(), wsStorage)
	if fileH == nil {
		t.Fatal("expected non-nil FileHandlers instance")
	}

	admWsH := handlers.NewAdminWorkspaceHandlers(st, stor, mustTestAuthorizer(t, st))
	if admWsH == nil {
		t.Fatal("expected non-nil AdminWorkspaceHandlers instance")
	}

	admUsrH := handlers.NewAdminUserHandlers(st.Users(), st.Workspaces(), st.Members(), st.Roles())
	if admUsrH == nil {
		t.Fatal("expected non-nil AdminUserHandlers instance")
	}

	admSupH := handlers.NewAdminSuperadminHandlers(st)
	if admSupH == nil {
		t.Fatal("expected non-nil AdminSuperadminHandlers instance")
	}

	provH := handlers.NewProviderHandlers(st.Providers(), st.Agents(), []byte("01234567890123456789012345678901"), nil, nil, st.ToolSettings())
	if provH == nil {
		t.Fatal("expected non-nil ProviderHandlers instance")
	}

	agentH := handlers.NewAgentHandlers(st.Agents(), st.Providers(), st.SessionEvents(), st.AgentSessions(), []byte("01234567890123456789012345678901"), nil, nil, nil, t.TempDir(), nil, nil, mustTestAuthorizer(t, st))
	if agentH == nil {
		t.Fatal("expected non-nil AgentHandlers instance")
	}

	r := gin.New()
	r.POST("/logout", authH.Logout)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected status 204 No Content on logout, got %d", w.Code)
	}
}
