package promptgen

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/promptdocs"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/secrets"
)

func TestService_GenerateForCreate_WritesDocumentsWithoutRow(t *testing.T) {
	ctx := context.Background()
	svc, st, key := setupTestService(t, func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
		return promptsToolCallMessage(
			"# Identity\nNew agent identity.",
			"# Soul\nNew agent soul.",
		), nil
	})

	ws := &domain.Workspace{ID: "ws-create-1", Slug: "ws-create-1", Name: "WS Create 1"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create ws: %v", err)
	}

	encKey, err := secrets.Encrypt(key, []byte(ws.ID), []byte("sk-secret-provider-key"))
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}

	p := &domain.ProviderConfig{
		ID:            "prov-create-1",
		WorkspaceID:   ws.ID,
		Type:          providers.TypeOpenAI,
		Name:          "OpenAI",
		KeyCiphertext: encKey,
		Enabled:       true,
	}
	if err := st.Providers().Create(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	wsDir := filepath.Join(t.TempDir(), "agents", "scout")
	if err := promptdocs.SeedWorkspace(wsDir); err != nil {
		t.Fatalf("seed workspace dir: %v", err)
	}

	// The agent exists only as an in-memory payload — no row anywhere.
	agent := &domain.Agent{
		ID:            "agent-not-persisted",
		WorkspaceID:   ws.ID,
		Slug:          "scout",
		Name:          "Scout",
		Role:          "scout",
		Brief:         "Scout the perimeter.",
		ProviderID:    p.ID,
		Model:         "gpt-4o",
		PromptsStatus: domain.PromptsStatusGenerating,
	}

	if err := svc.GenerateForCreate(ctx, wsDir, ws.ID, agent); err != nil {
		t.Fatalf("GenerateForCreate: %v", err)
	}

	// Documents are on disk — BOOTSTRAP.md seeded with the embedded template…
	id, soul, _, err := promptdocs.ReadPromptDocuments(wsDir)
	if err != nil {
		t.Fatalf("read documents: %v", err)
	}
	if id != "# Identity\nNew agent identity." || soul != "# Soul\nNew agent soul." {
		t.Errorf("unexpected document contents: %q / %q", id, soul)
	}
	boot, err := os.ReadFile(filepath.Join(wsDir, "BOOTSTRAP.md"))
	if err != nil {
		t.Fatalf("read BOOTSTRAP.md: %v", err)
	}
	if string(boot) != promptdocs.BootstrapTemplate {
		t.Errorf("BOOTSTRAP.md = %q, want the embedded template", string(boot))
	}

	// …and no agent row exists.
	if _, err := st.Agents().ByID(ctx, ws.ID, agent.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected no agent row after GenerateForCreate, got %v", err)
	}
}

func TestService_GenerateForCreate_FailureReturnsErrorAndWritesNothing(t *testing.T) {
	ctx := context.Background()
	svc, st, key := setupTestService(t, func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
		return nil, errors.New("401 unauthorized from provider")
	})

	ws := &domain.Workspace{ID: "ws-create-2", Slug: "ws-create-2", Name: "WS Create 2"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create ws: %v", err)
	}

	encKey, err := secrets.Encrypt(key, []byte(ws.ID), []byte("sk-secret-provider-key"))
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}

	p := &domain.ProviderConfig{
		ID:            "prov-create-2",
		WorkspaceID:   ws.ID,
		Type:          providers.TypeOpenAI,
		Name:          "OpenAI",
		KeyCiphertext: encKey,
		Enabled:       true,
	}
	if err := st.Providers().Create(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	wsDir := filepath.Join(t.TempDir(), "agents", "drifter")
	if err := promptdocs.SeedWorkspace(wsDir); err != nil {
		t.Fatalf("seed workspace dir: %v", err)
	}

	agent := &domain.Agent{
		ID:            "agent-also-not-persisted",
		WorkspaceID:   ws.ID,
		Slug:          "drifter",
		Name:          "Drifter",
		Role:          "drifter",
		Brief:         "Wander purposefully.",
		ProviderID:    p.ID,
		Model:         "gpt-4o",
		PromptsStatus: domain.PromptsStatusGenerating,
	}

	err = svc.GenerateForCreate(ctx, wsDir, ws.ID, agent)
	if err == nil {
		t.Fatal("expected GenerateForCreate to fail")
	}
	if !strings.Contains(err.Error(), "provider authentication failed") {
		t.Errorf("expected sanitized provider-auth error, got %q", err.Error())
	}

	// Nothing was written: no documents, no row.
	id, soul, boot, _ := promptdocs.ReadPromptDocuments(wsDir)
	if id != "" || soul != "" || boot != "" {
		t.Errorf("expected no documents on failure, got %q / %q / %q", id, soul, boot)
	}
	if _, err := st.Agents().ByID(ctx, ws.ID, agent.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected no agent row after failed generation, got %v", err)
	}
}
