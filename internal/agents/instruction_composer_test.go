package agents_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// fakeMemories is a minimal store.MemoryStore for composer tests: maps keyed
// by the composite identity, Get returning (nil, nil) when nothing is stored.
type fakeMemories struct {
	user      map[string]*domain.Memory // key: workspaceID + "|" + userID
	workspace map[string]*domain.Memory // key: workspaceID
}

func newFakeMemories() *fakeMemories {
	return &fakeMemories{
		user:      make(map[string]*domain.Memory),
		workspace: make(map[string]*domain.Memory),
	}
}

func (f *fakeMemories) UserMemory(_ context.Context, workspaceID, userID string) (*domain.Memory, error) {
	return f.user[workspaceID+"|"+userID], nil
}

func (f *fakeMemories) UpsertUserMemory(_ context.Context, workspaceID, userID, content string) error {
	f.user[workspaceID+"|"+userID] = &domain.Memory{Content: content, UpdatedAt: time.Now().UTC()}
	return nil
}

func (f *fakeMemories) AppendUserMemory(_ context.Context, workspaceID, userID, content string) error {
	key := workspaceID + "|" + userID
	mem, ok := f.user[key]
	if !ok {
		f.user[key] = &domain.Memory{Content: content, UpdatedAt: time.Now().UTC()}
		return nil
	}
	mem.Content += content
	return nil
}

func (f *fakeMemories) WorkspaceMemory(_ context.Context, workspaceID string) (*domain.Memory, error) {
	return f.workspace[workspaceID], nil
}

func (f *fakeMemories) UpsertWorkspaceMemory(_ context.Context, workspaceID, content string) error {
	f.workspace[workspaceID] = &domain.Memory{Content: content, UpdatedAt: time.Now().UTC()}
	return nil
}

func (f *fakeMemories) AppendWorkspaceMemory(_ context.Context, workspaceID, content string) error {
	mem, ok := f.workspace[workspaceID]
	if !ok {
		f.workspace[workspaceID] = &domain.Memory{Content: content, UpdatedAt: time.Now().UTC()}
		return nil
	}
	mem.Content += content
	return nil
}

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
		Memories:  newFakeMemories(),
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
		Memories:  newFakeMemories(),
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
		Memories:  newFakeMemories(),
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
		Memories:  newFakeMemories(),
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

func TestInstructionComposer_MemorySubsectionsCarryStoredContent(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tempDir, "AGENTS.md"), []byte("# Agents"), 0644)

	memories := newFakeMemories()
	if err := memories.UpsertWorkspaceMemory(ctx, "ws-1", "Ship on Thursdays."); err != nil {
		t.Fatalf("seed workspace memory: %v", err)
	}
	if err := memories.UpsertUserMemory(ctx, "ws-1", "user-1", "Prefers concise answers."); err != nil {
		t.Fatalf("seed user memory: %v", err)
	}

	composer := agents.NewInstructionComposer()
	result, err := composer.Compose(ctx, agents.ComposeParams{
		AgentDir:  tempDir,
		Workspace: &domain.Workspace{ID: "ws-1", Name: "Acme"},
		User:      &domain.User{ID: "user-1", Name: "Alice"},
		RoleName:  "Member",
		Memories:  memories,
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	wsIdx := strings.Index(result, "# Workspace")
	sharedIdx := strings.Index(result, "## Shared memory")
	sharedContentIdx := strings.Index(result, "Ship on Thursdays.")
	userIdx := strings.Index(result, "# Current User")
	userMemIdx := strings.Index(result, "## Memory")
	userContentIdx := strings.Index(result, "Prefers concise answers.")

	for name, idx := range map[string]int{
		"# Workspace": wsIdx, "## Shared memory": sharedIdx, "shared content": sharedContentIdx,
		"# Current User": userIdx, "## Memory": userMemIdx, "user memory content": userContentIdx,
	} {
		if idx < 0 {
			t.Fatalf("expected %s in output:\n%s", name, result)
		}
	}

	// The shared memory subsection lives inside the Workspace doc; the user
	// memory subsection inside the User doc.
	if !(wsIdx < sharedIdx && sharedIdx < sharedContentIdx && sharedContentIdx < userIdx) {
		t.Errorf("shared memory must render inside the Workspace doc:\n%s", result)
	}
	if !(userIdx < userMemIdx && userMemIdx < userContentIdx) {
		t.Errorf("user memory must render inside the User doc:\n%s", result)
	}
}

func TestInstructionComposer_EmptyMemoryOmitsSubsections(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tempDir, "AGENTS.md"), []byte("# Agents"), 0644)

	composer := agents.NewInstructionComposer()
	result, err := composer.Compose(ctx, agents.ComposeParams{
		AgentDir:  tempDir,
		Workspace: &domain.Workspace{ID: "ws-1", Name: "Acme"},
		User:      &domain.User{ID: "user-1", Name: "Alice"},
		RoleName:  "Member",
		Memories:  newFakeMemories(),
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	if strings.Contains(result, "## Shared memory") || strings.Contains(result, "## Memory") {
		t.Errorf("empty memory must omit its subsection entirely:\n%s", result)
	}
}

func TestInstructionComposer_MemoryFreshPerExecution(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tempDir, "AGENTS.md"), []byte("# Agents"), 0644)

	memories := newFakeMemories()
	params := agents.ComposeParams{
		AgentDir:  tempDir,
		Workspace: &domain.Workspace{ID: "ws-1", Name: "Acme"},
		User:      &domain.User{ID: "user-1", Name: "Alice"},
		RoleName:  "Member",
		Memories:  memories,
	}

	composer := agents.NewInstructionComposer()
	first, err := composer.Compose(ctx, params)
	if err != nil {
		t.Fatalf("first compose: %v", err)
	}
	if strings.Contains(first, "First-turn note.") {
		t.Fatalf("nothing appended yet, first turn must not carry it:\n%s", first)
	}

	// The agent appends to WORKSPACE.md and USER.md during the first turn;
	// the second execution's instruction must already carry the appended text.
	if err := memories.AppendWorkspaceMemory(ctx, "ws-1", "First-turn note."); err != nil {
		t.Fatalf("append workspace memory: %v", err)
	}
	if err := memories.AppendUserMemory(ctx, "ws-1", "user-1", "Second-turn note."); err != nil {
		t.Fatalf("append user memory: %v", err)
	}

	second, err := composer.Compose(ctx, params)
	if err != nil {
		t.Fatalf("second compose: %v", err)
	}
	if !strings.Contains(second, "First-turn note.") {
		t.Errorf("second turn must see the first turn's workspace append:\n%s", second)
	}
	if !strings.Contains(second, "Second-turn note.") {
		t.Errorf("second turn must see the first turn's user append:\n%s", second)
	}
}
