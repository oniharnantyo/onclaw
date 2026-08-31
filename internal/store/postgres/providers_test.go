//go:build integration

package postgres_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestIntegration_ProviderStore_CRUD(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

	// Seed workspaces
	ws1 := &domain.Workspace{Slug: "ws-prov-1", Name: "Provider Workspace 1"}
	if err := s.Workspaces().Create(ctx, ws1); err != nil {
		t.Fatalf("unexpected create workspace 1 error: %v", err)
	}

	ws2 := &domain.Workspace{Slug: "ws-prov-2", Name: "Provider Workspace 2"}
	if err := s.Workspaces().Create(ctx, ws2); err != nil {
		t.Fatalf("unexpected create workspace 2 error: %v", err)
	}

	// 1. Create provider
	p1 := &domain.ProviderConfig{
		WorkspaceID:   ws1.ID,
		Type:          "openai",
		Name:          "OpenAI Prod",
		BaseURL:       "https://api.openai.com",
		KeyCiphertext: "v1:nonce1:ciphertext1",
		KeyHint:       "1234",
		Enabled:       true,
	}
	if err := s.Providers().Create(ctx, p1); err != nil {
		t.Fatalf("unexpected create provider error: %v", err)
	}
	if p1.ID == "" {
		t.Fatal("expected provider ID to be assigned")
	}
	if p1.CreatedAt.IsZero() || p1.UpdatedAt.IsZero() {
		t.Fatal("expected timestamps to be set")
	}

	// 2. Validation on Create
	if err := s.Providers().Create(ctx, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil, got %v", err)
	}
	if err := s.Providers().Create(ctx, &domain.ProviderConfig{WorkspaceID: "", Type: "openai", Name: "No WS"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for missing workspace_id, got %v", err)
	}
	if err := s.Providers().Create(ctx, &domain.ProviderConfig{WorkspaceID: ws1.ID, Type: "", Name: "No Type"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for missing type, got %v", err)
	}
	if err := s.Providers().Create(ctx, &domain.ProviderConfig{WorkspaceID: ws1.ID, Type: "openai", Name: ""}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for missing name, got %v", err)
	}

	// Foreign key violation for non-existent workspace ID
	fakeWSID := uuid.NewString()
	err := s.Providers().Create(ctx, &domain.ProviderConfig{
		WorkspaceID: fakeWSID,
		Type:        "openai",
		Name:        "Invalid Workspace",
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on FK violation, got %v", err)
	}

	// 3. ByID lookup
	found, err := s.Providers().ByID(ctx, ws1.ID, p1.ID)
	if err != nil {
		t.Fatalf("unexpected ByID error: %v", err)
	}
	if found.ID != p1.ID || found.Name != "OpenAI Prod" || found.KeyCiphertext != "v1:nonce1:ciphertext1" || found.KeyHint != "1234" || !found.Enabled {
		t.Fatalf("unexpected provider found: %+v", found)
	}

	// Cross-tenant ByID lookup returns ErrNotFound
	_, err = s.Providers().ByID(ctx, ws2.ID, p1.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant ByID, got %v", err)
	}

	// Non-existent ByID
	_, err = s.Providers().ByID(ctx, ws1.ID, uuid.NewString())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for nonexistent ID, got %v", err)
	}

	// Invalid UUID format
	_, err = s.Providers().ByID(ctx, ws1.ID, "invalid-uuid")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for invalid UUID string, got %v", err)
	}

	// 4. Duplicate types allowed and ListForWorkspace
	p2 := &domain.ProviderConfig{
		WorkspaceID: ws1.ID,
		Type:        "openai",
		Name:        "OpenAI Sandbox",
		Enabled:     true,
	}
	if err := s.Providers().Create(ctx, p2); err != nil {
		t.Fatalf("expected duplicate type in same workspace to succeed, got %v", err)
	}

	p3 := &domain.ProviderConfig{
		WorkspaceID: ws2.ID,
		Type:        "anthropic",
		Name:        "Anthropic Main",
		Enabled:     true,
	}
	if err := s.Providers().Create(ctx, p3); err != nil {
		t.Fatalf("unexpected create p3 error: %v", err)
	}

	listWS1, err := s.Providers().ListForWorkspace(ctx, ws1.ID)
	if err != nil {
		t.Fatalf("unexpected ListForWorkspace error: %v", err)
	}
	if len(listWS1) != 2 {
		t.Fatalf("expected 2 providers for ws1, got %d", len(listWS1))
	}
	if listWS1[0].ID != p1.ID || listWS1[1].ID != p2.ID {
		t.Fatalf("expected providers in insertion order, got %+v", listWS1)
	}

	listWS2, err := s.Providers().ListForWorkspace(ctx, ws2.ID)
	if err != nil {
		t.Fatalf("unexpected ListForWorkspace error: %v", err)
	}
	if len(listWS2) != 1 {
		t.Fatalf("expected 1 provider for ws2, got %d", len(listWS2))
	}

	listEmpty, err := s.Providers().ListForWorkspace(ctx, uuid.NewString())
	if err != nil {
		t.Fatalf("unexpected ListForWorkspace error: %v", err)
	}
	if len(listEmpty) != 0 {
		t.Fatalf("expected 0 providers for nonexistent workspace, got %d", len(listEmpty))
	}

	// 5. Update
	p1.Name = "OpenAI Prod Updated"
	p1.BaseURL = "https://openai.acme-proxy.com"
	p1.KeyCiphertext = "v1:nonce2:ciphertext2"
	p1.KeyHint = "5678"
	p1.Enabled = false
	if err := s.Providers().Update(ctx, p1); err != nil {
		t.Fatalf("unexpected Update error: %v", err)
	}

	reloaded, err := s.Providers().ByID(ctx, ws1.ID, p1.ID)
	if err != nil {
		t.Fatalf("unexpected ByID after update: %v", err)
	}
	if reloaded.Name != "OpenAI Prod Updated" || reloaded.BaseURL != "https://openai.acme-proxy.com" ||
		reloaded.KeyCiphertext != "v1:nonce2:ciphertext2" || reloaded.KeyHint != "5678" || reloaded.Enabled {
		t.Fatalf("unexpected provider after update: %+v", reloaded)
	}

	// Update validation
	if err := s.Providers().Update(ctx, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil update, got %v", err)
	}
	if err := s.Providers().Update(ctx, &domain.ProviderConfig{ID: "", WorkspaceID: ws1.ID}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for update without ID, got %v", err)
	}
	if err := s.Providers().Update(ctx, &domain.ProviderConfig{ID: p1.ID, WorkspaceID: ""}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for update without WorkspaceID, got %v", err)
	}
	// Cross-tenant update returns ErrNotFound
	if err := s.Providers().Update(ctx, &domain.ProviderConfig{ID: p1.ID, WorkspaceID: ws2.ID, Name: "Hack"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant update, got %v", err)
	}

	// 6. Delete
	// Cross-tenant delete returns ErrNotFound
	if err := s.Providers().Delete(ctx, ws2.ID, p1.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant delete, got %v", err)
	}
	// Correct delete
	if err := s.Providers().Delete(ctx, ws1.ID, p1.ID); err != nil {
		t.Fatalf("unexpected Delete error: %v", err)
	}
	// Subsequent ByID returns ErrNotFound
	if _, err := s.Providers().ByID(ctx, ws1.ID, p1.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	// Subsequent Delete returns ErrNotFound
	if err := s.Providers().Delete(ctx, ws1.ID, p1.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for repeated delete, got %v", err)
	}
}

func TestIntegration_ProviderStore_WithTx(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

	ws := &domain.Workspace{Slug: "tx-prov-ws", Name: "Tx Provider WS"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected create workspace error: %v", err)
	}

	// Commit test
	err := s.WithTx(ctx, func(txStore store.Store) error {
		p := &domain.ProviderConfig{
			WorkspaceID:   ws.ID,
			Type:          "gemini",
			Name:          "Gemini Main",
			KeyCiphertext: "v1:nonce:gemini",
			KeyHint:       "9876",
			Enabled:       true,
		}
		return txStore.Providers().Create(ctx, p)
	})
	if err != nil {
		t.Fatalf("unexpected WithTx error: %v", err)
	}

	list, err := s.Providers().ListForWorkspace(ctx, ws.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 provider after tx commit, got %d (err: %v)", len(list), err)
	}
	if list[0].Name != "Gemini Main" {
		t.Fatalf("unexpected provider: %+v", list[0])
	}

	// Rollback test
	rollbackErr := errors.New("abort provider transaction")
	err = s.WithTx(ctx, func(txStore store.Store) error {
		p := &domain.ProviderConfig{
			WorkspaceID: ws.ID,
			Type:        "openrouter",
			Name:        "OpenRouter Main",
			Enabled:     true,
		}
		if err := txStore.Providers().Create(ctx, p); err != nil {
			return err
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("expected rollback error, got %v", err)
	}

	listAfterRollback, err := s.Providers().ListForWorkspace(ctx, ws.ID)
	if err != nil || len(listAfterRollback) != 1 {
		t.Fatalf("expected still 1 provider after rollback, got %d (err: %v)", len(listAfterRollback), err)
	}
}
