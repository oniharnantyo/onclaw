package agents_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

func TestInstructionComposer_AllDocumentsPresent(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	_ = os.WriteFile(filepath.Join(tempDir, "AGENTS.md"), []byte("# Agents Overview"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "IDENTITY.md"), []byte("# Identity: Assistant"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "SOUL.md"), []byte("# Soul: Helpful"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "BOOTSTRAP.md"), []byte("# Bootstrap: Welcome"), 0644)

	ws := &domain.Workspace{
		Name:        "Acme Corp",
		Description: "Engineering Team",
	}
	user := &domain.User{
		Name:  "Alice",
		Email: "alice@example.com",
	}

	composer := agents.NewInstructionComposer()
	result, err := composer.Compose(ctx, agents.ComposeParams{
		AgentDir:  tempDir,
		Workspace: ws,
		User:      user,
		RoleName:  "Admin",
	})
	if err != nil {
		t.Fatalf("unexpected compose error: %v", err)
	}

	// Verify order: AGENTS, IDENTITY, SOUL, WORKSPACE, USER, BOOTSTRAP
	agentsIdx := strings.Index(result, "# Agents Overview")
	identityIdx := strings.Index(result, "# Identity: Assistant")
	soulIdx := strings.Index(result, "# Soul: Helpful")
	wsIdx := strings.Index(result, "# Workspace")
	userIdx := strings.Index(result, "# Current User")
	bootstrapIdx := strings.Index(result, "# Bootstrap: Welcome")

	if agentsIdx < 0 || identityIdx < 0 || soulIdx < 0 || wsIdx < 0 || userIdx < 0 || bootstrapIdx < 0 {
		t.Fatalf("one or more expected documents missing in output:\n%s", result)
	}

	if !(agentsIdx < identityIdx && identityIdx < soulIdx && soulIdx < wsIdx && wsIdx < userIdx && userIdx < bootstrapIdx) {
		t.Fatalf("documents not in expected order (agents < identity < soul < ws < user < bootstrap):\n%s", result)
	}

	// Verify workspace content
	if !strings.Contains(result, "Acme Corp") || !strings.Contains(result, "Engineering Team") {
		t.Errorf("workspace details missing from output:\n%s", result)
	}

	// Verify user content
	if !strings.Contains(result, "Alice") || !strings.Contains(result, "alice@example.com") || !strings.Contains(result, "Admin") {
		t.Errorf("user details missing from output:\n%s", result)
	}
}

func TestInstructionComposer_UserVariesByCaller(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	_ = os.WriteFile(filepath.Join(tempDir, "AGENTS.md"), []byte("# Agents"), 0644)

	ws := &domain.Workspace{Name: "Acme Corp"}

	composer := agents.NewInstructionComposer()

	user1 := &domain.User{Name: "Alice", Email: "alice@example.com"}
	res1, err := composer.Compose(ctx, agents.ComposeParams{
		AgentDir:  tempDir,
		Workspace: ws,
		User:      user1,
		RoleName:  "Owner",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	user2 := &domain.User{Name: "Bob", Email: "bob@example.com"}
	res2, err := composer.Compose(ctx, agents.ComposeParams{
		AgentDir:  tempDir,
		Workspace: ws,
		User:      user2,
		RoleName:  "Member",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(res1, "Alice") || !strings.Contains(res1, "Owner") {
		t.Errorf("expected res1 to contain Alice and Owner, got:\n%s", res1)
	}
	if strings.Contains(res1, "Bob") {
		t.Errorf("res1 should not contain Bob")
	}

	if !strings.Contains(res2, "Bob") || !strings.Contains(res2, "Member") {
		t.Errorf("expected res2 to contain Bob and Member, got:\n%s", res2)
	}
	if strings.Contains(res2, "Alice") {
		t.Errorf("res2 should not contain Alice")
	}
}

func TestInstructionComposer_MissingDocumentsTolerated(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	// Only AGENTS.md exists (e.g. failed prompt generation where IDENTITY, SOUL, BOOTSTRAP are absent)
	_ = os.WriteFile(filepath.Join(tempDir, "AGENTS.md"), []byte("# Default Agents Config"), 0644)

	ws := &domain.Workspace{Name: "My Workspace"}
	user := &domain.User{Name: "Charlie", Email: "charlie@example.com"}

	composer := agents.NewInstructionComposer()
	result, err := composer.Compose(ctx, agents.ComposeParams{
		AgentDir:  tempDir,
		Workspace: ws,
		User:      user,
		RoleName:  "Viewer",
	})
	if err != nil {
		t.Fatalf("unexpected error when some prompt files are missing: %v", err)
	}

	if !strings.Contains(result, "# Default Agents Config") {
		t.Errorf("expected AGENTS.md content to be present")
	}
	if !strings.Contains(result, "# Workspace") || !strings.Contains(result, "My Workspace") {
		t.Errorf("expected WORKSPACE.md virtual content to be present")
	}
	if !strings.Contains(result, "# Current User") || !strings.Contains(result, "Charlie") {
		t.Errorf("expected USER.md virtual content to be present")
	}
	if strings.Contains(result, "IDENTITY") || strings.Contains(result, "BOOTSTRAP") {
		t.Errorf("expected absent documents to not appear in result")
	}
}
