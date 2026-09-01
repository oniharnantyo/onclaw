package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/auth"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	storagefake "github.com/oniharnantyo/onclaw/internal/storage/fake"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

func TestHandlers_New(t *testing.T) {
	gin.SetMode(gin.TestMode)

	st := storefake.New()
	stor := storagefake.New()
	issuer := auth.NewJWTIssuer(auth.JWTConfig{
		Secret: "secret-for-handlers-unit-tests-32-bytes!!",
		TTL:    time.Hour,
	})
	authSvc := auth.NewService(st, issuer, nil)

	authH := handlers.NewAuthHandlers(authSvc, stor)
	if authH == nil {
		t.Fatal("expected non-nil AuthHandlers instance")
	}

	wsH := handlers.NewWorkspaceHandlers(st, []byte("01234567890123456789012345678901"), nil, nil, nil, t.TempDir())
	if wsH == nil {
		t.Fatal("expected non-nil WorkspaceHandlers instance")
	}

	memH := handlers.NewMemberHandlers(st, stor)
	if memH == nil {
		t.Fatal("expected non-nil MemberHandlers instance")
	}

	roleH := handlers.NewRoleHandlers(st)
	if roleH == nil {
		t.Fatal("expected non-nil RoleHandlers instance")
	}

	userH := handlers.NewUserHandlers(st, stor)
	if userH == nil {
		t.Fatal("expected non-nil UserHandlers instance")
	}

	fileH := handlers.NewFileHandlers(stor)
	if fileH == nil {
		t.Fatal("expected non-nil FileHandlers instance")
	}

	admWsH := handlers.NewAdminWorkspaceHandlers(st, stor)
	if admWsH == nil {
		t.Fatal("expected non-nil AdminWorkspaceHandlers instance")
	}

	admUsrH := handlers.NewAdminUserHandlers(st)
	if admUsrH == nil {
		t.Fatal("expected non-nil AdminUserHandlers instance")
	}

	admSupH := handlers.NewAdminSuperadminHandlers(st)
	if admSupH == nil {
		t.Fatal("expected non-nil AdminSuperadminHandlers instance")
	}

	provH := handlers.NewProviderHandlers(st, []byte("01234567890123456789012345678901"), nil, nil)
	if provH == nil {
		t.Fatal("expected non-nil ProviderHandlers instance")
	}

	agentH := handlers.NewAgentHandlers(st, []byte("01234567890123456789012345678901"), nil, nil, nil, t.TempDir())
	if agentH == nil {
		t.Fatal("expected non-nil AgentHandlers instance")
	}

	skillH := handlers.NewSkillHandlers(st)
	if skillH == nil {
		t.Fatal("expected non-nil SkillHandlers instance")
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
