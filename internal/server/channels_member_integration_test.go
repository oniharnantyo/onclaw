//go:build integration

package server_test

// Channels member-access integration (fix-role-permission-audit 3.4): with
// channels.read/channels.write in the built-in Member set, a member lists
// channels, posts a message that mentions an agent channel-member (a run
// fires on the channel's session), and manages the roster — through the real
// router over the real PostgreSQL store. No route changes: the channels
// routes are channels.*-gated and the built-in Member role now holds both.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/channels"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/promptgen"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/services"
	storagefake "github.com/oniharnantyo/onclaw/internal/storage/fake"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/postgres"
)

// chMemberBaseDSN resolves the integration DSN (the setupTestSchema pattern).
func chMemberBaseDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		dsn = "postgres://localhost:5432/postgres?sslmode=disable"
	}
	return dsn
}

// chMemberTestStore builds a migrated, isolated per-test PostgreSQL store.
func chMemberTestStore(t *testing.T) (store.Store, string) {
	t.Helper()
	ctx := context.Background()
	baseDSN := chMemberBaseDSN(t)

	conn, err := pgx.Connect(ctx, baseDSN)
	if err != nil {
		t.Skipf("skipping integration test: database connection failed: %v", err)
	}
	defer conn.Close(ctx)

	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random schema suffix: %v", err)
	}
	schemaName := fmt.Sprintf("chan_member_%x", b)

	if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schemaName)); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	separator := "?"
	if strings.Contains(baseDSN, "?") {
		separator = "&"
	}
	schemaDSN := fmt.Sprintf("%s%ssearch_path=%s,public", baseDSN, separator, schemaName)

	migrator := postgres.NewMigrator(schemaDSN)
	if err := migrator.Up(); err != nil {
		_, _ = conn.Exec(ctx, fmt.Sprintf("DROP SCHEMA %s CASCADE", schemaName))
		t.Fatalf("run migrations: %v", err)
	}

	st, err := postgres.New(ctx, schemaDSN)
	if err != nil {
		_, _ = conn.Exec(ctx, fmt.Sprintf("DROP SCHEMA %s CASCADE", schemaName))
		t.Fatalf("open postgres store: %v", err)
	}

	t.Cleanup(func() {
		_ = st.Close()
		cleanupConn, err := pgx.Connect(context.Background(), baseDSN)
		if err == nil {
			_, _ = cleanupConn.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName))
			_ = cleanupConn.Close(context.Background())
		}
	})

	return st, schemaDSN
}

// randRead was folded into crypto/rand usage above.

// chMemberStubModel answers every model call with a fixed text message so
// the mention run's model call never touches the network.
type chMemberStubModel struct{}

func (chMemberStubModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	return &schema.Message{Content: "on it"}, nil
}

func (chMemberStubModel) Stream(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	sr, sw := schema.Pipe[*schema.Message](1)
	_ = sw.Send(&schema.Message{Content: "on it"}, nil)
	sw.Close()
	return sr, nil
}

// TestIntegration_ChannelsMemberAccess drives the member channel lane
// end-to-end: list → post with an @agent mention (run fires) → roster add.
func TestIntegration_ChannelsMemberAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	st, _ := chMemberTestStore(t)

	issuer := services.NewJWTIssuer(services.JWTConfig{
		Secret: "integration-secret-key-32-bytes-long!!",
		TTL:    time.Hour * 24,
	})
	authSvc := services.NewAuthService(st.Users(), st.Members(), issuer, nil)
	encKey := []byte("01234567890123456789012345678901")
	agentSvc := promptgen.NewService(st.Agents(), st.Providers(), encKey, promptgen.WithModelFactory(func(_ context.Context, _ string, _ providers.Credential, _ string) (model.BaseChatModel, error) {
		return chMemberStubModel{}, nil
	}))
	r := server.NewRouter(server.RouterOptions{
		Store:         st,
		Storage:       storagefake.New(),
		Issuer:        issuer,
		Auth:          authSvc,
		EncryptionKey: encKey,
		AgentService:  agentSvc,
		WorkspaceDir:  filepath.Join(t.TempDir(), "workspaces"),
	})

	// Seed: workspace with the built-in role sets, an owner, a member, and
	// an agent on the #ops roster.
	ws := &domain.Workspace{Slug: "chanmem", Name: "Chan Member WS", Timezone: "UTC"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	ownerRole := &domain.Role{WorkspaceID: ws.ID, Name: domain.RoleOwner, IsOwner: true, Permissions: domain.OwnerPermissions, BuiltIn: true}
	memberRole := &domain.Role{WorkspaceID: ws.ID, Name: domain.RoleMember, Permissions: domain.MemberPermissions, BuiltIn: true}
	for _, role := range []*domain.Role{ownerRole, memberRole} {
		if err := st.Roles().Create(ctx, role); err != nil {
			t.Fatalf("create role: %v", err)
		}
	}
	owner := &domain.User{Email: "chan-owner@example.com", Name: "Owner"}
	member := &domain.User{Email: "chan-member@example.com", Name: "Member"}
	for _, u := range []*domain.User{owner, member} {
		if err := st.Users().Create(ctx, u); err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: owner.ID, RoleID: ownerRole.ID}); err != nil {
		t.Fatalf("add owner membership: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: member.ID, RoleID: memberRole.ID}); err != nil {
		t.Fatalf("add member membership: %v", err)
	}

	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "OpenAI", Enabled: true}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	agent := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "atlas",
		Name:        "Atlas",
		ProviderID:  prov.ID,
		Model:       "gpt-4o",
		Temperature: 1.0,
	}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	channel := &domain.Channel{WorkspaceID: ws.ID, Slug: "ops", Name: "#ops"}
	if err := st.Channels().CreateChannel(ctx, channel); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := st.Channels().AddChannelMember(ctx, &domain.ChannelMember{
		WorkspaceID: ws.ID,
		ChannelID:   channel.ID,
		MemberType:  domain.ChannelMemberTypeAgent,
		AgentID:     agent.ID,
	}); err != nil {
		t.Fatalf("add agent to roster: %v", err)
	}

	memberToken, err := issuer.Issue(ctx, member)
	if err != nil {
		t.Fatalf("issue member token: %v", err)
	}

	do := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var reader io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			reader = bytes.NewReader(b)
		}
		req := httptest.NewRequest(method, path, reader)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Authorization", "Bearer "+memberToken)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	// 1. Member lists channels (channels.read).
	rec := do(http.MethodGet, "/api/v1/workspaces/chanmem/channels", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("member list channels: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "#ops") {
		t.Fatalf("member channel list missing #ops: %s", rec.Body.String())
	}

	// 2. Member posts a message mentioning the agent (channels.write); the
	// mention summons the agent — a run fires on the channel's session.
	rec = do(http.MethodPost, "/api/v1/workspaces/chanmem/channels/"+channel.ID+"/messages", map[string]string{
		"body": "@atlas please check the deploy pipeline",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("member post message: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	chanSession := channels.ChannelSessionID(channel.ID, agent.ID)
	deadline := time.Now().Add(30 * time.Second)
	events := 0
	for time.Now().Before(deadline) {
		rows, err := st.SessionEvents().LoadEvents(ctx, store.LoadSessionEventsParams{
			WorkspaceID: ws.ID,
			SessionID:   chanSession,
		})
		if err != nil {
			t.Fatalf("load channel session events: %v", err)
		}
		if events = len(rows); events > 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if events == 0 {
		t.Fatalf("channel run never produced transcript events for session %s", chanSession)
	}

	// 3. Member manages the roster (channels.write): adds the owner as a
	// channel member.
	rec = do(http.MethodPost, "/api/v1/workspaces/chanmem/channels/"+channel.ID+"/members", map[string]any{
		"member_type": string(domain.ChannelMemberTypeUser),
		"user_id":     owner.ID,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("member add channel member: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	roster, err := st.Channels().ListChannelMembers(ctx, ws.ID, channel.ID)
	if err != nil {
		t.Fatalf("list roster: %v", err)
	}
	foundOwner := false
	for _, m := range roster {
		if m.MemberType == domain.ChannelMemberTypeUser && m.UserID == owner.ID {
			foundOwner = true
		}
	}
	if !foundOwner {
		t.Fatalf("owner missing from roster after member add: %+v", roster)
	}
}
